-- name: GetIdentity :one
SELECT id::text, user_id::text, provider_kind, provider_key, subject, display_name, email, created_at
FROM user_identities
WHERE provider_kind = sqlc.arg(provider_kind) AND provider_key = sqlc.arg(provider_key) AND subject = sqlc.arg(subject);

-- name: ListIdentitiesByUser :many
SELECT id::text, user_id::text, provider_kind, provider_key, subject, display_name, email, created_at
FROM user_identities
WHERE user_id = sqlc.arg(user_id)::uuid
ORDER BY created_at, id;

-- name: CreateIdentity :one
INSERT INTO user_identities (user_id, provider_kind, provider_key, subject, display_name, email)
VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(provider_kind),
    sqlc.arg(provider_key),
    sqlc.arg(subject),
    sqlc.narg(display_name),
    sqlc.narg(email)
)
RETURNING id::text, user_id::text, provider_kind, provider_key, subject, display_name, email, created_at;

-- name: UpdateIdentityMetadata :exec
UPDATE user_identities
SET display_name = sqlc.narg(display_name), email = sqlc.narg(email)
WHERE provider_kind = sqlc.arg(provider_kind) AND provider_key = sqlc.arg(provider_key) AND subject = sqlc.arg(subject);

-- name: DeleteIdentityUnlessLast :execrows
DELETE FROM user_identities identity
WHERE identity.id = sqlc.arg(id)::uuid
  AND identity.user_id = sqlc.arg(user_id)::uuid
  AND EXISTS (
      SELECT 1 FROM user_identities other
      WHERE other.user_id = identity.user_id AND other.id <> identity.id
  );

-- name: CreateSession :exec
INSERT INTO auth_sessions (id_hash, user_id, expires_at)
VALUES (sqlc.arg(id_hash), sqlc.arg(user_id)::uuid, sqlc.arg(expires_at));

-- name: ResolveSession :one
UPDATE auth_sessions
SET last_seen_at = now()
WHERE id_hash = sqlc.arg(id_hash) AND expires_at > now()
RETURNING user_id::text, expires_at, created_at, last_seen_at;

-- name: DeleteSession :execrows
DELETE FROM auth_sessions
WHERE id_hash = sqlc.arg(id_hash);

-- name: DeleteExpiredAuthFlows :exec
DELETE FROM auth_flows
WHERE expires_at <= sqlc.arg(now) OR consumed_at <= sqlc.arg(now) - interval '10 minutes';

-- name: CreateOAuthFlow :one
INSERT INTO auth_flows (
    phase, operation, provider_kind, provider_key, initiating_user_id, parent_flow_id,
    state_hash, browser_binding_hash, nonce, pkce_verifier, expires_at
) VALUES (
    'oauth', sqlc.arg(operation), sqlc.arg(provider_kind), sqlc.arg(provider_key),
    sqlc.narg(initiating_user_id)::uuid, sqlc.narg(parent_flow_id)::uuid,
    sqlc.arg(state_hash), sqlc.arg(browser_binding_hash), sqlc.arg(nonce),
    sqlc.arg(pkce_verifier), sqlc.arg(expires_at)
)
RETURNING id::text, operation, provider_kind, provider_key,
          initiating_user_id, parent_flow_id, state_hash,
          browser_binding_hash, nonce, pkce_verifier, expires_at;

-- name: ConsumeOAuthFlow :one
WITH matched AS (
    SELECT source.id, source.nonce, source.pkce_verifier
    FROM auth_flows source
    WHERE source.state_hash = sqlc.arg(lookup_state_hash)
      AND source.phase = 'oauth'
      AND source.consumed_at IS NULL
      AND source.expires_at > sqlc.arg(now)
      AND source.browser_binding_hash = sqlc.arg(browser_binding_hash)
      AND source.provider_kind = sqlc.arg(provider_kind)
      AND source.provider_key = sqlc.arg(provider_key)
      AND source.operation = sqlc.arg(operation)
      AND source.initiating_user_id IS NOT DISTINCT FROM sqlc.narg(initiating_user_id)::uuid
    FOR UPDATE
), consumed AS (
    UPDATE auth_flows flow
    SET consumed_at = sqlc.arg(now), nonce = NULL, pkce_verifier = NULL
    FROM matched
    WHERE flow.id = matched.id
    RETURNING flow.id::text AS id, flow.operation, flow.provider_kind, flow.provider_key,
              flow.initiating_user_id, flow.parent_flow_id, flow.state_hash,
              flow.browser_binding_hash, matched.nonce, matched.pkce_verifier,
              flow.expires_at
)
SELECT * FROM consumed;

-- name: GetAuthFlowByState :one
SELECT id::text, phase, operation, provider_kind, provider_key,
       initiating_user_id, parent_flow_id, browser_binding_hash,
       expires_at, consumed_at
FROM auth_flows
WHERE state_hash = sqlc.arg(state_hash);

-- name: CreatePendingIdentity :one
INSERT INTO auth_flows (
    phase, operation, provider_kind, provider_key, subject, initiating_user_id,
    parent_flow_id, browser_binding_hash, display_name, email,
    game_nick_suggestion, expires_at
) VALUES (
    'pending', sqlc.arg(operation), sqlc.arg(provider_kind), sqlc.arg(provider_key),
    sqlc.arg(subject), sqlc.narg(initiating_user_id)::uuid,
    sqlc.narg(parent_flow_id)::uuid, sqlc.arg(browser_binding_hash),
    sqlc.narg(display_name), sqlc.narg(email), sqlc.narg(game_nick_suggestion),
    sqlc.arg(expires_at)
)
RETURNING id::text, operation, provider_kind, provider_key, subject,
          initiating_user_id, parent_flow_id, browser_binding_hash,
          display_name, email, game_nick_suggestion, expires_at;

-- name: GetPendingIdentity :one
SELECT id::text, operation, provider_kind, provider_key, subject,
       initiating_user_id, parent_flow_id, browser_binding_hash,
       display_name, email, game_nick_suggestion, expires_at, consumed_at
FROM auth_flows
WHERE id = sqlc.arg(id)::uuid AND phase = 'pending';

-- name: ConsumePendingIdentity :one
UPDATE auth_flows
SET consumed_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)::uuid
  AND phase = 'pending'
  AND consumed_at IS NULL
  AND expires_at > sqlc.arg(now)
  AND browser_binding_hash = sqlc.arg(browser_binding_hash)
RETURNING id::text;
