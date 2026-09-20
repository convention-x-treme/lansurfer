-- name: CreateGameRequest :one
INSERT INTO game_requests (game_id, creator_id, max_players, starts_at, description)
VALUES (
    sqlc.arg(game_id)::uuid,
    sqlc.arg(creator_id)::uuid,
    sqlc.narg(max_players)::integer,
    sqlc.arg(starts_at),
    sqlc.arg(description)
)
RETURNING id::text, game_id::text, creator_id::text, max_players,
          starts_at, description, status, created_at, updated_at;

-- name: AddCreatorMembership :exec
INSERT INTO game_request_members (game_request_id, user_id)
VALUES (sqlc.arg(game_request_id)::uuid, sqlc.arg(user_id)::uuid);

-- name: RecordGameRequestActivity :exec
INSERT INTO game_request_activity (game_request_id, actor_id, kind)
VALUES (sqlc.arg(game_request_id)::uuid, sqlc.arg(actor_id)::uuid, sqlc.arg(kind));

-- name: LockGameRequest :one
SELECT creator_id::text, max_players, status
FROM game_requests
WHERE id = sqlc.arg(id)::uuid
FOR UPDATE;

-- name: CountGameRequestMembers :one
SELECT count(*) FROM game_request_members WHERE game_request_id = sqlc.arg(game_request_id)::uuid;

-- name: GameRequestMemberExists :one
SELECT EXISTS (
    SELECT 1 FROM game_request_members
    WHERE game_request_id = sqlc.arg(game_request_id)::uuid
      AND user_id = sqlc.arg(user_id)::uuid
);

-- name: AddGameRequestMember :exec
INSERT INTO game_request_members (game_request_id, user_id)
VALUES (sqlc.arg(game_request_id)::uuid, sqlc.arg(user_id)::uuid);

-- name: RemoveGameRequestMember :execrows
DELETE FROM game_request_members
WHERE game_request_id = sqlc.arg(game_request_id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid;

-- name: UpdateGameRequestStatus :execrows
UPDATE game_requests
SET status = sqlc.arg(status), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'open';

-- RecordGameRequestView is idempotent per (player, request): a repeat visit
-- refreshes the timestamp instead of growing history, so "Recently viewed"
-- lists distinct requests newest-first.
-- name: RecordGameRequestView :exec
INSERT INTO game_request_views (user_id, game_request_id, viewed_at)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(game_request_id)::uuid, now())
ON CONFLICT (user_id, game_request_id) DO UPDATE SET viewed_at = now();
