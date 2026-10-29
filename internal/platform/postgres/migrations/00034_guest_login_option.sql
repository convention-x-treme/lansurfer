-- +goose Up
ALTER TABLE auth_guest_access ADD COLUMN show_on_login boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE auth_guest_access DROP COLUMN show_on_login;
