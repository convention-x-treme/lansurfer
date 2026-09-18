-- name: ListActiveGameRequests :many
SELECT gr.id::text AS request_id,
       g.id::text AS game_id,
       g.name AS game_name,
       g.slug AS game_slug,
       enrichment.cover_image_id,
       enrichment.game_type,
       enrichment.platform_names,
       enrichment.first_release_date,
       gr.creator_id::text AS creator_id, u.display_name AS creator_name,
       gr.max_players, gr.starts_at, gr.description, gr.status,
       gr.created_at, gr.updated_at,
       count(m.user_id) AS member_count,
       bool_or(m.user_id = sqlc.arg(viewer_id)::uuid) AS viewer_joined,
       bool_or(gs.user_id IS NOT NULL) AS viewer_subscribed
FROM game_requests gr
JOIN games g ON g.id = gr.game_id
JOIN users u ON u.id = gr.creator_id
JOIN game_request_members m ON m.game_request_id = gr.id
LEFT JOIN game_subscriptions gs ON gs.game_id = gr.game_id
  AND gs.user_id = sqlc.arg(viewer_id)::uuid
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
WHERE gr.status = 'open'
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
  AND (NOT sqlc.arg(followed_only)::boolean OR gs.user_id IS NOT NULL)
GROUP BY gr.id, g.id, g.name, g.slug, enrichment.game_id, u.display_name
HAVING NOT sqlc.arg(open_places_only)::boolean
    OR gr.max_players IS NULL
    OR count(m.user_id) < gr.max_players
ORDER BY gr.starts_at, gr.created_at, gr.id;

-- name: GetGameRequest :one
SELECT gr.id::text AS request_id,
       g.id::text AS game_id,
       g.name AS game_name,
       g.slug AS game_slug,
       enrichment.cover_image_id,
       enrichment.game_type,
       enrichment.platform_names,
       enrichment.first_release_date,
       gr.creator_id::text AS creator_id, u.display_name AS creator_name,
       gr.max_players, gr.starts_at, gr.description, gr.status,
       gr.created_at, gr.updated_at,
       count(m.user_id) AS member_count,
       bool_or(m.user_id = sqlc.arg(viewer_id)::uuid) AS viewer_joined,
       bool_or(gs.user_id IS NOT NULL) AS viewer_subscribed
FROM game_requests gr
JOIN games g ON g.id = gr.game_id
JOIN users u ON u.id = gr.creator_id
JOIN game_request_members m ON m.game_request_id = gr.id
LEFT JOIN game_subscriptions gs ON gs.game_id = gr.game_id
  AND gs.user_id = sqlc.arg(viewer_id)::uuid
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
WHERE gr.id = sqlc.arg(id)::uuid
GROUP BY gr.id, g.id, g.name, g.slug, enrichment.game_id, u.display_name;

-- name: ListGameRequestMembers :many
SELECT u.id::text AS user_id, u.display_name, m.joined_at
FROM game_request_members m
JOIN users u ON u.id = m.user_id
WHERE m.game_request_id = sqlc.arg(game_request_id)::uuid
ORDER BY m.joined_at, u.id;

-- name: ListGameRequestActivity :many
SELECT a.kind, a.occurred_at, a.actor_id::text AS actor_id, u.display_name AS actor_name
FROM game_request_activity a
JOIN users u ON u.id = a.actor_id
WHERE a.game_request_id = sqlc.arg(game_request_id)::uuid
ORDER BY a.sequence DESC;

-- TrendingGames ranks games by Game Request activity inside the rolling window
-- the caller states in the UI. Ranking comes from recorded request activity, not
-- from search queries or page views.
-- name: TrendingGames :many
SELECT g.id::text AS game_id, g.name AS game_name,
       count(*) AS activity_count,
       count(DISTINCT a.game_request_id) AS request_count
FROM game_request_activity a
JOIN game_requests gr ON gr.id = a.game_request_id
JOIN games g ON g.id = gr.game_id
WHERE a.occurred_at >= sqlc.arg(since)
  AND a.kind IN ('created', 'joined')
GROUP BY g.id, g.name
ORDER BY count(*) DESC, count(DISTINCT a.game_request_id) DESC, lower(g.name), g.id
LIMIT sqlc.arg(result_limit);

-- CountOpenGameRequests backs the navigation badge, which states how many Game
-- Requests are open on this installation and is therefore deliberately not
-- affected by the viewer's discovery filters.
-- name: CountOpenGameRequests :one
SELECT count(*) FROM game_requests WHERE status = 'open';
