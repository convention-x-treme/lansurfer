-- +goose Up
ALTER TABLE user_identities
    ADD COLUMN avatar_ref text NULL CHECK (avatar_ref IS NULL OR char_length(avatar_ref) BETWEEN 1 AND 2048),
    ADD COLUMN avatar_key text NULL CHECK (avatar_key IS NULL OR avatar_key ~ '^[a-f0-9]{32}$'),
    ADD COLUMN avatar_selected boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT user_identities_avatar_ref_key_check CHECK ((avatar_ref IS NULL) = (avatar_key IS NULL));
CREATE UNIQUE INDEX user_identities_avatar_selected_unique_idx
    ON user_identities (user_id)
    WHERE avatar_selected;
CREATE INDEX user_identities_avatar_key_idx
    ON user_identities (avatar_key)
    WHERE avatar_selected;
ALTER TABLE auth_flows
    ADD COLUMN avatar_ref text NULL CHECK (avatar_ref IS NULL OR char_length(avatar_ref) BETWEEN 1 AND 2048);

-- +goose Down
ALTER TABLE auth_flows DROP COLUMN avatar_ref;
DROP INDEX user_identities_avatar_key_idx;
DROP INDEX user_identities_avatar_selected_unique_idx;
ALTER TABLE user_identities
    DROP CONSTRAINT user_identities_avatar_ref_key_check,
    DROP COLUMN avatar_selected,
    DROP COLUMN avatar_key,
    DROP COLUMN avatar_ref;
