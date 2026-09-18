# IGDB Discovery UX Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make IGDB-backed Game discovery feel like one coherent LANsurfer catalog by keeping materialization invisible, retaining last-known-good IGDB enrichment on local Games, and using the same discovery flow in `/games` and the Game Request composer.

**Architecture:** Materialized Games remain authoritative local domain objects. A new IGDB-specific enrichment snapshot stores only presentation metadata for linked local Games and is refreshed from successful IGDB generations without overwriting local name/slug/aliases. Player-facing discovery returns one unified projection for local and unmaterialized provider Games, with PostgreSQL-only ranked search, provider availability state, and bounded result signaling; the same read model powers Games, My Games, and Game Request selection.

**Tech Stack:** Go 1.27.1, PostgreSQL, pgx v5, sqlc 1.31.1, goose 3.28.0, chi, templ, HTMX, standard-library `net/http`, existing Taskfile workflows.

**Spec:** `docs/superpowers/specs/2026-09-18-igdb-discovery-ux-refinement-design.md`

## Global Constraints

- The local PostgreSQL Game catalog remains authoritative domain state.
- IGDB remains optional discovery/enrichment infrastructure.
- No player-facing request may call Twitch or IGDB directly.
- Materialization identity remains provider + external ID; never merge by name, slug, alias, or fuzzy similarity.
- Local Game name, slug, active state, and local aliases are never rewritten by later IGDB syncs.
- Persist only IGDB presentation enrichment: cover image ID, Game Type, platform names, first release date, and provider updated time.
- Enrichment uses last-known-good semantics: later empty/null provider values do not erase a stored non-empty value.
- A disappeared, disabled, stale, or temporarily failing provider never deletes local Games, follows, requests, activity, or stored enrichment.
- Search starts at 2 trimmed characters, uses about a 300 ms debounce in the browser, and renders at most 50 results.
- Search ranking is exact match, then prefix, then contains; within one relevance class, followed Games sort before other materialized Games, then alphabetical name, then stable identity.
- `Find Players` and `Follow` are independent actions; creating a Game Request never auto-follows a Game.
- Search, GET selection, and composer selection are read-only; provider materialization happens only during Follow or the final validated Game Request POST.
- Player UI must not expose `materialized`, provider-record, sync-generation, `Local`, or `IGDB` origin status.
- No Game detail page, provider browser pagination/infinite scroll, player free-form Game creation, existing-local-to-IGDB linking workflow, generic provider metadata framework, or Trending covers.
- Generated templ/sqlc output is produced by `task generate`; never hand-edit generated files.

---

## File Structure

The refinement should keep responsibilities separated as follows:

- `internal/platform/postgres/migrations/00008_igdb_enrichment.sql` — durable last-known-good IGDB enrichment attached to materialized local Games.
- `internal/game/presentation.go` — reusable player-facing Game presentation and selection/result types.
- `internal/game/provider.go` — provider status contract only; add catalog-availability state without moving presentation types back here.
- `internal/game/discovery.go` — orchestration: local/start state, 2-character threshold, provider enablement, search result state, read-only selection, Follow/Resolve.
- `internal/game/postgres/queries/games.sql` — local Browse/My Games projections with enrichment.
- `internal/game/postgres/queries/igdb.sql` — enrichment upsert/refresh, ranked unified discovery search, and read-only selection lookups.
- `internal/game/postgres/igdb_store.go` — provider-status mapping and repository mapping for search/selection.
- `internal/game/postgres/materialization.go` — persist enrichment in the same materialization transaction.
- `internal/query/gamerequest/query.go` and `queries/game_requests.sql` — Game Request list/detail projection with local Game enrichment; Trending remains text-only.
- `internal/web/pages/game_presentation.templ` — shared cover/abbreviation + metadata rendering used by Games and Game Requests.
- `internal/web/pages/games.templ` — discovery-first Games page and unified Game cards.
- `internal/web/handler/games.go` — adapt HTTP rendering to `DiscoveryPage`.
- `internal/web/pages/game_requests.templ` — discovery-first composer, selected Game card, list/detail cover presentation.
- `internal/web/handler/game_requests.go` — shared discovery dependency, read-only selection endpoints, preserved form state, final materialization boundary.
- `internal/web/router.go` and `internal/app/app.go` — wire the same discovery service/status source into Games and Game Requests.
- Tests remain beside the existing units; PostgreSQL behavior stays in `internal/platform/postgres/postgres_integration_test.go`.

---

### Task 1: Persist last-known-good IGDB enrichment

**Files:**
- Create: `internal/platform/postgres/migrations/00008_igdb_enrichment.sql`
- Modify: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/postgres/materialization.go`
- Modify: `internal/game/postgres/igdb_store.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Generate: `internal/game/postgres/sqlcgen/*.go`

**Interfaces:**
- Consumes: existing `game_external_ids`, active IGDB generation, `Store.Materialize`, `Store.ActivateGeneration`.
- Produces: durable table `game_igdb_enrichment`; sqlc queries `UpsertIGDBEnrichmentForExternalID` and `RefreshIGDBEnrichmentFromGeneration`.

- [ ] **Step 1: Add failing integration coverage for materialization and last-known-good refresh**

Add focused tests to `postgres_integration_test.go`:

```go
func TestIGDBMaterializationPersistsEnrichment(t *testing.T) {
    // Arrange one active provider row with cover/type/platform/release/provider_updated_at.
    // Materialize provider ID 100.
    // Query game_igdb_enrichment by the returned local game ID.
    // Assert every enrichment field matches the active provider row.
}

func TestIGDBRefreshUpdatesOnlyNonEmptyEnrichment(t *testing.T) {
    // Generation 1: materialize with cover "co-old", type "main game",
    // platforms {"PC"}, release date, provider_updated_at.
    // Generation 2: same ID with cover "co-new", empty type,
    // empty platforms, nil release, newer provider_updated_at.
    // Activate generation 2.
    // Assert cover/provider_updated_at changed, while type/platform/release
    // kept the previous non-empty values.
}

func TestIGDBDisappearanceKeepsEnrichmentAndReappearanceRefreshesIt(t *testing.T) {
    // Materialize ID 100 with cover "co-old".
    // Activate a generation without ID 100 and assert the external mapping
    // is archived but game_igdb_enrichment still contains "co-old".
    // Activate a later generation containing ID 100 with "co-returned".
    // Assert mapping is restored and enrichment becomes "co-returned".
}
```

