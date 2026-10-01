-- name: CreateUser :one
INSERT INTO users (game_nick, role)
VALUES (sqlc.arg(game_nick), sqlc.arg(role))
RETURNING id::text, game_nick, role, created_at, updated_at;

-- name: GetUser :one
SELECT id::text, game_nick, role, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id)::uuid;

-- name: ListUsers :many
SELECT id::text, game_nick, role, created_at, updated_at
FROM users
ORDER BY created_at, id;

-- name: SetUserRole :one
UPDATE users
SET role = sqlc.arg(role), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING id::text, game_nick, role, created_at, updated_at;

-- name: SetUserGameNick :one
UPDATE users
SET game_nick = sqlc.arg(game_nick), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING id::text, game_nick, role, created_at, updated_at;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = sqlc.arg(id)::uuid;

-- The CTE locks every administrator row in id order, so two administrators
-- deleting each other concurrently serialize and the second sees count = 1.
-- name: DeleteUserUnlessLastAdmin :execrows
WITH admins AS (
    SELECT id FROM users WHERE role = 'admin' ORDER BY id FOR UPDATE
)
DELETE FROM users
WHERE id = sqlc.arg(id)::uuid
  AND (role <> 'admin' OR (SELECT count(*) FROM admins) > 1);
