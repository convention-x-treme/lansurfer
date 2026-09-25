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
INSERT INTO notification_discord_create_reservation (destination_id, game_request_id)
VALUES (sqlc.arg(destination_id)::uuid, sqlc.arg(game_request_id)::uuid)
ON CONFLICT (destination_id, game_request_id) DO NOTHING;

-- name: ReleaseDiscordCreate :execrows
DELETE FROM notification_discord_create_reservation
WHERE destination_id = sqlc.arg(destination_id)::uuid
  AND game_request_id = sqlc.arg(game_request_id)::uuid;