Use direct SQL in the test only for asserting the new storage row; production code must go through sqlc.

- [ ] **Step 2: Run the focused integration tests and confirm failure**

Run:

```bash
task test:integration
```

Expected: the new tests fail because `game_igdb_enrichment` and its update path do not exist.

- [ ] **Step 3: Create migration 00008**

Create:

```sql
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
```

Do not add provider-specific columns to `games`.

- [ ] **Step 4: Add sqlc upsert/refresh queries**

Add `UpsertIGDBEnrichmentForExternalID` to copy the active-generation provider row for one already-linked local Game:

```sql
-- name: UpsertIGDBEnrichmentForExternalID :exec
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
```

Add `RefreshIGDBEnrichmentFromGeneration` using the same `INSERT ... ON CONFLICT` semantics, but sourcing every IGDB mapping present in `sqlc.arg(generation_id)`.

- [ ] **Step 5: Wire enrichment into materialization and generation activation**

In `materializeIGDBGame`:

1. after an existing mapping is found and restored, call `UpsertIGDBEnrichmentForExternalID` before commit;
2. for a newly-created mapping, call the same query after `CreateIGDBExternalID` and before commit.

In `ActivateGeneration`, after `RestorePresentIGDBExternalIDs` and before deleting retired generations, call:

```go
if err := queries.RefreshIGDBEnrichmentFromGeneration(ctx, databaseGenerationID); err != nil {
    return fmt.Errorf("refresh IGDB enrichment: %w", err)
}
```

This keeps snapshot refresh atomic with generation activation.

- [ ] **Step 6: Generate and run tests**

Run:

```bash
task generate
task test:integration
go test -race ./internal/game/...
```

Expected: all existing IGDB materialization/archival tests plus the three new enrichment tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/postgres/migrations/00008_igdb_enrichment.sql         internal/game/postgres/queries/igdb.sql         internal/game/postgres/materialization.go         internal/game/postgres/igdb_store.go         internal/game/postgres/sqlcgen         internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: persist IGDB game enrichment"
```

---

### Task 2: Introduce one reusable player-facing Game presentation

**Files:**
- Create: `internal/game/presentation.go`
- Modify: `internal/game/game.go`
- Modify: `internal/game/provider.go`
- Modify: `internal/game/postgres/queries/games.sql`
- Modify: `internal/game/postgres/store.go`
- Modify: `internal/game/discovery.go`
- Modify: `internal/game/discovery_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Generate: `internal/game/postgres/sqlcgen/*.go`

**Interfaces:**
- Consumes: `game_igdb_enrichment` from Task 1.
- Produces:

```go
type GamePresentation struct {
    Name             string
    Slug             string
    GameType         string
    PlatformNames    []string
    FirstReleaseDate *time.Time
    CoverImageID     string
}

type SubscriptionGame struct {
    ID string
    GamePresentation
    Subscribed bool
}

type DiscoveryItem struct {
    LocalGameID string
    Reference   ProviderReference
    GamePresentation
    Subscribed   bool
    Materialized bool
}
```

- [ ] **Step 1: Write failing unit tests for enriched local mapping**

Extend `discovery_test.go`:

```go
func TestDiscoveryBrowseEmptySearchKeepsLocalEnrichment(t *testing.T) {
    release := time.Date(2002, 7, 3, 0, 0, 0, 0, time.UTC)
    subs := &fakeSubscriptions{browse: []SubscriptionGame{{
        ID: "local-1",
        GamePresentation: GamePresentation{
            Name: "Warcraft III", Slug: "warcraft-iii",
            CoverImageID: "co1abc", GameType: "Main Game",
            PlatformNames: []string{"PC (Microsoft Windows)"},
            FirstReleaseDate: &release,
        },
    }}}
    service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs)

    got, err := service.Browse(t.Context(), "user-1", "")
    if err != nil { t.Fatal(err) }
    if len(got) != 1 {
        t.Fatalf("items = %#v", got)
    }
    if got[0].CoverImageID != "co1abc" || got[0].GameType != "Main Game" {
        t.Fatalf("item = %#v", got[0])
    }
}
```

Task 2 intentionally keeps the existing `Browse(...) ([]DiscoveryItem, error)` contract. Task 3 is the commit that changes it to `DiscoveryPage`.

- [ ] **Step 2: Add failing integration coverage for Browse/My Games enrichment**

Add a test that:

1. creates/materializes an IGDB Game with a cover;
2. follows it;
3. calls the store's local Browse and My Games paths;
4. asserts both return cover/type/platform/release from `game_igdb_enrichment`;
5. disables/deactivates no provider data in the test, proving the local read itself is snapshot-backed.

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test -race ./internal/game -run 'Discovery.*Enrichment|Local.*Enrichment' -count=1
task test:integration
```

Expected: new enrichment assertions fail because `SubscriptionGame` and local queries do not expose snapshot data.

- [ ] **Step 4: Add GamePresentation and embed it in player projections**

Create `internal/game/presentation.go` with the exact `GamePresentation` type above.

Move presentation-only fields out of the inline `DiscoveryItem` declaration in `provider.go` by embedding `GamePresentation`.

Update `SubscriptionGame` in `game.go` to embed the same presentation type.

Do not add provider identity to `GamePresentation`; identity remains on the outer discovery/local types.

- [ ] **Step 5: Enrich local Browse/My Games SQL**

Change both `BrowseSubscriptionGames` and `ListSubscribedGames` to:

```sql
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
```

and select:

```sql
enrichment.cover_image_id,
enrichment.game_type,
enrichment.platform_names,
enrichment.first_release_date
```

Keep local `g.name` and `g.slug` as the source of truth.

Order empty local Browse by:

```sql
ORDER BY subscribed DESC, lower(g.name), g.id
```

For non-empty local-only Browse, keep the existing textual match for now; Task 3 replaces search ranking for all searches.

- [ ] **Step 6: Map sqlc rows into GamePresentation**

Add one focused mapper in `store.go`, for example:

```go
func textValue(value pgtype.Text) string {
    if !value.Valid {
        return ""
    }
    return value.String
}

