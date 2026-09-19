# IGDB Discovery Integration Design

Status: Draft for implementation-plan review  
Date: 2026-09-17  
Branch: `feat/igdb-discovery`

## Context

LANsurfer 1.0 keeps the PostgreSQL-backed local `Game` catalog authoritative. IGDB is an optional discovery provider for normal self-hosted deployments and a required dependency for the selected pilot gate. The approved architecture requires a locally synchronized provider index, not live IGDB queries on player requests.

The existing `game` module already owns local Games, aliases, subscriptions, and provider/external identities. The schema already contains `game_external_ids(provider, external_id)` with a uniqueness constraint on provider + external ID. The new integration must extend those boundaries instead of introducing a second authoritative Game model.

Current relevant implementation points:

- `internal/game/game.go` owns the authoritative Game service and subscription service.
- `internal/game/postgres/` owns Game persistence.
- `internal/platform/config/config.go` loads environment-only infrastructure configuration.
- `internal/app/app.go` is the composition root and starts the HTTP server.
- `internal/platform/postgres/migrations/00003_events_and_game_catalog.sql` already defines `game_external_ids`.
- `docs/ARCHITECTURE.md` section 17 defines the approved Game Catalog and IGDB architecture.

As of 2026-09-17, the official IGDB API uses Twitch OAuth client credentials for server-to-server access, the API base is `https://api.igdb.com/v4`, requests are POST-based, and IGDB documents a 4 requests/second limit with at most 8 open requests. The Games endpoint supports relationship expansion such as `game_type.type`, `platforms.name`, `alternative_names.name`, and `cover.image_id`, so the first implementation can build the lightweight index from the Games endpoint without a provider-specific graph mirror.

References:

- https://api-docs.igdb.com/
- https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/

## Goals

1. Add optional IGDB credentials without making application startup depend on IGDB availability.
2. Build a complete local lightweight index of IGDB game records in PostgreSQL.
3. Never expose a partially completed first baseline to players.
4. Keep the previous successful index active if a later refresh fails.
5. Refresh automatically once per day and allow a global administrator to request `Sync now`.
6. Expose operator states `disabled`, `not synced`, `syncing`, `current`, `stale`, and `failed`.
7. Search authoritative local Games plus the active IGDB index by Game name and alternative names.
8. Apply player-visible type policy at search time using semantic IGDB Game Type names, not hardcoded numeric enum IDs.
9. Do not create a local Game merely because a provider result was searched or viewed.
10. Materialize exactly one authoritative local Game when a signed-in player follows an IGDB result or creates a Game Request for it.
11. Preserve local Games and provider identity if an IGDB record later disappears.
12. Keep cover bytes remote; persist only the IGDB cover image reference required to construct the approved `images.igdb.com` URL.

## Non-goals

- No generic multi-provider framework. IGDB is the only scheduled catalog provider.
- No live IGDB API call in a player-facing HTTP request.
- No local cover proxy, image cache, or object storage.
- No full IGDB graph mirror.
- No external scheduler, message broker, Redis, or additional runtime service.
- No automatic name-based merge between provider Games and manually managed local Games.
- No automatic mutation of an already materialized local Game's authoritative name/slug when IGDB later changes.
- No raw player search-query persistence or analytics pipeline.

## Design decisions

### 1. Configuration

Extend `config.Config` with an optional IGDB configuration:

```go
type IGDB struct {
    ClientID     string
    ClientSecret string
}
```

Environment variables:

```text
LANSURFER_IGDB_CLIENT_ID
LANSURFER_IGDB_CLIENT_SECRET
```

Rules:

- both empty: IGDB is disabled and this is a healthy configuration;
- one set without the other: configuration load fails;
- both set: construct the IGDB adapter, but do not authenticate or contact IGDB during application startup.

Add the variables, disabled by default, to `.env.example` and operator documentation.

### 2. IGDB HTTP adapter

