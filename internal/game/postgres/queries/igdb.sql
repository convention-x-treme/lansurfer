-- name: FailAbandonedIGDBGenerations :exec
-- Before staging a new generation, retire any leftover 'syncing' row left
-- behind by a process that exited before calling CompleteIGDBGeneration or
-- FailIGDBGeneration. LANsurfer 1.0 is single-instance only and
-- IGDBSyncService.TryStart already keeps at most one sync running per
-- process (spec: "Automatic/manual sync cannot overlap in one process"), so
-- a still-'syncing' row observed here can only be a leftover from a
-- previous process incarnation, never a generation this very call is
-- racing with. ActivateIGDBGeneration always flips status away from
-- 'syncing' before setting active_generation_id, so a 'syncing' row is by
-- construction never the active generation and this can never modify it.
UPDATE igdb_sync_generations
SET completed_at = now(),
    status = 'failed',
    error = 'previous synchronization did not complete'
WHERE status = 'syncing';

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

-- name: ActiveIGDBGenerationID :one
-- Read outside BeginGeneration's own writes so DeleteInactiveIGDBGenerations
-- can run before FailAbandonedIGDBGenerations/BeginIGDBGeneration touch
-- anything in that same transaction (see BeginGeneration's ordering
-- comment).
SELECT active_generation_id FROM igdb_provider_state WHERE singleton;

-- name: DeleteInactiveIGDBGenerations :exec
-- Terminal (complete/failed) generations other than the currently active one
-- carry no further diagnostic value -- igdb_provider_state.last_error/
-- last_attempt_succeeded already hold the operator-visible failure state
-- independently of this table -- so they must not survive here. This keeps
-- a full-sync retry that fails late from leaving its staged
-- igdb_game_index rows in PostgreSQL forever. A generation still pointed to
-- by igdb_provider_state.latest_attempt_generation_id must stay excluded
-- regardless: that column's FK forbids deleting it, and it is exactly the
-- generation whichever call is running right now may itself have just
-- created or just marked failed/complete, so it must never be swept up in
-- the same call. active_generation_id is NULL until the first sync ever
-- succeeds, so that comparison must stay NULL-safe or nothing would ever be
-- deleted before that first success.
DELETE FROM igdb_sync_generations
WHERE (sqlc.arg(active_generation_id)::uuid IS NULL OR id <> sqlc.arg(active_generation_id)::uuid)
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

-- name: FindIGDBMaterializedGame :one
-- Loads the authoritative local Game already mapped to an IGDB external ID.
-- The unique (lower(provider), external_id) index makes this at most one row,
-- and that index is also the concurrency anchor materialization retries on.
SELECT local_game.id::text AS id,
       local_game.name AS name,
       local_game.slug AS slug,
       local_game.active AS active,
       local_game.created_at AS created_at,
       local_game.updated_at AS updated_at,
       (mapping.archived_at IS NOT NULL)::boolean AS archived
FROM game_external_ids mapping
JOIN games local_game ON local_game.id = mapping.game_id
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.external_id = sqlc.arg(external_id)::text;

-- name: RestoreIGDBExternalID :exec
-- Spec section 9 step 2: resolving a mapping whose provider record is present
-- in the active generation again clears the archive tombstone. The local
-- Game, its follows, requests, and activity are never touched by archival.
UPDATE game_external_ids mapping
SET archived_at = NULL
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.external_id = sqlc.arg(external_id)::text
  AND mapping.archived_at IS NOT NULL
  AND EXISTS (
      SELECT 1
      FROM igdb_game_index provider_game
      JOIN igdb_provider_state state
        ON state.singleton
       AND state.active_generation_id = provider_game.generation_id
      WHERE provider_game.igdb_id::text = mapping.external_id
  );

-- name: ActiveIGDBProviderGame :one
-- Reloads the authoritative provider row from the ACTIVE generation only.
-- Materialization never trusts browser-supplied provider metadata: only the
-- provider reference crosses the boundary, and every value used to create the
-- local Game (name, slug, alternative names) is read here. An incomplete or
-- retired generation is invisible, so no row means the provider record is not
-- currently offered and nothing may be fabricated from it.
SELECT provider_game.name,
       provider_game.slug,
       provider_game.alternative_names
FROM igdb_game_index provider_game
JOIN igdb_provider_state state
  ON state.singleton
 AND state.active_generation_id = provider_game.generation_id
WHERE provider_game.igdb_id = sqlc.arg(igdb_id);

-- name: GameSlugTaken :one
SELECT EXISTS (
    SELECT 1 FROM games WHERE games.slug = sqlc.arg(slug)::text
)::boolean AS taken;