func presentation(name, slug string, gameType, cover pgtype.Text, platforms []string, release pgtype.Timestamptz) game.GamePresentation {
    return game.GamePresentation{
        Name: name,
        Slug: slug,
        GameType: textValue(gameType),
        PlatformNames: nonNilStrings(platforms),
        FirstReleaseDate: timestamptzPointer(release),
        CoverImageID: textValue(cover),
    }
}
```

Use it for both Browse and My Games so the two paths cannot drift.

Update `discoveryItemFromLocal` to copy the embedded presentation.

- [ ] **Step 7: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/game/... -count=1
task test:integration
```

Expected: local Games retain enrichment in Browse and My Games.

- [ ] **Step 8: Commit**

```bash
git add internal/game/presentation.go         internal/game/game.go         internal/game/provider.go         internal/game/postgres/queries/games.sql         internal/game/postgres/store.go         internal/game/discovery.go         internal/game/discovery_test.go         internal/game/postgres/sqlcgen         internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: expose enriched local game presentation"
```

---

### Task 3: Make discovery stateful, ranked, bounded, and provider-aware

**Files:**
- Modify: `internal/game/presentation.go`
- Modify: `internal/game/provider.go`
- Modify: `internal/game/discovery.go`
- Modify: `internal/game/discovery_test.go`
- Modify: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/postgres/igdb_store.go`
- Modify: `internal/game/igdb_sync_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Generate: `internal/game/postgres/sqlcgen/*.go`

**Interfaces:**
- Consumes: enriched local projections from Task 2 and existing `IGDBSyncService.Status`.
- Produces:

```go
type DiscoverySearchResult struct {
    Items   []DiscoveryItem
    HasMore bool
}

type DiscoveryPage struct {
    Items          []DiscoveryItem
    HasMore        bool
    SearchTooShort bool
    ProviderStatus ProviderStatus
}

type ProviderStatusSource interface {
    Status(context.Context) (ProviderStatus, error)
}

type DiscoveryRepository interface {
    Search(context.Context, string, string, int, bool) (DiscoverySearchResult, error)
}

func NewDiscoveryService(
    discovery DiscoveryRepository,
    materializer MaterializationRepository,
    subscriptions SubscriptionRepository,
    providerStatus ProviderStatusSource,
) *DiscoveryService

func (s *DiscoveryService) Browse(context.Context, string, string) (DiscoveryPage, error)
```

Extend:

```go
type ProviderStatus struct {
    State            ProviderState
    LastAttempt      *time.Time
    LastSuccess      *time.Time
    RecordCount      int64
    LastError        string
    CatalogAvailable bool
}
```

- [ ] **Step 1: Write failing service tests**

Add this status fake beside the existing discovery fakes:

```go
type fakeProviderStatus struct {
    status ProviderStatus
    err    error
    calls  int
}

func (f *fakeProviderStatus) Status(context.Context) (ProviderStatus, error) {
    f.calls++
    return f.status, f.err
}
```

Extend `fakeDiscoveryRepository` with `lastIncludeProvider bool` and update its Search method to the Task 3 signature.

Replace the old empty/local and bounded-search tests with explicit page-state tests:

```go
func TestDiscoveryBrowseEmptySearchReturnsLANListWithoutProviderSearch(t *testing.T) {
    repo := &fakeDiscoveryRepository{}
    status := &fakeProviderStatus{status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true}}
    subs := &fakeSubscriptions{browse: []SubscriptionGame{{ID: "local-1", GamePresentation: GamePresentation{Name: "Local"}}}}
    service := NewDiscoveryService(repo, nil, subs, status)

    page, err := service.Browse(t.Context(), "user-1", "")
    if err != nil { t.Fatal(err) }
    if repo.calls != 0 || len(page.Items) != 1 || page.SearchTooShort {
        t.Fatalf("page=%#v repo.calls=%d", page, repo.calls)
    }
}

func TestDiscoveryBrowseOneCharacterDoesNotSearchProvider(t *testing.T) {
    repo := &fakeDiscoveryRepository{}
    service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
        status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true},
    })

    page, err := service.Browse(t.Context(), "user-1", " c ")
    if err != nil { t.Fatal(err) }
    if repo.calls != 0 || !page.SearchTooShort {
        t.Fatalf("page=%#v calls=%d", page, repo.calls)
    }
}

func TestDiscoveryBrowsePassesProviderEnabledAndFiftyResultLimit(t *testing.T) {
    repo := &fakeDiscoveryRepository{}
    service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
        status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true},
    })

    _, err := service.Browse(t.Context(), "user-1", "wc")
    if err != nil { t.Fatal(err) }
    if repo.lastLimit != 50 || !repo.lastIncludeProvider {
        t.Fatalf("limit=%d includeProvider=%v", repo.lastLimit, repo.lastIncludeProvider)
    }
}

func TestDiscoveryBrowseDisabledProviderStillUsesUnifiedLocalSearch(t *testing.T) {
    repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
        {LocalGameID: "local-1", GamePresentation: GamePresentation{Name: "Local"}, Materialized: true},
    }}
    service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
        status: ProviderStatus{State: ProviderDisabled},
    })

    page, err := service.Browse(t.Context(), "user-1", "wc")
    if err != nil { t.Fatal(err) }
    if repo.calls != 1 || repo.lastIncludeProvider {
        t.Fatalf("calls=%d includeProvider=%v, want 1/false: a disabled provider still searches through the unified query, gated to local-sourced rows only, never through provider rows", repo.calls, repo.lastIncludeProvider)
    }
    if len(page.Items) != 1 || !page.Items[0].Materialized {
        t.Fatalf("page=%#v, want only the local-sourced item the fake returned", page)
    }
}
```

Also add a test that `DiscoveryPage.ProviderStatus` is returned unchanged for player copy decisions.

Add a `Resolve` counterpart proving the disabled-provider rule Step 6 describes actually has coverage, not only Browse:

```go
func TestDiscoveryResolveRejectsWhenProviderDisabled(t *testing.T) {
    materializer := &fakeMaterializer{result: Game{ID: "local-1"}}
    service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{}, &fakeProviderStatus{
        status: ProviderStatus{State: ProviderDisabled},
    })

    if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
        t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
    }
    if materializer.calls != 0 {
        t.Fatalf("materializer.calls = %d, want 0", materializer.calls)
    }
}
```

