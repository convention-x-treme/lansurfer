-- +goose Up
CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    is_current boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE UNIQUE INDEX events_one_current_idx ON events (is_current) WHERE is_current;
CREATE INDEX events_starts_at_idx ON events (starts_at DESC);

CREATE TABLE games (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    slug text NOT NULL CHECK (
        char_length(slug) BETWEEN 1 AND 120
        AND slug = lower(slug)
        AND slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'
    ),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (slug)
);

CREATE UNIQUE INDEX games_name_unique_idx ON games (lower(btrim(name)));

CREATE TABLE game_aliases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id uuid NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    alias text NOT NULL CHECK (char_length(btrim(alias)) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX game_aliases_game_alias_unique_idx ON game_aliases (game_id, lower(btrim(alias)));

CREATE TABLE game_external_ids (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id uuid NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (char_length(btrim(provider)) BETWEEN 1 AND 80),
    external_id text NOT NULL CHECK (char_length(btrim(external_id)) BETWEEN 1 AND 255),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX game_external_ids_provider_id_unique_idx
    ON game_external_ids (lower(btrim(provider)), external_id);
CREATE UNIQUE INDEX game_external_ids_game_provider_unique_idx
    ON game_external_ids (game_id, lower(btrim(provider)));

-- +goose Down
DROP TABLE game_external_ids;
DROP TABLE game_aliases;
DROP TABLE games;
DROP TABLE events;
