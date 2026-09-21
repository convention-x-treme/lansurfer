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
    search_text,
    summary,
    genre_names,
    game_mode_names
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
    sqlc.arg(search_text),
    sqlc.narg(summary),
    sqlc.arg(genre_names),
    sqlc.arg(game_mode_names)
FROM generation;

-- name: StoreIGDBMultiplayerModes :exec
-- Stages the platform-specific multiplayer modes of one provider record
-- inside the still-syncing generation. Called only for records that have
-- modes; the composite FK to igdb_game_index ties every row to its parent
-- record, so generation cleanup removes them together.
WITH generation AS (
    SELECT id
    FROM igdb_sync_generations
    WHERE id = sqlc.arg(generation_id)::uuid
      AND status = 'syncing'
    FOR SHARE
), modes AS (
    -- One jsonb parameter carries the mode rows of a single provider
    -- record; array order is the ordinal. Each field is cast explicitly
    -- because sqlc cannot infer types through a record-function column
    -- definition list, and JSON null casts to SQL NULL for the nullable
    -- platform reference and unreported player limits.
    SELECT (row_number() OVER ())::int - 1 AS ordinal,
           (entry.m ->> 'platform_igdb_id')::bigint AS platform_igdb_id,
           (entry.m ->> 'platform_name')::text AS platform_name,
           (entry.m ->> 'campaign_coop')::boolean AS campaign_coop,
           (entry.m ->> 'drop_in')::boolean AS drop_in,
           (entry.m ->> 'lan_coop')::boolean AS lan_coop,
           (entry.m ->> 'offline_coop')::boolean AS offline_coop,
           (entry.m ->> 'online_coop')::boolean AS online_coop,
           (entry.m ->> 'split_screen')::boolean AS split_screen,
           (entry.m ->> 'split_screen_online')::boolean AS split_screen_online,
           (entry.m ->> 'offline_coop_max')::int AS offline_coop_max,
           (entry.m ->> 'offline_max')::int AS offline_max,
           (entry.m ->> 'online_coop_max')::int AS online_coop_max,
           (entry.m ->> 'online_max')::int AS online_max
    FROM jsonb_array_elements(sqlc.arg(modes)::jsonb) AS entry(m)
)
INSERT INTO igdb_game_index_multiplayer_modes (
    generation_id,
    igdb_id,
    ordinal,
    platform_igdb_id,
    platform_name,
    campaign_coop,
    drop_in,
    lan_coop,
    offline_coop,
    online_coop,
    split_screen,
    split_screen_online,
    offline_coop_max,
    offline_max,
    online_coop_max,
    online_max
)
SELECT generation.id,
       sqlc.arg(igdb_id),
       modes.ordinal,
       modes.platform_igdb_id,
       modes.platform_name,
       modes.campaign_coop,
       modes.drop_in,
       modes.lan_coop,
       modes.offline_coop,
       modes.online_coop,
       modes.split_screen,
       modes.split_screen_online,
       modes.offline_coop_max,
       modes.offline_max,
       modes.online_coop_max,
       modes.online_max
FROM generation
CROSS JOIN modes;

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
-- record_count is the active (last successfully activated) generation's
-- count. staged_record_count is separate on purpose: it reports how many
-- rows the in-progress generation has accumulated so far, so an operator can
-- see a running full sync move. latest_attempt_generation_id is the only
-- pointer to that generation, and ActivateIGDBGeneration/FailIGDBGeneration
-- make its row terminal, so a terminal or absent attempt yields 0. The count
-- is served by the igdb_game_index primary key (generation_id, igdb_id) and
-- is read-only, so it never blocks the sync's own batch inserts.
-- ponytail: one count(*) per status read. If this ever measures hot, maintain
-- a counter column in StoreIGDBProviderGame's statement instead.
SELECT state.active_generation_id,
       state.last_attempt_at,
       state.last_attempt_succeeded,
       state.last_success_at,
       state.last_error,
       COALESCE(generation.record_count, 0)::bigint AS record_count,
       (SELECT count(*)
        FROM igdb_sync_generations attempt
        JOIN igdb_game_index staged ON staged.generation_id = attempt.id
        WHERE attempt.id = state.latest_attempt_generation_id
          AND attempt.status = 'syncing') AS staged_record_count
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