Step 6 removes the mutable `DisableProvider` method this task's own predecessor commit (c2cea97) added, together with its two tests
(`TestDiscoveryBrowseWithProviderDisabledUsesLocalSearchOnlyEvenForNonEmptySearch`,
`TestDiscoveryResolveWithProviderDisabledReportsNoProviderRecordWithoutMaterializing`)
in `discovery_test.go` — both call `service.DisableProvider()`, which no longer
exists once Step 6 lands, so the package will not compile until they are deleted.
Delete both; the two tests above (plus the Browse ones already listed in this
Step) are their status-source-driven replacements, covering the same scenarios.

- [ ] **Step 2: Add failing provider-status tests**

Update store/sync tests so:

```go
if !status.CatalogAvailable {
    t.Fatal("active generation must mark catalog available")
}
```

and a no-active-generation status returns `CatalogAvailable == false`.

- [ ] **Step 3: Add failing PostgreSQL ranking/bounding tests**

Expand `TestDiscoverySearchPostgreSQL` or add focused tests with records that prove:

1. exact name/alias outranks prefix;
2. prefix outranks contains;
3. within one rank, followed local outranks unfollowed materialized;
4. materialized outranks unmaterialized within one rank;
5. final tie is lower(name), then stable local/provider identity;
6. materialized result presentation comes from `game_igdb_enrichment`, not directly from the active provider row;
7. `include_provider=false` returns no unmaterialized provider rows even if an active generation exists;
8. repository fetches at most 51 rows, returns the first 50, and sets `HasMore=true` only when row 51 exists;
9. a deactivated materialized local Game's provider record does not reappear as an unmaterialized candidate (Track C regression from before commit c2cea97; mirrors Task 5 Step 2 item 5's `FindProviderDiscoveryGame` rule) — materialize a provider record, deactivate the linked local Game, search by the provider's own name/alias text, and assert the result set contains neither a materialized row (the Game is inactive) nor an unmaterialized row for that external ID.

- [ ] **Step 4: Run tests and confirm failure**

Run:

```bash
go test -race ./internal/game -run Discovery -count=1
task test:integration
```

Expected: compile/test failure until the new result/status contracts and SQL are implemented.

- [ ] **Step 5: Implement catalog availability**

In `Store.Status` set:

```go
CatalogAvailable: row.ActiveGenerationID.Valid,
```

The in-process `ProviderSyncing` overlay must preserve this boolean, so a refresh in progress after a successful baseline remains usable while an initial sync reports `CatalogAvailable=false`.

- [ ] **Step 6: Replace mutable DisableProvider with ProviderStatusSource**

Remove `providerEnabled` and `DisableProvider` from `DiscoveryService`.

Store a `ProviderStatusSource` on the service. In `Browse`:

```go
status, err := s.providerStatus.Status(ctx)
if err != nil {
    return DiscoveryPage{}, err
}

search, err = validateSearch(search)
if err != nil {
    return DiscoveryPage{}, err
}

if search == "" || utf8.RuneCountInString(search) == 1 {
    local, err := s.subscriptions.Browse(ctx, userID, "")
    if err != nil { return DiscoveryPage{}, err }
    return DiscoveryPage{
        Items: mapLocal(local),
        SearchTooShort: search != "",
        ProviderStatus: status,
    }, nil
}

includeProvider := status.State != ProviderDisabled
result, err := s.discovery.Search(ctx, userID, search, discoveryProviderRowLimit, includeProvider)
if err != nil { return DiscoveryPage{}, err }
return DiscoveryPage{
    Items: result.Items,
    HasMore: result.HasMore,
    ProviderStatus: status,
}, nil
```

Do not use `CatalogAvailable` to suppress provider search; an active-generation join naturally returns no provider rows before the baseline exists. `CatalogAvailable` is for UI state.

In `Resolve`, query status and reject only `ProviderDisabled` before calling the materializer. A configured provider with no active row continues to return `ErrProviderRecordNotFound` from materialization.

- [ ] **Step 7: Implement unified ranked Search SQL**

Replace the current alphabetical Search query with candidate CTEs that carry `match_rank`, `subscribed`, `materialized`, and one stable `identity_key`.

Use this rank expression for local name/aliases:

```sql
CASE
    WHEN lower(g.name) = lower(sqlc.arg(search)::text)
      OR EXISTS (
          SELECT 1 FROM game_aliases a
          WHERE a.game_id = g.id
            AND lower(a.alias) = lower(sqlc.arg(search)::text)
      ) THEN 1
    WHEN lower(g.name) LIKE lower(sqlc.arg(search)::text) || '%'
      OR EXISTS (
          SELECT 1 FROM game_aliases a
          WHERE a.game_id = g.id
            AND lower(a.alias) LIKE lower(sqlc.arg(search)::text) || '%'
      ) THEN 2
    ELSE 3
END AS match_rank
```

Use the equivalent expression against provider `name` and `alternative_names` via `unnest(provider_game.alternative_names)`.

Provider candidates must be gated by:

```sql
sqlc.arg(include_provider)::boolean
```

Materialized provider candidates must render:

- local `games.name` / `games.slug`;
- local `game_igdb_enrichment` presentation fields;
- local subscription state;
- and require the linked local Game to be `active` (an inactive linked Game renders no materialized candidate for that identity).

Unmaterialized candidates render active-generation provider fields, but a provider record with an existing non-archived `game_external_ids` mapping must be excluded from the unmaterialized candidates **independent of the mapped local Game's `active` state** — not only when it is active. Gate this exclusion on mapping existence (`archived_at IS NULL`) alone, mirroring `FindProviderDiscoveryGame`'s rule (Task 5, Step 4) and the Track C fix in commit c2cea97. Combined with the materialized-candidate requirement above, a provider record mapped to a *deactivated* local Game then renders as neither candidate: it disappears from search entirely, exactly like a deactivated local Game with no provider mapping. Do not require `games.active` in that exclusion check — that was the exact regression this fix already closed once, and rewriting the query from scratch here can reintroduce it.

Deduplicate by logical identity:

- local/materialized: `'local:' || local_game_id`;
- unmaterialized: `'provider:igdb:' || external_id`.

Keep the best `match_rank` for a duplicated materialized candidate, then final order:

```sql
ORDER BY
    match_rank,
    subscribed DESC,
    materialized DESC,
    lower(name),
    identity_key
LIMIT sqlc.arg(fetch_limit)::int
```

The repository calls the query with `fetch_limit = limit + 1`, trims row 51, and sets `HasMore`.

- [ ] **Step 8: Rewire app and compile handlers**

Construct `igdbSyncService` before `DiscoveryService` in `app.Run`:

```go
igdbSyncService := newIGDBSync(ctx, cfg.IGDB, gameStore, logger)
gameDiscovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, igdbSyncService)
```

Remove the `DisableProvider` call.

Update `handler.GameDiscovery.Browse` and `Games.Browse` to accept/forward `game.DiscoveryPage`. At this task, templates may still render only `page.Items`; Task 4 uses the new state fields.

Update router fakes/tests to satisfy the new interface.

- [ ] **Step 9: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/game/... ./internal/app ./internal/web/... -count=1
task test:integration
```

Expected: ranking, threshold, provider-disabled behavior, and has-more semantics all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/game         internal/app/app.go         internal/web/handler/games.go         internal/web/router_test.go         internal/game/postgres         internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: refine game discovery search model"
```

---

### Task 4: Make /games discovery-first and visually stable

**Files:**
- Create: `internal/web/pages/game_presentation.templ`
- Modify: `internal/web/pages/games.templ`
- Modify: `internal/web/pages/games_test.go`
- Modify: `internal/web/handler/games.go`
- Modify: `internal/web/router_test.go`
- Generate: `internal/web/pages/*_templ.go`

**Interfaces:**
- Consumes: `game.DiscoveryPage`, `game.DiscoveryItem`, `game.GamePresentation`.
- Produces shared templ component:

```go
templ GameIdentity(p game.GamePresentation, compact bool)
```

and a Games page that renders all player states without exposing materialization origin.

- [ ] **Step 1: Write failing page tests for discovery-first states**

Add tests that render `GameCatalog` and assert:

```go
func TestGameCatalogEmptySearchIsDiscoveryFirst(t *testing.T) {
    // page has no local items, ProviderStatus current/catalog available.
    // Assert "Find your game".
    // Assert absence of "0 games" and "No games found".
}

func TestGameCatalogOneCharacterPromptsForTwoCharacters(t *testing.T) {
    // SearchTooShort=true, q="c".
    // Assert "Type at least 2 characters to search."
}

func TestGameCatalogNoUsableCatalogShowsAvailabilityState(t *testing.T) {
    // no locals, CatalogAvailable=false, failed/disabled status.
    // Assert "No games are available yet."
}

func TestGameCatalogInitialSyncShowsGettingReady(t *testing.T) {
    // no locals, State=syncing, CatalogAvailable=false.
    // Assert "Game discovery is getting ready."
}

func TestGameCatalogBroadSearchShowsRefineHint(t *testing.T) {
    // HasMore=true.
    // Assert "Too many matches" and "refine your search".
}
```

Add an admin-viewer case proving only admins get an administration link in the no-catalog state.

- [ ] **Step 2: Write failing unified-card tests**

Render one unmaterialized and one materialized item with the same presentation fields.

Assert both contain:

- cover URL when `CoverImageID` exists;
- same name/type/platform/year text;
- `Find Players`;
- Follow or Following control.

Assert neither contains:

```text
Not yet in LANsurfer catalog
materialized
Local
IGDB
```

Use exact checks only for player-visible labels; do not reject the substring `igdb` inside a hidden action URL.

Add a local no-cover test that still renders the abbreviation fallback.

- [ ] **Step 3: Write failing HTMX search behavior test**

Assert the Games search form contains:

```text
input changed delay:300ms
search from:(#game-search)
hx-sync="this:replace"
hx-replace-url="true"
```

and does **not** contain `hx-push-url`.

Keep a normal GET submit button.

- [ ] **Step 4: Run page/router tests and confirm failure**

Run:

```bash
go test -race ./internal/web/pages ./internal/web -run 'GameCatalog|GameDiscovery|GameFollow' -count=1
```

Expected: new copy/markup tests fail against the current catalog/grid.

- [ ] **Step 5: Create shared GameIdentity component**

Move cover URL construction and name/metadata rendering into `game_presentation.templ`.

`GameIdentity` must:

- render the approved `images.igdb.com` URL only from `CoverImageID`;
- use the abbreviation tile when no cover exists;
- render metadata from Game Type, platform names, and release year when available;
- render no provider-origin label.

Keep `referrerpolicy="no-referrer"` and lazy-loading on cover images.

- [ ] **Step 6: Rebuild Games page around DiscoveryPage**

Render the page in this order:

1. top bar with `Games` and `My Games`;
2. live search;
3. if `SearchTooShort`, the 2-character hint;
4. if search is empty:
   - discovery-first heading/copy;
   - `Games on this LAN` only when local items exist;
   - provider readiness/no-catalog state only when no local items exist;
     - `not synced` or initial `syncing` without a usable baseline: `Game discovery is getting ready.`;
     - `disabled` or initial `failed` without a usable baseline: `No games are available yet.`;
     - any state with `CatalogAvailable=true`: do not show a provider failure/readiness warning to ordinary players;
5. if search is non-empty and at least 2 characters:
   - `HasMore` refinement hint;
   - matching unified cards;
   - `No games found for "<query>"` only when there are no results.

Do not render a zero-count catalog label in the initial state.

- [ ] **Step 7: Unify card actions**

Use one card template for both materialized and unmaterialized items.

Action helpers must choose:

```go
func gameRequestAction(item game.DiscoveryItem) string {
    if item.LocalGameID != "" {
        return "/game-requests/new?game_id=" + url.QueryEscape(item.LocalGameID)
    }
    return "/game-requests/new?provider=" + url.QueryEscape(item.Reference.Provider) +
        "&external_id=" + url.QueryEscape(item.Reference.ExternalID)
}
```

`Find Players` is always visible.

For Follow:

- local item: POST `/games/{gameID}/follow`;
- provider item: POST `/games/providers/{provider}/{externalID}/follow`;
- followed item: render `Following` + local unfollow when a local ID exists.

The provider Follow HTMX target must remain only the follow-control region so the existing cover/metadata card is not replaced by a poorer local card.

