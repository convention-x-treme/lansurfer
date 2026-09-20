# Issue #46 — Richer IGDB metadata (summary, genres, game modes, multiplayer modes)

Status: plan (execution aid). Source: GitHub issue #46. Scope: ingestion, persistence, Admin UI visibility only. Player-facing presentation is a separate design task.

## Decisions (grill session, 2026-09-21)

| Decision | Choice |
|---|---|
| Numeric player limits (`offline_coop_max`, `offline_max`, `online_coop_max`, `online_max`) | Absent or explicit `0` in IGDB JSON → `NULL`. The issue's "missing ≠ zero" requirement cannot be honored literally because IGDB itself does not distinguish; `0` is IGDB's "unset". Verify against a live IGDB response during implementation. |
| Multiplayer mode platform reference | Store raw IGDB `platform_igdb_id` alongside resolved `platform_name`. Name resolves from the game's own `platforms.id,platforms.name` expansion; id survives renames and unresolvable ids. |
| Refresh semantics when a successful generation reports empty metadata | Keep old snapshot (last-known-good), consistent with the existing enrichment array rule (`cardinality(EXCLUDED.x) > 0`). Applies to summary, genre/mode arrays, and multiplayer-mode child rows. |
| `summary` storage | Full text, no truncation. TOAST compresses; revisit only if storage measurably hurts. |
| Admin UI surface | Admin game card (`/admin/games`) plus IGDB link search results (`/admin/games/{id}/igdb`). No new route. |

## Plan

### 1. Migration `00009_igdb_metadata.sql` (forward-only)

- `igdb_game_index`: `summary text NULL`, `genre_names text[] NOT NULL DEFAULT '{}'`, `game_mode_names text[] NOT NULL DEFAULT '{}'`.
- New `igdb_game_index_multiplayer_modes`:
  - `(generation_id, igdb_id)` composite FK → `igdb_game_index(generation_id, igdb_id) ON DELETE CASCADE`; `ordinal int`
  - `platform_igdb_id bigint NULL`, `platform_name text NULL`
  - booleans `campaign_coop, drop_in, lan_coop, offline_coop, online_coop, split_screen, split_screen_online`
  - `offline_coop_max, offline_max, online_coop_max, online_max` integer NULL (NULL ≠ 0 in queries)
  - PK `(generation_id, igdb_id, ordinal)`
- `game_igdb_enrichment`: same three columns.
- New `game_igdb_enrichment_multiplayer_modes`: `game_id` FK → `games(id) ON DELETE CASCADE` (or enrichment PK) + same shape minus generation columns.
- Generation cleanup needs no change: `DeleteInactiveIGDBGenerations` → cascade → `igdb_game_index` → mode rows.

### 2. Provider model + IGDB client

- `gameFields` += `summary, genres.name, game_modes.name, multiplayer_modes.*, platforms.id` (id needed to map each mode's platform to a name). Confirm `multiplayer_modes.*` expansion works as expected against the live API; fall back to `multiplayer_modes.platform` expansion if needed.
- `game.ProviderGame` += `Summary string`, `GenreNames []string`, `GameModeNames []string`, `MultiplayerModes []MultiplayerMode`.
- `MultiplayerMode`: platform id/name, 7 booleans, 4 `*int` limits (nil when absent/0).
- `mapProviderGame` maps all fields; client fixtures extended (summary, genres, modes, platform id resolution, absent-field case).

### 3. Persistence (sqlc, `task generate`)

- `StoreIGDBProviderGame` gains 3 columns; new batched insert for mode rows (`unnest`, same transaction per batch as the parent rows).
- `UpsertIGDBEnrichmentForExternalID` and `RefreshIGDBEnrichmentFromGeneration` extend the existing last-known-good merge:
  - summary `COALESCE(NULLIF(btrim(EXCLUDED.summary), ''), old)`
  - arrays `cardinality(EXCLUDED.x) > 0` rule
  - mode child rows: delete + reinsert only when the new generation has rows for the game; otherwise keep old rows.
- Materialization inherits this via the existing upsert; no separate path.

### 4. Admin UI

- `game.GamePresentation` and `game.AdminProviderGame` gain the new fields; `SearchActiveIGDBGamesForAdmin` and the admin List query pull enrichment columns + mode rows.
- `admin.templ`: game card shows summary (clamped), genre/mode badges, per-platform multiplayer rows with limits; link-page results render the same from provider rows. Regenerate templ.
- Player-facing templates, `search_text`, discovery queries unchanged.

### 5. Tests

- Unit: client mapping fixtures.
- Integration: migration; generation swap atomicity; failed generation leaves enrichment untouched; cleanup removes mode rows with generation; enrichment refresh replaces modes when new data exists and keeps old when absent; NULL-vs-0 limit handling.
- Web: admin card/link-results rendering (existing `router_test.go` pattern).
- `task generate`, `task test`, `task test:integration`; existing sync/materialization tests must pass unchanged.
