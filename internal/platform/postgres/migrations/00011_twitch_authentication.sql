-- +goose Up
ALTER TABLE user_identities DROP CONSTRAINT user_identities_provider_kind_check;
ALTER TABLE user_identities
    ADD CONSTRAINT user_identities_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord', 'twitch'));

ALTER TABLE auth_flows DROP CONSTRAINT auth_flows_provider_kind_check;
ALTER TABLE auth_flows
    ADD CONSTRAINT auth_flows_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord', 'twitch'));

-- +goose Down
ALTER TABLE auth_flows DROP CONSTRAINT auth_flows_provider_kind_check;
ALTER TABLE auth_flows
    ADD CONSTRAINT auth_flows_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord'));

ALTER TABLE user_identities DROP CONSTRAINT user_identities_provider_kind_check;
ALTER TABLE user_identities
    ADD CONSTRAINT user_identities_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord'));