- [ ] **Step 8: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/web/pages ./internal/web/... -count=1
```

Expected: initial, search, follow, empty-state, and live-search tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/web/pages/game_presentation.templ         internal/web/pages/games.templ         internal/web/pages/games_test.go         internal/web/handler/games.go         internal/web/router_test.go         internal/web/pages/*_templ.go
git commit -m "feat: make games discovery first"
```

---

### Task 5: Add read-only Game selection for the composer

**Files:**
- Modify: `internal/game/presentation.go`
- Modify: `internal/game/provider.go`
- Modify: `internal/game/discovery.go`
- Modify: `internal/game/discovery_test.go`
- Modify: `internal/game/postgres/queries/igdb.sql`
- Modify: `internal/game/postgres/igdb_store.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Generate: `internal/game/postgres/sqlcgen/*.go`

**Interfaces:**
- Consumes: unified Game presentation/search from Tasks 2–3.
- Produces:

```go
var ErrInvalidGameSelection = errors.New("invalid game selection")

type GameSelection struct {
    LocalGameID string
    Reference   ProviderReference
}

type DiscoveryRepository interface {
    Search(context.Context, string, string, int, bool) (DiscoverySearchResult, error)
    FindLocal(context.Context, string, string) (DiscoveryItem, error)
    FindProvider(context.Context, string, ProviderReference) (DiscoveryItem, error)
}

func (s *DiscoveryService) Select(
    context.Context,
    string,
    GameSelection,
) (DiscoveryItem, error)
```

- [ ] **Step 1: Write failing service tests**

Add:

```go
func TestDiscoverySelectLocalIsReadOnly(t *testing.T) {
    // LocalGameID only -> FindLocal called, materializer not called.
}

func TestDiscoverySelectProviderIsReadOnly(t *testing.T) {
    // provider reference only -> FindProvider called, materializer not called.
}

func TestDiscoverySelectRejectsMissingOrAmbiguousIdentity(t *testing.T) {
    tests := []GameSelection{
        {},
        {LocalGameID: "local-1", Reference: ProviderReference{Provider: "igdb", ExternalID: "100"}},
    }
    // Expect ErrInvalidGameSelection and zero repository/materializer calls.
}

func TestDiscoverySelectProviderRejectsDisabledProvider(t *testing.T) {
    // ProviderStatus=disabled -> ErrProviderRecordNotFound, FindProvider not called.
}
```

- [ ] **Step 2: Add failing PostgreSQL selection tests**

Test:

1. local selection returns local name/slug + retained enrichment + current viewer Follow state;
2. unmaterialized provider selection returns active-generation provider presentation without inserting into `games` or `game_external_ids`;
3. a materialized provider reference returns the linked local Game/presentation once;
4. a provider record absent from the active generation returns `ErrProviderRecordNotFound`;
5. a deactivated materialized local Game is not resurrected as an unmaterialized provider choice.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test -race ./internal/game -run DiscoverySelect -count=1
task test:integration
```

- [ ] **Step 4: Implement FindLocal and FindProvider SQL**

`FindLocalDiscoveryGame`:

- selects one active local Game by ID;
- left joins subscription for viewer;
- left joins `game_igdb_enrichment`;
- returns `Materialized=true`.

`FindProviderDiscoveryGame`:

- requires the provider row to belong to the active generation;
- if it maps to an active local Game, returns the local Game + local enrichment + subscription state;
- if no mapping exists, returns provider presentation with `Materialized=false`;
- if a mapping exists only to an inactive local Game, return no row rather than surfacing a duplicate unmaterialized card.

- [ ] **Step 5: Implement Select validation/orchestration**

Validate exactly one identity source:

```go
hasLocal := strings.TrimSpace(selection.LocalGameID) != ""
hasProvider := selection.Reference != (ProviderReference{})
if hasLocal == hasProvider {
    return DiscoveryItem{}, ErrInvalidGameSelection
}
```

For provider selection:

1. normalize provider reference;
2. read provider status;
3. reject `ProviderDisabled`;
4. call `FindProvider`.

Never call `Materialize` from `Select`.

- [ ] **Step 6: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/game/... -count=1
task test:integration
```

- [ ] **Step 7: Commit**

```bash
git add internal/game         internal/game/postgres         internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: add read only game selection"
```

---

### Task 6: Enrich Game Request list and detail presentation

**Files:**
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Modify: `internal/query/gamerequest/query.go`
- Modify: `internal/query/gamerequest/query_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Generate: `internal/query/gamerequest/sqlcgen/*.go`
- Generate: `internal/web/pages/game_requests_templ.go`

**Interfaces:**
- Consumes: `game.GamePresentation` and `GameIdentity`.
- Produces:

```go
type Request struct {
    ID               string
    GameID           string
    Game             game.GamePresentation
    CreatorID        string
    CreatorName      string
    MaxPlayers       *int32
    StartsAt         time.Time
    Description      string
    Status           domain.Status
    CreatedAt        time.Time
    MemberCount      int64
    ViewerJoined     bool
    ViewerSubscribed bool
    Members          []Member
    Activity         []Activity
}
```

`TrendingGame` remains unchanged and text-only.

- [ ] **Step 1: Write failing query/integration tests**

Extend request query tests/integration setup so a Game Request for an enriched local Game returns:

```go
if request.Game.CoverImageID != "co1abc" ||
   request.Game.Name != "Warcraft III" ||
   request.Game.GameType != "Main Game" {
    t.Fatalf("game = %#v", request.Game)
}
```

Cover both `ListActive` and `ByID`.

- [ ] **Step 2: Write failing page tests**

Add:

```go
func TestGameRequestRowUsesCoverWhenAvailable(t *testing.T) {
    // Request.Game has CoverImageID.
    // Assert images.igdb.com cover URL and absence of abbreviation tile text.
}

func TestGameRequestRowFallsBackToAbbreviationWithoutCover(t *testing.T) {
    // Empty cover -> existing abbreviation rendering remains.
}

func TestTrendingRemainsTextOnly(t *testing.T) {
    // TrendingSection must not render an image even when request Games elsewhere have covers.
}
```

Add a detail test that the Game identity at the top/detail area includes cover and metadata.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test -race ./internal/query/gamerequest ./internal/web/pages -run 'GameRequest|Trending' -count=1
task test:integration
```

- [ ] **Step 4: Extend request SQL with local enrichment**

For `ListActiveGameRequests` and `GetGameRequest`:

```sql
LEFT JOIN game_igdb_enrichment enrichment ON enrichment.game_id = g.id
```

Select:

```sql
g.id::text AS game_id,
g.name AS game_name,
g.slug AS game_slug,
enrichment.cover_image_id,
enrichment.game_type,
enrichment.platform_names,
enrichment.first_release_date
```

Keep Trending SQL unchanged.

- [ ] **Step 5: Map Request.Game once**

Import `internal/game` in `internal/query/gamerequest/query.go`.

Replace the expanding positional `requestFromFields` signature with a small internal input struct so list/detail cannot accidentally reorder new presentation fields:

```go
type requestFields struct {
    ID string
    GameID string
    Game game.GamePresentation
    CreatorID string
    CreatorName string
    MaxPlayers pgtype.Int4
    StartsAt pgtype.Timestamptz
    Description string
    Status string
    CreatedAt pgtype.Timestamptz
    MemberCount int64
    ViewerJoined bool
    ViewerSubscribed bool
}
```

Both sqlc row mappers construct `requestFields`, then one `requestFromFields` builds `Request`.

- [ ] **Step 6: Use shared GameIdentity in list/detail**

Replace the hard-coded abbreviation/name pair in `GameRequestRow` with `GameIdentity(request.Game, true)`.

Use `GameIdentity(request.Game, false)` in the Game Request detail's primary Game presentation.

Do not add cover markup to `TrendingSection`.

- [ ] **Step 7: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/query/gamerequest ./internal/web/pages -count=1
task test:integration
```

- [ ] **Step 8: Commit**

```bash
git add internal/query/gamerequest         internal/web/pages/game_requests.templ         internal/web/pages/game_requests_test.go         internal/web/pages/game_requests_templ.go         internal/platform/postgres/postgres_integration_test.go
git commit -m "feat: enrich game request presentation"
```

---

### Task 7: Replace the Game Request local-only selector with shared discovery

**Files:**
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Modify: `internal/web/router.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/query/gamerequest/query.go`
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Generate: templ/sqlc output

**Interfaces:**
- Consumes: `DiscoveryService.Browse`, `DiscoveryService.Select`, and existing `GameResolver.Resolve`.
- Produces handler dependency:

```go
type GameRequestDiscovery interface {
    Browse(context.Context, string, string) (game.DiscoveryPage, error)
    Select(context.Context, string, game.GameSelection) (game.DiscoveryItem, error)
}
```

Update constructor:

```go
func NewGameRequests(
    commands GameRequestCommands,
    queries GameRequestQueries,
    resolver GameResolver,
    discovery GameRequestDiscovery,
    brand branding.Brand,
) *GameRequests
```

Add authenticated fragment routes:

```text
GET /game-requests/new/discovery
GET /game-requests/new/selection
```

- [ ] **Step 1: Write failing handler tests for global Create flow**

Add tests:

```go
func TestNewGameRequestUsesDiscoveryWithoutMaterializing(t *testing.T) {
    // GET /game-requests/new?q=war
    // discovery.Browse called; resolver calls == 0.
}

func TestGameRequestDiscoveryFragmentUsesSameSearchModel(t *testing.T) {
    // GET /game-requests/new/discovery?q=war
    // returns the discovery fragment and passes exact query to Browse.
}

func TestGameRequestSelectionIsReadOnly(t *testing.T) {
    // GET /game-requests/new/selection?provider=igdb&external_id=100
    // discovery.Select called; resolver calls == 0.
}

func TestGameRequestSelectionAcceptsLocalGameID(t *testing.T) {
    // game_id=local-1 -> GameSelection{LocalGameID:"local-1"}.
}
```

The fake discovery should record Browse/Select calls separately.

- [ ] **Step 2: Write failing composer page tests**

Cover:

1. initial page shows `Find your game` and the shared discovery list/LAN list;
2. selected Game renders `GameIdentity`, `Change game`, and hidden local or provider identity fields;
3. request detail fields are absent before first selection and present after selection;
4. the selected provider card contains the real Game name/cover, never `Selected from IGDB search results.`;
5. the change-game discovery search uses the same 300 ms/replace-URL interaction model;
6. selection HTMX swaps only selected-game/hidden-field regions, so start/max-player/description inputs are not replaced and keep browser-entered values.

Use stable IDs:

```text
#game-request-selected-game
#game-request-game-fields
#game-request-discovery
#game-request-details
```

- [ ] **Step 3: Write failing POST/error-state tests**

Add:

```go
func TestCreateGameRequestProviderMissingPreservesFormAndAsksForAnotherGame(t *testing.T) {
    // Valid request fields + provider selection.
    // resolver returns game.ErrProviderRecordNotFound.
    // Expect 422, body contains:
    // "This game is no longer available from the game catalog. Choose another game."
    // and original max_players/start/description values.
}

func TestCreateGameRequestKeepsMaterializedGameWhenCommandFails(t *testing.T) {
    // resolver returns local Game, commands.Create fails.
    // Assert resolver called once and no compensating delete/unmaterialize call exists.
    // Re-render selects the returned local game ID and preserves form values.
}
```

Keep and adapt the existing validation tests proving invalid request fields do not call `Resolve`.

- [ ] **Step 4: Run handler/page tests and confirm failure**

Run:

```bash
go test -race ./internal/web/handler ./internal/web/pages ./internal/web -run 'GameRequest.*(Discovery|Selection|Create|New)' -count=1
```

- [ ] **Step 5: Remove CreationOptions from the composer contract**

Delete `CreationOptions` from `handler.GameRequestQueries` and remove:

- `requestquery.GameOption`;
- `Store.CreationOptions`;
- sqlc query `ActiveGamesForGameRequest`;

after the discovery composer is wired.

Do not remove any query used by other request/list functionality.

- [ ] **Step 6: Implement full-page New and discovery fragment**

`New`:

1. read current user;
2. parse optional `game_id` or provider/external ID into `game.GameSelection`;
3. when a selection exists, call `discovery.Select`;
4. when no selection exists, call `discovery.Browse(ctx, userID, q)`;
5. render the composer with the selected item, discovery page, form defaults, and no materialization.

`Discovery` fragment handler:

```go
page, err := h.discovery.Browse(r.Context(), account.ID, r.URL.Query().Get("q"))
if err != nil { /* existing logged error path */ }
render(w, r, http.StatusOK, pages.GameRequestDiscovery(page, q, true))
```

The boolean indicates change-game mode so result controls use HTMX selection swaps instead of initial full-page navigation.

- [ ] **Step 7: Implement read-only selection fragment**

`Selection` parses exactly the same identity fields as `New`, calls `discovery.Select`, and for HTMX returns:

- replacement markup for `#game-request-selected-game`;
- out-of-band replacement for `#game-request-game-fields`.

