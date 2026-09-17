# IGDB Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional, failure-tolerant IGDB synchronization, PostgreSQL-backed player discovery, and concurrency-safe materialization into LANsurfer's authoritative Game catalog.

**Architecture:** A concrete IGDB adapter streams the complete provider catalog into generation-scoped PostgreSQL rows. Only a completely synchronized generation becomes active. Player requests read only PostgreSQL; following or creating a Game Request from a provider result resolves it through the `game` module into one authoritative local Game keyed by `provider + external_id`.

**Tech Stack:** Go 1.27.1, PostgreSQL, pgx v5, sqlc 1.31.1, goose 3.28.0, chi, templ, HTMX, standard-library `net/http`, existing Taskfile workflows.

**Spec:** `docs/superpowers/specs/2026-09-17-igdb-discovery-design.md`

## Global Constraints

- The local PostgreSQL `Game` catalog remains authoritative. The IGDB index is discovery infrastructure, never a second domain model.
- Both IGDB environment variables empty means healthy `disabled`; partial configuration is an error.
- Application startup and `/readyz` must not depend on Twitch/IGDB reachability.
- No player-facing request may call IGDB directly.
- Use standard-library HTTP, no third-party IGDB wrapper.
- Keep IGDB request starts at least 250 ms apart and use sequential page requests.
- Synchronize all IGDB Games. Apply visible/hidden Game Type policy only when reading player search results.
- Never expose a partial first baseline. Never replace a successful active generation with a failed refresh.
- Never merge by Game name or slug.
- Concurrent materialization of one IGDB ID must converge on one local Game via `game_external_ids` uniqueness.
- Persist only `cover.image_id`; do not cache/proxy cover bytes.
- No Redis, message broker, generic provider framework, SPA, external scheduler, or additional runtime service.
- Generated templ/sqlc output is produced by `task generate`, never hand-edited.
- Each task is one reviewable commit and uses failing-test -> minimal implementation -> passing-test discipline.

## Stable Interfaces Used Across Tasks

These names/signatures are authoritative for this plan. Do not invent alternate contracts in later tasks.

```go
// internal/game/provider.go

type ProviderReference struct {
    Provider   string
    ExternalID string
}

type ProviderGame struct {
    Reference         ProviderReference
    Name              string
    Slug              string
    GameType          string
    FirstReleaseDate  *time.Time
    ParentExternalID  string
    VersionParentID   string
    PlatformNames     []string
    AlternativeNames  []string
    CoverImageID      string
    ProviderUpdatedAt *time.Time
    Checksum          string
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
    Status(context.Context, time.Time) (ProviderStatus, error)
}

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

type DiscoveryRepository interface {
    Search(context.Context, string, string, int) ([]DiscoveryItem, error)
}

type MaterializationRepository interface {
    Materialize(context.Context, ProviderReference) (Game, error)
}
```

---

### Task 1: Configuration and provider-domain contracts

**Files:**
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Modify: `.env.example`
- Create: `internal/game/provider.go`
- Create: `internal/game/provider_test.go`

**Produces:** `config.IGDB`, `Config.IGDB`, the stable interfaces above, `NewProviderReference`, `VisibleProviderGameType`.

- [ ] **Step 1: Add failing configuration tests**

```go
func TestLoadIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "secret")
    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error = %v", err) }
    if cfg.IGDB == nil || cfg.IGDB.ClientID != "client" || cfg.IGDB.ClientSecret != "secret" {
        t.Fatalf("IGDB = %#v", cfg.IGDB)
    }
}

func TestLoadIGDBDisabledWhenCredentialsEmpty(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")
    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error = %v", err) }
    if cfg.IGDB != nil { t.Fatalf("IGDB = %#v, want nil", cfg.IGDB) }
}

func TestLoadRejectsPartialIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")
    if _, err := Load(); err == nil { t.Fatal("Load() accepted partial IGDB configuration") }
}
```

- [ ] **Step 2: Add failing provider-policy tests**

