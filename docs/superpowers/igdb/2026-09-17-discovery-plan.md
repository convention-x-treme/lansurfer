# IGDB Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional, failure-tolerant IGDB synchronization, PostgreSQL-backed player discovery, and concurrency-safe materialization into LANsurfer's authoritative Game catalog.

**Architecture:** A concrete IGDB adapter streams the complete provider catalog into generation-scoped PostgreSQL rows. Only a completely synchronized generation becomes active. Player requests read only PostgreSQL; following or creating a Game Request from a provider result resolves it through the `game` module into one authoritative local Game keyed by `provider + external_id`.

**Tech Stack:** Go 1.27.1, PostgreSQL, pgx v5, sqlc 1.31.1, goose 3.28.0, chi, templ, HTMX, standard-library `net/http`, existing Taskfile workflows.

**Spec:** `docs/superpowers/igdb/2026-09-17-discovery-design.md`

## Global Constraints

- The local PostgreSQL `Game` catalog remains authoritative. The IGDB index is discovery infrastructure, never a second domain model.
- Both IGDB environment variables empty means healthy `disabled`; partial configuration is an error.
- Application startup and `/readyz` must not depend on Twitch/IGDB reachability.
- No player-facing request may call IGDB directly.
- Use standard-library HTTP; do not add an IGDB wrapper.
- Keep IGDB request starts at least 250 ms apart and use sequential page requests.
- Synchronize all IGDB Games. Apply visible/hidden Game Type policy only when reading player search results.
- Never expose a partial first baseline. Never replace a successful active generation with a failed refresh.
- Never merge by Game name or slug.
- Concurrent materialization of one IGDB ID must converge on one local Game via `game_external_ids` uniqueness.
- Persist only `cover.image_id`; do not cache/proxy cover bytes.
- No Redis, message broker, generic provider framework, SPA, external scheduler, or additional runtime service.
- Generated templ/sqlc output is produced by `task generate`, never hand-edited.
- Each task is one reviewable commit and follows failing test -> minimal implementation -> passing test.

## Stable Interfaces

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

### Task 1: Configuration and provider contracts

**Files:**
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Modify: `.env.example`
- Create: `internal/game/provider.go`
- Create: `internal/game/provider_test.go`

- [ ] **Step 1: Write failing config tests**

```go
func TestLoadIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "secret")
    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error = %v", err) }
    if cfg.IGDB == nil || cfg.IGDB.ClientID != "client" || cfg.IGDB.ClientSecret != "secret" { t.Fatalf("IGDB = %#v", cfg.IGDB) }
}

func TestLoadIGDBDisabledWhenEmpty(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")
    cfg, err := Load()
    if err != nil { t.Fatalf("Load() error = %v", err) }
    if cfg.IGDB != nil { t.Fatalf("IGDB = %#v", cfg.IGDB) }
}

func TestLoadRejectsPartialIGDBConfiguration(t *testing.T) {
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
    t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")
    if _, err := Load(); err == nil { t.Fatal("partial IGDB configuration accepted") }
}
```

- [ ] **Step 2: Write failing provider tests**

```go
func TestNewProviderReference(t *testing.T) {
    ref, err := NewProviderReference(" IGDB ", " 123 ")
    if err != nil { t.Fatalf("error = %v", err) }
    if ref != (ProviderReference{Provider:"igdb", ExternalID:"123"}) { t.Fatalf("ref = %#v", ref) }
    for _, tc := range [][2]string{{"", "1"}, {"igdb", ""}, {"steam", "1"}, {"igdb", "abc"}} {
        if _, err := NewProviderReference(tc[0], tc[1]); !errors.Is(err, ErrInvalidProviderReference) { t.Fatalf("%q/%q error = %v", tc[0], tc[1], err) }
    }
}

func TestVisibleProviderGameType(t *testing.T) {
    visible := []string{"Main Game", "Standalone Expansion", "Remake", "Remaster", "Port", "Expanded Game"}
    hidden := []string{"DLC Addon", "Bundle", "Mod", "Episode", "Season", "Pack", "Update", "future type", ""}
    for _, value := range visible { if !VisibleProviderGameType(value) { t.Errorf("%q hidden", value) } }
    for _, value := range hidden { if VisibleProviderGameType(value) { t.Errorf("%q visible", value) } }
}
```

