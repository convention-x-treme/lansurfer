-- +goose Up
CREATE TABLE game_igdb_enrichment (
    game_id uuid PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    external_id text NOT NULL UNIQUE CHECK (external_id ~ '^[1-9][0-9]*$'),
    cover_image_id text NULL,
    game_type text NULL,
    platform_names text[] NOT NULL DEFAULT '{}',
    first_release_date timestamptz NULL,
    provider_updated_at timestamptz NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO game_igdb_enrichment (
    game_id,
    external_id,
    cover_image_id,
    game_type,
    platform_names,
    first_release_date,
    provider_updated_at
)
SELECT
    mapping.game_id,
    mapping.external_id,
    provider_game.cover_image_id,
    provider_game.game_type,
    provider_game.platform_names,
    provider_game.first_release_date,
    provider_game.igdb_updated_at
FROM game_external_ids mapping
JOIN igdb_provider_state state
  ON state.singleton
 AND state.active_generation_id IS NOT NULL
JOIN igdb_game_index provider_game
  ON provider_game.generation_id = state.active_generation_id
 AND provider_game.igdb_id::text = mapping.external_id
WHERE lower(btrim(mapping.provider)) = 'igdb';

-- +goose Down
DROP TABLE game_igdb_enrichment;
