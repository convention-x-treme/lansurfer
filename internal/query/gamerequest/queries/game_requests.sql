-- name: ListActiveGameRequests :many
SELECT gr.id::text AS request_id, e.name AS event_name,
       g.name AS game_name,
       gr.creator_id::text AS creator_id, u.display_name AS creator_name,
       gr.max_players, gr.starts_at, gr.description, gr.status,
       gr.created_at, gr.updated_at,
       count(m.user_id) AS member_count,
       bool_or(m.user_id = sqlc.arg(viewer_id)::uuid) AS viewer_joined
FROM game_requests gr
JOIN events e ON e.id = gr.event_id
JOIN games g ON g.id = gr.game_id
JOIN users u ON u.id = gr.creator_id
JOIN game_request_members m ON m.game_request_id = gr.id
WHERE gr.status = 'open' AND e.is_current
GROUP BY gr.id, e.name, g.name, u.display_name
ORDER BY gr.starts_at, gr.created_at, gr.id;

-- name: GetGameRequest :one
SELECT gr.id::text AS request_id, e.name AS event_name,
       g.name AS game_name,
       gr.creator_id::text AS creator_id, u.display_name AS creator_name,
       gr.max_players, gr.starts_at, gr.description, gr.status,
       gr.created_at, gr.updated_at,
       count(m.user_id) AS member_count,
       bool_or(m.user_id = sqlc.arg(viewer_id)::uuid) AS viewer_joined
FROM game_requests gr
JOIN events e ON e.id = gr.event_id
JOIN games g ON g.id = gr.game_id
JOIN users u ON u.id = gr.creator_id
JOIN game_request_members m ON m.game_request_id = gr.id
WHERE gr.id = sqlc.arg(id)::uuid
GROUP BY gr.id, e.name, g.name, u.display_name;

-- name: ListGameRequestMembers :many
SELECT u.id::text AS user_id, u.display_name, m.joined_at
FROM game_request_members m
JOIN users u ON u.id = m.user_id
WHERE m.game_request_id = sqlc.arg(game_request_id)::uuid
ORDER BY m.joined_at, u.id;

-- name: CurrentEventForGameRequest :one
SELECT id::text, name FROM events WHERE is_current;

-- name: ActiveGamesForGameRequest :many
SELECT id::text, name FROM games WHERE active ORDER BY lower(name), id;