- [ ] **Step 3: Verify failure**

```bash
go test ./internal/platform/config ./internal/game -run 'IGDB|Provider|Visible' -count=1
```

- [ ] **Step 4: Implement**

```go
// config.go
type IGDB struct { ClientID, ClientSecret string }

func loadIGDB() (*IGDB, error) {
    id := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_ID"))
    secret := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_SECRET"))
    if id == "" && secret == "" { return nil, nil }
    if id == "" || secret == "" { return nil, errors.New("LANSURFER_IGDB_CLIENT_ID and LANSURFER_IGDB_CLIENT_SECRET must be configured together") }
    return &IGDB{ClientID:id, ClientSecret:secret}, nil
}
```

Add `IGDB *IGDB` to `Config`, load it in `Load()`, and append both variables empty in `.env.example`.

```go
// provider.go
var ErrInvalidProviderReference = errors.New("invalid provider reference")
var ErrProviderRecordNotFound = errors.New("provider record not found")

func NewProviderReference(provider, externalID string) (ProviderReference, error) {
    provider = strings.ToLower(strings.TrimSpace(provider))
    externalID = strings.TrimSpace(externalID)
    if provider != "igdb" || externalID == "" { return ProviderReference{}, ErrInvalidProviderReference }
    value, err := strconv.ParseInt(externalID, 10, 64)
    if err != nil || value <= 0 { return ProviderReference{}, ErrInvalidProviderReference }
    return ProviderReference{Provider:provider, ExternalID:externalID}, nil
}

func VisibleProviderGameType(value string) bool {
    switch strings.ToLower(strings.TrimSpace(value)) {
    case "main game", "standalone expansion", "remake", "remaster", "port", "expanded game": return true
    default: return false
    }
}
```

Add all Stable Interfaces above to `provider.go`.

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

**Produces:** `NewClient(clientID, clientSecret string, options ...Option) *Client` implementing `game.ProviderCatalog`.

- [ ] **Step 1: Write failing paging/auth test**

```go
func TestGamesGetsTokenAndUsesKeysetPaging(t *testing.T) {
    var tokenCalls, gameCalls int
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/oauth2/token":
            tokenCalls++
            _, _ = io.WriteString(w, `{"access_token":"token-1","expires_in":3600,"token_type":"bearer"}`)
        case "/v4/games":
            gameCalls++
            body, _ := io.ReadAll(r.Body)
            if r.Header.Get("Client-ID") != "client" || r.Header.Get("Authorization") != "Bearer token-1" { t.Fatalf("headers = %#v", r.Header) }
            if gameCalls == 1 {
                if !bytes.Contains(body, []byte("where id > 0")) { t.Fatalf("query = %s", body) }
                _, _ = io.WriteString(w, `[{"id":1,"name":"One","slug":"one","game_type":{"type":"Main Game"}},{"id":2,"name":"Two","slug":"two","game_type":{"type":"Remake"}}]`)
            } else {
                if !bytes.Contains(body, []byte("where id > 2")) { t.Fatalf("query = %s", body) }
                _, _ = io.WriteString(w, `[{"id":3,"name":"Three","slug":"three","game_type":{"type":"Port"}}]`)
            }
        default: http.NotFound(w, r)
        }
    }))
    defer server.Close()
    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithPageSize(2), WithRequestInterval(0))
    var ids []string
    err := client.Games(t.Context(), func(batch []game.ProviderGame) error { for _, item := range batch { ids = append(ids, item.Reference.ExternalID) }; return nil })
    if err != nil { t.Fatalf("Games() error = %v", err) }
    if !slices.Equal(ids, []string{"1","2","3"}) { t.Fatalf("ids = %#v", ids) }
    if tokenCalls != 1 || gameCalls != 2 { t.Fatalf("token=%d games=%d", tokenCalls, gameCalls) }
}
```

- [ ] **Step 2: Write failing retry/error tests**

