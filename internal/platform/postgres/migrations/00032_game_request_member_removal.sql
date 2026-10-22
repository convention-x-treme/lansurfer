-- +goose Up
-- A Host removing a participant is its own activity kind: 'left' would read as
-- if the removed player initiated it. actor_id is the removed participant.
ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'removed', 'started', 'closed', 'cancelled', 'request_updated'));

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'removed', 'started', 'closed', 'cancelled', 'request_updated'));

-- +goose Down
DELETE FROM notification_work WHERE kind = 'removed';
DELETE FROM game_request_activity WHERE kind = 'removed';
ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'request_updated'));
ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'request_updated'));
