-- name: GetDiscordDestination :one
SELECT destination_id::text AS destination_id, state, credential_ciphertext, guild_id, channel_id, webhook_name,
       connected_at, updated_at, broken_at, broken_reason
FROM notification_discord_destination
WHERE id = true;

-- name: GetDiscordDestinationStatus :one
SELECT destination_id::text AS destination_id, state, guild_id, channel_id, webhook_name, connected_at, updated_at,
       broken_at, broken_reason
FROM notification_discord_destination
WHERE id = true;

-- name: UpsertDiscordDestination :exec
INSERT INTO notification_discord_destination
    (id, state, credential_ciphertext, guild_id, channel_id, webhook_name, connected_at, updated_at)
VALUES (true, 'connected', sqlc.arg(credential_ciphertext), sqlc.arg(guild_id), sqlc.arg(channel_id),
        sqlc.narg(webhook_name), now(), now())
-- Every successful connect gets a new generation; failures keep the old
-- row and generation because the replacement is one atomic statement.
ON CONFLICT (id) DO UPDATE SET
    state = 'connected',
    destination_id = gen_random_uuid(),
    credential_ciphertext = EXCLUDED.credential_ciphertext,
    guild_id = EXCLUDED.guild_id,
    channel_id = EXCLUDED.channel_id,
    webhook_name = EXCLUDED.webhook_name,
    connected_at = now(),
    updated_at = now(),
    broken_at = NULL,
    broken_reason = NULL;

-- name: MarkDiscordDestinationBroken :execrows
-- Generation-scoped: matches only if destination_id is still the exact
-- current connected generation, so a caller that raced a replacement can
-- never break the new destination.
UPDATE notification_discord_destination
SET state = 'broken', credential_ciphertext = NULL, broken_at = now(),
    broken_reason = sqlc.arg(broken_reason), updated_at = now()
WHERE id = true AND state = 'connected' AND destination_id = sqlc.arg(destination_id)::uuid;

-- name: DeleteDiscordDestination :one
-- Returns the disconnected generation's identity so the caller can
-- generation-scope suppression to exactly that target, never a broader
-- transport-wide sweep.
DELETE FROM notification_discord_destination WHERE id = true
RETURNING destination_id::text AS destination_id;

-- name: CreateDiscordSetupState :one
INSERT INTO notification_discord_setup_state
    (state_hash, initiating_admin_id, browser_binding_hash, expires_at)
VALUES (sqlc.arg(state_hash), sqlc.arg(initiating_admin_id)::uuid,
        sqlc.arg(browser_binding_hash), sqlc.arg(expires_at))
RETURNING id::text, expires_at;

-- name: ConsumeDiscordSetupState :one
UPDATE notification_discord_setup_state
SET consumed_at = sqlc.arg(now)
WHERE state_hash = sqlc.arg(state_hash)
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg(now)
  AND browser_binding_hash = sqlc.arg(browser_binding_hash)
  AND initiating_admin_id = sqlc.arg(initiating_admin_id)::uuid
RETURNING id::text, initiating_admin_id::text;

-- name: GetDiscordMessageSync :one
SELECT destination_id::text AS destination_id, game_request_id::text AS game_request_id,
       message_id, applied_version, created_at, updated_at
FROM notification_discord_message_sync
WHERE destination_id = sqlc.arg(destination_id)::uuid
  AND game_request_id = sqlc.arg(game_request_id)::uuid;

-- name: AdvanceDiscordMessageSync :execrows
INSERT INTO notification_discord_message_sync (destination_id, game_request_id, message_id, applied_version)
VALUES (sqlc.arg(destination_id)::uuid, sqlc.arg(game_request_id)::uuid,
        sqlc.arg(message_id), sqlc.arg(applied_version))
