# IGDB Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional, failure-tolerant IGDB synchronization, local discovery, and concurrency-safe materialization into LANsurfer's authoritative Game catalog.

**Architecture:** IGDB is a concrete adapter behind narrow `game`-module interfaces. Full provider scans are written into PostgreSQL generations and atomically activated only after a complete sync; player search joins local Games with the active provider generation, and follow/Game Request flows materialize provider results into authoritative local Games through `game_external_ids`.

**Tech Stack:** Go 1.27.1, PostgreSQL, pgx v5, sqlc 1.31.1, goose 3.28.0, chi, templ, HTMX, standard-library `net/http`, existing Taskfile workflows.

**Spec:** `docs/superpowers/specs/2026-09-17-igdb-discovery-design.md`

## Global Constraints

- LANsurfer's local `Game` catalog remains authoritative; the synchronized IGDB index is discovery infrastructure only.
- Do not make application startup or `/readyz` depend on IGDB availability.
- Do not issue live IGDB calls from player-facing HTTP requests.
- `LANSURFER_IGDB_CLIENT_ID` and `LANSURFER_IGDB_CLIENT_SECRET` are environment configuration; both empty means disabled, partial configuration is an error.
- Use the standard library HTTP client; do not add an IGDB wrapper dependency.
- Respect the documented IGDB limit with one sequential page request at a time and at least 250 ms between request starts.
- Synchronize all IGDB Game records; apply visible/hidden Game Type policy only at search time.
- Never expose a partial initial baseline. Later failed refreshes must leave the last successful generation active.
- Never merge provider results into local Games by name or slug.
- Concurrent materialization of one `provider + external_id` must converge on exactly one local Game.
- Keep remote cover bytes remote; persist only `cover.image_id`.
- No Redis, message broker, additional runtime service, generic provider framework, SPA, or external scheduler.
- Generated `*_templ.go` and `sqlcgen` files are regenerated with `task generate`; do not hand-edit them.
- Every task follows TDD: failing focused test first, minimal implementation, focused pass, then broader verification and commit.

---

## File Map

**Create**

- `internal/game/provider.go` — provider DTOs, references, type-visibility policy, operator state types.
- `internal/game/provider_test.go` — pure policy/reference tests.
- `internal/game/igdb/client.go` — Twitch token acquisition, IGDB requests, pacing, pagination.
- `internal/game/igdb/client_test.go` — `httptest.Server` contract tests for auth, paging, retries, limits.
- `internal/game/igdb_sync.go` — full-generation sync orchestration and single-flight scheduler-facing service.
- `internal/game/igdb_sync_test.go` — fake provider/repository tests for activation/failure semantics.
- `internal/game/discovery.go` — search, materialization, provider-follow orchestration.
- `internal/game/discovery_test.go` — service tests for local/provider results and validation.
- `internal/game/postgres/queries/igdb.sql` — sqlc queries for generations, provider state, index, discovery, materialization.
- `internal/game/postgres/igdb_store.go` — sync/index/discovery persistence.
- `internal/game/postgres/materialization.go` — atomic provider-to-local-Game materialization transaction.
- `internal/platform/postgres/migrations/00006_igdb_discovery.sql` — provider-index schema and existing Game constraint changes.

**Modify**

- `internal/platform/config/config.go`, `config_test.go` — optional IGDB credentials.
- `.env.example` — documented disabled-by-default IGDB variables.
- `internal/game/game.go` — keep existing catalog API, adjust duplicate-name semantics only where required.
- `internal/game/postgres/store.go` and `queries/games.sql` — coexist with duplicate Game names; preserve slug/alias guarantees.
- `internal/platform/postgres/postgres_integration_test.go` — migration, generation swap, materialization/race coverage.
- `internal/app/app.go` — wire IGDB adapter/services and start/cancel background sync after HTTP start.
- `internal/web/router.go`, `router_test.go` — inject discovery/sync dependencies and routes.
- `internal/web/handler/games.go` — discovery browse and provider follow.
- `internal/web/pages/games.templ` — distinguish local/provider results and provider actions.
- `internal/web/handler/game_requests.go`, `game_requests_test.go` — provider reference on create flow.
- `internal/web/pages/game_requests.templ` — preserve provider reference in new-request form.
- `internal/web/handler/administration.go` — IGDB status and manual sync actions.
- `internal/web/pages/admin.templ` — focused IGDB operator section.
- `docs/PRODUCT.md`, `docs/ARCHITECTURE.md` only if implementation reveals a factual mismatch; do not broaden approved scope.

---

### Task 1: Optional IGDB configuration

**Files:**
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Modify: `.env.example`

