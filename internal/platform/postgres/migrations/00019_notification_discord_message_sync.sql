-- +goose Up
-- The singleton boolean is only a row key. A fresh UUID identifies each
-- successful OAuth connection, including replacement of the singleton row.
-- Existing connected destinations receive one identity during migration.
ALTER TABLE notification_discord_destination
    ADD COLUMN destination_id uuid NOT NULL DEFAULT gen_random_uuid();

-- No foreign key to the singleton destination: replacement and disconnect
-- must retain old message references under their original identity.
CREATE TABLE notification_discord_message_sync (
    destination_id uuid NOT NULL,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    message_id text NULL CHECK (message_id IS NULL OR char_length(message_id) BETWEEN 1 AND 32),
    applied_version bigint NOT NULL CHECK (applied_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (destination_id, game_request_id)
);

-- +goose Down
DROP TABLE notification_discord_message_sync;
ALTER TABLE notification_discord_destination DROP COLUMN destination_id;
