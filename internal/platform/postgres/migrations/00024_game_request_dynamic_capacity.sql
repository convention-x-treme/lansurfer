-- +goose Up
-- A Host can now change a Game Request's maximum player count while it is open
-- or playing. The change is request-local history and notification handoff
-- like any other, so it gets its own activity kind plus the before/after values
-- the Activity section renders.
--
-- The two values are typed columns, not a generic payload: NULL means
-- "unlimited", and the check below keeps them from becoming a place to stash
-- data for other kinds.
--
-- `game_requests.max_players` is deliberately left alone. Older rows may hold
-- `1`, and the original intent behind them is unknown, so nothing here
-- rewrites or constrains them. The `>= 2` rule applies to new input only.
ALTER TABLE game_request_activity
    ADD COLUMN previous_max_players integer NULL,
    ADD COLUMN new_max_players integer NULL;

ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'capacity_changed'));

-- The previous value may be a legacy `1`; the new value is always valid input.
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_capacity_metadata_check
    CHECK (
        (kind <> 'capacity_changed' AND previous_max_players IS NULL AND new_max_players IS NULL)
        OR (
            kind = 'capacity_changed'
            AND previous_max_players IS DISTINCT FROM new_max_players
            AND (previous_max_players IS NULL OR previous_max_players >= 1)
            AND (new_max_players IS NULL OR new_max_players >= 2)
        )
    );

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'capacity_changed'));

-- +goose Down
-- Developer-only path. Production never runs DOWN automatically. Forget the
-- capacity history before restoring the narrower kind checks and dropping the
-- columns that described it.
DELETE FROM notification_work WHERE kind = 'capacity_changed';
DELETE FROM game_request_activity WHERE kind = 'capacity_changed';

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));

ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_capacity_metadata_check;
ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));

ALTER TABLE game_request_activity
    DROP COLUMN new_max_players,
    DROP COLUMN previous_max_players;