```go
func TestGamesRefreshesTokenOnceAfter401(t *testing.T) {
    var tokenCalls, gameCalls int
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/oauth2/token" { tokenCalls++; _, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3600}`, tokenCalls); return }
        gameCalls++
        if gameCalls == 1 { w.WriteHeader(http.StatusUnauthorized); return }
        _, _ = io.WriteString(w, `[]`)
    }))
    defer server.Close()
    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithRequestInterval(0))
    if err := client.Games(t.Context(), func([]game.ProviderGame) error { return nil }); err != nil { t.Fatalf("error = %v", err) }
    if tokenCalls != 2 || gameCalls != 2 { t.Fatalf("token=%d games=%d", tokenCalls, gameCalls) }
}

func TestGamesRejectsMalformedJSON(t *testing.T) {
    server := tokenAndGamesServer(t, `{`)
    defer server.Close()
    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithRequestInterval(0))
    if err := client.Games(t.Context(), func([]game.ProviderGame) error { return nil }); err == nil { t.Fatal("malformed JSON accepted") }
}

func TestGamesRejectsOversizedResponse(t *testing.T) {
    server := tokenAndGamesServer(t, `[`+strings.Repeat(" ", 64)+`]`)
    defer server.Close()
    client := NewClient("client", "secret", WithTokenURL(server.URL+"/oauth2/token"), WithGamesURL(server.URL+"/v4/games"), WithRequestInterval(0), WithMaxResponseBytes(32))
    if err := client.Games(t.Context(), func([]game.ProviderGame) error { return nil }); err == nil { t.Fatal("oversized response accepted") }
}
```

`tokenAndGamesServer` returns a valid token on `/oauth2/token` and writes the supplied body on `/v4/games`.

- [ ] **Step 3: Verify failure**

```bash
go test ./internal/game/igdb -count=1
```

- [ ] **Step 4: Implement client**

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

The Games body is exactly:

```text
fields id,name,slug,game_type.type,first_release_date,parent_game,version_parent,platforms.name,alternative_names.name,cover.image_id,updated_at,checksum;
where id > <lastID>;
sort id asc;
limit <pageSize>;
```

Acquire Twitch app tokens with client credentials, cache until shortly before expiry, invalidate/reacquire once after a 401, bound token/Games response bodies, map JSON to `game.ProviderGame`, and pace request starts under one mutex. Stop when page length is less than `pageSize`.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/game/igdb -race -count=1
git add internal/game/igdb
git commit -m "feat: add IGDB API client"
```

---

### Task 3: Generation schema and persistence

**Files:**
- Create: `internal/platform/postgres/migrations/00006_igdb_discovery.sql`
- Create: `internal/game/postgres/queries/igdb.sql`
- Create: `internal/game/postgres/igdb_store.go`
- Modify/generated: `internal/game/postgres/sqlcgen/*`
- Modify: `internal/platform/postgres/postgres_integration_test.go`

- [ ] **Step 1: Write failing integration test**

```go
func TestIGDBGenerationPersistence(t *testing.T) {
    pool := newTestPool(t)
    if err := Migrate(t.Context(), pool, testLogger()); err != nil { t.Fatalf("Migrate() error = %v", err) }
    store := gamepostgres.NewStore(pool)
    id, err := store.BeginGeneration(t.Context())
    if err != nil { t.Fatalf("BeginGeneration: %v", err) }
    batch := []game.ProviderGame{{Reference:game.ProviderReference{Provider:"igdb",ExternalID:"100"}, Name:"Shared Name", Slug:"shared", GameType:"Main Game"}}
    if err := store.StoreProviderGames(t.Context(), id, batch); err != nil { t.Fatalf("StoreProviderGames: %v", err) }
    before, _ := store.Status(t.Context(), time.Now().UTC())
    if before.State != game.ProviderNotSynced { t.Fatalf("before = %#v", before) }
    if err := store.ActivateGeneration(t.Context(), id, 1); err != nil { t.Fatalf("ActivateGeneration: %v", err) }
    after, _ := store.Status(t.Context(), time.Now().UTC())
    if after.State != game.ProviderCurrent || after.RecordCount != 1 { t.Fatalf("after = %#v", after) }
}
```

