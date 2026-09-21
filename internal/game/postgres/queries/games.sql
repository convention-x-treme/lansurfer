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

-- name: BrowseSubscriptionGames :many
-- Empty search (the "browse the catalog" case) prioritizes followed Games
-- first. A non-empty search (the local-only fallback for a disabled
-- provider) keeps the pre-existing plain name order: the empty-search guard
-- makes every row's ORDER BY sort key false there, so followed status never
-- reorders results. PostgreSQL's SELECT-list output-alias shorthand for
-- ORDER BY only applies to a bare name, not a name nested in an expression,
-- so the followed-game EXISTS check is repeated in the ORDER BY clause
-- rather than referenced through the subscribed alias.
SELECT g.id::text, g.name, g.slug,
       enrichment.cover_image_id,
       enrichment.game_type,
       enrichment.platform_names,
       enrichment.first_release_date,
       EXISTS (
           SELECT 1
           FROM game_subscriptions gs
           WHERE gs.game_id = g.id
             AND gs.user_id = sqlc.arg(user_id)::uuid
       ) AS subscribed
FROM games g
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
WHERE g.active
  AND (
      sqlc.arg(search)::text = ''
      OR strpos(lower(g.name), lower(sqlc.arg(search)::text)) > 0
      OR EXISTS (
          SELECT 1
          FROM game_aliases ga
          WHERE ga.game_id = g.id
            AND strpos(lower(ga.alias), lower(sqlc.arg(search)::text)) > 0
      )
  )
ORDER BY (
    sqlc.arg(search)::text = ''
    AND EXISTS (
        SELECT 1
        FROM game_subscriptions gs
        WHERE gs.game_id = g.id
          AND gs.user_id = sqlc.arg(user_id)::uuid
    )
) DESC, lower(g.name), g.id;

-- name: ListSubscribedGames :many
SELECT g.id::text, g.name, g.slug,
       enrichment.cover_image_id,
       enrichment.game_type,
       enrichment.platform_names,
       enrichment.first_release_date,
       true AS subscribed
FROM game_subscriptions gs
JOIN games g ON g.id = gs.game_id
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
WHERE gs.user_id = sqlc.arg(user_id)::uuid
  AND g.active
ORDER BY lower(g.name), g.id;

-- name: FollowGame :execrows
INSERT INTO game_subscriptions (user_id, game_id)
SELECT sqlc.arg(user_id)::uuid, g.id
FROM games g
WHERE g.id = sqlc.arg(game_id)::uuid
  AND g.active
ON CONFLICT (user_id, game_id) DO NOTHING;

-- name: ActiveGameExists :one
SELECT EXISTS (
    SELECT 1 FROM games WHERE id = sqlc.arg(game_id)::uuid AND active
);

-- name: UnfollowGame :exec
DELETE FROM game_subscriptions
WHERE user_id = sqlc.arg(user_id)::uuid
  AND game_id = sqlc.arg(game_id)::uuid;

-- name: ListGameFollowers :many
SELECT user_id::text FROM game_subscriptions WHERE game_id = sqlc.arg(game_id)::uuid;
