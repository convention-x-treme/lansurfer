-- name: GetDiscordDestination :one
SELECT state, credential_ciphertext, guild_id, channel_id, webhook_name,
       connected_at, updated_at, broken_at, broken_reason
FROM notification_discord_destination
WHERE id = true;

-- name: GetDiscordDestinationStatus :one
SELECT state, guild_id, channel_id, webhook_name, connected_at, updated_at,
       broken_at, broken_reason
FROM notification_discord_destination
WHERE id = true;

-- name: UpsertDiscordDestination :exec
INSERT INTO notification_discord_destination
    (id, state, credential_ciphertext, guild_id, channel_id, webhook_name, connected_at, updated_at)
VALUES (true, 'connected', sqlc.arg(credential_ciphertext), sqlc.arg(guild_id), sqlc.arg(channel_id),
        sqlc.narg(webhook_name), now(), now())
ON CONFLICT (id) DO UPDATE SET
    state = 'connected',
    credential_ciphertext = EXCLUDED.credential_ciphertext,
    guild_id = EXCLUDED.guild_id,
    channel_id = EXCLUDED.channel_id,
    webhook_name = EXCLUDED.webhook_name,
    connected_at = now(),
    updated_at = now(),
    broken_at = NULL,
    broken_reason = NULL;

-- name: MarkDiscordDestinationBroken :execrows
UPDATE notification_discord_destination
SET state = 'broken', credential_ciphertext = NULL, broken_at = now(),
    broken_reason = sqlc.arg(broken_reason), updated_at = now()
WHERE id = true AND state = 'connected';

-- name: DeleteDiscordDestination :execrows
DELETE FROM notification_discord_destination WHERE id = true;

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