**Interfaces:**
- Produces: `config.IGDB{ClientID, ClientSecret string}` and `Config.IGDB *IGDB`.
- Consumes: environment only; no network access.

- [ ] **Step 1: Write failing configuration tests**

Add table cases equivalent to:

```go
func TestLoadIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "secret")

    cfg, err := Load()
    if err != nil {
        t.Fatalf("Load() error = %v", err)
    }
    if cfg.IGDB == nil || cfg.IGDB.ClientID != "client" || cfg.IGDB.ClientSecret != "secret" {
        t.Fatalf("IGDB = %#v", cfg.IGDB)
    }
}

func TestLoadRejectsPartialIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")

    if _, err := Load(); err == nil {
        t.Fatal("Load() unexpectedly accepted partial IGDB configuration")
    }
}
```

Also assert both variables empty yields `cfg.IGDB == nil`.

- [ ] **Step 2: Run the focused test and verify failure**

Run:

```bash
go test ./internal/platform/config -run IGDB -count=1
```

Expected: compile failure because `Config.IGDB` does not exist.

- [ ] **Step 3: Add the minimal configuration loader**

Add:

```go
type IGDB struct {
    ClientID     string
    ClientSecret string
}

func loadIGDB() (*IGDB, error) {
    clientID := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_ID"))
    clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_SECRET"))
    if clientID == "" && clientSecret == "" {
        return nil, nil
    }
    if clientID == "" || clientSecret == "" {
        return nil, errors.New("LANSURFER_IGDB_CLIENT_ID and LANSURFER_IGDB_CLIENT_SECRET must be configured together")
    }
    return &IGDB{ClientID: clientID, ClientSecret: clientSecret}, nil
}
```

Call `loadIGDB()` from `Load()` and store it on `Config` without network activity.

Append to `.env.example`:

```text
# Optional IGDB discovery provider. Configure both values or leave both empty.
LANSURFER_IGDB_CLIENT_ID=
LANSURFER_IGDB_CLIENT_SECRET=
```

- [ ] **Step 4: Run focused and package tests**

```bash
go test ./internal/platform/config -count=1
go test ./internal/platform/config -race -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/config/config.go internal/platform/config/config_test.go .env.example
git commit -m "feat: add optional IGDB configuration"
```

---

### Task 2: Provider types and player-visible Game Type policy

**Files:**
- Create: `internal/game/provider.go`
- Create: `internal/game/provider_test.go`

**Interfaces:**
- Produces: `ProviderReference`, `ProviderGame`, `ProviderState`, `ProviderStatus`, `ProviderCatalog`, `ProviderIndexRepository`, `VisibleProviderGameType(string) bool`.
- Consumes: standard library only.

- [ ] **Step 1: Write policy/reference tests**

Use exact public behavior:

```go
func TestVisibleProviderGameType(t *testing.T) {
    visible := []string{"Main Game", "Standalone Expansion", "Remake", "Remaster", "Port", "Expanded Game"}
    hidden := []string{"DLC Addon", "Bundle", "Mod", "Episode", "Season", "Pack", "Update", "future-new-type", ""}

    for _, value := range visible {
        if !VisibleProviderGameType(value) {
            t.Errorf("VisibleProviderGameType(%q) = false", value)
        }
    }
    for _, value := range hidden {
        if VisibleProviderGameType(value) {
            t.Errorf("VisibleProviderGameType(%q) = true", value)
        }
    }
}

func TestProviderReferenceValidation(t *testing.T) {
    if _, err := NewProviderReference("igdb", "123"); err != nil {
        t.Fatalf("valid reference: %v", err)
    }
    for _, tc := range [][2]string{{"", "123"}, {"igdb", ""}, {"steam", "123"}} {
        if _, err := NewProviderReference(tc[0], tc[1]); err == nil {
            t.Fatalf("reference %q/%q unexpectedly valid", tc[0], tc[1])
        }
    }
}
```

- [ ] **Step 2: Run the test to verify failure**

```bash
go test ./internal/game -run 'Provider|Visible' -count=1
```

Expected: compile failure for missing types/functions.

- [ ] **Step 3: Add the provider contracts**

Define these exact shapes, keeping transport JSON types out of the domain package:

```go
var ErrInvalidProviderReference = errors.New("invalid provider reference")
var ErrProviderRecordNotFound = errors.New("provider record not found")

type ProviderReference struct {
    Provider   string
    ExternalID string
}

type ProviderGame struct {
    Reference        ProviderReference
    Name             string
    Slug             string
    GameType         string
    FirstReleaseDate *time.Time
    ParentExternalID string
    VersionParentID  string
    PlatformNames    []string
    AlternativeNames []string
    CoverImageID     string
    ProviderUpdatedAt *time.Time
    Checksum         string
}

type ProviderState string

const (
    ProviderDisabled  ProviderState = "disabled"
    ProviderNotSynced ProviderState = "not synced"
    ProviderSyncing   ProviderState = "syncing"
    ProviderCurrent   ProviderState = "current"
    ProviderStale     ProviderState = "stale"
    ProviderFailed    ProviderState = "failed"
)

type ProviderStatus struct {
    State       ProviderState
    LastAttempt *time.Time
    LastSuccess *time.Time
    RecordCount int64
    LastError   string
}

type ProviderCatalog interface {
    Games(context.Context, func([]ProviderGame) error) error
}

type ProviderIndexRepository interface {
    BeginGeneration(context.Context) (string, error)
    StoreProviderGames(context.Context, string, []ProviderGame) error
    ActivateGeneration(context.Context, string, int64) error
    FailGeneration(context.Context, string, string) error
    Status(context.Context, bool, bool, time.Time) (ProviderStatus, error)
}
```

