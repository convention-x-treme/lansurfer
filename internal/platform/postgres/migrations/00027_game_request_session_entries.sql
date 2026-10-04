-- +goose Up
CREATE TABLE game_request_session_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    game_request_id uuid NOT NULL REFERENCES game_requests(id) ON DELETE CASCADE,
    key text NOT NULL,
    value text NOT NULL,
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (char_length(key) BETWEEN 1 AND 40),
    CHECK (char_length(value) BETWEEN 1 AND 300),
    CHECK (position(E'\r' in key) = 0 AND position(E'\n' in key) = 0),
    CHECK (position(E'\r' in value) = 0 AND position(E'\n' in value) = 0)
);
CREATE UNIQUE INDEX game_request_session_entries_request_key_idx ON game_request_session_entries (game_request_id, lower(key));
CREATE INDEX game_request_session_entries_request_sequence_idx ON game_request_session_entries (game_request_id, sequence);
-- +goose Down
DROP TABLE game_request_session_entries;
