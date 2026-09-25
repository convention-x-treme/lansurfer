-- name: InsertBaselineMarker :execrows
INSERT INTO notification_baseline (id) VALUES (true)
ON CONFLICT (id) DO NOTHING;

-- name: MaterializeBaselineWork :exec
INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at, baseline)
SELECT sequence, game_request_id, actor_id, kind, occurred_at, true
FROM game_request_activity
WHERE origin = 'live'
ON CONFLICT (activity_sequence) DO NOTHING;

-- name: DiscoverNewActivity :many
SELECT a.sequence, a.game_request_id::text AS game_request_id, a.actor_id::text AS actor_id,
       a.kind, a.occurred_at
FROM game_request_activity a
WHERE a.origin = 'live'
  AND NOT EXISTS (SELECT 1 FROM notification_work w WHERE w.activity_sequence = a.sequence)
ORDER BY a.sequence
LIMIT sqlc.arg(row_limit);

-- name: InsertNotificationWork :one
INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at, baseline)
VALUES (
    sqlc.arg(activity_sequence),
    sqlc.arg(game_request_id)::uuid,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(kind),
    sqlc.arg(occurred_at),
    false
)
ON CONFLICT (activity_sequence) DO NOTHING
RETURNING id;

-- name: InsertNotificationDelivery :exec
INSERT INTO notification_delivery (work_id, transport, target_key)
VALUES (sqlc.arg(work_id), sqlc.arg(transport), sqlc.arg(target_key))
ON CONFLICT (work_id, transport, target_key) DO NOTHING;

-- name: ClaimDueDeliveries :many
WITH claimed AS (
    UPDATE notification_delivery
    SET status = 'claimed', claimed_until = sqlc.arg(claimed_until), updated_at = now()
    WHERE id IN (
        SELECT id FROM notification_delivery
        WHERE (status = 'pending' AND next_attempt_at <= now())
           OR (status = 'claimed' AND claimed_until <= now())
        ORDER BY next_attempt_at
        LIMIT sqlc.arg(row_limit)
        FOR UPDATE SKIP LOCKED
    )
    RETURNING id, work_id, transport, target_key, attempt_count
)
SELECT c.id, c.transport, c.target_key, c.attempt_count,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM claimed c
JOIN notification_work w ON w.id = c.work_id;

-- name: RecordDeliverySuccess :exec
UPDATE notification_delivery
SET status = 'succeeded', claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliveryRetry :exec
UPDATE notification_delivery
SET status = 'pending', attempt_count = attempt_count + 1, next_attempt_at = sqlc.arg(next_attempt_at),
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliveryExhausted :exec
UPDATE notification_delivery
SET status = 'exhausted', attempt_count = attempt_count + 1,
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliveryPermanent :exec
UPDATE notification_delivery
SET status = 'permanent', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliveryUncertain :exec
UPDATE notification_delivery
SET status = 'uncertain', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliverySuppressed :exec
UPDATE notification_delivery
SET status = 'suppressed', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'claimed';

-- name: RecordDeliverySuperseded :execrows
-- Only Discord orchestration can supersede unfinished Discord work. Keep
-- the reason a server-generated activity sequence, never provider text.
UPDATE notification_delivery
SET status = 'superseded', claimed_until = NULL,
    last_error = 'superseded by activity sequence ' || sqlc.arg(applied_version)::bigint::text,
    updated_at = now()
WHERE id = sqlc.arg(id) AND transport = 'discord'
  AND status IN ('pending', 'claimed') AND sqlc.arg(applied_version)::bigint > 0;

-- name: ClaimedDiscordDelivery :one
-- Lock the exact claim before provider mutation. A zero ID resolves the
-- unique (activity, target) claim for the legacy Transport.Send entry point.
SELECT d.id FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.transport = 'discord' AND d.target_key = sqlc.arg(target_key)
  AND w.activity_sequence = sqlc.arg(activity_sequence)
  AND w.game_request_id = sqlc.arg(game_request_id)::uuid
  AND d.status = 'claimed'
  AND (sqlc.arg(delivery_id)::bigint = 0 OR d.id = sqlc.arg(delivery_id)::bigint)
FOR UPDATE OF d;

-- name: NewerDiscordDelivery :one
SELECT COALESCE(max(w.activity_sequence), 0)::bigint
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.transport = 'discord' AND d.target_key = sqlc.arg(target_key)
  AND w.game_request_id = sqlc.arg(game_request_id)::uuid
  AND w.activity_sequence > sqlc.arg(activity_sequence)
  AND d.status IN ('pending', 'claimed');

-- name: SuppressDeliveriesByTransport :execrows
-- Bulk-suppresses every still-pending or still-claimed delivery for one
-- transport, e.g. when a Discord destination disconnects or breaks. It
-- never touches a delivery already in a terminal state (succeeded,
-- permanent, exhausted, uncertain, or already suppressed): suppression
-- only ever intercepts work that has not finished yet.
UPDATE notification_delivery
SET status = 'suppressed', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE transport = sqlc.arg(transport) AND status IN ('pending', 'claimed');

-- name: ListProblemDeliveries :many
SELECT d.id, d.transport, d.target_key, d.status, d.attempt_count, d.last_error, d.updated_at,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.status IN ('exhausted', 'permanent', 'uncertain', 'suppressed')
ORDER BY d.updated_at DESC;

-- name: GetDeliveryDetail :one
SELECT d.id, d.transport, d.target_key, d.status,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.id = sqlc.arg(id);

-- name: ReopenDeliveryForManualRetry :one
UPDATE notification_delivery
SET status = 'pending', attempt_count = 0, next_attempt_at = now(),
    claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status IN ('exhausted', 'permanent')
RETURNING id;

-- name: ReopenUncertainDeliveryForManualRetry :one
UPDATE notification_delivery
SET status = 'pending', attempt_count = 0, next_attempt_at = now(),
    claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'uncertain'
RETURNING id;