`NewProviderReference` accepts only provider `igdb`, trims whitespace, and requires a non-empty decimal external ID. Normalize known IGDB Game Type strings case-insensitively in `VisibleProviderGameType`; unknown values return false.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/game -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/provider.go internal/game/provider_test.go
git commit -m "feat: define IGDB provider contracts"
```

---

### Task 3: IGDB HTTP client with token caching, pacing, and keyset paging

**Files:**
- Create: `internal/game/igdb/client.go`
- Create: `internal/game/igdb/client_test.go`

**Interfaces:**
- Consumes: `game.ProviderGame`, implements `game.ProviderCatalog`.
- Produces: `igdb.NewClient(clientID, clientSecret string, options ...Option) *Client` and test-only options for endpoints/clock/sleep.

- [ ] **Step 1: Write `httptest.Server` tests for token and paging**

Cover these exact observable cases:

```go
func TestGamesGetsOneTokenAndPagesByLastID(t *testing.T) { /* token endpoint once; games endpoint receives id > 0 then id > 2; callback sees all records */ }
func TestGamesRefreshesTokenOnceAfter401(t *testing.T) { /* first IGDB request 401; second token acquired; page replayed once */ }
func TestGamesRejectsOversizedOrMalformedResponse(t *testing.T) { /* non-array/malformed JSON returns error */ }
func TestGamesPacesRequestStarts(t *testing.T) { /* injected sleep/clock records >=250ms between second and later IGDB starts */ }
```

For the paging fixture return two records on page one and one on page two by using a test page size of 2 through an option.

- [ ] **Step 2: Verify the tests fail**

```bash
go test ./internal/game/igdb -count=1
```

Expected: package/functions do not exist.

- [ ] **Step 3: Implement the narrow client**

Use constants:

```go
const (
    defaultTokenURL = "https://id.twitch.tv/oauth2/token"
    defaultGamesURL = "https://api.igdb.com/v4/games"
    defaultPageSize = 500
    minRequestInterval = 250 * time.Millisecond
    maxResponseBytes = 8 << 20
)
```

Token request:

```text
POST {tokenURL}?client_id=<id>&client_secret=<secret>&grant_type=client_credentials
```

IGDB request headers:

```text
Client-ID: <client id>
Authorization: Bearer <app token>
Content-Type: text/plain
```

Build the page body from the spec with `where id > <lastID>; sort id asc; limit <pageSize>;`. Decode an internal JSON struct and map it into `game.ProviderGame`; use decimal `strconv.FormatInt(id, 10)` for external IDs. Cache tokens with their expiry and refresh shortly before expiry. A 401 invalidates the token and permits exactly one token-refresh/replay for that page.

Keep pacing inside the client behind a mutex so parallel callers cannot exceed the request-start interval even if future code calls it concurrently.

- [ ] **Step 4: Run focused tests with race detector**

```bash
go test ./internal/game/igdb -race -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/igdb/client.go internal/game/igdb/client_test.go
git commit -m "feat: add IGDB API client"
```

---

### Task 4: PostgreSQL provider-index schema and sqlc persistence

**Files:**
- Create: `internal/platform/postgres/migrations/00006_igdb_discovery.sql`
- Create: `internal/game/postgres/queries/igdb.sql`
- Create: `internal/game/postgres/igdb_store.go`
- Modify/generated: `internal/game/postgres/sqlcgen/*`
- Modify: `internal/platform/postgres/postgres_integration_test.go`

**Interfaces:**
- Consumes: `game.ProviderIndexRepository`, `game.ProviderGame`, existing `game_external_ids`.
- Produces: `(*postgres.Store).BeginGeneration`, `StoreProviderGames`, `ActivateGeneration`, `FailGeneration`, `Status`.

- [ ] **Step 1: Write failing migration/integration coverage**

Add `TestIGDBGenerationPersistence` that migrates a clean DB and verifies:

```go
store := gamepostgres.NewStore(pool)
generationID, err := store.BeginGeneration(ctx)
// stage two ProviderGame rows
// before ActivateGeneration: discovery query returns zero provider rows
// ActivateGeneration(..., 2)
// provider status reports current + RecordCount 2
// second failed generation leaves first generation active
```

Extend `TestEventAndGameCatalogPersistence` to create two Games with identical names but different unique slugs and expect both inserts to succeed after migration.

- [ ] **Step 2: Verify integration tests fail**

```bash
task test:integration
```

Expected: compile/schema failure because migration and store methods do not exist.

- [ ] **Step 3: Add migration `00006_igdb_discovery.sql`**

The Up migration must contain the concrete schema:

```sql
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
    active_generation_id uuid NULL REFERENCES igdb_sync_generations(id),
    last_attempt_at timestamptz NULL,
    last_attempt_succeeded boolean NULL,
    last_success_at timestamptz NULL,
    last_error text NULL
);
INSERT INTO igdb_provider_state (singleton) VALUES (true);

CREATE TABLE igdb_game_index (
    generation_id uuid NOT NULL REFERENCES igdb_sync_generations(id) ON DELETE CASCADE,
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
CREATE INDEX igdb_game_index_search_trgm_idx ON igdb_game_index USING gin (search_text gin_trgm_ops);
```

The Down migration drops the IGDB tables/index extension-owned objects only, removes `archived_at`, and recreates `games_name_unique_idx` only after deleting no data; because production DOWN migrations are not automatic, document in a SQL comment that DOWN can fail if duplicate names were created after Up.

- [ ] **Step 4: Add sqlc queries and Store methods**

Queries must include:

```sql
-- name: BeginIGDBGeneration :one
INSERT INTO igdb_sync_generations (status) VALUES ('syncing') RETURNING id::text;

-- name: InsertIGDBGame :exec
INSERT INTO igdb_game_index (...columns...)
VALUES (...args...);

-- name: ActivateIGDBGeneration :exec
UPDATE igdb_sync_generations SET status='complete', completed_at=now(), record_count=sqlc.arg(record_count) WHERE id=sqlc.arg(id)::uuid;

-- name: SetActiveIGDBGeneration :exec
UPDATE igdb_provider_state SET active_generation_id=sqlc.arg(id)::uuid, last_attempt_at=now(), last_attempt_succeeded=true, last_success_at=now(), last_error=NULL WHERE singleton;

-- name: FailIGDBGeneration :exec
UPDATE igdb_sync_generations SET status='failed', completed_at=now(), error=sqlc.arg(error) WHERE id=sqlc.arg(id)::uuid;
```

`ActivateGeneration` wraps generation completion, state swap, external-ID archival reconciliation, and old-generation cleanup in one pgx transaction. Sanitize persisted errors to one line and max 500 runes.

- [ ] **Step 5: Generate sqlc and run integration tests**

```bash
task generate
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/postgres/migrations/00006_igdb_discovery.sql internal/game/postgres/queries/igdb.sql internal/game/postgres/igdb_store.go internal/game/postgres/sqlcgen internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: persist IGDB sync generations"
```

---

### Task 5: Full-sync service and failure-safe generation activation

**Files:**
- Create: `internal/game/igdb_sync.go`
- Create: `internal/game/igdb_sync_test.go`

**Interfaces:**
- Consumes: `ProviderCatalog`, `ProviderIndexRepository`.
- Produces: `NewIGDBSyncService(catalog ProviderCatalog, repository ProviderIndexRepository, now func() time.Time) *IGDBSyncService`, `Sync(context.Context) error`, `Status(context.Context, enabled bool) (ProviderStatus, error)`, `TryStart(context.Context) bool`.

- [ ] **Step 1: Write fake-based sync tests**

Create fakes proving:

```go
func TestIGDBSyncActivatesOnlyAfterCompleteCatalog(t *testing.T) { /* callback batches -> activate once at end */ }
func TestIGDBSyncFailureMarksAttemptAndDoesNotActivate(t *testing.T) { /* catalog returns error after first batch */ }
func TestIGDBSyncSingleFlight(t *testing.T) { /* second TryStart while first blocked returns false */ }
```

The repository fake records `BeginGeneration`, staged batches, `ActivateGeneration`, and `FailGeneration` calls.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/game -run IGDBSync -count=1
```

Expected: missing service.

- [ ] **Step 3: Implement minimal sync orchestration**

Core flow:

