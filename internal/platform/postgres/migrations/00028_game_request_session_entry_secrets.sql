-- +goose Up
ALTER TABLE game_request_session_entries ADD COLUMN is_secret boolean NOT NULL DEFAULT false;
-- +goose Down
ALTER TABLE game_request_session_entries DROP COLUMN is_secret;
