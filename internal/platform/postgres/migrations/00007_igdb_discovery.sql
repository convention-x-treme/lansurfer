-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

DROP INDEX games_name_unique_idx;

ALTER TABLE game_external_ids ADD COLUMN archived_at timestamptz NULL;

CREATE TABLE igdb_sync_generations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    status text NOT NULL CHECK (status IN ('syncing', 'complete', 'failed')),
    record_count bigint NOT NULL DEFAULT 0 CHECK (record_count >= 0),
    error text NULL
);

CREATE TABLE igdb_provider_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    active_generation_id uuid NULL REFERENCES igdb_sync_generations (id),
    latest_attempt_generation_id uuid NULL REFERENCES igdb_sync_generations (id),
    last_attempt_at timestamptz NULL,
    last_attempt_succeeded boolean NULL,
    last_success_at timestamptz NULL,
    last_error text NULL
);

INSERT INTO igdb_provider_state (singleton) VALUES (true);

CREATE TABLE igdb_game_index (
    generation_id uuid NOT NULL REFERENCES igdb_sync_generations (id) ON DELETE CASCADE,
    igdb_id bigint NOT NULL CHECK (igdb_id > 0),
    name text NOT NULL,
    slug text NOT NULL,
    game_type text NULL,
    first_release_date timestamptz NULL,
    parent_igdb_id bigint NULL,
    version_parent_igdb_id bigint NULL,
    platform_names text[] NOT NULL DEFAULT '{}',
    alternative_names text[] NOT NULL DEFAULT '{}',
    cover_image_id text NULL,
    igdb_updated_at timestamptz NULL,
    checksum uuid NULL,
    search_text text NOT NULL,
    PRIMARY KEY (generation_id, igdb_id)
);

CREATE INDEX igdb_game_index_search_trgm_idx
    ON igdb_game_index USING gin (search_text gin_trgm_ops);

-- +goose Down
DROP TABLE igdb_game_index;
DROP TABLE igdb_provider_state;
DROP TABLE igdb_sync_generations;
-- Dropping this column permanently destroys every archive/tombstone record
-- (game_external_ids.archived_at) with no way to recover which provider
-- mappings had been archived; there is no warning beyond this comment.
-- Recreating the index below may also fail after duplicate names exist.
-- DOWN is explicitly operator-invoked only and is never run automatically.
ALTER TABLE game_external_ids DROP COLUMN archived_at;
CREATE UNIQUE INDEX games_name_unique_idx ON games (lower(btrim(name)));