-- name: FindLocalDiscoveryGame :one
-- Read-only composer selection by local Game ID (Task 5,
-- GameSelection.LocalGameID). Targets one precise row by primary key -- not
-- the ranked/deduplicated Search query -- so it never inherits Search's
-- DISTINCT ON tie-break nondeterminism. Returns the active local Game's own
-- current name/slug, its retained last-known-good game_igdb_enrichment
-- snapshot, and the given viewer's own Follow state. No row means the Game
-- does not exist or is not active; the caller (DiscoveryService.Select)
-- never fabricates a result for it.
SELECT g.id::text, g.name, g.slug,
       enrichment.cover_image_id,
       enrichment.game_type,
       enrichment.platform_names,
       enrichment.first_release_date,
       enrichment.summary,
       enrichment.genre_names,
       EXISTS (
           SELECT 1
           FROM game_subscriptions gs
           WHERE gs.game_id = g.id
             AND gs.user_id = sqlc.arg(user_id)::uuid
       ) AS subscribed
FROM games g
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
WHERE g.id = sqlc.arg(game_id)::uuid
  AND g.active;

-- name: FindProviderDiscoveryGame :one
-- Read-only composer selection by IGDB provider reference (Task 5,
-- GameSelection.Reference). The provider row must belong to the ACTIVE
-- generation, exactly like ActiveIGDBProviderGame/Search's provider_matches
-- CTE: a still-syncing or already-retired generation is invisible, so an
-- absent row here (empty provider_row) means ErrProviderRecordNotFound to
-- the caller regardless of which arm below it would otherwise have hit.
--
-- materialized_row and unmaterialized_row are constructed to be mutually
-- exclusive so this can never return more than one row:
--   * materialized_row exists only when a non-archived game_external_ids
--     mapping exists AND it points at a still-active local Game. It
--     returns that Game's own current name/slug plus its retained
--     game_igdb_enrichment snapshot and the viewer's own Follow state,
--     mirroring Search's materialized_provider_rows.
--   * unmaterialized_row exists only when NO non-archived mapping exists
--     at all (independent of any local Game's active state, same as
--     Search's unmaterialized_provider_rows exclusion, Track C fix
--     c2cea97). It returns the active generation's own provider
--     presentation directly.
-- A mapping that exists but points only at a deactivated local Game
-- therefore satisfies neither arm: materialized_row's active-Game join
-- fails, and unmaterialized_row's NOT EXISTS is blocked by the mapping's
-- own presence, so the deactivated Game's provider record renders as no
-- row at all rather than resurfacing as a duplicate unmaterialized choice.
WITH active_generation AS (
    SELECT active_generation_id AS id
    FROM igdb_provider_state
    WHERE singleton AND active_generation_id IS NOT NULL
), provider_row AS (
    SELECT provider_game.igdb_id::text AS external_id,
           provider_game.name,
           provider_game.slug,
           provider_game.game_type,
           provider_game.platform_names,
           provider_game.first_release_date,
           provider_game.cover_image_id,
           provider_game.summary,
           provider_game.genre_names
    FROM igdb_game_index provider_game
    JOIN active_generation ON active_generation.id = provider_game.generation_id
    WHERE provider_game.igdb_id = sqlc.arg(igdb_id)
), materialized_row AS (
    SELECT
        provider_row.external_id,
        local_game.id::text AS local_game_id,
        local_game.name::text AS name,
        local_game.slug::text AS slug,
        enrichment.game_type,
        enrichment.platform_names,
        enrichment.first_release_date,
        enrichment.cover_image_id,
        enrichment.summary,
        enrichment.genre_names,
        true::boolean AS materialized,
        (subscription.game_id IS NOT NULL)::boolean AS subscribed
    FROM provider_row
    JOIN game_external_ids mapping
        ON lower(btrim(mapping.provider)) = 'igdb'
       AND mapping.external_id = provider_row.external_id
       AND mapping.archived_at IS NULL
    JOIN games local_game
        ON local_game.id = mapping.game_id
       AND local_game.active
    LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = local_game.id
    LEFT JOIN game_subscriptions subscription
        ON subscription.game_id = local_game.id
       AND subscription.user_id = sqlc.arg(user_id)::uuid
), unmaterialized_row AS (
    SELECT
        provider_row.external_id,
        ''::text AS local_game_id,
        provider_row.name::text AS name,
        provider_row.slug::text AS slug,
        provider_row.game_type,
        provider_row.platform_names,
        provider_row.first_release_date,
        provider_row.cover_image_id,
        provider_row.summary,
        provider_row.genre_names,
        false::boolean AS materialized,
        false::boolean AS subscribed
    FROM provider_row
    WHERE NOT EXISTS (
        SELECT 1
        FROM game_external_ids mapping
        WHERE lower(btrim(mapping.provider)) = 'igdb'
          AND mapping.external_id = provider_row.external_id
          AND mapping.archived_at IS NULL
    )
)
SELECT external_id, local_game_id, name, slug, game_type, platform_names,
       first_release_date, cover_image_id, summary, genre_names,
       materialized, subscribed
