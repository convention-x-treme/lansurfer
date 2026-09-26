-- +goose Up
-- Existing unresolved creates have no provable owner. NULL keeps them blocking
-- automatic sends and prevents the administrator ownership override.
ALTER TABLE notification_discord_create_reservation
    ADD COLUMN owner_delivery_id bigint REFERENCES notification_delivery (id);

-- +goose Down
ALTER TABLE notification_discord_create_reservation
    DROP COLUMN owner_delivery_id;