ON CONFLICT (destination_id, game_request_id) DO UPDATE
SET message_id = EXCLUDED.message_id, applied_version = EXCLUDED.applied_version, updated_at = now()
WHERE notification_discord_message_sync.applied_version < EXCLUDED.applied_version
   OR (notification_discord_message_sync.applied_version = EXCLUDED.applied_version
       AND notification_discord_message_sync.message_id IS NULL);

-- name: ClearDiscordMessageSync :execrows
UPDATE notification_discord_message_sync
SET message_id = NULL, updated_at = now()
WHERE destination_id = sqlc.arg(destination_id)::uuid
  AND game_request_id = sqlc.arg(game_request_id)::uuid
  AND message_id = sqlc.arg(message_id)
  AND applied_version = sqlc.arg(applied_version);

-- name: ReserveDiscordCreate :execrows
INSERT INTO notification_discord_create_reservation (destination_id, game_request_id, owner_delivery_id)
VALUES (sqlc.arg(destination_id)::uuid, sqlc.arg(game_request_id)::uuid, sqlc.arg(owner_delivery_id)::bigint)
ON CONFLICT (destination_id, game_request_id) DO NOTHING;

-- name: ReleaseDiscordCreate :execrows
DELETE FROM notification_discord_create_reservation
WHERE destination_id = sqlc.arg(destination_id)::uuid
  AND game_request_id = sqlc.arg(game_request_id)::uuid
  AND owner_delivery_id = sqlc.arg(owner_delivery_id)::bigint;

-- name: ReopenUncertainDiscordDeliveryWithReservation :one
-- The destination row lock is the correctness boundary for eligibility:
-- reconnect, disconnect, and break serialize with this statement. An old
-- unowned reservation remains blocking; a competitor cannot delete it.
WITH current_destination AS MATERIALIZED (
    SELECT destination_id FROM notification_discord_destination
    WHERE id = true AND state = 'connected'
      AND destination_id = sqlc.arg(destination_id)::uuid
    FOR SHARE
),
delivery_lookup AS MATERIALIZED (
    SELECT d.id, d.status, w.game_request_id
    FROM notification_delivery d
    JOIN notification_work w ON w.id = d.work_id
    JOIN current_destination cd ON d.target_key = 'd1:' || cd.destination_id::text
    WHERE d.id = sqlc.arg(id) AND d.transport = 'discord'
    FOR UPDATE OF d
),
matching_reservation AS MATERIALIZED (
    SELECT r.owner_delivery_id
    FROM notification_discord_create_reservation r
    JOIN delivery_lookup dl ON dl.game_request_id = r.game_request_id
    WHERE r.destination_id = sqlc.arg(destination_id)::uuid
    FOR UPDATE OF r
),
deleted_reservation AS (
    DELETE FROM notification_discord_create_reservation r
    USING delivery_lookup dl
    WHERE r.destination_id = sqlc.arg(destination_id)::uuid
      AND r.game_request_id = dl.game_request_id
      AND r.owner_delivery_id = dl.id AND dl.status = 'uncertain'
      AND EXISTS (SELECT 1 FROM matching_reservation mr WHERE mr.owner_delivery_id = dl.id)
    RETURNING 1
),
reopened AS (
    UPDATE notification_delivery d
    SET status = 'pending', attempt_count = 0, next_attempt_at = now(),
        claimed_until = NULL, last_error = NULL, updated_at = now()
    WHERE d.id = sqlc.arg(id)
      AND EXISTS (SELECT 1 FROM delivery_lookup WHERE status = 'uncertain')
      AND (NOT EXISTS (SELECT 1 FROM matching_reservation)
           OR EXISTS (SELECT 1 FROM deleted_reservation))
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM current_destination) AS generation_matches,
       EXISTS (SELECT 1 FROM matching_reservation) AS reservation_found,
       EXISTS (SELECT 1 FROM delivery_lookup WHERE status = 'uncertain') AS delivery_uncertain,
       EXISTS (SELECT 1 FROM reopened) AS reopened;