Add a concrete adapter under `internal/game/igdb/`. Keep the consuming interfaces in the `game` module.

Use the standard library `net/http`; do not add a third-party IGDB wrapper. The API surface needed by LANsurfer is narrow enough that an additional wrapper dependency would add more compatibility surface than value.

The adapter owns:

- Twitch client-credentials token acquisition;
- in-memory app-access-token caching until shortly before expiry;
- one token refresh and one replay after a 401 from IGDB;
- bounded HTTP timeouts;
- response-size limits and JSON decoding;
- IGDB API rate limiting;
- keyset pagination for the Games endpoint.

Use a single shared limiter per client and start no more than one page request at a time in the initial implementation. A request-start interval of at least 250 ms stays within IGDB's documented 4 requests/second limit without adding another dependency. Sequential paging also stays below the documented maximum of 8 open requests.

Use keyset pagination rather than offsets:

```text
fields id,name,slug,game_type.type,first_release_date,parent_game,
       version_parent,platforms.name,alternative_names.name,
       cover.image_id,updated_at,checksum;
where id > <last_id>;
sort id asc;
limit 500;
```

Stop when a page contains fewer than 500 records.

The adapter maps IGDB JSON into a small provider DTO owned by the consuming `game` package. Provider API types must not leak into handlers or authoritative `Game` entities.

### 3. Provider-index persistence uses generations

A refresh must never replace the active index incrementally. Instead, every sync writes a new staging generation and atomically activates it only after the complete upstream scan succeeds.

Add a forward migration, expected to be `00006_igdb_discovery.sql`, containing the following concepts.

#### `igdb_sync_generations`

One row per attempted full sync:

- `id uuid primary key`
- `started_at timestamptz not null`
- `completed_at timestamptz null`
- `status text not null` constrained to `syncing`, `complete`, `failed`
- `record_count bigint not null default 0`
- `error text null`

Failed generations may be deleted after their diagnostic state has been copied to provider state. Completed generations are retained only while active or while needed for a safe post-swap cleanup.

#### `igdb_provider_state`

A singleton row containing:

- `active_generation_id uuid null`
- `last_attempt_at timestamptz null`
- `last_attempt_succeeded boolean null`
- `last_success_at timestamptz null`
- `last_error text null`

Player search reads only `active_generation_id`. Before the first successful baseline it is null, so no IGDB player result is visible.

Operator state is derived with this precedence:

1. `disabled` when credentials are not configured;
2. `syncing` while this process has a sync in flight;
3. `failed` when the most recent completed attempt failed;
4. `not synced` when no active generation exists;
5. `stale` when the active generation is older than 36 hours;
6. `current` otherwise.

The 36-hour stale threshold is an implementation-health signal around the required daily refresh, not a product latency SLO.

#### `igdb_game_index`

One row per IGDB Game per generation:

- `generation_id uuid not null`
- `igdb_id bigint not null`
- `name text not null`
- `slug text not null`
- `game_type text null`
- `first_release_date timestamptz null`
- `parent_igdb_id bigint null`
- `version_parent_igdb_id bigint null`
- `platform_names text[] not null default '{}'`
- `alternative_names text[] not null default '{}'`
- `cover_image_id text null`
- `igdb_updated_at timestamptz null`
- `checksum uuid null`
- `search_text text not null`
- primary key `(generation_id, igdb_id)`

`search_text` is normalized from the primary name plus alternative names at ingestion time. It is discovery infrastructure, not a persisted user-query log.

Enable PostgreSQL `pg_trgm` and add a trigram index suitable for case-insensitive substring search over `search_text`. This avoids scanning the full provider index for each player search while requiring no new runtime service.

### 4. Full sync algorithm

The sync service belongs to the Game capability because it populates Game discovery infrastructure and reconciles provider identities. It must not write Game Request state.

For each sync:

