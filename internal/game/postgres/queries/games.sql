-- name: ListGames :many
SELECT g.id::text, g.name, g.slug, g.active, g.created_at, g.updated_at,
       COALESCE(mapping.external_id, '')::text AS igdb_external_id,
       (enrichment.game_id IS NOT NULL)::boolean AS igdb_enriched,
       (SELECT count(*) FROM game_requests request WHERE request.game_id = g.id)::bigint AS game_request_count,
       (SELECT count(*) FROM game_subscriptions subscription WHERE subscription.game_id = g.id)::bigint AS subscription_count
FROM games g
LEFT JOIN game_external_ids mapping
  ON mapping.game_id = g.id
 AND lower(btrim(mapping.provider)) = 'igdb'
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
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

-- CountActiveGames states how large the authoritative local catalog is. It
-- counts local Games only: the synchronized provider index is a search
-- surface, not a catalog the navigation can meaningfully total.
-- name: CountActiveGames :one
SELECT count(*) FROM games WHERE active;

-- name: CountSubscribedGames :one
SELECT count(*)
FROM game_subscriptions gs
JOIN games g ON g.id = gs.game_id
WHERE gs.user_id = sqlc.arg(user_id)::uuid AND g.active;

-- name: SearchActiveIGDBGamesForAdmin :many
SELECT provider_game.igdb_id::text AS external_id,
       provider_game.name,
       COALESCE(provider_game.game_type, '')::text AS game_type,
       provider_game.platform_names,
       provider_game.first_release_date,
       COALESCE(provider_game.cover_image_id, '')::text AS cover_image_id,
       COALESCE(linked_game.id::text, '')::text AS linked_game_id,
       COALESCE(linked_game.name, '')::text AS linked_game_name
FROM igdb_game_index provider_game
JOIN igdb_provider_state state
  ON state.singleton
 AND state.active_generation_id = provider_game.generation_id
LEFT JOIN game_external_ids mapping
  ON lower(btrim(mapping.provider)) = 'igdb'
 AND mapping.external_id = provider_game.igdb_id::text
LEFT JOIN games linked_game ON linked_game.id = mapping.game_id
WHERE provider_game.igdb_id::text = sqlc.arg(search)::text
   OR strpos(lower(provider_game.search_text), lower(sqlc.arg(search)::text)) > 0
ORDER BY (provider_game.igdb_id::text = sqlc.arg(search)::text) DESC,
         (lower(provider_game.name) = lower(sqlc.arg(search)::text)) DESC,
         (lower(provider_game.name) LIKE lower(sqlc.arg(search)::text) || '%') DESC,
         lower(provider_game.name), provider_game.igdb_id
LIMIT 25;

-- name: LockAdminGame :one
SELECT id::text
FROM games
WHERE id = sqlc.arg(game_id)::uuid
FOR UPDATE;

-- name: LockGamesForMerge :many
SELECT id::text
FROM games
WHERE id IN (sqlc.arg(source_id)::uuid, sqlc.arg(target_id)::uuid)
ORDER BY id
FOR UPDATE;

-- name: AdminGameIGDBLink :one
SELECT external_id
FROM game_external_ids
WHERE game_id = sqlc.arg(game_id)::uuid
  AND lower(btrim(provider)) = 'igdb';

-- name: IGDBLinkOwner :one
SELECT linked_game.id::text AS game_id, linked_game.name AS game_name
FROM game_external_ids mapping
JOIN games linked_game ON linked_game.id = mapping.game_id
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.external_id = sqlc.arg(external_id)::text;

-- name: DeleteGameIGDBEnrichment :exec
DELETE FROM game_igdb_enrichment
WHERE game_id = sqlc.arg(game_id)::uuid;

-- name: DeleteGameIGDBLink :execrows
DELETE FROM game_external_ids
WHERE game_id = sqlc.arg(game_id)::uuid
  AND lower(btrim(provider)) = 'igdb';

-- name: MoveGameRequests :exec
UPDATE game_requests
SET game_id = sqlc.arg(target_id)::uuid
WHERE game_id = sqlc.arg(source_id)::uuid;

-- name: CopyGameSubscriptions :exec
INSERT INTO game_subscriptions (user_id, game_id, created_at)
SELECT user_id, sqlc.arg(target_id)::uuid, created_at
FROM game_subscriptions
WHERE game_id = sqlc.arg(source_id)::uuid
ON CONFLICT (user_id, game_id) DO NOTHING;

-- name: DeleteGameSubscriptions :exec
DELETE FROM game_subscriptions
WHERE game_id = sqlc.arg(game_id)::uuid;

-- name: CopyGameAliases :exec
INSERT INTO game_aliases (game_id, alias, created_at)
SELECT sqlc.arg(target_id)::uuid, alias, created_at
FROM game_aliases
WHERE game_id = sqlc.arg(source_id)::uuid
ON CONFLICT DO NOTHING;

-- name: MoveGameIGDBLink :exec
UPDATE game_external_ids
SET game_id = sqlc.arg(target_id)::uuid
WHERE game_id = sqlc.arg(source_id)::uuid
  AND lower(btrim(provider)) = 'igdb';

-- name: MoveGameIGDBEnrichment :exec
UPDATE game_igdb_enrichment
SET game_id = sqlc.arg(target_id)::uuid,
    updated_at = now()
WHERE game_id = sqlc.arg(source_id)::uuid;

-- name: GameUsage :one
SELECT (SELECT count(*) FROM game_requests WHERE game_id = sqlc.arg(game_id)::uuid)::bigint AS game_request_count,
       (SELECT count(*) FROM game_subscriptions WHERE game_id = sqlc.arg(game_id)::uuid)::bigint AS subscription_count;

-- name: DeleteGame :execrows
DELETE FROM games
WHERE id = sqlc.arg(game_id)::uuid;