In the existing Game catalog test, create `Shared Name` twice with slugs `shared-a` and `shared-b`; both must succeed.

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
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), started_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz NULL,
  status text NOT NULL CHECK (status IN ('syncing','complete','failed')), record_count bigint NOT NULL DEFAULT 0 CHECK(record_count>=0), error text NULL
);
CREATE TABLE igdb_provider_state (
  singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), active_generation_id uuid NULL REFERENCES igdb_sync_generations(id),
  last_attempt_at timestamptz NULL, last_attempt_succeeded boolean NULL, last_success_at timestamptz NULL, last_error text NULL
);
INSERT INTO igdb_provider_state(singleton) VALUES(true);
CREATE TABLE igdb_game_index (
  generation_id uuid NOT NULL REFERENCES igdb_sync_generations(id) ON DELETE CASCADE, igdb_id bigint NOT NULL CHECK(igdb_id>0),
  name text NOT NULL, slug text NOT NULL, game_type text NULL, first_release_date timestamptz NULL, parent_igdb_id bigint NULL,
  version_parent_igdb_id bigint NULL, platform_names text[] NOT NULL DEFAULT '{}', alternative_names text[] NOT NULL DEFAULT '{}',
  cover_image_id text NULL, igdb_updated_at timestamptz NULL, checksum uuid NULL, search_text text NOT NULL,
  PRIMARY KEY(generation_id,igdb_id)
);
CREATE INDEX igdb_game_index_search_trgm_idx ON igdb_game_index USING gin(search_text gin_trgm_ops);
-- +goose Down
DROP TABLE igdb_game_index;
DROP TABLE igdb_provider_state;
DROP TABLE igdb_sync_generations;
ALTER TABLE game_external_ids DROP COLUMN archived_at;
-- May fail after duplicate names exist; DOWN is explicitly operator-invoked only.
CREATE UNIQUE INDEX games_name_unique_idx ON games(lower(btrim(name)));
```

Do not drop `pg_trgm` in Down.

- [ ] **Step 4: Implement sqlc/store methods**

Implement every `ProviderIndexRepository` method. `ActivateGeneration` is one pgx transaction: mark complete, set active generation and success timestamps, reconcile IGDB external-ID archival, then delete older non-active generations. `FailGeneration` writes one-line max-500-rune error and never changes active generation. `Status(ctx,now)` returns failed if latest attempt failed, otherwise not-synced if no active generation, stale if last success is older than 36h, current otherwise.

- [ ] **Step 5: Generate, verify, commit**

```bash
task generate
task test:integration
git add internal/platform/postgres/migrations/00006_igdb_discovery.sql internal/game/postgres internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: persist IGDB index generations"
```

---

### Task 4: Full sync, daily scheduling, and admin status

**Files:**
- Create: `internal/game/igdb_sync.go`, `internal/game/igdb_sync_test.go`
- Create: `internal/app/igdb.go`, `internal/app/igdb_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/web/router.go`, `internal/web/router_test.go`
- Modify: `internal/web/handler/administration.go`
- Modify: `internal/web/pages/admin.templ`
- Regenerate: `internal/web/pages/admin_templ.go`

**Produces:**

```go
func NewIGDBSyncService(ProviderCatalog, ProviderIndexRepository, func() time.Time) *IGDBSyncService
func (s *IGDBSyncService) TryStart(context.Context) bool
func (s *IGDBSyncService) Status(context.Context) (ProviderStatus, error)
```

- [ ] **Step 1: Write failing sync tests**

```go
func TestIGDBSyncActivatesOnlyAfterCatalogCompletes(t *testing.T) {
    repo := newFakeProviderIndexRepository()
    catalog := fakeCatalog{batches:[][]ProviderGame{{{Reference:ProviderReference{Provider:"igdb",ExternalID:"1"},Name:"One"}},{{Reference:ProviderReference{Provider:"igdb",ExternalID:"2"},Name:"Two"}}}}
    service := NewIGDBSyncService(catalog, repo, time.Now)
    if !service.TryStart(t.Context()) { t.Fatal("TryStart=false") }
    repo.wait(t)
    if repo.activateCount != 1 || repo.failCount != 0 || repo.activatedRecords != 2 { t.Fatalf("repo = %#v", repo) }
}