FROM materialized_row
UNION ALL
SELECT external_id, local_game_id, name, slug, game_type, platform_names,
       first_release_date, cover_image_id, summary, genre_names,
       materialized, subscribed
FROM unmaterialized_row;

-- name: GameSlugTaken :one
SELECT EXISTS (
    SELECT 1 FROM games WHERE games.slug = sqlc.arg(slug)::text
)::boolean AS taken;

-- name: CreateMaterializedGameAliases :exec
-- Copying provider alternative names is best effort and batched into one
-- round-trip: a duplicate alias must never abort materialization, so any
-- uniqueness conflict is ignored.
INSERT INTO game_aliases (game_id, alias)
SELECT sqlc.arg(game_id)::uuid, alias
FROM unnest(sqlc.arg(aliases)::text[]) AS alias
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
    first_release_date, provider_updated_at,
    summary, genre_names, game_mode_names
)
SELECT
    mapping.game_id,
    mapping.external_id,
    provider_game.cover_image_id,
    provider_game.game_type,
    provider_game.platform_names,
    provider_game.first_release_date,
    provider_game.igdb_updated_at,
    provider_game.summary,
    provider_game.genre_names,
    provider_game.game_mode_names
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
    summary = COALESCE(NULLIF(btrim(EXCLUDED.summary), ''), game_igdb_enrichment.summary),
    genre_names = CASE
        WHEN cardinality(EXCLUDED.genre_names) > 0 THEN EXCLUDED.genre_names
        ELSE game_igdb_enrichment.genre_names
    END,
    game_mode_names = CASE
        WHEN cardinality(EXCLUDED.game_mode_names) > 0 THEN EXCLUDED.game_mode_names
        ELSE game_igdb_enrichment.game_mode_names
    END,
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
    first_release_date, provider_updated_at,
    summary, genre_names, game_mode_names
)
SELECT
    mapping.game_id,
    mapping.external_id,
    provider_game.cover_image_id,
    provider_game.game_type,
    provider_game.platform_names,
    provider_game.first_release_date,
    provider_game.igdb_updated_at,
    provider_game.summary,
    provider_game.genre_names,
    provider_game.game_mode_names
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
    summary = COALESCE(NULLIF(btrim(EXCLUDED.summary), ''), game_igdb_enrichment.summary),
    genre_names = CASE
        WHEN cardinality(EXCLUDED.genre_names) > 0 THEN EXCLUDED.genre_names
        ELSE game_igdb_enrichment.genre_names
    END,
    game_mode_names = CASE
        WHEN cardinality(EXCLUDED.game_mode_names) > 0 THEN EXCLUDED.game_mode_names
        ELSE game_igdb_enrichment.game_mode_names
    END,
    updated_at = now();

