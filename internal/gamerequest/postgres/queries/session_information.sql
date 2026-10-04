-- name: ReadGameRequestSessionInformation :many
SELECT gr.id::text AS request_id, gr.status,
       EXISTS (SELECT 1 FROM game_request_members m WHERE m.game_request_id = gr.id AND m.user_id = sqlc.arg(viewer_id)::uuid) AS is_member,
       gr.creator_id = sqlc.arg(viewer_id)::uuid AS can_edit,
       CASE WHEN e.id IS NULL THEN ''::text ELSE e.id::text END AS entry_id, e.key, e.value, e.sequence, e.created_at, e.updated_at
FROM game_requests gr
LEFT JOIN game_request_session_entries e
  ON e.game_request_id = gr.id
 AND gr.status IN ('open', 'playing')
 AND EXISTS (SELECT 1 FROM game_request_members m WHERE m.game_request_id = gr.id AND m.user_id = sqlc.arg(viewer_id)::uuid)
WHERE gr.id = sqlc.arg(game_request_id)::uuid
ORDER BY e.sequence;

-- name: GameRequestSessionInformationAccess :one
SELECT gr.status, EXISTS (SELECT 1 FROM game_request_members m WHERE m.game_request_id = gr.id AND m.user_id = sqlc.arg(user_id)::uuid) AS is_member
FROM game_requests gr WHERE gr.id = sqlc.arg(game_request_id)::uuid;

-- name: CountGameRequestSessionEntries :one
SELECT count(*) FROM game_request_session_entries WHERE game_request_id = sqlc.arg(game_request_id)::uuid;

-- name: CreateGameRequestSessionEntry :one
INSERT INTO game_request_session_entries (game_request_id, key, value)
VALUES (sqlc.arg(game_request_id)::uuid, sqlc.arg(key), sqlc.arg(value))
RETURNING id::text, game_request_id::text, key, value, sequence, created_at, updated_at;

-- name: GetGameRequestSessionEntry :one
SELECT id::text, key, value FROM game_request_session_entries
WHERE game_request_id = sqlc.arg(game_request_id)::uuid AND id = sqlc.arg(id)::uuid;

-- name: UpdateGameRequestSessionEntry :execrows
UPDATE game_request_session_entries SET key = sqlc.arg(key), value = sqlc.arg(value), updated_at = now()
WHERE game_request_id = sqlc.arg(game_request_id)::uuid AND id = sqlc.arg(id)::uuid
  AND (key IS DISTINCT FROM sqlc.arg(key) OR value IS DISTINCT FROM sqlc.arg(value));

-- name: DeleteGameRequestSessionEntry :execrows
DELETE FROM game_request_session_entries WHERE game_request_id = sqlc.arg(game_request_id)::uuid AND id = sqlc.arg(id)::uuid;