```go
func TestNewProviderReference(t *testing.T) {
    ref, err := NewProviderReference(" IGDB ", " 123 ")
    if err != nil { t.Fatalf("NewProviderReference() error = %v", err) }
    if ref.Provider != "igdb" || ref.ExternalID != "123" { t.Fatalf("ref = %#v", ref) }
    for _, tc := range [][2]string{{"", "1"}, {"igdb", ""}, {"steam", "1"}, {"igdb", "abc"}} {
        if _, err := NewProviderReference(tc[0], tc[1]); !errors.Is(err, ErrInvalidProviderReference) {
            t.Fatalf("NewProviderReference(%q,%q) error = %v", tc[0], tc[1], err)
        }
    }
}

func TestVisibleProviderGameType(t *testing.T) {
    for _, value := range []string{"Main Game", "Standalone Expansion", "Remake", "Remaster", "Port", "Expanded Game"} {
        if !VisibleProviderGameType(value) { t.Errorf("%q should be visible", value) }
    }
    for _, value := range []string{"DLC Addon", "Bundle", "Mod", "Episode", "Season", "Pack", "Update", "future type", ""} {
        if VisibleProviderGameType(value) { t.Errorf("%q should be hidden", value) }
    }
}
```

- [ ] **Step 3: Verify failure**

```bash
go test ./internal/platform/config ./internal/game -run 'IGDB|Provider|Visible' -count=1
```

Expected: missing IGDB config/provider declarations.

- [ ] **Step 4: Implement config loading and provider types**

Add to config:

```go
type IGDB struct { ClientID, ClientSecret string }

func loadIGDB() (*IGDB, error) {
    id := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_ID"))
    secret := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_SECRET"))
    if id == "" && secret == "" { return nil, nil }
    if id == "" || secret == "" {
        return nil, errors.New("LANSURFER_IGDB_CLIENT_ID and LANSURFER_IGDB_CLIENT_SECRET must be configured together")
    }
    return &IGDB{ClientID: id, ClientSecret: secret}, nil
}
```

Add `IGDB *IGDB` to `Config`, call `loadIGDB()` in `Load()`, and append disabled-by-default variables to `.env.example`.

In `provider.go`, add the stable types/interfaces and:

```go
var ErrInvalidProviderReference = errors.New("invalid provider reference")
var ErrProviderRecordNotFound = errors.New("provider record not found")

func NewProviderReference(provider, externalID string) (ProviderReference, error) {
    provider = strings.ToLower(strings.TrimSpace(provider))
    externalID = strings.TrimSpace(externalID)
    if provider != "igdb" || externalID == "" {
        return ProviderReference{}, ErrInvalidProviderReference
    }
    if _, err := strconv.ParseInt(externalID, 10, 64); err != nil {
        return ProviderReference{}, ErrInvalidProviderReference
    }
    return ProviderReference{Provider: provider, ExternalID: externalID}, nil
}

func VisibleProviderGameType(value string) bool {
    switch strings.ToLower(strings.TrimSpace(value)) {
    case "main game", "standalone expansion", "remake", "remaster", "port", "expanded game":
        return true
    default:
        return false
    }
}
```

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/platform/config ./internal/game -race -count=1
git add .env.example internal/platform/config internal/game/provider.go internal/game/provider_test.go
git commit -m "feat: define IGDB configuration and provider contracts"
```

---

### Task 2: IGDB HTTP adapter

**Files:**
- Create: `internal/game/igdb/client.go`
- Create: `internal/game/igdb/client_test.go`

**Consumes:** `game.ProviderCatalog`, `game.ProviderGame`.
**Produces:** `igdb.NewClient(clientID, clientSecret string, options ...Option) *Client`.

- [ ] **Step 1: Write concrete paging/auth test**

Use an `httptest.Server` with one handler that distinguishes token and Games paths:

```go
func TestGamesGetsTokenAndUsesKeysetPaging(t *testing.T) {
    var tokenCalls, gameCalls int
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/oauth2/token":
            tokenCalls++
            w.Header().Set("Content-Type", "application/json")
            _, _ = io.WriteString(w, `{"access_token":"token-1","expires_in":3600,"token_type":"bearer"}`)
        case "/v4/games":
            gameCalls++
            body, _ := io.ReadAll(r.Body)
            if r.Header.Get("Client-ID") != "client" || r.Header.Get("Authorization") != "Bearer token-1" {
                t.Fatalf("headers = %#v", r.Header)
            }
            if gameCalls == 1 {
                if !bytes.Contains(body, []byte("where id > 0")) { t.Fatalf("first query = %s", body) }
                _, _ = io.WriteString(w, `[{"id":1,"name":"One","slug":"one","game_type":{"type":"Main Game"}},{"id":2,"name":"Two","slug":"two","game_type":{"type":"Remake"}}]`)
            } else {
                if !bytes.Contains(body, []byte("where id > 2")) { t.Fatalf("second query = %s", body) }
                _, _ = io.WriteString(w, `[{"id":3,"name":"Three","slug":"three","game_type":{"type":"Port"}}]`)
            }
        default:
            http.NotFound(w, r)
        }
    }))
    defer server.Close()

    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithPageSize(2), WithRequestInterval(0))
    var ids []string
    err := client.Games(t.Context(), func(batch []game.ProviderGame) error {
        for _, item := range batch { ids = append(ids, item.Reference.ExternalID) }
        return nil
    })
    if err != nil { t.Fatalf("Games() error = %v", err) }
    if diff := cmp.Diff([]string{"1", "2", "3"}, ids); diff != "" { t.Fatalf("ids mismatch (-want +got):\n%s", diff) }
    if tokenCalls != 1 || gameCalls != 2 { t.Fatalf("calls token=%d games=%d", tokenCalls, gameCalls) }
}
```

If the repo should not add `go-cmp`, replace the `cmp.Diff` line with `slices.Equal` from the standard library; do not add a testing dependency only for this assertion.

- [ ] **Step 2: Write concrete 401 replay test**

```go
func TestGamesRefreshesTokenOnceAfter401(t *testing.T) {
    var tokenCalls, gameCalls int
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/oauth2/token" {
            tokenCalls++
            _, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3600,"token_type":"bearer"}`, tokenCalls)
            return
        }
        gameCalls++
        if gameCalls == 1 { w.WriteHeader(http.StatusUnauthorized); return }
        _, _ = io.WriteString(w, `[]`)
    }))
    defer server.Close()
    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithRequestInterval(0))
    if err := client.Games(t.Context(), func([]game.ProviderGame) error { return nil }); err != nil { t.Fatalf("Games() error = %v", err) }
    if tokenCalls != 2 || gameCalls != 2 { t.Fatalf("calls token=%d games=%d", tokenCalls, gameCalls) }
}
```

Add malformed JSON and response-size tests using the same server pattern; both must return non-nil errors.

- [ ] **Step 3: Verify failure**

```bash
go test ./internal/game/igdb -count=1
```

Expected: package/client missing.

- [ ] **Step 4: Implement client**

Use:

```go
const (
    defaultTokenURL = "https://id.twitch.tv/oauth2/token"
    defaultGamesURL = "https://api.igdb.com/v4/games"
    defaultPageSize = 500
    minRequestInterval = 250 * time.Millisecond
    maxResponseBytes = 8 << 20
)
```

The Games APICalypse body must request:

```text
fields id,name,slug,game_type.type,first_release_date,parent_game,version_parent,platforms.name,alternative_names.name,cover.image_id,updated_at,checksum;
where id > <lastID>;
sort id asc;
limit <pageSize>;
```