The response must not include `#game-request-details`; this is what preserves already-entered request data in the browser.

For a normal non-HTMX request, redirect to the canonical full-page `/game-requests/new?...selection...` URL.

- [ ] **Step 8: Rebuild composer markup**

The initial unselected state:

- shows `Find your game`;
- uses `GameRequestDiscovery` with the same page state/copy rules as Games;
- does not show max-player/start/description controls yet.

The selected state:

- renders selected Game card via `GameIdentity`;
- renders `Change game`;
- includes `#game-request-game-fields` hidden identity inputs;
- renders `#game-request-details` with the existing request inputs.

The change-game UI may stay open in a native `<details>` or adjacent panel, but its GET search form must not be nested inside the POST create form.

- [ ] **Step 9: Preserve form state on POST failures**

Keep the existing order:

1. parse form;
2. validate max players/start/description;
3. only then resolve provider if no local Game ID;
4. create request.

On `ErrProviderRecordNotFound` during final provider resolution:

- return `422 Unprocessable Entity`, not a dead-end 404;
- keep the posted form values;
- clear the unavailable selected provider from the rendered selection;
- render the actionable catalog-unavailable copy and discovery/change-game UI.

If `Resolve` succeeds and `commands.Create` later fails, set `form.GameID = resolved.ID` before re-render so the now-materialized local Game remains selected.

