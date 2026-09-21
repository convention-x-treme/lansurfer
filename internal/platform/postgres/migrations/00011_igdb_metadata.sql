-- +goose Up
ALTER TABLE igdb_game_index
    ADD COLUMN summary text NULL,
    ADD COLUMN genre_names text[] NOT NULL DEFAULT '{}',
    ADD COLUMN game_mode_names text[] NOT NULL DEFAULT '{}';

-- Platform-specific multiplayer capabilities for one provider record.
-- Player limits are nullable: IGDB omits unset limits (and reports 0 for
-- them), so NULL is the only faithful "not reported" representation.
CREATE TABLE igdb_game_index_multiplayer_modes (
    generation_id uuid NOT NULL,
    igdb_id bigint NOT NULL CHECK (igdb_id > 0),
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    platform_igdb_id bigint NULL CHECK (platform_igdb_id > 0),
    platform_name text NULL,
    campaign_coop boolean NOT NULL DEFAULT false,
    drop_in boolean NOT NULL DEFAULT false,
    lan_coop boolean NOT NULL DEFAULT false,
    offline_coop boolean NOT NULL DEFAULT false,
    online_coop boolean NOT NULL DEFAULT false,
    split_screen boolean NOT NULL DEFAULT false,
    split_screen_online boolean NOT NULL DEFAULT false,
    offline_coop_max integer NULL CHECK (offline_coop_max >= 0),
    offline_max integer NULL CHECK (offline_max >= 0),
    online_coop_max integer NULL CHECK (online_coop_max >= 0),
    online_max integer NULL CHECK (online_max >= 0),
    PRIMARY KEY (generation_id, igdb_id, ordinal),
    FOREIGN KEY (generation_id, igdb_id)
        REFERENCES igdb_game_index (generation_id, igdb_id)
        ON DELETE CASCADE
);

ALTER TABLE game_igdb_enrichment
    ADD COLUMN summary text NULL,
    ADD COLUMN genre_names text[] NOT NULL DEFAULT '{}',
    ADD COLUMN game_mode_names text[] NOT NULL DEFAULT '{}';

-- Last-known-good multiplayer snapshot for one materialized Game. Removing
-- the enrichment row (unlink, Game deletion) removes its modes with it.
CREATE TABLE game_igdb_enrichment_multiplayer_modes (
    game_id uuid NOT NULL REFERENCES game_igdb_enrichment (game_id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    platform_igdb_id bigint NULL CHECK (platform_igdb_id > 0),
    platform_name text NULL,
    campaign_coop boolean NOT NULL DEFAULT false,
    drop_in boolean NOT NULL DEFAULT false,
    lan_coop boolean NOT NULL DEFAULT false,
    offline_coop boolean NOT NULL DEFAULT false,
    online_coop boolean NOT NULL DEFAULT false,
    split_screen boolean NOT NULL DEFAULT false,
    split_screen_online boolean NOT NULL DEFAULT false,
    offline_coop_max integer NULL CHECK (offline_coop_max >= 0),
    offline_max integer NULL CHECK (offline_max >= 0),
    online_coop_max integer NULL CHECK (online_coop_max >= 0),
    online_max integer NULL CHECK (online_max >= 0),
    PRIMARY KEY (game_id, ordinal)
);

-- +goose Down
DROP TABLE game_igdb_enrichment_multiplayer_modes;
ALTER TABLE game_igdb_enrichment
    DROP COLUMN summary,
    DROP COLUMN genre_names,
    DROP COLUMN game_mode_names;
DROP TABLE igdb_game_index_multiplayer_modes;
ALTER TABLE igdb_game_index
    DROP COLUMN summary,
    DROP COLUMN genre_names,
    DROP COLUMN game_mode_names;
