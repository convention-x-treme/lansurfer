-- name: BeginIGDBGeneration :one
WITH generation AS (
    INSERT INTO igdb_sync_generations (status)
    VALUES ('syncing')
    RETURNING id, started_at
), provider_state AS (
    UPDATE igdb_provider_state state
    SET latest_attempt_generation_id = generation.id,
        last_attempt_at = generation.started_at,
        last_attempt_succeeded = NULL,
        last_error = NULL
    FROM generation
    WHERE state.singleton
    RETURNING state.singleton
)
SELECT generation.id::text
FROM generation
CROSS JOIN provider_state;

-- name: StoreIGDBProviderGame :execrows
WITH generation AS (
    SELECT id
    FROM igdb_sync_generations
    WHERE id = sqlc.arg(generation_id)::uuid
      AND status = 'syncing'
    FOR SHARE
)
INSERT INTO igdb_game_index (
    generation_id,
    igdb_id,
    name,
    slug,
    game_type,
    first_release_date,
    parent_igdb_id,
    version_parent_igdb_id,
    platform_names,
    alternative_names,
    cover_image_id,
    igdb_updated_at,
    checksum,
    search_text
) SELECT
    generation.id,
    sqlc.arg(igdb_id),
    sqlc.arg(name),
    sqlc.arg(slug),
    sqlc.narg(game_type),
    sqlc.narg(first_release_date),
    sqlc.narg(parent_igdb_id),
    sqlc.narg(version_parent_igdb_id),
    sqlc.arg(platform_names),
    sqlc.arg(alternative_names),
    sqlc.narg(cover_image_id),
    sqlc.narg(igdb_updated_at),
    sqlc.narg(checksum),
    sqlc.arg(search_text)
FROM generation;

-- name: CompleteIGDBGeneration :execrows
UPDATE igdb_sync_generations
SET completed_at = now(),
    status = 'complete',
    record_count = sqlc.arg(record_count),
    error = NULL
WHERE id = sqlc.arg(generation_id)::uuid
  AND status = 'syncing';

-- name: ActivateIGDBGeneration :exec
UPDATE igdb_provider_state
SET active_generation_id = sqlc.arg(generation_id)::uuid,
    last_attempt_succeeded = CASE
        WHEN latest_attempt_generation_id = sqlc.arg(generation_id)::uuid THEN true
        ELSE last_attempt_succeeded
    END,
    last_success_at = now(),
    last_error = CASE
        WHEN latest_attempt_generation_id = sqlc.arg(generation_id)::uuid THEN NULL
        ELSE last_error
    END
WHERE singleton;

-- name: ArchiveMissingIGDBExternalIDs :exec
UPDATE game_external_ids external_id
SET archived_at = now()
WHERE lower(btrim(external_id.provider)) = 'igdb'
  AND NOT EXISTS (
      SELECT 1
      FROM igdb_game_index provider_game
      WHERE provider_game.generation_id = sqlc.arg(generation_id)::uuid
        AND provider_game.igdb_id::text = external_id.external_id
  );

-- name: RestorePresentIGDBExternalIDs :exec
UPDATE game_external_ids external_id
SET archived_at = NULL
WHERE lower(btrim(external_id.provider)) = 'igdb'
  AND EXISTS (
      SELECT 1
      FROM igdb_game_index provider_game
      WHERE provider_game.generation_id = sqlc.arg(generation_id)::uuid
        AND provider_game.igdb_id::text = external_id.external_id
  );

-- name: DeleteInactiveIGDBGenerations :exec
DELETE FROM igdb_sync_generations
WHERE id <> sqlc.arg(active_generation_id)::uuid
  AND status IN ('complete', 'failed')
  AND id IS DISTINCT FROM (
      SELECT latest_attempt_generation_id
      FROM igdb_provider_state
      WHERE singleton
  );

-- name: FailIGDBGeneration :one
UPDATE igdb_sync_generations
SET completed_at = now(),
    status = 'failed',
    error = sqlc.arg(error)
WHERE id = sqlc.arg(generation_id)::uuid
  AND status = 'syncing'
RETURNING completed_at;

-- name: RecordIGDBGenerationFailure :exec
UPDATE igdb_provider_state
SET last_attempt_succeeded = false,
    last_error = sqlc.arg(error)
WHERE singleton
  AND latest_attempt_generation_id = sqlc.arg(generation_id)::uuid;

-- name: IGDBProviderStatus :one
SELECT state.active_generation_id,
       state.last_attempt_at,
       state.last_attempt_succeeded,
       state.last_success_at,
       state.last_error,
       COALESCE(generation.record_count, 0)::bigint AS record_count
FROM igdb_provider_state state
LEFT JOIN igdb_sync_generations generation ON generation.id = state.active_generation_id
WHERE state.singleton;
