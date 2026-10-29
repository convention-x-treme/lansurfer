-- +goose Up
ALTER TABLE user_identities DROP CONSTRAINT user_identities_provider_kind_check;
ALTER TABLE user_identities
    ADD CONSTRAINT user_identities_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord', 'twitch', 'guest')),
    ADD CONSTRAINT user_identities_guest_provider_key_check
    CHECK (provider_kind <> 'guest' OR provider_key = 'guest');

-- One shared guest invite. The invite is encrypted with the operator data key
-- so administrators can revisit it without a recoverable plaintext secret.
CREATE TABLE auth_guest_access (
    singleton boolean PRIMARY KEY CHECK (singleton),
    enabled boolean NOT NULL DEFAULT false,
    invite_ciphertext bytea,
    CHECK (NOT enabled OR invite_ciphertext IS NOT NULL)
);
INSERT INTO auth_guest_access (singleton, enabled, invite_ciphertext) VALUES (true, false, NULL);

-- +goose Down
DROP TABLE auth_guest_access;
DELETE FROM user_identities WHERE provider_kind = 'guest';
ALTER TABLE user_identities DROP CONSTRAINT user_identities_guest_provider_key_check;
ALTER TABLE user_identities DROP CONSTRAINT user_identities_provider_kind_check;
ALTER TABLE user_identities
    ADD CONSTRAINT user_identities_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord', 'twitch'));