Do not add rollback/delete compensation.

- [ ] **Step 10: Wire routes and app dependency**

In `router.go`:

```go
requests.Get("/new", gameRequestsHandler.New)
requests.Get("/new/discovery", gameRequestsHandler.Discovery)
requests.Get("/new/selection", gameRequestsHandler.Selection)
```

Pass the same `gameDiscovery` instance to `NewGameRequests` from `web.NewRouter`; extend `RouterDependencies` only if necessary, preferring the existing `Discovery` dependency rather than introducing a second duplicate object.

- [ ] **Step 11: Generate and verify**

Run:

```bash
task generate
go test -race ./internal/web/... ./internal/query/gamerequest -count=1
task test:integration
```

Expected: both the global Create Game Request flow and `Find Players` from Games can select local/provider Games without GET-side materialization; valid final POST still materializes provider Games.

- [ ] **Step 12: Commit**

```bash
git add internal/web         internal/app/app.go         internal/query/gamerequest
git commit -m "feat: add discovery to game request composer"
```

---

### Task 8: Align durable docs and run the full refinement verification

**Files:**
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify only if needed for exact current behavior: `docs/ROADMAP.md`
- Verify: all modified Go/templ/SQL/generated files from Tasks 1–7

**Interfaces:**
- Consumes: completed implementation.
- Produces: long-lived documentation matching the implemented behavior and final verification evidence.

- [ ] **Step 1: Update PRODUCT.md with the player-facing model**

In the Game catalog / IGDB section, explicitly state:

```text
Games is a discovery-first surface. Players search one catalog without needing
to know whether a result is already a local Game or comes from the synchronized
IGDB index. Materialization is not a player-facing status.

Materialized IGDB-backed Games keep last-known-good presentation enrichment
(cover, Game Type, platforms, and release date) while local name, slug, active
state, and aliases remain authoritative.

Follow and Find Players are independent. Creating a Game Request does not
automatically follow the Game.
```

Document that `My Games` remains explicitly followed Games only.

- [ ] **Step 2: Update ARCHITECTURE.md with the enrichment boundary**

Add the concrete storage/read rule:

```text
Unmaterialized discovery reads presentation metadata from the active provider
generation. Once materialized, player-facing reads use the authoritative local
Game plus a local last-known-good IGDB enrichment snapshot. Successful provider
syncs refresh only non-empty enrichment fields; provider disappearance or
disablement does not remove the snapshot.
```

Document that Game Request list/detail read models may join this local enrichment, while the Game Request domain owns no IGDB state.

- [ ] **Step 3: Regenerate from a clean source state**

Run:

```bash
task generate
git status --short
```

Confirm generated templ/sqlc changes correspond only to source templates/queries changed by this refinement.

- [ ] **Step 4: Run full verification**

Run exactly:

```bash
task lint
task test
task test:integration
task build
```

Expected: all commands exit 0.

- [ ] **Step 5: Run focused semantic checks**

Run:

```bash
grep -R "Not yet in LANsurfer catalog" internal/web --exclude='*_templ.go'
grep -R "Selected from IGDB search results" internal/web --exclude='*_templ.go'
grep -R "api.igdb.com\|id.twitch.tv" internal/web internal/query internal/gamerequest
```

Expected:

- first command returns no player-template source match;
- second command returns no player-template source match;
- third command returns no player-facing web/query/domain call site.

The IGDB HTTP URLs remain confined to the IGDB adapter.

- [ ] **Step 6: Review the complete branch diff against main**

Run:

```bash
git diff main...HEAD --stat
git diff main...HEAD -- docs/PRODUCT.md docs/ARCHITECTURE.md
git status --short
```

Confirm:

- no unrelated refactor;
- no new runtime service/dependency;
- no provider-origin status leaked into player copy;
- no name-based merge;
- no cover added to Trending;
- working tree clean.

- [ ] **Step 7: Commit documentation/final generated changes if any**

```bash
git add docs/PRODUCT.md docs/ARCHITECTURE.md docs/ROADMAP.md
git add internal
git commit -m "docs: align IGDB discovery refinement"
```

If `docs/ROADMAP.md` required no factual change, do not stage it. If all generated code was already committed in prior tasks and no final source change occurred, the final commit contains only the documentation files.

