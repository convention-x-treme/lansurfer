-- +goose Up
-- One row per LANsurfer user's Web Push preferences. Absent row means the
-- defaults below (disabled, followed-only audience); a row is created only
-- when a user actually changes something, not eagerly for every account.
CREATE TABLE notification_push_preference (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    web_push_enabled boolean NOT NULL DEFAULT false,
    new_request_audience text NOT NULL DEFAULT 'followed'
        CHECK (new_request_audience IN ('followed', 'all')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One row per active browser Push subscription. endpoint/p256dh/auth are
-- stored only as AES-GCM ciphertext (see internal/platform/crypto); the
-- unique endpoint_fingerprint (a keyed HMAC of the plaintext endpoint) is
-- what lets "one endpoint belongs to at most one user" be enforced by a
-- constraint without ever persisting the raw endpoint.
CREATE TABLE notification_push_subscription (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label text NOT NULL,
    device_id_hash text NOT NULL,
    endpoint_fingerprint text NOT NULL UNIQUE,
    endpoint_ciphertext bytea NOT NULL,
    p256dh_ciphertext bytea NOT NULL,
    auth_ciphertext bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_push_subscription_user_idx ON notification_push_subscription (user_id);
CREATE INDEX notification_push_subscription_device_idx ON notification_push_subscription (user_id, device_id_hash);

-- +goose Down
DROP TABLE notification_push_subscription;
DROP TABLE notification_push_preference;