```go
func (s *IGDBSyncService) Sync(ctx context.Context) error {
    generationID, err := s.repository.BeginGeneration(ctx)
    if err != nil { return fmt.Errorf("begin IGDB generation: %w", err) }

    var count int64
    err = s.catalog.Games(ctx, func(batch []ProviderGame) error {
        if err := s.repository.StoreProviderGames(ctx, generationID, batch); err != nil {
            return err
        }
        count += int64(len(batch))
        return nil
    })
    if err != nil {
        _ = s.repository.FailGeneration(context.WithoutCancel(ctx), generationID, err.Error())
        return fmt.Errorf("synchronize IGDB: %w", err)
    }
    if err := s.repository.ActivateGeneration(ctx, generationID, count); err != nil {
        _ = s.repository.FailGeneration(context.WithoutCancel(ctx), generationID, err.Error())
        return fmt.Errorf("activate IGDB generation: %w", err)
    }
    return nil
}
```

Use a mutex/boolean for single-flight. `Status` derives `disabled` before querying persistence when credentials are absent; repository state derives not-synced/current/stale/failed and the service overlays syncing while in flight.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/game -race -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/igdb_sync.go internal/game/igdb_sync_test.go
git commit -m "feat: orchestrate IGDB full synchronization"
```

---

### Task 6: Wire background initial/daily synchronization without affecting readiness

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/web/router_test.go` only if dependency struct additions require fixtures.

**Interfaces:**
- Consumes: `config.IGDB`, `igdb.Client`, `game.IGDBSyncService`, `gamepostgres.Store`.
- Produces: application-owned background loop cancelled by the existing `Run` context.

- [ ] **Step 1: Add a testable scheduler helper and failing unit test**

Keep scheduling outside the HTTP handler. Add an unexported helper in `internal/app/app.go` or a focused `internal/app/igdb.go` if `app.go` becomes unwieldy:

```go
type syncStarter interface { TryStart(context.Context) bool }

func runDailyIGDBSync(ctx context.Context, syncer syncStarter, interval time.Duration) {
    syncer.TryStart(ctx)
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C: syncer.TryStart(ctx)
        }
    }
}
```

Test cancellation and one initial call with a fake; use a millisecond interval only in test.

- [ ] **Step 2: Verify the focused test fails**

```bash
go test ./internal/app -run IGDB -count=1
```

- [ ] **Step 3: Wire the enabled provider after DB/services are ready**

When `cfg.IGDB != nil`:

```go
igdbClient := igdb.NewClient(cfg.IGDB.ClientID, cfg.IGDB.ClientSecret)
igdbSync := game.NewIGDBSyncService(igdbClient, gameStore, time.Now)
```

Start `go runDailyIGDBSync(ctx, igdbSync, 24*time.Hour)` only after the HTTP server goroutine has started. Do not change `postgres.ReadyCheck(pool)` and do not return an error from `Run` when a background IGDB attempt fails; the sync service/logger records provider failure.

- [ ] **Step 4: Run package and full unit tests**

```bash
go test ./internal/app ./internal/game/... -race -count=1
task test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app internal/web/router_test.go
git commit -m "feat: schedule IGDB background synchronization"
```

---

### Task 7: Local + active-provider discovery read model

**Files:**
- Create: `internal/game/discovery.go`
- Create: `internal/game/discovery_test.go`
- Extend: `internal/game/postgres/queries/igdb.sql`
- Extend: `internal/game/postgres/igdb_store.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/pages/games.templ`
- Modify/generated: `internal/web/pages/games_templ.go`, `internal/game/postgres/sqlcgen/*`

**Interfaces:**
- Produces: `DiscoveryItem`, `DiscoveryRepository.Search(ctx, userID, search string, limit int)`, `DiscoveryService.Browse(ctx, userID, search string)`.
- Consumes: active generation only; no provider HTTP calls.

- [ ] **Step 1: Write service tests for empty search, visibility, and deduplication contract**

Define the result model:

```go
type DiscoveryItem struct {
    LocalGameID      string
    Reference        ProviderReference
    Name             string
    Slug             string
    GameType         string
    PlatformNames    []string
    FirstReleaseDate *time.Time
    CoverImageID     string
    Subscribed       bool
    Materialized     bool
}
```

Tests must prove:

- empty search calls only local browse and returns no unmaterialized provider rows;
- provider rows with hidden/unknown type are excluded;
- a provider row mapped by `game_external_ids` returns the authoritative local result once, not as a duplicate;
- at most 50 provider results are returned.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/game -run Discovery -count=1
```

- [ ] **Step 3: Add PostgreSQL discovery query**

The query must search only the active generation and use trigram/substring matching against normalized `search_text`. It also left-joins `game_external_ids` and `games` for `provider='igdb'` so materialized rows can be represented by the local Game. Keep user subscription lookup read-only.

Normalize search as lowercase trimmed text. Do not persist the query string.

- [ ] **Step 4: Implement `DiscoveryService.Browse` and update `/games`**

Change the handler dependency from `GameSubscriptions.Browse` to a discovery interface:

```go
type GameDiscovery interface {
    Browse(context.Context, string, string) ([]game.DiscoveryItem, error)
    FollowLocal(context.Context, string, string) error
    Unfollow(context.Context, string, string) error
    MyGames(context.Context, string) ([]game.SubscriptionGame, error)
}
```

Keep local results looking like the current cards. Provider cards show `Name`, semantic type/platform/release context, and an IGDB provider action. Do not show all IGDB records on empty search.

- [ ] **Step 5: Generate and run tests**

```bash
task generate
go test ./internal/game ./internal/web/handler ./internal/web -race -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game/discovery.go internal/game/discovery_test.go internal/game/postgres/queries/igdb.sql internal/game/postgres/igdb_store.go internal/game/postgres/sqlcgen internal/web/handler/games.go internal/web/pages/games.templ internal/web/pages/games_templ.go internal/web/router.go internal/web/router_test.go
git commit -m "feat: search synchronized IGDB games"
```

---

### Task 8: Atomic provider-result materialization

**Files:**
- Create: `internal/game/postgres/materialization.go`
- Extend: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/discovery.go`, `discovery_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Modify/generated: `internal/game/postgres/sqlcgen/*`

**Interfaces:**
- Produces: `MaterializationRepository.Materialize(ctx, ref ProviderReference) (Game, error)` and `DiscoveryService.Resolve(ctx, ref ProviderReference) (Game, error)`.
- Consumes: active provider index and existing `game_external_ids(provider, external_id)` uniqueness.

- [ ] **Step 1: Write integration tests for same-name and concurrent materialization**

Add tests that seed an active provider generation and assert:

```go
first, err := discovery.Resolve(ctx, game.ProviderReference{Provider: "igdb", ExternalID: "100"})
second, err := discovery.Resolve(ctx, game.ProviderReference{Provider: "igdb", ExternalID: "100"})
if first.ID != second.ID { t.Fatalf("materialization IDs differ") }
```

Run two goroutines resolving the same external ID and assert both return the same local Game ID and exactly one `game_external_ids` row exists.

Seed two provider rows with the same display name but IDs 100 and 101; materialize both and assert distinct local Game IDs/slugs.

- [ ] **Step 2: Verify integration failure**

```bash
task test:integration
```

- [ ] **Step 3: Implement transaction and deterministic slug collision handling**

Inside one pgx transaction:

1. read existing provider mapping and return it if present;
2. read provider row from the active generation;
3. choose local slug: valid unused provider slug, otherwise a bounded `<base>-igdb-<id>` form;
4. insert `games` row;
5. insert non-empty valid alternative names with `ON CONFLICT DO NOTHING` for aliases on that Game;
6. insert `game_external_ids(game_id,'igdb',external_id,archived_at=NULL)`;
7. commit.

If the external-ID insert gets SQLSTATE `23505`, roll back, re-read the winning provider mapping, and return it. Do not match by local name.

- [ ] **Step 4: Expose through `DiscoveryService.Resolve`**

Validate `ProviderReference` before persistence. Translate missing active provider row to `game.ErrProviderRecordNotFound`.

- [ ] **Step 5: Generate and verify**

```bash
task generate
go test ./internal/game -race -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game/discovery.go internal/game/discovery_test.go internal/game/postgres/materialization.go internal/game/postgres/queries/igdb.sql internal/game/postgres/sqlcgen internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: materialize IGDB games atomically"
```

---

### Task 9: Follow an unmaterialized provider result

**Files:**
- Modify: `internal/game/discovery.go`, `discovery_test.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/pages/games.templ`
- Modify: `internal/web/router.go`, `router_test.go`
- Regenerate: `internal/web/pages/games_templ.go`

**Interfaces:**
- Produces: `DiscoveryService.Follow(ctx, userID string, ref ProviderReference) (Game, error)`.
- Consumes: `Resolve` then existing subscription repository.

- [ ] **Step 1: Write failing service test**

Prove `Follow` materializes first, follows the resulting local ID once, and returns that local Game. A second call must be idempotent through the existing subscription behavior.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/game -run FollowProvider -count=1
```

- [ ] **Step 3: Implement service and provider route**

Service flow:

```go
func (s *DiscoveryService) Follow(ctx context.Context, userID string, ref ProviderReference) (Game, error) {
    item, err := s.Resolve(ctx, ref)
    if err != nil { return Game{}, err }
    if err := s.subscriptions.Follow(ctx, userID, item.ID); err != nil { return Game{}, err }
    return item, nil
}
```

Add authenticated route:

```go
games.Post("/providers/{provider}/{externalID}/follow", gamesHandler.FollowProvider)
```

`FollowProvider` constructs a validated reference from URL params and calls the discovery service. Provider result cards post to this route; after success render the local follow control using the returned local Game ID.

- [ ] **Step 4: Generate and run handler/router tests**

```bash
task generate
go test ./internal/game ./internal/web/handler ./internal/web -race -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/discovery.go internal/game/discovery_test.go internal/web/handler/games.go internal/web/pages/games.templ internal/web/pages/games_templ.go internal/web/router.go internal/web/router_test.go
git commit -m "feat: follow IGDB discovery results"
```

---

### Task 10: Create a Game Request from an IGDB result

**Files:**
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Regenerate: `internal/web/pages/game_requests_templ.go`
- Modify: `internal/web/router.go` dependency wiring if needed.

**Interfaces:**
- Consumes: `GameResolver.Resolve(context.Context, game.ProviderReference) (game.Game, error)` and existing `GameRequestCommands.Create`.
- Produces: provider-reference-aware create form without changing the core `gamerequest.Service.Create` signature.

- [ ] **Step 1: Write handler tests**

Add a fake resolver and assert:

- `/game-requests/new?provider=igdb&external_id=123` preserves provider fields in hidden form inputs;
- valid create with provider fields calls resolver, then calls `GameRequestCommands.Create` with the returned local Game ID;
- invalid request form does not materialize a Game;
- materialization failure re-renders a recoverable form error.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/web/handler -run 'GameRequest.*Provider|Provider.*GameRequest' -count=1
```

- [ ] **Step 3: Add a narrow resolver dependency**

Define in handler:

```go
type GameResolver interface {
    Resolve(context.Context, game.ProviderReference) (game.Game, error)
}
```

Extend `GameRequestForm` with `Provider` and `ExternalID`. Parse/validate max players/time/description first. Only after those fields are valid, resolve the provider reference and replace `form.GameID` with the authoritative local Game ID before calling the unchanged Game Request command.

Keep local `game_id` create behavior unchanged.

- [ ] **Step 4: Link provider cards to the create flow**

On a provider discovery card, `Find Players` points to:

```text
/game-requests/new?provider=igdb&external_id=<id>
```

Do not trust/display browser-posted provider name as authoritative data.

- [ ] **Step 5: Generate and verify**

```bash
task generate
go test ./internal/web/handler ./internal/web -race -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/handler/game_requests.go internal/web/handler/game_requests_test.go internal/web/pages/game_requests.templ internal/web/pages/game_requests_templ.go internal/web/pages/games.templ internal/web/pages/games_templ.go internal/web/router.go
git commit -m "feat: create requests from IGDB games"
```

---

### Task 11: Operator status and manual `Sync now`

**Files:**
- Modify: `internal/web/handler/administration.go`
- Modify: `internal/web/pages/admin.templ`
- Modify/generated: `internal/web/pages/admin_templ.go`
- Modify: `internal/web/router.go`, `router_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `ProviderSyncAdministration.Status(context.Context) (game.ProviderStatus, error)` and `TryStart(context.Context) bool`.
- Produces: `GET /admin/igdb`, `POST /admin/igdb/sync` guarded by existing `RequireAdmin`.

- [ ] **Step 1: Write router/handler tests**

Cover:

- anonymous/non-admin cannot access IGDB admin routes;
- disabled provider renders `disabled`, no secret/token, no active sync button;
- enabled current/failed/syncing states render status timestamps/count/error;
- POST sync starts when idle and redirects to `/admin/igdb?notice=igdb-sync-started`;
- POST while already syncing redirects with an already-running notice and does not start a second sync.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/web/... -run IGDB -count=1
```

- [ ] **Step 3: Add administration interface and routes**

Use:

```go
type ProviderSyncAdministration interface {
    Status(context.Context) (game.ProviderStatus, error)
    TryStart(context.Context) bool
}
```

Add to `RouterDependencies` and `NewAdministration`, then register:

```go
admin.Get("/igdb", administrationHandler.IGDB)
admin.Post("/igdb/sync", administrationHandler.SyncIGDB)
```

The page shows state, last attempt, last success, record count, bounded sanitized error, and the button only when provider is enabled and not currently syncing. Never expose client secret/access token.

- [ ] **Step 4: Wire enabled and disabled status adapters in `app.Run`**

Provide a small disabled implementation when `cfg.IGDB == nil` so the admin page can consistently show `disabled` without nil branching throughout handlers.

- [ ] **Step 5: Generate and verify**

```bash
task generate
go test ./internal/web/... ./internal/app ./internal/game/... -race -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/handler/administration.go internal/web/pages/admin.templ internal/web/pages/admin_templ.go internal/web/router.go internal/web/router_test.go internal/app/app.go
git commit -m "feat: add IGDB sync administration"
```

---

### Task 12: Provider disappearance, restart recovery, covers, and final verification

**Files:**
- Extend: `internal/game/postgres/queries/igdb.sql`
- Extend: `internal/game/postgres/igdb_store.go`
- Extend: `internal/platform/postgres/postgres_integration_test.go`
- Modify: player templates only if cover rendering is not already present.
- Modify: `docs/PRODUCT.md` / `docs/ARCHITECTURE.md` only for factual implementation notes that are missing, not new scope.

**Interfaces:**
- Consumes: completed generation activation and existing remote-cover CSP exception.
- Produces: archival/reappearance semantics proven by integration tests.

- [ ] **Step 1: Write integration tests for disappearance and restart cleanup**

Add cases that:

1. materialize IGDB ID 100;
2. activate a later complete generation without 100;
3. assert `game_external_ids.archived_at IS NOT NULL` while the local Game/subscription remains;
4. activate another generation containing 100 and assert `archived_at IS NULL`;
5. create a `syncing` generation, simulate process restart by constructing a new Store/service, start a fresh sync, and prove only the newly completed generation becomes active.

- [ ] **Step 2: Verify tests fail**

```bash
task test:integration
```

- [ ] **Step 3: Implement archival/reappearance and stale attempt handling**

During generation activation execute transactionally:

```sql
UPDATE game_external_ids ge
SET archived_at = now()
WHERE lower(btrim(ge.provider)) = 'igdb'
  AND NOT EXISTS (
      SELECT 1 FROM igdb_game_index i
      WHERE i.generation_id = sqlc.arg(generation_id)::uuid
        AND i.igdb_id::text = ge.external_id
  );

UPDATE game_external_ids ge
SET archived_at = NULL
WHERE lower(btrim(ge.provider)) = 'igdb'
  AND EXISTS (
      SELECT 1 FROM igdb_game_index i
      WHERE i.generation_id = sqlc.arg(generation_id)::uuid
        AND i.igdb_id::text = ge.external_id
  );
```

Before beginning a new attempt, mark abandoned `syncing` generations failed with a bounded message such as `previous synchronization did not complete`. Do not alter the active generation.

Render remote covers using only the stored `cover.image_id` and the already-approved IGDB image host; do not add downloads/caching.

- [ ] **Step 4: Run complete generation and verification suite**

```bash
task generate
task lint
task test
task test:integration
task build
```

Expected: every command exits 0.

- [ ] **Step 5: Inspect the final diff against the approved spec**

Run:

```bash
git diff main...HEAD --stat
git diff main...HEAD -- docs/PRODUCT.md docs/ARCHITECTURE.md AGENTS.md
git status --short
```

Expected: no unintended architecture/scope change, no untracked generated files, and only approved IGDB implementation/documentation changes.

- [ ] **Step 6: Commit final hardening**

```bash
git add internal docs
git commit -m "test: harden IGDB synchronization lifecycle"
```

---

## Final Acceptance Checklist

- [ ] Empty IGDB credentials produce a healthy disabled deployment.
- [ ] Partial credentials fail configuration load before startup.
- [ ] HTTP server and `/readyz` do not depend on IGDB reachability.
- [ ] First provider results are invisible until a complete baseline activates.
- [ ] A failed later refresh preserves the prior active generation.
- [ ] Automatic and manual sync cannot overlap in one application instance.
- [ ] Search uses PostgreSQL only and includes local Games even with IGDB disabled.
- [ ] Empty search does not dump the IGDB corpus.
- [ ] Hidden/unknown provider Game Types are not player-visible but remain synchronized.
- [ ] Materialized provider Games do not appear twice in discovery.
- [ ] Same-name IGDB records can materialize into distinct local Games.
- [ ] Concurrent materialization of one IGDB ID yields exactly one local Game.
- [ ] Follow of an IGDB result materializes then follows the authoritative Game.
- [ ] Game Request creation validates its normal form before materialization, then uses the resulting local Game ID.
- [ ] A disappearing provider record archives its identity without deleting local domain data; reappearance clears archival.
- [ ] Admin status includes disabled/not-synced/syncing/current/stale/failed without exposing credentials/tokens.
- [ ] Covers remain remote via the approved `images.igdb.com` exception; no cache/proxy/object storage is introduced.
- [ ] `task generate`, `task lint`, `task test`, `task test:integration`, and `task build` all pass.
