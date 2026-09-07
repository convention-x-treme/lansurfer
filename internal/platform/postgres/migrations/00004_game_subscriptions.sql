-- +goose Up
CREATE TABLE game_subscriptions (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    game_id uuid NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, game_id)
);

CREATE INDEX game_subscriptions_game_id_idx ON game_subscriptions (game_id);

-- +goose Down
DROP TABLE game_subscriptions;