1. create a `syncing` generation and record `last_attempt_at`;
2. page through all IGDB Games in ascending ID order;
3. normalize semantic Game Type names, platform names, alternative names, cover image ID, and relationship IDs into staging rows;
4. batch-insert each page into the staging generation;
5. if any token, HTTP, decoding, or persistence step fails, mark the generation failed, record a bounded/sanitized error, keep the previous active generation untouched, and stop;
6. after the final page, activate the staging generation in one PostgreSQL transaction;
7. in that activation transaction, reconcile existing `game_external_ids` rows for provider `igdb` against the newly active index;
8. record the successful timestamp and clear `last_error`;
9. delete older non-active index generations after successful activation.

Only complete successful generations become visible.

### 5. External identities and provider disappearance

Extend `game_external_ids` with:

```text
archived_at timestamptz null
```

The existing unique `(provider, external_id)` identity remains the concurrency anchor.

After a successful generation swap:

- an IGDB external identity whose external ID still exists in the active generation has `archived_at = null`;
- an IGDB external identity whose external ID is absent from the complete active generation gets `archived_at = now()`;
- the authoritative local Game, subscriptions, Game Requests, and activity are never deleted because of that archival;
- non-materialized provider records simply disappear with the retired generation.

A later IGDB refresh that sees the same ID again clears `archived_at`.

### 6. Local Game name uniqueness must change

The current schema has a global case-insensitive unique index on `games.name`. That invariant is incompatible with provider materialization: different IGDB records can legitimately share a display name, and the approved architecture explicitly forbids name-based automatic merging.

The IGDB migration therefore drops `games_name_unique_idx`. `games.slug` remains unique, and provider identity remains unique through `game_external_ids`.

Administrator-created duplicate names are allowed after this migration. UI/read models must disambiguate provider search results with available type/platform/release context rather than relying on display-name uniqueness.

`ErrAlreadyExists` remains useful for slug/provider identity collisions but must no longer imply global Game-name uniqueness.

### 7. Player discovery read model

Add a focused discovery projection in the Game capability. It combines:

- active authoritative local Games;
- the active IGDB generation when IGDB is enabled and a successful baseline exists;
- current-user subscription state;
- existing `game_external_ids` mappings.

A provider row already materialized as a local Game must not be rendered as a duplicate provider result. It is returned as the authoritative local Game.

An unmaterialized provider result contains an opaque provider reference (`provider=igdb`, external ID) plus discovery-only fields such as type, platform names, release date, and cover image ID. Handlers must not accept arbitrary provider metadata back from the browser; they accept the provider reference and reload the active index row server-side before materialization.

Empty-search behavior should preserve the current local-catalog experience and must not dump the whole IGDB corpus into the page. IGDB results are included only for an explicit non-empty search and are bounded (initially 50 results). Local/My Games flows remain available without IGDB.

### 8. Player-visible Game Type policy

Use the semantic string returned by `game_type.type`; do not branch on numeric Game Type IDs. The official IGDB API is moving enum-like concepts to table-backed references, and the approved LANsurfer architecture requires semantic policy.

The policy is implemented as a small pure function with tests. Known categories approved for ordinary discovery are mapped to the product concepts:

- main games;
- standalone expansions;
- remakes;
- remasters;
- ports;
- independently playable expanded/edition variants.

Known DLC/add-on, bundle, mod, episode/season, pack, and update categories are hidden by default.

Unknown or newly introduced semantic Game Types default to hidden until explicitly reviewed. This prevents a provider taxonomy change from silently broadening player-visible product scope.

All records are still synchronized regardless of visibility policy.

### 9. Materialization

Add one narrow materialization operation to the `game` module. Its input is only provider + external ID. The operation reloads the provider row from the active generation and does not trust browser-supplied names, slugs, or metadata.

Within one Game-module PostgreSQL transaction:

1. look up `game_external_ids(provider='igdb', external_id=<id>)`;
2. if it already maps to a Game, return that Game and clear `archived_at` when the provider row is active again;
3. otherwise read the active provider-index row;
4. create a local Game using the current provider name and a deterministic unique slug;
5. copy current provider alternative names as initial local aliases where they satisfy local validation and do not duplicate another alias on that Game;
6. insert the `game_external_ids` row;
7. commit and return the authoritative local Game.

Concurrent materializations of the same IGDB ID must converge on one local Game. The `(provider, external_id)` unique constraint is the final persistence guard; retry the read after a uniqueness race.

Slug policy:

- prefer the IGDB slug when it is valid and unused;
- on collision, use a bounded deterministic suffix such as `-igdb-<external-id>` while preserving the 120-character local slug limit;
- never merge by slug or name.

After materialization, later provider refreshes do not automatically rename the authoritative Game or replace its local aliases. Provider search metadata may change independently.

### 10. Follow and Game Request integration

Following an unmaterialized IGDB result calls the Game materialization operation and then follows the returned authoritative Game. Keep this orchestration inside the Game capability so the browser never needs to synthesize a local Game ID.

Creating a Game Request from an IGDB discovery result first validates request input that does not depend on a Game ID, then asks the Game capability to resolve/materialize the provider reference, then continues through the existing Game Request service using the returned local Game ID.

The materialization transaction itself is atomic and concurrency-safe. It is not necessary to share the Game module's transaction with Game Request persistence. If request creation fails after successful materialization, the local Game remains a valid authoritative catalog record; this is preferable to crossing module write ownership or introducing a generic distributed transaction abstraction.

### 11. Scheduling and manual sync

When IGDB is enabled, construct the sync service in `internal/app/app.go` but start its first run asynchronously only after the HTTP server has been started.

The application remains ready even when IGDB authentication or synchronization fails.

Use one in-process single-flight guard because stable 1.0 is single-application-instance. Manual and scheduled requests share the same guard:

- if no sync is running, start one in the application context;
- if one is already running, return the current state instead of starting another;
- on shutdown, cancel the active sync through the application context.

After the initial attempt, schedule another attempt every 24 hours. A failed attempt does not remove the previous active generation and does not accelerate core application traffic. A later scheduled or manual attempt may recover normally.

### 12. Admin/operator surface

Reuse existing global-administrator authorization. Add a focused IGDB section to the existing administration surface rather than a new general provider console.

Show:

- enabled/disabled state;
- operator state;
- last attempt time;
- last successful sync time;
- active indexed record count;
- a short sanitized last-error message when failed;
- `Sync now` when enabled.

Never render the client secret or access token.

### 13. Covers

Persist only IGDB `cover.image_id`. Rendering constructs the already approved `images.igdb.com` URL and keeps the existing CSP host allowlist, `Referrer-Policy: no-referrer`, placeholder behavior, and attribution/privacy documentation.

Do not download, mirror, proxy, or cache image bytes in this feature.

## Failure handling

- Missing credentials: healthy `disabled` state.
- Partial credentials: startup configuration error.
- Twitch token failure: current sync fails; application stays healthy.
- IGDB 401: invalidate cached app token, acquire one new token, retry the request once.
- IGDB 429/5xx/network timeout: fail the current full generation with a useful operator error; do not activate partial data. The next manual/daily run retries the full sync.
- Malformed record: fail the generation rather than silently claiming a complete baseline.
- Database error while staging: fail the generation; active generation remains untouched.
- Application restart during sync: the incomplete generation is never active. On the next run it can be marked/cleaned as interrupted and a fresh generation starts.
- Provider record deletion: handled only after a complete successful generation, so transient upstream failure cannot tombstone identities.

## Testing strategy

### Configuration tests

- both IGDB variables empty -> disabled;
- both set -> enabled config;
- either variable missing -> configuration error.

### IGDB adapter unit tests with `httptest.Server`

