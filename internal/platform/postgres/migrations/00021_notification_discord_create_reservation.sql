-- +goose Up
-- A row means one Discord create for this destination generation and Game
-- Request may have reached Discord, but its message ID is not durably
-- recorded. The row commits before the provider call, so a later automatic
-- attempt (for example after lease expiry) never creates a second message.
-- Recording the message ID deletes the row in the same transaction; only a
-- provider-proven rejection deletes it otherwise.
CREATE TABLE notification_discord_create_reservation (
    destination_id uuid NOT NULL,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    reserved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (destination_id, game_request_id)
);

-- +goose Down
DROP TABLE notification_discord_create_reservation;
