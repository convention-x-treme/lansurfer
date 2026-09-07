-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 100),
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (char_length(provider) BETWEEN 1 AND 2048),
    subject text NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 1024),
    display_name text CHECK (display_name IS NULL OR char_length(display_name) <= 255),
    email text CHECK (email IS NULL OR char_length(email) <= 320),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);

CREATE TABLE auth_sessions (
    id_hash bytea PRIMARY KEY CHECK (octet_length(id_hash) = 32),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX auth_sessions_user_id_idx ON auth_sessions (user_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions (expires_at);

-- +goose Down
DROP TABLE auth_sessions;
DROP TABLE user_identities;
DROP TABLE users;
