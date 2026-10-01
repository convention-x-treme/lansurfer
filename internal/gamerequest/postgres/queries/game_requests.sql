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

-- The lifecycle is three named transitions, not one generic status write: each
-- one carries the source state it is legal from, so a racing transaction cannot
-- silently rewrite a request out of a state it no longer holds.
-- name: StartGameRequest :execrows
UPDATE game_requests
SET status = 'playing', started_at = now(), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'open';

-- name: CancelGameRequest :execrows
UPDATE game_requests
SET status = 'cancelled', updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'open';

-- name: CloseGameRequest :execrows
UPDATE game_requests
SET status = 'closed', updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'playing';

-- RecordGameRequestView is idempotent per (player, request): a repeat visit
-- refreshes the timestamp instead of growing history, so "Recently viewed"
-- lists distinct requests newest-first.
-- name: RecordGameRequestView :exec
INSERT INTO game_request_views (user_id, game_request_id, viewed_at)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(game_request_id)::uuid, now())
ON CONFLICT (user_id, game_request_id) DO UPDATE SET viewed_at = now();

-- Chat is a plain chronological stream scoped to one request. Authorization is
-- decided by the caller from ChatAccess (or the locked request row for posting).
-- name: ChatAccess :one
SELECT gr.status,
       EXISTS (
           SELECT 1 FROM game_request_members m
           WHERE m.game_request_id = gr.id AND m.user_id = sqlc.arg(user_id)::uuid
       ) AS is_member
FROM game_requests gr
WHERE gr.id = sqlc.arg(game_request_id)::uuid;

-- name: CreateGameRequestMessage :one
WITH inserted AS (
    INSERT INTO game_request_messages (game_request_id, author_id, body)
    VALUES (sqlc.arg(game_request_id)::uuid, sqlc.arg(author_id)::uuid, sqlc.arg(body))
    RETURNING id, game_request_id, author_id, body, created_at
)
SELECT inserted.id::text AS id, inserted.game_request_id::text AS game_request_id,
       inserted.author_id::text AS author_id, COALESCE(u.game_nick, '')::text AS author_game_nick,
       inserted.body, inserted.created_at
FROM inserted
JOIN users u ON u.id = inserted.author_id;

-- Newest 200, returned oldest first.
-- name: ListGameRequestMessages :many
SELECT id, game_request_id, author_id, author_game_nick, body, created_at
FROM (
    SELECT m.id::text AS id, m.game_request_id::text AS game_request_id,
           m.author_id::text AS author_id, COALESCE(u.game_nick, '')::text AS author_game_nick,
           m.body, m.created_at, m.id AS sort_id
    FROM game_request_messages m
    JOIN users u ON u.id = m.author_id
    WHERE m.game_request_id = sqlc.arg(game_request_id)::uuid
    ORDER BY m.created_at DESC, m.id DESC
    LIMIT 200
) latest
ORDER BY created_at, sort_id;
