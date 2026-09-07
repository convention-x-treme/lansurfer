-- name: GetIdentity :one
SELECT id::text, user_id::text, provider, subject, display_name, email, created_at
FROM user_identities
WHERE provider = sqlc.arg(provider) AND subject = sqlc.arg(subject);

-- name: CreateIdentity :one
INSERT INTO user_identities (user_id, provider, subject, display_name, email)
VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(provider),
    sqlc.arg(subject),
    sqlc.narg(display_name),
    sqlc.narg(email)
)
RETURNING id::text, user_id::text, provider, subject, display_name, email, created_at;

-- name: UpdateIdentityMetadata :exec
UPDATE user_identities
SET display_name = sqlc.narg(display_name), email = sqlc.narg(email)
WHERE provider = sqlc.arg(provider) AND subject = sqlc.arg(subject);

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
