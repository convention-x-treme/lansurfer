-- +goose Up
-- Request detail changes are one durable, typed activity and notification handoff.
ALTER TABLE game_request_activity
    ADD COLUMN previous_max_players integer NULL,
    ADD COLUMN new_max_players integer NULL,
    ADD COLUMN previous_starts_at timestamptz NULL,
    ADD COLUMN new_starts_at timestamptz NULL,
    ADD COLUMN description_changed boolean NOT NULL DEFAULT false;

ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'request_updated'));

ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_details_metadata_check
    CHECK (
        (
            kind <> 'request_updated'
            AND previous_max_players IS NULL
            AND new_max_players IS NULL
            AND previous_starts_at IS NULL
            AND new_starts_at IS NULL
            AND description_changed = false
        )
        OR (
            kind = 'request_updated'
            AND (
                (previous_max_players IS NULL AND new_max_players IS NULL)
                OR previous_max_players IS DISTINCT FROM new_max_players
            )
            AND (previous_max_players IS NULL OR previous_max_players >= 1)
            AND (new_max_players IS NULL OR new_max_players >= 2)
            AND (
                (previous_starts_at IS NULL AND new_starts_at IS NULL)
                OR (
                    previous_starts_at IS NOT NULL
                    AND new_starts_at IS NOT NULL
                    AND previous_starts_at IS DISTINCT FROM new_starts_at
                )
            )
            AND (
                previous_max_players IS DISTINCT FROM new_max_players
                OR previous_starts_at IS DISTINCT FROM new_starts_at
                OR description_changed
            )
        )
    );

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled', 'request_updated'));