-- name: SearchDiscoveryGames :many
-- Unified, ranked, bounded discovery search over the authoritative local
-- Game catalog (local_matches) and the active IGDB generation
-- (provider_matches, gated by include_provider), never calling the provider
-- live. Within each candidate CTE, match_rank orders an exact name/alias
-- match ahead of a prefix match, ahead of a plain contains match. Candidates
-- are deduplicated by logical identity -- 'local:'||local_game_id for
-- anything already an authoritative local Game (whether matched by its own
-- current name/alias or by the active provider record's stale name/
-- alternative names), 'provider:igdb:'||external_id for a genuinely
-- unmaterialized provider record -- keeping whichever side matched with the
-- better rank; both sides already render identical content for the same
-- local Game, so which literal row wins only affects match_rank. The final
-- ORDER BY then ranks by match_rank, followed status, materialized status,
-- name, and finally the stable identity_key. Callers fetch fetch_limit =
-- limit + 1 rows and trim the last one to detect HasMore rather than
-- issuing a separate count query.
--
-- A materialized provider candidate always renders the local Game's own
-- name/slug plus its last-known-good game_igdb_enrichment snapshot, never
-- live provider fields (spec: "later provider refreshes do not
-- automatically rename the authoritative Game"), and requires the linked
-- local Game to still be active. An unmaterialized provider candidate is
-- filtered to player-visible Game Types and excluded whenever a
-- non-archived game_external_ids mapping already exists for it, independent
-- of the mapped local Game's active state (Track C fix, commit c2cea97): an
-- admin deactivating that Game must not make its provider record reappear
-- here as if it were new. Combined with the materialized-candidate active
-- requirement above, a provider record mapped to a deactivated local Game
-- renders as neither candidate, disappearing from search entirely.
WITH active_generation AS (
    SELECT active_generation_id AS id
    FROM igdb_provider_state
    WHERE singleton AND active_generation_id IS NOT NULL
), provider_matches AS (
    SELECT
        provider_game.igdb_id,
        provider_game.name,
        provider_game.slug,
        provider_game.game_type,
        provider_game.platform_names,
        provider_game.first_release_date,
        provider_game.cover_image_id,
        provider_game.summary,
        provider_game.genre_names,
        CASE
            WHEN lower(provider_game.name) = lower(sqlc.arg(search)::text)
              OR EXISTS (
                  SELECT 1 FROM unnest(provider_game.alternative_names) AS alt(name)
                  WHERE lower(alt.name) = lower(sqlc.arg(search)::text)
              ) THEN 1
            WHEN lower(provider_game.name) LIKE replace(replace(replace(
                lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
            ) || '%' ESCAPE '\'
              OR EXISTS (
                  SELECT 1 FROM unnest(provider_game.alternative_names) AS alt(name)
                  WHERE lower(alt.name) LIKE replace(replace(replace(
                      lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
                  ) || '%' ESCAPE '\'
              ) THEN 2
            ELSE 3
        END AS match_rank
    FROM igdb_game_index provider_game
    JOIN active_generation ON active_generation.id = provider_game.generation_id
    WHERE sqlc.arg(include_provider)::boolean
      -- LIKE (not strpos) so the gin_trgm_ops index on search_text
      -- (igdb_game_index_search_trgm_idx) actually accelerates this: trigram
      -- indexes speed up LIKE/ILIKE/~ pattern matches, never strpos(). The
      -- search term is escaped for the three LIKE special characters (\, %, _)
      -- before '%' wildcards are added around it, so a literal '%' or '_' in a
      -- player's search term matches only that literal character.
      AND provider_game.search_text LIKE '%' || replace(replace(replace(
          lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
      ) || '%' ESCAPE '\'
), materialized_provider_rows AS (
    SELECT
        provider_matches.igdb_id::text AS external_id,
        local_game.id::text AS local_game_id,
        local_game.name::text AS name,
        local_game.slug::text AS slug,
        enrichment.game_type,
        enrichment.platform_names,
        enrichment.first_release_date,
        enrichment.cover_image_id,
        enrichment.summary,
        enrichment.genre_names,
        true::boolean AS materialized,
        (subscription.game_id IS NOT NULL)::boolean AS subscribed,
        provider_matches.match_rank,
        ('local:' || local_game.id::text) AS identity_key
    FROM provider_matches
    JOIN game_external_ids mapping
        ON lower(btrim(mapping.provider)) = 'igdb'
       AND mapping.external_id = provider_matches.igdb_id::text
       AND mapping.archived_at IS NULL
    JOIN games local_game
        ON local_game.id = mapping.game_id
       AND local_game.active
    LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = local_game.id
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
        provider_matches.summary,
        provider_matches.genre_names,
        false::boolean AS materialized,
        false::boolean AS subscribed,
        provider_matches.match_rank,
        ('provider:igdb:' || provider_matches.igdb_id::text) AS identity_key
    FROM provider_matches
    WHERE lower(provider_matches.game_type) = ANY(sqlc.arg(visible_game_types)::text[])
      AND NOT EXISTS (
          SELECT 1
          FROM game_external_ids mapping
          WHERE lower(btrim(mapping.provider)) = 'igdb'
            AND mapping.external_id = provider_matches.igdb_id::text
            AND mapping.archived_at IS NULL
      )
), local_matches AS (
    SELECT
        ''::text AS external_id,
        g.id::text AS local_game_id,
        g.name::text AS name,
        g.slug::text AS slug,
        enrichment.game_type,
        enrichment.platform_names,
        enrichment.first_release_date,
        enrichment.cover_image_id,
        enrichment.summary,
        enrichment.genre_names,
        true::boolean AS materialized,
        (gs.game_id IS NOT NULL)::boolean AS subscribed,
        CASE
            WHEN lower(g.name) = lower(sqlc.arg(search)::text)
              OR EXISTS (
                  SELECT 1 FROM game_aliases a
                  WHERE a.game_id = g.id
                    AND lower(a.alias) = lower(sqlc.arg(search)::text)
              ) THEN 1
            WHEN lower(g.name) LIKE replace(replace(replace(
                lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
            ) || '%' ESCAPE '\'
              OR EXISTS (
                  SELECT 1 FROM game_aliases a
                  WHERE a.game_id = g.id
                    AND lower(a.alias) LIKE replace(replace(replace(
                        lower(sqlc.arg(search)::text), '\', '\\'), '%', '\%'), '_', '\_'
                    ) || '%' ESCAPE '\'
              ) THEN 2
            ELSE 3
        END AS match_rank,
        ('local:' || g.id::text) AS identity_key
    FROM games g
    LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
    LEFT JOIN game_subscriptions gs
        ON gs.game_id = g.id
       AND gs.user_id = sqlc.arg(user_id)::uuid
    WHERE g.active
      AND (
          strpos(lower(g.name), lower(sqlc.arg(search)::text)) > 0
          OR EXISTS (
              SELECT 1 FROM game_aliases ga
              WHERE ga.game_id = g.id
                AND strpos(lower(ga.alias), lower(sqlc.arg(search)::text)) > 0
          )
      )
), candidates AS (
    SELECT * FROM local_matches
    UNION ALL
    SELECT * FROM materialized_provider_rows
    UNION ALL
    SELECT * FROM unmaterialized_provider_rows
), best AS (
    SELECT DISTINCT ON (identity_key)
        external_id, local_game_id, name, slug, game_type, platform_names,
        first_release_date, cover_image_id, summary, genre_names,
        materialized, subscribed, match_rank, identity_key
    FROM candidates
    ORDER BY identity_key, match_rank
)
SELECT external_id, local_game_id, name, slug, game_type, platform_names,
       first_release_date, cover_image_id, summary, genre_names,
       materialized, subscribed
FROM best
ORDER BY
    match_rank,
    subscribed DESC,
    materialized DESC,
    lower(name),
    identity_key
LIMIT sqlc.arg(fetch_limit)::int;

-- name: ClearIGDBEnrichmentMultiplayerModesForExternalID :exec
-- Deletes one linked Game's last-known-good multiplayer snapshot, but only
-- when the active generation reports mode rows for that provider record.
-- Paired with InsertIGDBEnrichmentMultiplayerModesForExternalID in the same
-- transaction: clear-then-insert in two statements, because data-modifying
-- CTEs in one statement share one snapshot and the insert would collide
-- with rows the delete in the same statement has not removed yet. A
-- provider record with no mode rows never clears anything (the same
-- last-known-good rule the scalar/array columns use).
DELETE FROM game_igdb_enrichment_multiplayer_modes child
WHERE EXISTS (
    SELECT 1
    FROM igdb_game_index_multiplayer_modes m
    JOIN igdb_provider_state state ON state.singleton
    JOIN igdb_game_index provider_game
      ON provider_game.generation_id = state.active_generation_id
     AND provider_game.igdb_id = m.igdb_id
    WHERE provider_game.igdb_id::text = sqlc.arg(external_id)::text
) AND child.game_id = (
    SELECT mapping.game_id
    FROM game_external_ids mapping
    WHERE lower(btrim(mapping.provider)) = 'igdb'
      AND mapping.external_id = sqlc.arg(external_id)::text
);

-- name: InsertIGDBEnrichmentMultiplayerModesForExternalID :exec
-- Copies the active generation's mode rows into one linked Game's
-- last-known-good snapshot. Runs after the matching Clear... query; inserts
-- nothing when the provider record has no mode rows.
INSERT INTO game_igdb_enrichment_multiplayer_modes (
    game_id, ordinal, platform_igdb_id, platform_name,
    campaign_coop, drop_in, lan_coop, offline_coop, online_coop,
    split_screen, split_screen_online,
    offline_coop_max, offline_max, online_coop_max, online_max
)
SELECT mapping.game_id,
       m.ordinal,
       m.platform_igdb_id,
       m.platform_name,
       m.campaign_coop,
       m.drop_in,
       m.lan_coop,
       m.offline_coop,
       m.online_coop,
       m.split_screen,
       m.split_screen_online,
       m.offline_coop_max,
       m.offline_max,
       m.online_coop_max,
       m.online_max
FROM game_external_ids mapping
JOIN igdb_provider_state state ON state.singleton
JOIN igdb_game_index provider_game
  ON provider_game.generation_id = state.active_generation_id
 AND provider_game.igdb_id::text = mapping.external_id
JOIN igdb_game_index_multiplayer_modes m
  ON m.generation_id = provider_game.generation_id
 AND m.igdb_id = provider_game.igdb_id
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.external_id = sqlc.arg(external_id)::text;

-- name: ClearIGDBEnrichmentMultiplayerModesFromGeneration :exec
-- Same clear rule as ClearIGDBEnrichmentMultiplayerModesForExternalID for
-- every IGDB mapping at once, from the generation just activated: per game,
-- snapshot rows are removed only when that generation reports modes for the
-- record. Paired with InsertIGDBEnrichmentMultiplayerModesFromGeneration.
DELETE FROM game_igdb_enrichment_multiplayer_modes child
USING game_external_ids mapping
WHERE lower(btrim(mapping.provider)) = 'igdb'
  AND mapping.game_id = child.game_id
  AND EXISTS (
      SELECT 1
      FROM igdb_game_index_multiplayer_modes m
      WHERE m.generation_id = sqlc.arg(generation_id)::uuid
        AND m.igdb_id::text = mapping.external_id
  );

-- name: InsertIGDBEnrichmentMultiplayerModesFromGeneration :exec
-- Copies the just-activated generation's mode rows into the last-known-good
-- snapshot of every mapped Game. Runs after the matching Clear... query;
-- games whose provider record has no mode rows are untouched.
INSERT INTO game_igdb_enrichment_multiplayer_modes (
    game_id, ordinal, platform_igdb_id, platform_name,
    campaign_coop, drop_in, lan_coop, offline_coop, online_coop,
    split_screen, split_screen_online,
    offline_coop_max, offline_max, online_coop_max, online_max
)
SELECT mapping.game_id,
       m.ordinal,
       m.platform_igdb_id,
       m.platform_name,
       m.campaign_coop,
       m.drop_in,
       m.lan_coop,
       m.offline_coop,
       m.online_coop,
       m.split_screen,
       m.split_screen_online,
       m.offline_coop_max,
       m.offline_max,
       m.online_coop_max,
       m.online_max
FROM game_external_ids mapping
JOIN igdb_game_index provider_game
  ON provider_game.generation_id = sqlc.arg(generation_id)::uuid
 AND provider_game.igdb_id::text = mapping.external_id
JOIN igdb_game_index_multiplayer_modes m
  ON m.generation_id = provider_game.generation_id
 AND m.igdb_id = provider_game.igdb_id
WHERE lower(btrim(mapping.provider)) = 'igdb';

-- name: IGDBMultiplayerModesForExternalIDs :many
-- Read-only: the active generation's multiplayer modes for the given
-- provider records, used by the Admin UI. Rows are ordered by igdb_id and
-- ordinal; the caller groups them by external ID.
SELECT m.igdb_id::text AS external_id,
       m.ordinal,
       m.platform_igdb_id,
       m.platform_name,
       m.campaign_coop,
       m.drop_in,
       m.lan_coop,
       m.offline_coop,
       m.online_coop,
       m.split_screen,
       m.split_screen_online,
       m.offline_coop_max,
       m.offline_max,
       m.online_coop_max,
       m.online_max
FROM igdb_game_index_multiplayer_modes m
JOIN igdb_provider_state state
  ON state.singleton
 AND state.active_generation_id = m.generation_id
JOIN unnest(sqlc.arg(external_ids)::bigint[]) AS wanted(igdb_id)
  ON wanted.igdb_id = m.igdb_id
ORDER BY m.igdb_id, m.ordinal;
