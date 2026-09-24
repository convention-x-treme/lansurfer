-- +goose Up
CREATE INDEX igdb_game_index_cover_image_id_idx
    ON igdb_game_index (cover_image_id) WHERE cover_image_id IS NOT NULL;

-- +goose Down
DROP INDEX igdb_game_index_cover_image_id_idx;
