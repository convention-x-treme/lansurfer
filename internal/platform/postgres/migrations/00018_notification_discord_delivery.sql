-- +goose Up
-- Singleton instance Discord delivery destination, using the same `id
-- boolean PRIMARY KEY DEFAULT true CHECK (id)` trick as notification_baseline
-- to make "at most one row" a database constraint rather than an
-- application convention. Absence of a row means "not configured".
--
-- The webhook ID and token are never stored in the clear: they live only
-- inside credential_ciphertext, one versioned AES-GCM blob encrypted with
-- the operator's data-encryption key (see internal/platform/crypto
-- and internal/notification/discord_credentials.go). guild_id, channel_id,
-- webhook_name, state, and the timestamps below are safe, queryable
-- metadata that administrators may see directly.
--
-- The trailing CHECK ties state to the presence of the encrypted blob and
-- the broken diagnostics: a connected destination always has credentials
-- and never has broken diagnostics, and a broken destination has had its
-- now-unusable credentials deleted but keeps its failure diagnostics, per
-- the approved "Broken destination state" design decision.
CREATE TABLE notification_discord_destination (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    state text NOT NULL CHECK (state IN ('connected', 'broken')),
    credential_ciphertext bytea NULL,
    guild_id text NOT NULL CHECK (char_length(guild_id) BETWEEN 1 AND 32),
    channel_id text NOT NULL CHECK (char_length(channel_id) BETWEEN 1 AND 32),
    webhook_name text NULL CHECK (webhook_name IS NULL OR char_length(webhook_name) <= 200),
    connected_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    broken_at timestamptz NULL,
    broken_reason text NULL CHECK (broken_reason IS NULL OR char_length(broken_reason) <= 500),
    CHECK (
        (state = 'connected' AND credential_ciphertext IS NOT NULL
            AND broken_at IS NULL AND broken_reason IS NULL)
        OR
        (state = 'broken' AND credential_ciphertext IS NULL
            AND broken_at IS NOT NULL AND broken_reason IS NOT NULL)
    )
);

-- Short-lived, one-use OAuth setup state for the "configure Discord
-- destination" administrator flow, following the same state/browser-binding
-- hash shape as auth_flows (internal/platform/postgres/migrations/00009_...)
-- but owned by the notification module: this flow never creates or touches
-- a Discord authentication identity, so it does not belong in auth_flows.
CREATE TABLE notification_discord_setup_state (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash bytea NOT NULL UNIQUE CHECK (octet_length(state_hash) = 32),
    initiating_admin_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    browser_binding_hash bytea NOT NULL CHECK (octet_length(browser_binding_hash) = 32),
    operation text NOT NULL DEFAULT 'configure_discord_destination'
        CHECK (operation = 'configure_discord_destination'),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX notification_discord_setup_state_expires_at_idx
    ON notification_discord_setup_state (expires_at);

-- +goose Down
DROP TABLE notification_discord_setup_state;
DROP TABLE notification_discord_destination;
