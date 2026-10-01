-- +goose Up
-- A Game Request can now be played, not just opened and then closed. `started_at`
-- is the durable record of when the Host actually started the round, written in
-- the same transaction as the `playing` status and the `started` activity row.
--
-- The column is deliberately NULL-able: a request that closed before this
-- lifecycle existed never had a start to record, and inventing one would be
-- fabricating history. Hence the check below allows any `closed` row regardless
-- of `started_at`, while still making the timestamp impossible to lose while
-- `playing` and impossible to keep once the round is over.
ALTER TABLE game_requests ADD COLUMN started_at timestamptz NULL;

ALTER TABLE game_requests DROP CONSTRAINT game_requests_status_check;
ALTER TABLE game_requests
    ADD CONSTRAINT game_requests_status_check
    CHECK (status IN ('open', 'playing', 'closed', 'cancelled'));

ALTER TABLE game_requests
    ADD CONSTRAINT game_requests_started_at_check
    CHECK (
        (status IN ('open', 'cancelled') AND started_at IS NULL)
        OR (status = 'playing' AND started_at IS NOT NULL)
        OR status = 'closed'
    );

-- Discovery and realtime reads treat `open` and `playing` as one active set, so
-- the partial index has to cover both, not only requests nobody has started yet.
DROP INDEX game_requests_open_idx;
CREATE INDEX game_requests_active_idx
    ON game_requests (starts_at, created_at)
    WHERE status IN ('open', 'playing');

-- Starting is a durable lifecycle change like joining or closing, so it belongs
-- to both the request history and the notification handoff that reads it.
ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));

-- +goose Down
-- Developer-only path. Production never runs DOWN automatically, so this has to
-- undo the lifecycle by hand: forget start events, settle any round still
-- playing as closed, restore the old checks and index, then drop the column.
DELETE FROM notification_work WHERE kind = 'started';
DELETE FROM game_request_activity WHERE kind = 'started';

UPDATE game_requests SET status = 'closed', started_at = NULL WHERE status = 'playing';

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'closed', 'cancelled'));

ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'closed', 'cancelled'));

DROP INDEX game_requests_active_idx;
CREATE INDEX game_requests_open_idx
    ON game_requests (starts_at, created_at) WHERE status = 'open';

ALTER TABLE game_requests DROP CONSTRAINT game_requests_started_at_check;
ALTER TABLE game_requests DROP CONSTRAINT game_requests_status_check;
ALTER TABLE game_requests
    ADD CONSTRAINT game_requests_status_check
    CHECK (status IN ('open', 'closed', 'cancelled'));

ALTER TABLE game_requests DROP COLUMN started_at;
