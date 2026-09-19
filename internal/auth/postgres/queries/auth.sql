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
