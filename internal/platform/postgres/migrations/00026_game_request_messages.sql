-- +goose Up
-- Private chat for one Game Request. Messages belong to exactly one request and
-- go with it (and with their author's account, like every other record
-- attributable to a player). Chat is deliberately not Game Request activity:
-- it is not lifecycle history and never feeds notification work.
--
-- `id` breaks ties between messages sharing a transaction timestamp, so the
-- chronological order is stable.
CREATE TABLE game_request_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    author_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_request_messages_request_idx
    ON game_request_messages (game_request_id, created_at, id);
CREATE INDEX game_request_messages_author_idx ON game_request_messages (author_id);

-- +goose Down
DROP TABLE game_request_messages;
