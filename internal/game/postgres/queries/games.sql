-- name: ListGames :many
SELECT g.id::text, g.name, g.slug, g.active, g.created_at, g.updated_at
FROM games g
WHERE sqlc.arg(search)::text = ''
   OR strpos(lower(g.name), lower(sqlc.arg(search)::text)) > 0
   OR EXISTS (
       SELECT 1
       FROM game_aliases ga
       WHERE ga.game_id = g.id
         AND strpos(lower(ga.alias), lower(sqlc.arg(search)::text)) > 0
   )
ORDER BY g.active DESC, lower(g.name), g.id;

-- name: ListGameAliases :many
SELECT id::text, alias
FROM game_aliases
WHERE game_id = sqlc.arg(game_id)::uuid
ORDER BY lower(alias), id;

-- name: CreateGame :one
INSERT INTO games (name, slug)
VALUES (sqlc.arg(name), sqlc.arg(slug))
RETURNING id::text, name, slug, active, created_at, updated_at;

-- name: UpdateGame :one
UPDATE games
SET name = sqlc.arg(name), slug = sqlc.arg(slug), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING id::text, name, slug, active, created_at, updated_at;

-- name: SetGameActive :execrows
UPDATE games
SET active = sqlc.arg(active), updated_at = now()
WHERE id = sqlc.arg(id)::uuid;

-- name: CreateGameAlias :one
INSERT INTO game_aliases (game_id, alias)
VALUES (sqlc.arg(game_id)::uuid, sqlc.arg(alias))
RETURNING id::text, alias;

-- name: DeleteGameAlias :execrows
DELETE FROM game_aliases
WHERE id = sqlc.arg(id)::uuid AND game_id = sqlc.arg(game_id)::uuid;
