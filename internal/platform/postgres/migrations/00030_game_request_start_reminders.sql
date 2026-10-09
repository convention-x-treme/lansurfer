-- +goose Up
ALTER TABLE notification_work
    ADD COLUMN previous_starts_at timestamptz NULL,
    ADD COLUMN new_starts_at timestamptz NULL;

CREATE TABLE notification_start_reminder (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    game_request_id uuid NOT NULL REFERENCES game_requests(id) ON DELETE CASCADE,
    starts_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (game_request_id, starts_at)
);
ALTER TABLE notification_delivery ALTER COLUMN work_id DROP NOT NULL;
ALTER TABLE notification_delivery ADD COLUMN start_reminder_id bigint
    REFERENCES notification_start_reminder(id) ON DELETE CASCADE;
ALTER TABLE notification_delivery ADD COLUMN send_started_at timestamptz;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_parent_check
    CHECK ((work_id IS NOT NULL) <> (start_reminder_id IS NOT NULL));
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_reminder_transport_check
    CHECK (start_reminder_id IS NULL OR transport = 'webpush');
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_reminder_send_check
    CHECK (send_started_at IS NULL OR start_reminder_id IS NOT NULL);
CREATE UNIQUE INDEX notification_delivery_start_reminder_target_idx
    ON notification_delivery(start_reminder_id, transport, target_key)
    WHERE start_reminder_id IS NOT NULL;

-- +goose Down
DROP INDEX notification_delivery_start_reminder_target_idx;
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_reminder_send_check;
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_reminder_transport_check;
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_parent_check;
ALTER TABLE notification_delivery DROP COLUMN send_started_at;
ALTER TABLE notification_delivery DROP COLUMN start_reminder_id;
ALTER TABLE notification_delivery ALTER COLUMN work_id SET NOT NULL;
DROP TABLE notification_start_reminder;
ALTER TABLE notification_work
    DROP COLUMN new_starts_at,
    DROP COLUMN previous_starts_at;
