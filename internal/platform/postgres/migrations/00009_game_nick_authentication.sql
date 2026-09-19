-- +goose Up
ALTER TABLE users ADD COLUMN game_nick text NULL;
ALTER TABLE users DROP COLUMN display_name;

CREATE UNIQUE INDEX users_game_nick_unique_idx
    ON users (lower(game_nick))
    WHERE game_nick IS NOT NULL;

ALTER TABLE user_identities RENAME COLUMN provider TO provider_key;
ALTER TABLE user_identities ADD COLUMN provider_kind text NOT NULL DEFAULT 'oidc';
ALTER TABLE user_identities DROP CONSTRAINT user_identities_provider_subject_key;
ALTER TABLE user_identities
    ADD CONSTRAINT user_identities_provider_kind_check
    CHECK (provider_kind IN ('oidc', 'discord')),
    ADD CONSTRAINT user_identities_provider_namespace_subject_key
    UNIQUE (provider_kind, provider_key, subject),
    ADD CONSTRAINT user_identities_user_provider_namespace_key
    UNIQUE (user_id, provider_kind, provider_key);

CREATE TABLE auth_flows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phase text NOT NULL CHECK (phase IN ('oauth', 'pending')),
    operation text NOT NULL CHECK (operation IN ('login', 'link', 'resolution')),
    provider_kind text NOT NULL CHECK (provider_kind IN ('oidc', 'discord')),
    provider_key text NOT NULL CHECK (char_length(provider_key) BETWEEN 1 AND 2048),
    subject text NULL CHECK (subject IS NULL OR char_length(subject) BETWEEN 1 AND 1024),
    initiating_user_id uuid NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_flow_id uuid NULL REFERENCES auth_flows (id) ON DELETE CASCADE,
    state_hash bytea NULL CHECK (state_hash IS NULL OR octet_length(state_hash) = 32),
    browser_binding_hash bytea NOT NULL CHECK (octet_length(browser_binding_hash) = 32),
    nonce text NULL,
    pkce_verifier text NULL,
    display_name text NULL CHECK (display_name IS NULL OR char_length(display_name) <= 255),
    email text NULL CHECK (email IS NULL OR char_length(email) <= 320),
    game_nick_suggestion text NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (
        (phase = 'oauth' AND state_hash IS NOT NULL AND nonce IS NOT NULL AND pkce_verifier IS NOT NULL)
        OR (phase = 'pending' AND state_hash IS NULL)
    ),
    CHECK ((operation = 'link') = (initiating_user_id IS NOT NULL))
);

CREATE UNIQUE INDEX auth_flows_state_hash_unique_idx
    ON auth_flows (state_hash)
    WHERE state_hash IS NOT NULL;
CREATE INDEX auth_flows_expires_at_idx ON auth_flows (expires_at);

-- +goose Down
DROP TABLE auth_flows;
ALTER TABLE user_identities
    DROP CONSTRAINT user_identities_user_provider_namespace_key,
    DROP CONSTRAINT user_identities_provider_namespace_subject_key,
    DROP CONSTRAINT user_identities_provider_kind_check;
ALTER TABLE user_identities DROP COLUMN provider_kind;
ALTER TABLE user_identities RENAME COLUMN provider_key TO provider;
ALTER TABLE user_identities ADD CONSTRAINT user_identities_provider_subject_key UNIQUE (provider, subject);
DROP INDEX users_game_nick_unique_idx;
ALTER TABLE users DROP COLUMN game_nick;
-- users.display_name intentionally cannot be restored: the forward migration
-- deliberately discards private prototype display names rather than treating
-- them as public Game Nicks.