func TestIGDBSyncFailureDoesNotActivate(t *testing.T) {
    repo := newFakeProviderIndexRepository()
    catalog := fakeCatalog{err:errors.New("upstream failed")}
    service := NewIGDBSyncService(catalog, repo, time.Now)
    if !service.TryStart(t.Context()) { t.Fatal("TryStart=false") }
    repo.wait(t)
    if repo.activateCount != 0 || repo.failCount != 1 { t.Fatalf("repo = %#v", repo) }
}

func TestIGDBSyncSingleFlight(t *testing.T) {
    repo := newFakeProviderIndexRepository()
    block := make(chan struct{})
    service := NewIGDBSyncService(blockingCatalog{release:block}, repo, time.Now)
    if !service.TryStart(t.Context()) { t.Fatal("first TryStart=false") }
    if service.TryStart(t.Context()) { t.Fatal("second TryStart=true") }
    close(block)
    repo.wait(t)
}
```

Fakes implement the stable interfaces and use a `done chan struct{}` to make asynchronous assertions deterministic.

- [ ] **Step 2: Implement single-flight sync**

`TryStart` claims a mutex-protected flag, starts one goroutine, and clears the flag in `defer`. The goroutine begins/stages/activates one generation. On any provider/store error it records failure with a fresh `context.WithTimeout(context.Background(), 5*time.Second)`. `Status` loads repository state then overlays `ProviderSyncing` while the flag is true.

- [ ] **Step 3: Write and implement scheduler test**

```go
func TestRunDailyIGDBSyncStartsImmediatelyAndStops(t *testing.T) {
    ctx, cancel := context.WithCancel(t.Context())
    fake := &countingSyncStarter{calls:make(chan struct{}, 4)}
    done := make(chan struct{})
    go func(){ runDailyIGDBSync(ctx, fake, 5*time.Millisecond); close(done) }()
    <-fake.calls
    cancel()
    <-done
    count := fake.count.Load()
    time.Sleep(15*time.Millisecond)
    if fake.count.Load() != count { t.Fatal("sync continued after cancellation") }
}
```

Implementation:

```go
func runDailyIGDBSync(ctx context.Context, syncer syncStarter, interval time.Duration) {
    syncer.TryStart(ctx)
    ticker := time.NewTicker(interval); defer ticker.Stop()
    for { select { case <-ctx.Done(): return; case <-ticker.C: syncer.TryStart(ctx) } }
}
```

- [ ] **Step 4: Wire app and admin**

Build client/sync service when config is enabled, start scheduler only after the HTTP server goroutine starts, and leave `Ready: postgres.ReadyCheck(pool)` unchanged. When disabled, inject an object whose `Status` returns `ProviderDisabled` and `TryStart` returns false.

Admin contract:

```go
type ProviderSyncAdministration interface {
    Status(context.Context) (game.ProviderStatus, error)
    TryStart(context.Context) bool
}
```

Routes:

```go
admin.Get("/igdb", administrationHandler.IGDB)
admin.Post("/igdb/sync", administrationHandler.SyncIGDB)
```

Page fields: state, last attempt, last success, record count, sanitized error; hide sync action when disabled or syncing; never render secret/token.

- [ ] **Step 5: Generate, verify, commit**

```bash
task generate
go test ./internal/game ./internal/app ./internal/web/... -race -count=1
git add internal/game/igdb_sync.go internal/game/igdb_sync_test.go internal/app internal/web
git commit -m "feat: run and administer IGDB synchronization"
```

---

### Task 5: PostgreSQL-only discovery

**Files:**
- Create: `internal/game/discovery.go`, `internal/game/discovery_test.go`
- Extend: `internal/game/postgres/queries/igdb.sql`, `internal/game/postgres/igdb_store.go`
- Modify: `internal/web/handler/games.go`, `internal/web/pages/games.templ`, `internal/web/router.go`, `internal/web/router_test.go`
- Modify: `internal/app/app.go`
- Regenerate: `internal/game/postgres/sqlcgen/*`, `internal/web/pages/games_templ.go`

**Produces:**

```go
type DiscoveryService struct {
    discovery     DiscoveryRepository
    materializer  MaterializationRepository
    subscriptions SubscriptionRepository
}
func NewDiscoveryService(DiscoveryRepository, MaterializationRepository, SubscriptionRepository) *DiscoveryService
func (s *DiscoveryService) Browse(context.Context,string,string) ([]DiscoveryItem,error)
func (s *DiscoveryService) MyGames(context.Context,string) ([]SubscriptionGame,error)
func (s *DiscoveryService) FollowLocal(context.Context,string,string) error
func (s *DiscoveryService) Unfollow(context.Context,string,string) error
```

- [ ] **Step 1: Write failing discovery tests**

```go
func TestDiscoveryBrowseFiltersHiddenTypes(t *testing.T) {
    repo := &fakeDiscoveryRepository{items:[]DiscoveryItem{
        {Reference:ProviderReference{Provider:"igdb",ExternalID:"1"},Name:"Visible",GameType:"Main Game"},
        {Reference:ProviderReference{Provider:"igdb",ExternalID:"2"},Name:"Hidden",GameType:"DLC Addon"},
    }}
    service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
    got, err := service.Browse(t.Context(), "user-1", "game")
    if err != nil { t.Fatalf("Browse: %v", err) }
    if len(got)!=1 || got[0].Name!="Visible" { t.Fatalf("got=%#v", got) }
}

func TestDiscoveryBrowseEmptySearchUsesLocalCatalogOnly(t *testing.T) {
    repo := &fakeDiscoveryRepository{}
    subs := &fakeSubscriptions{browse:[]SubscriptionGame{{ID:"local-1",Name:"Local",Slug:"local"}}}
    service := NewDiscoveryService(repo, nil, subs)
    got, err := service.Browse(t.Context(), "user-1", "")
    if err != nil { t.Fatalf("Browse: %v", err) }
    if repo.calls != 0 || len(got)!=1 || got[0].LocalGameID!="local-1" { t.Fatalf("repo.calls=%d got=%#v", repo.calls, got) }
}
```

- [ ] **Step 2: Implement SQL search**

Non-empty search reads only the active generation, matches normalized `search_text`, limits provider rows to 50, and left-joins `game_external_ids`, `games`, and current-user subscription state. Materialized provider rows return authoritative local ID/name/slug and are emitted once. Empty search stays on existing local browse only.

- [ ] **Step 3: Adapt handler/template**

Handler contract:

```go
type GameDiscovery interface {
    Browse(context.Context,string,string) ([]game.DiscoveryItem,error)
    MyGames(context.Context,string) ([]game.SubscriptionGame,error)
    FollowLocal(context.Context,string,string) error
    Unfollow(context.Context,string,string) error
}
```

Local cards preserve current controls. Provider cards render name plus type/platform/release disambiguation and a remote cover derived only from `CoverImageID`; no provider name/slug is posted as trusted data. Empty search never renders unmaterialized provider rows.

- [ ] **Step 4: Wire, generate, verify, commit**

Construct `DiscoveryService(gameStore, nil, gameStore)` for now; Task 6 replaces nil with the same store after materialization is implemented.

```bash
task generate
go test ./internal/game ./internal/web/... -race -count=1
task test:integration
git add internal/game/discovery.go internal/game/discovery_test.go internal/game/postgres internal/web internal/app/app.go
git commit -m "feat: search synchronized IGDB catalog"
```

---

### Task 6: Atomic materialization

**Files:**
- Create: `internal/game/postgres/materialization.go`
- Extend: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/discovery.go`, `internal/game/discovery_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Modify: `internal/app/app.go`
- Regenerate: `internal/game/postgres/sqlcgen/*`

- [ ] **Step 1: Write failing integration tests**

```go
first, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb",ExternalID:"100"})
if err != nil { t.Fatalf("Resolve first: %v", err) }
again, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb",ExternalID:"100"})
if err != nil { t.Fatalf("Resolve again: %v", err) }
second, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider:"igdb",ExternalID:"101"})
if err != nil { t.Fatalf("Resolve second: %v", err) }
if first.ID!=again.ID { t.Fatalf("same ID produced %s/%s", first.ID, again.ID) }
if first.ID==second.ID || first.Slug==second.Slug { t.Fatalf("distinct provider games collapsed: %#v %#v", first, second) }
```

Seed 100 and 101 with the same provider display name. Add a goroutine test using `sync.WaitGroup` with two result slots; both concurrent `Resolve(...100)` results must have the same local ID, and SQL count for provider/external ID 100 must equal 1.

- [ ] **Step 2: Implement transaction**

One pgx transaction performs: existing mapping lookup; active provider-row lookup; deterministic unique slug selection; local Game insert; valid alternative-name alias inserts; `game_external_ids(provider='igdb', external_id=...)` insert; commit. On external-ID SQLSTATE 23505, rollback and re-read the winner. Never match by name.

Slug order: valid unused provider slug first; otherwise bounded `<base>-igdb-<externalID>` within 120 chars.

- [ ] **Step 3: Implement Resolve and wire materializer**

```go
func (s *DiscoveryService) Resolve(ctx context.Context, ref ProviderReference) (Game,error) {
    normalized, err := NewProviderReference(ref.Provider, ref.ExternalID)
    if err != nil { return Game{}, err }
    if s.materializer == nil { return Game{}, ErrProviderRecordNotFound }
    return s.materializer.Materialize(ctx, normalized)
}
```

Change app construction to `NewDiscoveryService(gameStore, gameStore, gameStore)`.

- [ ] **Step 4: Generate, verify, commit**

```bash
task generate
go test ./internal/game -race -count=1
task test:integration
git add internal/game internal/platform/postgres/postgres_integration_test.go internal/app/app.go
git commit -m "feat: materialize IGDB games atomically"
```

---

### Task 7: Follow provider result

**Files:**
- Modify: `internal/game/discovery.go`, `internal/game/discovery_test.go`
- Modify: `internal/web/handler/games.go`, `internal/web/pages/games.templ`, `internal/web/router.go`, `internal/web/router_test.go`
- Regenerate: `internal/web/pages/games_templ.go`

- [ ] **Step 1: Write failing service test**

```go
func TestDiscoveryFollowMaterializesThenSubscribes(t *testing.T) {
    materializer := &fakeMaterializer{game:Game{ID:"local-1",Name:"Game",Slug:"game"}}
    subscriptions := &fakeSubscriptions{}
    service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
    got, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider:"igdb",ExternalID:"100"})
    if err != nil { t.Fatalf("Follow: %v", err) }
    if got.ID!="local-1" || subscriptions.followedGameID!="local-1" { t.Fatalf("got=%#v subs=%#v", got, subscriptions) }
}
```

- [ ] **Step 2: Implement service/route**

```go
func (s *DiscoveryService) Follow(ctx context.Context, userID string, ref ProviderReference) (Game,error) {
    item, err := s.Resolve(ctx, ref); if err != nil { return Game{}, err }
    if err := s.subscriptions.Follow(ctx, userID, item.ID); err != nil { return Game{}, err }
    return item, nil
}
```

Route:

```go
games.Post("/providers/{provider}/{externalID}/follow", gamesHandler.FollowProvider)
```

The handler validates URL params and renders the ordinary local follow control with the returned local Game ID.

- [ ] **Step 3: Generate, verify, commit**

```bash
task generate
go test ./internal/game ./internal/web/... -race -count=1
git add internal/game/discovery.go internal/game/discovery_test.go internal/web
git commit -m "feat: follow IGDB discovery results"
```

---

### Task 8: Create Game Request from provider result

**Files:**
- Modify: `internal/web/handler/game_requests.go`, `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`, `internal/web/pages/games.templ`, `internal/web/router.go`
- Regenerate: `internal/web/pages/game_requests_templ.go`, `internal/web/pages/games_templ.go`

- [ ] **Step 1: Add resolver contract and failing tests**

```go
type GameResolver interface { Resolve(context.Context, game.ProviderReference) (game.Game,error) }
```

Add a fake resolver with `calls int` and `game game.Game`. In the existing Game Request handler test fixture:

```go
resolver := &fakeGameResolver{game:game.Game{ID:"local-1",Name:"Provider Game",Slug:"provider-game"}}
handler := NewGameRequests(commands, queries, resolver, brand)
```

Test a valid POST carrying `provider=igdb&external_id=100` and normal request fields; assert `resolver.calls==1` and the command recorder received `gameID=="local-1"`. Submit the same provider fields with `max_players=0`; assert response is validation error and `resolver.calls==0`.

- [ ] **Step 2: Implement form flow**

Extend `pages.GameRequestForm` with `Provider` and `ExternalID`. `New` copies query values. The template emits hidden provider fields when both are present. `Create` validates max players/time/description first; only if normal validation succeeds and `GameID` is empty does it validate provider reference, call resolver, replace `form.GameID`, then call existing `GameRequestCommands.Create` unchanged.

Provider card link:

```text
/game-requests/new?provider=igdb&external_id=<externalID>
```

- [ ] **Step 3: Generate, verify, commit**

```bash
task generate
go test ./internal/web/... -race -count=1
task test:integration
git add internal/web
git commit -m "feat: create game requests from IGDB results"
```

---

### Task 9: Lifecycle hardening and final verification

**Files:**
- Extend: `internal/game/postgres/queries/igdb.sql`, `internal/game/postgres/igdb_store.go`
- Extend: `internal/platform/postgres/postgres_integration_test.go`
- Modify docs only if observable implementation differs from an existing factual sentence; do not change scope.

- [ ] **Step 1: Add disappearance/reappearance test**

Materialize 100, activate a complete generation without 100, then query:

```sql
SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100';
SELECT count(*) FROM games g JOIN game_external_ids e ON e.game_id=g.id WHERE lower(e.provider)='igdb' AND e.external_id='100';
```

Assert `true` and `1`. Activate another complete generation containing 100 and assert `archived_at IS NULL`.

- [ ] **Step 2: Add abandoned-sync recovery test**

Insert a generation with `status='syncing'`, construct a fresh Store/service, run a successful sync, then assert the abandoned row is `failed`, the new row is `complete`, and provider state points to the new row.

- [ ] **Step 3: Implement reconciliation**

```sql
UPDATE game_external_ids ge SET archived_at=now()
WHERE lower(btrim(ge.provider))='igdb'
AND NOT EXISTS (SELECT 1 FROM igdb_game_index i WHERE i.generation_id=sqlc.arg(generation_id)::uuid AND i.igdb_id::text=ge.external_id);

UPDATE game_external_ids ge SET archived_at=NULL
WHERE lower(btrim(ge.provider))='igdb'
AND EXISTS (SELECT 1 FROM igdb_game_index i WHERE i.generation_id=sqlc.arg(generation_id)::uuid AND i.igdb_id::text=ge.external_id);
```

Before beginning a new generation, mark abandoned `syncing` rows failed with `previous synchronization did not complete`; never modify the active generation. Covers continue to use only `cover.image_id` and the approved `images.igdb.com` host with existing CSP/no-referrer behavior.

- [ ] **Step 4: Run complete verification**

```bash
task generate
task lint
task test
task test:integration
task build
git diff main...HEAD --stat
git status --short
```

Expected: all task commands exit 0; no untracked generated files; only approved IGDB changes.

- [ ] **Step 5: Commit**

```bash
git add internal docs
git commit -m "test: harden IGDB synchronization lifecycle"
```

---

## Final Acceptance Checklist

- [ ] Empty credentials => healthy disabled provider; partial credentials => config error.
- [ ] Startup/readiness does not depend on IGDB.
- [ ] First baseline invisible until complete; failed refresh preserves prior active generation.
- [ ] Automatic/manual sync cannot overlap in one process.
- [ ] Search is PostgreSQL-only; empty search does not dump provider corpus.
- [ ] Hidden/unknown Game Types remain synchronized but not player-visible.
- [ ] Materialized provider rows are not duplicated in search.
- [ ] Same-name provider records materialize to distinct local Games.
- [ ] Concurrent same-ID materialization yields one local Game.
- [ ] Provider Follow materializes then subscribes.
- [ ] Provider Game Request validates form before materialization and uses existing command afterward.
- [ ] Disappearance archives identity without deleting local data; reappearance clears archival.
- [ ] Admin exposes disabled/not-synced/syncing/current/stale/failed without secrets.
- [ ] No cover cache/proxy/object storage.
- [ ] `task generate`, `task lint`, `task test`, `task test:integration`, `task build` all pass.
