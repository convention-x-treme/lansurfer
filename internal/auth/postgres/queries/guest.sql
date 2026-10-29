-- name: LockGuestAccess :one
SELECT enabled FROM auth_guest_access WHERE singleton FOR UPDATE;

-- name: GetGuestAccess :one
SELECT enabled, invite_ciphertext FROM auth_guest_access WHERE singleton;

-- name: SetGuestAccessEnabled :execrows
UPDATE auth_guest_access
SET enabled = sqlc.arg(enabled),
    invite_ciphertext = COALESCE(invite_ciphertext, sqlc.narg(invite_ciphertext)::bytea)
WHERE singleton;

-- name: RotateGuestInvite :execrows
UPDATE auth_guest_access SET invite_ciphertext = sqlc.arg(invite_ciphertext)::bytea WHERE singleton;

-- name: LockUserForIdentity :one
SELECT id::text FROM users WHERE id = sqlc.arg(id)::uuid FOR UPDATE;

-- name: IsGuestUser :one
SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = sqlc.arg(user_id)::uuid AND provider_kind = 'guest');

-- name: InsertIdentityIfAbsent :one
INSERT INTO user_identities (user_id, provider_kind, provider_key, subject, display_name, email, avatar_ref, avatar_key, avatar_selected)
VALUES (
    sqlc.arg(user_id)::uuid, sqlc.arg(provider_kind), sqlc.arg(provider_key), sqlc.arg(subject),
    sqlc.narg(display_name), sqlc.narg(email), sqlc.narg(avatar_ref), sqlc.narg(avatar_key), sqlc.arg(avatar_selected)
)
ON CONFLICT DO NOTHING
RETURNING id::text, user_id::text, provider_kind, provider_key, subject, display_name, email, avatar_ref, avatar_key, avatar_selected, created_at;

-- name: DeleteGuestIdentity :exec
DELETE FROM user_identities WHERE user_id = sqlc.arg(user_id)::uuid AND provider_kind = 'guest';