-- name: CreateMaterializedGameAlias :exec
-- Copying a provider alternative name is best effort: a duplicate alias must
-- never abort materialization, so any uniqueness conflict is ignored.
INSERT INTO game_aliases (game_id, alias)
VALUES (sqlc.arg(game_id)::uuid, sqlc.arg(alias))
ON CONFLICT DO NOTHING;

-- name: CreateIGDBExternalID :exec
-- The final persistence guard for concurrent materialization. A SQLSTATE
-- 23505 here means another transaction already committed the authoritative
-- local Game for this provider record.
INSERT INTO game_external_ids (game_id, provider, external_id)
VALUES (sqlc.arg(game_id)::uuid, 'igdb', sqlc.arg(external_id)::text);

-- name: UpsertIGDBEnrichmentForExternalID :exec
-- Copies the active-generation provider row's presentation snapshot for one
-- already-linked local Game. Called from materialization, for both a
-- freshly-created mapping and one found/restored from an earlier
-- materialization. The ON CONFLICT clause is a last-known-good merge: a new
-- empty/null provider value never blanks out a previously stored non-empty
-- one, only a genuinely non-empty new value replaces it.
INSERT INTO game_igdb_enrichment (
    game_id, external_id, cover_image_id, game_type, platform_names,
    first_release_date, provider_updated_at
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
JOIN igdb_provider_state state ON state.singleton
JOIN igdb_game_index provider_game
  ON provider_game.generation_id = state.active_generation_id
 AND provider_game.igdb_id::text = mapping.external_id
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.external_id = sqlc.arg(external_id)::text
ON CONFLICT (game_id) DO UPDATE
SET cover_image_id = COALESCE(NULLIF(EXCLUDED.cover_image_id, ''), game_igdb_enrichment.cover_image_id),
    game_type = COALESCE(NULLIF(btrim(EXCLUDED.game_type), ''), game_igdb_enrichment.game_type),
    platform_names = CASE
        WHEN cardinality(EXCLUDED.platform_names) > 0 THEN EXCLUDED.platform_names
        ELSE game_igdb_enrichment.platform_names
    END,
    first_release_date = COALESCE(EXCLUDED.first_release_date, game_igdb_enrichment.first_release_date),
    provider_updated_at = COALESCE(EXCLUDED.provider_updated_at, game_igdb_enrichment.provider_updated_at),
    updated_at = now();

-- name: RefreshIGDBEnrichmentFromGeneration :exec
-- Refreshes last-known-good enrichment for every IGDB mapping whose
-- provider record is present in the generation just activated. Called from
-- ActivateGeneration, atomically with generation activation. A mapping
-- whose provider record is absent from this generation (disappeared) has no
-- matching provider_game row here and is simply left untouched, keeping its
-- last-known-good snapshot exactly as it was. Uses the same last-known-good
-- ON CONFLICT semantics as UpsertIGDBEnrichmentForExternalID.
INSERT INTO game_igdb_enrichment (
    game_id, external_id, cover_image_id, game_type, platform_names,
    first_release_date, provider_updated_at
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
JOIN igdb_game_index provider_game
  ON provider_game.generation_id = sqlc.arg(generation_id)::uuid
 AND provider_game.igdb_id::text = mapping.external_id
WHERE lower(btrim(mapping.provider)) = 'igdb'
ON CONFLICT (game_id) DO UPDATE
SET cover_image_id = COALESCE(NULLIF(EXCLUDED.cover_image_id, ''), game_igdb_enrichment.cover_image_id),
    game_type = COALESCE(NULLIF(btrim(EXCLUDED.game_type), ''), game_igdb_enrichment.game_type),
    platform_names = CASE
        WHEN cardinality(EXCLUDED.platform_names) > 0 THEN EXCLUDED.platform_names
        ELSE game_igdb_enrichment.platform_names
    END,
    first_release_date = COALESCE(EXCLUDED.first_release_date, game_igdb_enrichment.first_release_date),
    provider_updated_at = COALESCE(EXCLUDED.provider_updated_at, game_igdb_enrichment.provider_updated_at),
    updated_at = now();

-- name: SearchDiscoveryGames :many
-- Combines authoritative local Games (matched by name/alias, exactly like
-- the pre-IGDB local browse) with the active IGDB generation (matched by
-- normalized search_text), so local search keeps working unchanged when
-- IGDB is disabled or has no active generation yet. Never calls the
-- provider live. A provider row already materialized into an active local
-- Game (materialized_provider_rows) is always returned regardless of its
-- current Game Type or the row cap -- spec: "later provider refreshes do
-- not automatically rename the authoritative Game" and must not make it
-- disappear from search either. Only genuinely unmaterialized provider rows
-- (unmaterialized_provider_rows) are filtered to player-visible Game Types
-- and capped, so hidden-type/overflow matches cannot crowd out visible new
-- results. A materialized provider row is returned exactly once: sourced
-- from the provider side when its own search_text matched, or from the
-- local side when only its current local name/alias matched; never both.
WITH active_generation AS (
    SELECT active_generation_id AS id
    FROM igdb_provider_state
    WHERE singleton AND active_generation_id IS NOT NULL
), provider_matches AS (
    SELECT provider_game.igdb_id,
           provider_game.name,
           provider_game.slug,
           provider_game.game_type,
           provider_game.platform_names,
           provider_game.first_release_date,
           provider_game.cover_image_id
    FROM igdb_game_index provider_game
    JOIN active_generation ON active_generation.id = provider_game.generation_id
    -- LIKE (not strpos) so the gin_trgm_ops index on search_text
    -- (igdb_game_index_search_trgm_idx) actually accelerates this: trigram
    -- indexes speed up LIKE/ILIKE/~ pattern matches, never strpos(). The
    -- search term is escaped for the three LIKE special characters (\, %, _)
    -- before '%' wildcards are added around it, so a literal '%' or '_' in a
    -- player's search term matches only that literal character.
    WHERE provider_game.search_text LIKE '%' || replace(replace(replace(
        lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
    ) || '%' ESCAPE '\'
), materialized_provider_rows AS (
    SELECT
        provider_matches.igdb_id::text AS external_id,
        local_game.id::text AS local_game_id,
        local_game.name::text AS name,
        local_game.slug::text AS slug,
        provider_matches.game_type,
        provider_matches.platform_names,
        provider_matches.first_release_date,
        provider_matches.cover_image_id,
        true::boolean AS materialized,
        (subscription.game_id IS NOT NULL)::boolean AS subscribed
    FROM provider_matches
    JOIN game_external_ids mapping
        ON lower(btrim(mapping.provider)) = 'igdb'
       AND mapping.external_id = provider_matches.igdb_id::text
       AND mapping.archived_at IS NULL
    JOIN games local_game
        ON local_game.id = mapping.game_id
       AND local_game.active
    LEFT JOIN game_subscriptions subscription
        ON subscription.game_id = local_game.id
       AND subscription.user_id = sqlc.arg(user_id)::uuid
), unmaterialized_provider_rows AS (
    SELECT
        provider_matches.igdb_id::text AS external_id,
        ''::text AS local_game_id,
        provider_matches.name::text AS name,
        provider_matches.slug::text AS slug,
        provider_matches.game_type,
        provider_matches.platform_names,
        provider_matches.first_release_date,
        provider_matches.cover_image_id,
        false::boolean AS materialized,
        false::boolean AS subscribed
    FROM provider_matches
    WHERE lower(provider_matches.game_type) = ANY(sqlc.arg(visible_game_types)::text[])
      -- A non-archived mapping means this provider record is already
      -- materialized, independent of the mapped local Game's active state:
      -- an admin deactivating that Game must not make the same provider
      -- record reappear here as if it were new (it stays invisible instead,
      -- exactly like a deactivated local Game in local_rows below, because
      -- materialized_provider_rows also requires local_game.active).
      AND NOT EXISTS (
          SELECT 1
          FROM game_external_ids mapping
          WHERE lower(btrim(mapping.provider)) = 'igdb'
            AND mapping.external_id = provider_matches.igdb_id::text
            AND mapping.archived_at IS NULL
      )
    ORDER BY lower(provider_matches.name), provider_matches.igdb_id
    LIMIT sqlc.arg(row_limit)::int
), provider_rows AS (
    SELECT * FROM materialized_provider_rows
    UNION ALL
    SELECT * FROM unmaterialized_provider_rows
), local_rows AS (
    SELECT
        ''::text AS external_id,
        g.id::text AS local_game_id,
        g.name::text AS name,
        g.slug::text AS slug,
        NULL::text AS game_type,
        ARRAY[]::text[] AS platform_names,
        NULL::timestamptz AS first_release_date,
        NULL::text AS cover_image_id,
        true::boolean AS materialized,
        EXISTS (
            SELECT 1 FROM game_subscriptions gs
            WHERE gs.game_id = g.id AND gs.user_id = sqlc.arg(user_id)::uuid
        )::boolean AS subscribed
    FROM games g
    WHERE g.active
      AND (
          strpos(lower(g.name), lower(sqlc.arg(search)::text)) > 0
          OR EXISTS (
              SELECT 1 FROM game_aliases ga
              WHERE ga.game_id = g.id
                AND strpos(lower(ga.alias), lower(sqlc.arg(search)::text)) > 0
          )
      )
      AND NOT EXISTS (
          SELECT 1 FROM provider_rows pr WHERE pr.local_game_id = g.id::text
      )
)
SELECT combined.* FROM (
    SELECT * FROM local_rows
    UNION ALL
    SELECT * FROM provider_rows
) combined
ORDER BY lower(combined.name), combined.external_id;
