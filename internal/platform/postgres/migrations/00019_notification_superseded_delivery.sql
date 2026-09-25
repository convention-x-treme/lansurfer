-- +goose Up
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain', 'suppressed', 'superseded'));

-- +goose Down
-- Preserve terminal history: downgrade must not silently relabel these rows.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM notification_delivery WHERE status = 'superseded') THEN
        RAISE EXCEPTION 'cannot remove superseded delivery state while rows remain';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain', 'suppressed'));
