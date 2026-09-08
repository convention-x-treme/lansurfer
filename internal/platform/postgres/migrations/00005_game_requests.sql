-- +goose Up
CREATE TABLE game_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    game_id uuid NOT NULL REFERENCES games (id) ON DELETE RESTRICT,
    creator_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    max_players integer CHECK (max_players IS NULL OR max_players >= 1),
    starts_at timestamptz NOT NULL,
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_requests_active_event_idx
    ON game_requests (event_id, starts_at, created_at) WHERE status = 'open';
CREATE INDEX game_requests_game_id_idx ON game_requests (game_id);
CREATE INDEX game_requests_creator_id_idx ON game_requests (creator_id);

CREATE TABLE game_request_members (
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (game_request_id, user_id)
);

CREATE INDEX game_request_members_user_id_idx ON game_request_members (user_id);

-- +goose Down
DROP TABLE game_request_members;
DROP TABLE game_requests;
