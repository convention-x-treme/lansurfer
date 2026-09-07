-- name: CreateUser :one
INSERT INTO users (display_name, role)
VALUES (sqlc.arg(display_name), sqlc.arg(role))
RETURNING id::text, display_name, role, created_at, updated_at;

-- name: GetUser :one
SELECT id::text, display_name, role, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id)::uuid;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = sqlc.arg(id)::uuid;
