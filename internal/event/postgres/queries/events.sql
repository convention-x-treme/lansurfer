-- name: ListEvents :many
SELECT id::text, name, starts_at, ends_at, is_current, created_at, updated_at
FROM events
ORDER BY is_current DESC, starts_at DESC, name;

-- name: CurrentEventID :one
SELECT id::text FROM events WHERE is_current;

-- name: CreateEvent :one
INSERT INTO events (name, starts_at, ends_at)
VALUES (sqlc.arg(name), sqlc.arg(starts_at), sqlc.arg(ends_at))
RETURNING id::text, name, starts_at, ends_at, is_current, created_at, updated_at;

-- name: UpdateEvent :one
UPDATE events
SET name = sqlc.arg(name), starts_at = sqlc.arg(starts_at), ends_at = sqlc.arg(ends_at), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING id::text, name, starts_at, ends_at, is_current, created_at, updated_at;

-- name: ClearCurrentEvent :exec
UPDATE events SET is_current = false, updated_at = now() WHERE is_current;

-- name: MarkCurrentEvent :execrows
UPDATE events SET is_current = true, updated_at = now() WHERE id = sqlc.arg(id)::uuid;