- client-credentials token request and cache;
- token expiry margin;
- 401 causes one token refresh and one replay;
- non-2xx and malformed JSON errors are bounded and sanitized;
- fields/query body is stable and contains only approved lightweight fields;
- keyset pagination continues by last ID and stops on a short page;
- request pacing respects the configured limiter.

### PostgreSQL integration tests

- first incomplete generation is invisible;
- first successful generation becomes active atomically;
- failed refresh leaves the prior generation active;
- successful refresh replaces the active generation;
- removed unmaterialized rows disappear;
- removed materialized IGDB identities become archived without deleting local state;
- reappearing identities are unarchived;
- duplicate Game names are allowed after dropping the old unique-name index;
- provider search finds primary and alternative names;
- hidden semantic types do not appear in ordinary discovery;
- unknown semantic types default hidden.

### Materialization concurrency tests

- many concurrent materializations of one IGDB ID yield one `games` row and one external identity;
- two different IGDB IDs with the same display name yield two Games;
- local slug collision uses the deterministic IGDB suffix;
- materialization copies safe aliases once and later provider refreshes do not rename the local Game;
- an archived materialized Game remains usable.

### HTTP/web tests

- disabled IGDB preserves current local search behavior;
- no provider result is visible before baseline completion;
- materialized provider rows render once as local Games;
- admin status is global-admin-only;
- `Sync now` does not start overlapping runs;
- secrets/tokens never appear in rendered output.

Run `task generate`, `task lint`, `task test`, and `task test:integration` for implementation changes.

## Implementation slices

### Slice 1 — synchronization foundation

- config and tests;
- IGDB HTTP adapter and tests;
- migration for generations/provider state/index, `archived_at`, duplicate Game names, and trigram search support;
- PostgreSQL sync repository;
- sync service, generation swap, daily scheduling, manual trigger;
- admin status surface;
- no player provider search yet.

### Slice 2 — discovery

- combined local/provider discovery projection;
- semantic type visibility policy;
- bounded player search;
- cover-reference rendering;
- materialized-result de-duplication;
- no provider materialization action yet.

### Slice 3 — materialization and write integration

- atomic Game materialization;
- follow-provider-result flow;
- Game Request provider-reference flow;
- concurrency and failure tests;
- final end-to-end pilot discovery checks.

These slices may be implemented as separate reviewable PRs or as staged commits on this branch. Do not collapse them into one unreviewable change if implementation grows materially beyond this design.

## Explicit design trade-offs

### Full-generation rebuild vs incremental sync

Choose full-generation rebuilds for 1.0. They are easier to prove complete, naturally detect deletions, make partial-baseline isolation straightforward, and avoid complex change cursors. IGDB's page size and rate limits make a lightweight sequential rebuild practical for a daily background job. Incremental refresh can be reconsidered only if measured sync duration becomes a real operational problem.

### Direct API search vs local index

Choose the local index. Direct search would make player latency and availability depend on IGDB, violate the approved offline/failure model, complicate rate limiting, and make provider disappearance/materialization reconciliation harder.

### Generic provider framework vs IGDB-specific adapter

Choose the IGDB-specific adapter behind narrow Game-module interfaces. Additional catalog providers are not scheduled for 1.0, so a generic provider framework would be speculative architecture.

## Acceptance criteria

The design is implemented when:

- LANsurfer starts and works normally with IGDB disabled or unreachable;
- a complete successful baseline can be built without exposing partial provider results;
- a failed later refresh leaves the prior provider index usable;
- daily and manual sync are non-overlapping and observable;
- player discovery searches local and active provider data with approved semantic type visibility;
- search/view alone never creates a Game;
- follow or Game Request creation can materialize an IGDB result exactly once under concurrency;
- different provider records are never merged merely because their names match;
- provider disappearance never deletes authoritative LANsurfer state;
- cover bytes are never mirrored;
- relevant unit/integration/web tests and repository linting pass.