Token acquisition is `POST` to Twitch client credentials, cache the app token until shortly before `expires_in`, retry one Games request after exactly one 401 by invalidating/reacquiring the token, bound response bodies with `http.MaxBytesReader` equivalent (`io.LimitReader` + overflow check for client responses), and pace request starts under a mutex. Stop when a page has fewer than `pageSize` rows.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/game/igdb -race -count=1
git add internal/game/igdb
git commit -m "feat: add IGDB API client"
```

---

### Task 3: Generation schema and provider-index persistence

**Files:**
- Create: `internal/platform/postgres/migrations/00006_igdb_discovery.sql`
- Create: `internal/game/postgres/queries/igdb.sql`
- Create: `internal/game/postgres/igdb_store.go`
- Modify/generated: `internal/game/postgres/sqlcgen/*`
- Modify: `internal/platform/postgres/postgres_integration_test.go`

**Implements:** `game.ProviderIndexRepository`.

- [ ] **Step 1: Add failing integration test**

```go
func TestIGDBGenerationPersistence(t *testing.T) {
    pool := newTestPool(t)
    if err := Migrate(t.Context(), pool, testLogger()); err != nil { t.Fatalf("Migrate() error = %v", err) }
    store := gamepostgres.NewStore(pool)
    id, err := store.BeginGeneration(t.Context())
    if err != nil { t.Fatalf("BeginGeneration() error = %v", err) }
    batch := []game.ProviderGame{{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"}, Name: "Shared Name", Slug: "shared", GameType: "Main Game"}}
    if err := store.StoreProviderGames(t.Context(), id, batch); err != nil { t.Fatalf("StoreProviderGames() error = %v", err) }
    before, err := store.Status(t.Context(), time.Now().UTC())
    if err != nil { t.Fatalf("Status() error = %v", err) }
    if before.State != game.ProviderNotSynced { t.Fatalf("before state = %q", before.State) }
    if err := store.ActivateGeneration(t.Context(), id, 1); err != nil { t.Fatalf("ActivateGeneration() error = %v", err) }
    after, err := store.Status(t.Context(), time.Now().UTC())
    if err != nil { t.Fatalf("Status() error = %v", err) }
    if after.State != game.ProviderCurrent || after.RecordCount != 1 { t.Fatalf("after = %#v", after) }
}
```

Also extend the existing Game catalog integration test to create two local Games named `Shared Name` with slugs `shared-name-a` and `shared-name-b`; both must succeed.

- [ ] **Step 2: Verify failure**

```bash
task test:integration
```

- [ ] **Step 3: Add migration**

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
DROP INDEX games_name_unique_idx;
ALTER TABLE game_external_ids ADD COLUMN archived_at timestamptz NULL;

CREATE TABLE igdb_sync_generations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    status text NOT NULL CHECK (status IN ('syncing','complete','failed')),
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
INSERT INTO igdb_provider_state(singleton) VALUES (true);

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
CREATE INDEX igdb_game_index_search_trgm_idx ON igdb_game_index USING gin(search_text gin_trgm_ops);

-- +goose Down
DROP TABLE igdb_game_index;
DROP TABLE igdb_provider_state;
DROP TABLE igdb_sync_generations;
ALTER TABLE game_external_ids DROP COLUMN archived_at;
-- This can fail after duplicate local names have been created; DOWN is operator-invoked only.
CREATE UNIQUE INDEX games_name_unique_idx ON games(lower(btrim(name)));
```

Do not drop `pg_trgm` in Down; another database object/operator may use the extension.

- [ ] **Step 4: Add sqlc/store operations**

Implement exact repository methods from the stable interface. `ActivateGeneration` must run in one pgx transaction: mark generation complete, point `igdb_provider_state.active_generation_id` to it, set success timestamps/count, archive/restore matching existing IGDB external identities, and delete old non-active generation rows. `FailGeneration` stores a single-line max-500-rune error and never changes `active_generation_id`.

`Status(ctx, now)` derives:

```text
failed  -> most recent completed attempt failed
not synced -> no active generation and latest attempt did not fail
stale -> active generation last_success_at < now-36h
current -> active generation newer than threshold
```

`disabled` and `syncing` are service-level overlays, not DB states.

- [ ] **Step 5: Generate, verify, commit**

```bash
task generate
task test:integration
git add internal/platform/postgres/migrations/00006_igdb_discovery.sql internal/game/postgres internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: persist IGDB index generations"
```

---

### Task 4: Full-sync service, scheduling, and operator state

**Files:**
- Create: `internal/game/igdb_sync.go`
- Create: `internal/game/igdb_sync_test.go`
- Create: `internal/app/igdb.go`
- Create: `internal/app/igdb_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/web/router.go`, `router_test.go`
- Modify: `internal/web/handler/administration.go`
- Modify: `internal/web/pages/admin.templ`
- Regenerate: `internal/web/pages/admin_templ.go`

**Produces:**

```go
func NewIGDBSyncService(catalog ProviderCatalog, repository ProviderIndexRepository, now func() time.Time) *IGDBSyncService
func (s *IGDBSyncService) TryStart(context.Context) bool
func (s *IGDBSyncService) Status(context.Context) (ProviderStatus, error)
```

- [ ] **Step 1: Add concrete service tests**

Use fakes with recorded calls. The core success assertion is:

```go
func TestIGDBSyncActivatesOnlyAfterCatalogCompletes(t *testing.T) {
    catalog := &fakeCatalog{batches: [][]ProviderGame{{{Reference: ProviderReference{Provider:"igdb", ExternalID:"1"}, Name:"One"}}, {{Reference: ProviderReference{Provider:"igdb", ExternalID:"2"}, Name:"Two"}}}}
    repo := &fakeProviderIndexRepository{generationID: "generation-1"}
    service := NewIGDBSyncService(catalog, repo, time.Now)
    if !service.TryStart(t.Context()) { t.Fatal("TryStart() = false") }
    repo.wait(t)
    if repo.activateCount != 1 || repo.failCount != 0 || repo.recordCount != 2 { t.Fatalf("repo = %#v", repo) }
}
```

Add one failure test where the catalog returns `errors.New("upstream failed")` after the first batch and assert `activateCount==0`, `failCount==1`; add one blocking-catalog test that calls `TryStart` twice and expects `true` then `false`.

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/game -run IGDBSync -count=1
```

- [ ] **Step 3: Implement sync/single-flight behavior**

`TryStart` claims a mutex-protected `syncing` flag, launches one goroutine, and releases the flag on exit. The goroutine begins a generation, streams all provider batches into it, activates only after `Games` completes, or calls `FailGeneration` on any catalog/store error. Use a short background timeout for recording a failed attempt after request-context cancellation; do not use an unbounded `context.WithoutCancel` DB write.

`Status(ctx)` loads repository status and overlays `ProviderSyncing` while the service flag is true.

- [ ] **Step 4: Add scheduler test and implementation**

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

Test with a fake counter and a short interval; cancel and assert no calls occur after cancellation.

- [ ] **Step 5: Wire app without touching readiness**

When config is enabled, build `igdb.NewClient`, `game.NewIGDBSyncService`, and start `go runDailyIGDBSync(ctx, syncer, 24*time.Hour)` only after the HTTP server goroutine starts. Keep `Ready: postgres.ReadyCheck(pool)` unchanged. When disabled, use a small `disabledProviderSync` implementation whose `Status` returns `ProviderDisabled` and whose `TryStart` returns false.

- [ ] **Step 6: Add focused admin routes**

```go
type ProviderSyncAdministration interface {
    Status(context.Context) (game.ProviderStatus, error)
    TryStart(context.Context) bool
}
```

Add dependency to `RouterDependencies`/`Administration`, then:

```go
admin.Get("/igdb", administrationHandler.IGDB)
admin.Post("/igdb/sync", administrationHandler.SyncIGDB)
```

The page renders state, last attempt, last success, record count and sanitized error. Hide/disable `Sync now` for `disabled` and `syncing`. Never render credentials/tokens.

- [ ] **Step 7: Verify and commit**

```bash
task generate
go test ./internal/game ./internal/app ./internal/web/... -race -count=1
git add internal/game/igdb_sync.go internal/game/igdb_sync_test.go internal/app internal/web
git commit -m "feat: run and administer IGDB synchronization"
```

---

### Task 5: PostgreSQL-only player discovery

**Files:**
- Create: `internal/game/discovery.go`
- Create: `internal/game/discovery_test.go`
- Extend: `internal/game/postgres/queries/igdb.sql`
- Extend: `internal/game/postgres/igdb_store.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/pages/games.templ`
- Modify: `internal/web/router.go`, `router_test.go`
- Regenerate: `internal/game/postgres/sqlcgen/*`, `internal/web/pages/games_templ.go`

**Produces:**

```go
type DiscoveryService struct { /* private fields: discovery, materializer, subscriptions */ }
func NewDiscoveryService(DiscoveryRepository, MaterializationRepository, SubscriptionRepository) *DiscoveryService
func (s *DiscoveryService) Browse(context.Context, string, string) ([]DiscoveryItem, error)
func (s *DiscoveryService) MyGames(context.Context, string) ([]SubscriptionGame, error)
func (s *DiscoveryService) FollowLocal(context.Context, string, string) error
func (s *DiscoveryService) Unfollow(context.Context, string, string) error
```

- [ ] **Step 1: Add failing pure service tests**

Use a fake discovery repository. Empty search must not call provider search; non-empty search validates max length and filters hidden provider types:

```go
func TestDiscoveryBrowseFiltersHiddenProviderTypes(t *testing.T) {
    repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
        {Reference: ProviderReference{Provider:"igdb", ExternalID:"1"}, Name:"Visible", GameType:"Main Game"},
        {Reference: ProviderReference{Provider:"igdb", ExternalID:"2"}, Name:"Hidden", GameType:"DLC Addon"},
    }}
    service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
    got, err := service.Browse(t.Context(), "user-1", "game")
    if err != nil { t.Fatalf("Browse() error = %v", err) }
    if len(got) != 1 || got[0].Name != "Visible" { t.Fatalf("got = %#v", got) }
}
```

Add an empty-search test asserting `repo.calls==0` and that local `SubscriptionRepository.Browse(ctx,userID,"")` supplies the local results.

- [ ] **Step 2: Add SQL discovery query**

For non-empty search, read only `igdb_provider_state.active_generation_id`, apply `search_text ILIKE '%' || lower(search) || '%'` or trigram-supported equivalent, `LIMIT 50`, and left-join `game_external_ids`/`games`/`game_subscriptions`. If an IGDB row is already materialized, return the local Game identity once; do not emit a second provider card.

For empty search, preserve the existing local catalog query; do not query the provider index.

- [ ] **Step 3: Adapt `/games` to `DiscoveryItem`**

Change handler dependency to:

```go
type GameDiscovery interface {
    Browse(context.Context, string, string) ([]game.DiscoveryItem, error)
    MyGames(context.Context, string) ([]game.SubscriptionGame, error)
    FollowLocal(context.Context, string, string) error
    Unfollow(context.Context, string, string) error
}
```

Local cards keep current behavior. Unmaterialized provider cards show name, type/platform/release disambiguation, `cover.image_id`-derived remote image if present, and provider action controls; do not send provider name/slug as trusted hidden fields.

- [ ] **Step 4: Wire service and verify**

Construct `DiscoveryService` in `app.Run`, initially passing `nil` materializer because Task 6 adds provider mutation actions. Route local follow to `FollowLocal` and existing unfollow unchanged.

```bash
task generate
go test ./internal/game ./internal/web/... -race -count=1
task test:integration
git add internal/game/discovery.go internal/game/discovery_test.go internal/game/postgres internal/web internal/app/app.go
git commit -m "feat: search synchronized IGDB catalog"
```

---

### Task 6: Atomic IGDB materialization

**Files:**
- Create: `internal/game/postgres/materialization.go`
- Extend: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/discovery.go`, `discovery_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Regenerate: `internal/game/postgres/sqlcgen/*`
- Modify: `internal/app/app.go`

**Implements:** `MaterializationRepository.Materialize` and `DiscoveryService.Resolve`.

- [ ] **Step 1: Add concrete integration assertions**

Seed an active generation with IDs 100 and 101 sharing the name `Same Name`.

```go
first, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb", ExternalID:"100"})
if err != nil { t.Fatalf("Resolve first: %v", err) }
again, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb", ExternalID:"100"})
if err != nil { t.Fatalf("Resolve again: %v", err) }
second, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb", ExternalID:"101"})
if err != nil { t.Fatalf("Resolve second: %v", err) }
if first.ID != again.ID { t.Fatalf("same provider ID produced %s and %s", first.ID, again.ID) }
if first.ID == second.ID || first.Slug == second.Slug { t.Fatalf("distinct provider records collapsed: %#v %#v", first, second) }
```

Add a two-goroutine test resolving external ID 100 concurrently; both results must have the same local ID and SQL `count(*)` for that external ID must be 1.

- [ ] **Step 2: Implement transaction**

Within one pgx transaction:

1. lookup `game_external_ids(provider='igdb', external_id=$1)` and return mapped Game if present;
2. load the provider row from the active generation or return `ErrProviderRecordNotFound`;
3. choose provider slug if valid/unused, otherwise deterministic bounded `<base>-igdb-<externalID>`;
4. insert local `games` row;
5. insert valid alternative names as aliases with per-Game conflict ignore;
6. insert `game_external_ids(..., archived_at=NULL)`;
7. commit.

On SQLSTATE `23505` from external identity race, rollback then re-read and return the winning mapping. Never lookup by name.

- [ ] **Step 3: Implement and test `Resolve`**

```go
func (s *DiscoveryService) Resolve(ctx context.Context, ref ProviderReference) (Game, error) {
    normalized, err := NewProviderReference(ref.Provider, ref.ExternalID)
    if err != nil { return Game{}, err }
    if s.materializer == nil { return Game{}, ErrProviderRecordNotFound }
    return s.materializer.Materialize(ctx, normalized)
}
```

Update `app.Run` to pass `gameStore` as materializer.

- [ ] **Step 4: Generate, verify, commit**

```bash
task generate
go test ./internal/game -race -count=1
task test:integration
git add internal/game internal/platform/postgres/postgres_integration_test.go internal/app/app.go
git commit -m "feat: materialize IGDB games atomically"
```

---

### Task 7: Follow provider discovery results

**Files:**
- Modify: `internal/game/discovery.go`, `discovery_test.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/pages/games.templ`
- Modify: `internal/web/router.go`, `router_test.go`
- Regenerate: `internal/web/pages/games_templ.go`

**Produces:** `DiscoveryService.Follow(ctx, userID string, ref ProviderReference) (Game, error)`.

- [ ] **Step 1: Add failing service test**

```go
func TestDiscoveryFollowMaterializesThenSubscribes(t *testing.T) {
    materializer := &fakeMaterializer{game: Game{ID:"local-1", Name:"Game", Slug:"game"}}
    subscriptions := &fakeSubscriptions{}
    service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
    got, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider:"igdb", ExternalID:"100"})
    if err != nil { t.Fatalf("Follow() error = %v", err) }
    if got.ID != "local-1" || subscriptions.followedGameID != "local-1" { t.Fatalf("game=%#v subscriptions=%#v", got, subscriptions) }
}
```

- [ ] **Step 2: Implement service and authenticated route**

```go
func (s *DiscoveryService) Follow(ctx context.Context, userID string, ref ProviderReference) (Game, error) {
    item, err := s.Resolve(ctx, ref)
    if err != nil { return Game{}, err }
    if err := s.subscriptions.Follow(ctx, userID, item.ID); err != nil { return Game{}, err }
    return item, nil
}
```

Add:

```go
games.Post("/providers/{provider}/{externalID}/follow", gamesHandler.FollowProvider)
```

The handler validates URL params, calls `Follow`, then renders the ordinary local `GameFollowControl` using the returned local Game ID. Provider result cards post only provider/external ID.

- [ ] **Step 3: Generate, verify, commit**

```bash
task generate
go test ./internal/game ./internal/web/... -race -count=1
git add internal/game/discovery.go internal/game/discovery_test.go internal/web
git commit -m "feat: follow IGDB discovery results"
```

---

### Task 8: Create Game Requests from provider results

**Files:**
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/games.templ`
- Modify: `internal/web/router.go`
- Regenerate: `internal/web/pages/game_requests_templ.go`, `games_templ.go`

**Consumes:** `DiscoveryService.Resolve`; leaves `gamerequest.Service.Create` unchanged.

- [ ] **Step 1: Add handler fake and tests**

Define:

```go
type GameResolver interface {
    Resolve(context.Context, game.ProviderReference) (game.Game, error)
}
```

Test `GET /game-requests/new?provider=igdb&external_id=100` renders hidden `provider=igdb` and `external_id=100` fields. For POST, use a fake resolver returning `game.Game{ID:"local-1"}` and a fake command recorder; assert command `Create` receives `local-1`.

Also submit invalid `max_players=0` with provider fields and assert resolver call count remains zero: form validation must happen before materialization.

- [ ] **Step 2: Implement form/reference flow**

Extend `pages.GameRequestForm` with `Provider` and `ExternalID`. `New` reads provider query values. `parseCreateForm` parses ordinary request fields first. In `Create`, if `GameID` is empty and provider fields are set, call `NewProviderReference`, then `resolver.Resolve`, put the returned local ID into `form.GameID`, and call existing `GameRequestCommands.Create`.

Provider cards link to:

```text
/game-requests/new?provider=igdb&external_id=<id>
```

Never trust browser-posted provider metadata other than provider/external ID.

- [ ] **Step 3: Generate, verify, commit**

```bash
task generate
go test ./internal/web/... -race -count=1
task test:integration
git add internal/web
git commit -m "feat: create game requests from IGDB results"
```

---

### Task 9: Disappearance/reappearance, restart cleanup, covers, final verification

**Files:**
- Extend: `internal/game/postgres/queries/igdb.sql`
- Extend: `internal/game/postgres/igdb_store.go`
- Extend: `internal/platform/postgres/postgres_integration_test.go`
- Modify templates only if cover rendering remains incomplete.
- Modify `docs/PRODUCT.md` / `docs/ARCHITECTURE.md` only if observable implementation facts need synchronization; do not change approved scope.

- [ ] **Step 1: Add disappearance integration test**

Materialize IGDB ID 100, activate a later complete generation without 100, then assert:

```sql
SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100';
SELECT count(*) FROM games g JOIN game_external_ids e ON e.game_id=g.id WHERE lower(e.provider)='igdb' AND e.external_id='100';
```

Expected values are `true` and `1`. Activate a third generation containing 100 and expect `archived_at IS NULL`.

- [ ] **Step 2: Add abandoned-sync recovery test**

Create a `syncing` generation directly, construct a fresh Store/service (simulating process restart), run a new successful sync, and assert the abandoned generation is marked failed while the newly completed generation becomes active.

- [ ] **Step 3: Implement lifecycle reconciliation**

During activation run:

```sql
UPDATE game_external_ids ge SET archived_at=now()
WHERE lower(btrim(ge.provider))='igdb'
AND NOT EXISTS (
    SELECT 1 FROM igdb_game_index i
    WHERE i.generation_id=sqlc.arg(generation_id)::uuid
      AND i.igdb_id::text=ge.external_id
);

UPDATE game_external_ids ge SET archived_at=NULL
WHERE lower(btrim(ge.provider))='igdb'
AND EXISTS (
    SELECT 1 FROM igdb_game_index i
    WHERE i.generation_id=sqlc.arg(generation_id)::uuid
      AND i.igdb_id::text=ge.external_id
);
```

Before `BeginGeneration`, mark old `syncing` generations failed with `previous synchronization did not complete`; never alter the active generation. Remote cover rendering uses only `cover.image_id` with the already-approved `images.igdb.com` host and existing `no-referrer`/CSP behavior.

- [ ] **Step 4: Run final verification**

```bash
task generate
task lint
task test
task test:integration
task build
git diff main...HEAD --stat
git status --short
```

Expected: all task commands exit 0; no untracked generated files; diff contains only approved IGDB implementation/docs.

- [ ] **Step 5: Commit final hardening**

```bash
git add internal docs
git commit -m "test: harden IGDB synchronization lifecycle"
```

---

## Final Acceptance Checklist

- [ ] Empty credentials => healthy disabled provider; partial credentials => configuration error.
- [ ] Startup/readiness does not depend on IGDB.
- [ ] First baseline is invisible until complete.
- [ ] Failed refresh keeps prior generation active.
- [ ] Automatic/manual sync cannot overlap in one process.
- [ ] Search is PostgreSQL-only; empty search does not dump provider corpus.
- [ ] Hidden and unknown Game Types are synchronized but not player-visible.
- [ ] Materialized provider rows are not duplicated in search.
- [ ] Same-name IGDB records materialize to distinct local Games.
- [ ] Concurrent same-ID materialization yields one local Game.
- [ ] Provider Follow materializes then follows the local Game.
- [ ] Provider Game Request validates normal form first, then materializes, then calls existing Game Request command.
- [ ] Disappearing provider rows archive identity without deleting local domain data; reappearance unarchives.
- [ ] Admin exposes `disabled`, `not synced`, `syncing`, `current`, `stale`, `failed` without secrets.
- [ ] Covers remain remote; no cache/proxy/object storage is introduced.
- [ ] `task generate`, `task lint`, `task test`, `task test:integration`, and `task build` all pass.
