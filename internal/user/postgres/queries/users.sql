-- name: CreateUser :one
INSERT INTO users (display_name, role)
VALUES (sqlc.arg(display_name), sqlc.arg(role))
RETURNING id::text, display_name, role, created_at, updated_at;

-- name: GetUser :one
SELECT id::text, display_name, role, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id)::uuid;

-- name: ListUsers :many
SELECT id::text, display_name, role, created_at, updated_at
FROM users
ORDER BY created_at, id;

-- name: SetUserRole :one
UPDATE users
SET role = sqlc.arg(role), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING id::text, display_name, role, created_at, updated_at;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = sqlc.arg(id)::uuid;
