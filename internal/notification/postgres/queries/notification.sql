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
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryRetry :exec
UPDATE notification_delivery
SET status = 'pending', attempt_count = attempt_count + 1, next_attempt_at = sqlc.arg(next_attempt_at),
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryExhausted :exec
UPDATE notification_delivery
SET status = 'exhausted', attempt_count = attempt_count + 1,
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryPermanent :exec
UPDATE notification_delivery
SET status = 'permanent', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryUncertain :exec
UPDATE notification_delivery
SET status = 'uncertain', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ListProblemDeliveries :many
SELECT d.id, d.transport, d.target_key, d.status, d.attempt_count, d.last_error, d.updated_at,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.status IN ('exhausted', 'permanent', 'uncertain')
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
