# IGDB Cover Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve IGDB covers same-origin from `/media/igdb/{size}/{imageID}` so player browsers never contact `images.igdb.com`.

**Architecture:** A new `internal/game/cover` package owns validation, a disk or memory store, a bounded negative cache, `singleflight` coalescing and an 8-slot cold-miss semaphore. `internal/game/igdb` gets a fixed-origin fetcher; `internal/game/postgres` answers whether a cover ID is known. A public web handler translates `cover` errors to HTTP. Templates point at the new route; CSP drops IGDB.

**Tech Stack:** Go 1.27 stdlib, `golang.org/x/sync/singleflight` (already indirect), chi, templ, sqlc, goose, PostgreSQL.

**Spec:** `docs/superpowers/igdb/2026-09-22-cover-proxy-design.md`. Canonical docs (PRODUCT, ARCHITECTURE, ROADMAP, README, AGENTS.md) were already aligned in `e54ec4e`; this plan changes code, deployment and tests only.

## Global Constraints

- Sizes: `small` → `t_cover_small`, `big` → `t_cover_big`. Anything else is `404`.
- Image ID: `^[a-z0-9]{1,32}$`, checked before any store, catalog or fetch access.
- Upstream origin constant `https://images.igdb.com`, path `/igdb/image/upload/{variant}/{id}.jpg`.
- Upstream: 5 s client timeout, no redirects, status `200` only, ≤ 1 MiB (`1<<20`), sniffed type `image/jpeg|image/png|image/webp`.
- Shared cold-miss deadline 5 s covering catalog + fetch.
- Negative cache: 10 min from insertion, 4,096 entries, oldest evicted, reads do not extend.
- Concurrency: 8 cold-miss slots per process, nonblocking; full → `ErrBusy` → `503` + `Retry-After: 1`, never negative-cached.
- Memory store cap: 64 MiB, fixed.
- `200`: `Cache-Control: public, max-age=31536000, immutable`; every non-200: `Cache-Control: no-store`.
- Route is public (outside the authenticated group).
- No configurable caps/TTLs/timeouts; no request-rate limiter; no new dependencies besides promoting `golang.org/x/sync` to direct.

## Review Focus

1. A caller that disconnects while another waits on the same key must not cancel the shared fetch → covered in Task 3 cancellation test.
2. A DB outage must not hide covers for 10 minutes → Task 3 "catalog error not remembered" test.
3. The 9th distinct cold key during saturation must get `503` immediately, not queue → Task 3 admission test and Task 4 router test.
4. Path traversal (`../x`, `%2f`) must never reach disk or upstream → Task 3 validation table (zero calls asserted).
5. A slow upstream must release its slot → Task 3 deadline test (slot reusable after timeout).

---

### Task 1: Known-cover lookup in PostgreSQL

**Files:**
- Create: `internal/platform/postgres/migrations/00013_igdb_cover_index.sql`
- Modify: `internal/game/postgres/queries/igdb.sql` (append query)
- Modify: `internal/game/postgres/igdb_store.go` (append method)
- Regenerate: `internal/game/postgres/sqlcgen/igdb.sql.go`
- Test: `internal/platform/postgres/postgres_integration_test.go` (append test)

**Interfaces:**
- Produces: `func (s *gamepostgres.Store) CoverImageKnown(ctx context.Context, imageID string) (bool, error)`

- [ ] **Step 1: Write the failing integration test** (append):

```go
func TestCoverImageKnown(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := gamepostgres.NewStore(pool)
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{{
		Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"},
		Name:      "Indexed", Slug: "indexed", CoverImageID: "coindexed",
	}}); err != nil {
		t.Fatalf("StoreProviderGames: %v", err)
	}
	var gameID string
	if err := pool.QueryRow(t.Context(), `INSERT INTO games (name, slug) VALUES ('Enriched', 'enriched') RETURNING id`).Scan(&gameID); err != nil {
		t.Fatalf("insert game: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO game_igdb_enrichment (game_id, external_id, cover_image_id) VALUES ($1, '77', 'coenriched')`, gameID); err != nil {
		t.Fatalf("insert enrichment: %v", err)
	}

	for id, want := range map[string]bool{"coindexed": true, "coenriched": true, "counknown": false} {
		got, err := store.CoverImageKnown(t.Context(), id)
		if err != nil {
			t.Fatalf("CoverImageKnown(%q): %v", id, err)
		}
		if got != want {
			t.Errorf("CoverImageKnown(%q) = %v, want %v", id, got, want)
		}
	}
}
```

- [ ] **Step 2: Run** `task test:integration` → compile failure: `store.CoverImageKnown undefined`.

- [ ] **Step 3: Migration** `00013_igdb_cover_index.sql`:

```sql
-- +goose Up
CREATE INDEX igdb_game_index_cover_image_id_idx
    ON igdb_game_index (cover_image_id) WHERE cover_image_id IS NOT NULL;

-- +goose Down
DROP INDEX igdb_game_index_cover_image_id_idx;
```

- [ ] **Step 4: Query** (append to `queries/igdb.sql`):

```sql
-- name: CoverImageKnown :one
SELECT EXISTS (SELECT 1 FROM igdb_game_index WHERE cover_image_id = sqlc.arg(cover_image_id)::text)
    OR EXISTS (SELECT 1 FROM game_igdb_enrichment WHERE cover_image_id = sqlc.arg(cover_image_id)::text) AS known;
```

Run `go run github.com/sqlc-dev/sqlc/cmd/sqlc@<SQLC_VERSION from Taskfile.yml> generate`.

- [ ] **Step 5: Store method** (append to `igdb_store.go`):

```go
// CoverImageKnown reports whether any synchronized provider record or local
// enrichment snapshot references imageID, so the cover proxy only ever
// fetches covers LANsurfer itself renders.
func (s *Store) CoverImageKnown(ctx context.Context, imageID string) (bool, error) {
	known, err := s.queries.CoverImageKnown(ctx, imageID)
	if err != nil {
		return false, fmt.Errorf("look up IGDB cover image: %w", err)
	}
	return known, nil
}
```

- [ ] **Step 6: Run** `task test:integration` → PASS.
- [ ] **Step 7: Commit** `feat(game): look up known IGDB cover IDs`.

---

### Task 2: Fixed-origin upstream fetcher

**Files:**
- Create: `internal/game/igdb/cover.go`
- Test: `internal/game/igdb/cover_test.go`

**Interfaces:**
- Produces: `func NewCoverFetcher() *CoverFetcher`, `func (f *CoverFetcher) Fetch(ctx context.Context, size, imageID string) ([]byte, error)`

- [ ] **Step 1: Failing tests** (`cover_test.go`):

```go
package igdb

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 64)...)

func TestCoverFetcherRequestsFixedPathPerSize(t *testing.T) {
	for size, want := range map[string]string{
		"small": "/igdb/image/upload/t_cover_small/co1abc.jpg",
		"big":   "/igdb/image/upload/t_cover_big/co1abc.jpg",
	} {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
			_, _ = w.Write(jpegBytes)
		}))
		image, err := newCoverFetcher(server.URL).Fetch(t.Context(), size, "co1abc")
		server.Close()
		if err != nil {
			t.Fatalf("Fetch(%s) error = %v", size, err)
		}
		if got != want || !bytes.Equal(image, jpegBytes) {
			t.Errorf("Fetch(%s) path = %q, want %q", size, got, want)
		}
	}
}

func TestCoverFetcherRejectsUnknownSize(t *testing.T) {
	if _, err := newCoverFetcher("http://127.0.0.1:1").Fetch(t.Context(), "medium", "co1abc"); err == nil {
		t.Fatal("Fetch(medium) error = nil")
	}
}

func TestCoverFetcherRejectsBadResponses(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.invalid/x.jpg", http.StatusFound)
		},
		"status": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(jpegBytes)
		},
		"html": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("<!doctype html><html></html>"))
		},
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(jpegBytes, make([]byte, maxCoverBytes)...))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if _, err := newCoverFetcher(server.URL).Fetch(t.Context(), "big", "co1abc"); err == nil {
				t.Fatal("Fetch() error = nil")
			}
		})
	}
}

func TestCoverFetcherTimesOut(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	fetcher := newCoverFetcher(server.URL)
	fetcher.client.Timeout = 50 * time.Millisecond
	if _, err := fetcher.Fetch(t.Context(), "big", "co1abc"); err == nil {
		t.Fatal("Fetch() error = nil, want timeout")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/game/igdb -run CoverFetcher` → compile failure.

- [ ] **Step 3: Implement** `cover.go`:

```go
package igdb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// coverOrigin is fixed: no request input ever selects the upstream host.
	coverOrigin   = "https://images.igdb.com"
	coverTimeout  = 5 * time.Second
	maxCoverBytes = 1 << 20
)

var coverVariants = map[string]string{"small": "t_cover_small", "big": "t_cover_big"}

// CoverFetcher downloads one IGDB cover image from the fixed images.igdb.com
// origin for the same-origin cover proxy.
type CoverFetcher struct {
	origin string
	client *http.Client
}

func NewCoverFetcher() *CoverFetcher { return newCoverFetcher(coverOrigin) }

func newCoverFetcher(origin string) *CoverFetcher {
	return &CoverFetcher{origin: origin, client: &http.Client{
		Timeout: coverTimeout,
		// A redirect could point anywhere; treat it as a failed fetch.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (f *CoverFetcher) Fetch(ctx context.Context, size, imageID string) ([]byte, error) {
	variant, ok := coverVariants[size]
	if !ok {
		return nil, fmt.Errorf("unknown IGDB cover size %q", size)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.origin+"/igdb/image/upload/"+variant+"/"+url.PathEscape(imageID)+".jpg", nil)
	if err != nil {
		return nil, fmt.Errorf("build IGDB cover request: %w", err)
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch IGDB cover: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch IGDB cover: status %d", response.StatusCode)
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxCoverBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read IGDB cover: %w", err)
	}
	if len(image) > maxCoverBytes {
		return nil, fmt.Errorf("IGDB cover exceeds %d bytes", maxCoverBytes)
	}
	switch contentType := http.DetectContentType(image); contentType {
	case "image/jpeg", "image/png", "image/webp":
		return image, nil
	default:
		return nil, fmt.Errorf("IGDB cover has content type %q", contentType)
	}
}
```

- [ ] **Step 4: Run** `go test -race ./internal/game/igdb` → PASS.
- [ ] **Step 5: Commit** `feat(igdb): fetch covers from the fixed IGDB image origin`.

---

### Task 3: `cover` package (stores, negative cache, service)

**Files:**
- Create: `internal/game/cover/cover.go`, `internal/game/cover/failures.go`, `internal/game/cover/memory.go`, `internal/game/cover/disk.go`
- Test: `internal/game/cover/cover_test.go`, `internal/game/cover/store_test.go`
- Modify: `go.mod` (`golang.org/x/sync` becomes direct via `go mod tidy`)

**Interfaces:**
- Consumes: nothing from earlier tasks (interfaces are consumer-owned).
- Produces:
  - `var ErrNotFound, ErrBusy error`
  - `const MemoryCapacity = 64 << 20`
  - `type Store interface { Get(size, id string) ([]byte, bool, error); Put(size, id string, image []byte) error }`
  - `type Fetcher interface { Fetch(ctx context.Context, size, id string) ([]byte, error) }`
  - `type Catalog interface { CoverImageKnown(ctx context.Context, id string) (bool, error) }`
  - `func NewService(store Store, fetcher Fetcher, catalog Catalog, logger *slog.Logger) *Service`
  - `func (s *Service) Get(ctx context.Context, size, id string) ([]byte, error)`
  - `func NewMemoryStore(capacity int) *MemoryStore`, `func NewDiskStore(dir string) (*DiskStore, error)`

- [ ] **Step 1: Store tests** (`store_test.go`):

```go
package cover

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryStoreEvictsLeastRecentlyUsed(t *testing.T) {
	store := NewMemoryStore(10)
	_ = store.Put("big", "a", []byte("aaaa"))
	_ = store.Put("big", "b", []byte("bbbb"))
	if _, ok, _ := store.Get("big", "a"); !ok { // a is now most recent
		t.Fatal("a missing")
	}
	_ = store.Put("big", "c", []byte("cccc")) // evicts b
	if _, ok, _ := store.Get("big", "b"); ok {
		t.Error("b survived eviction")
	}
	for _, id := range []string{"a", "c"} {
		if _, ok, _ := store.Get("big", id); !ok {
			t.Errorf("%s evicted", id)
		}
	}
	_ = store.Put("big", "huge", make([]byte, 11))
	if _, ok, _ := store.Get("big", "huge"); ok {
		t.Error("oversize image stored")
	}
	if _, ok, _ := store.Get("small", "a"); ok {
		t.Error("sizes share a key")
	}
}

func TestDiskStoreRoundTrip(t *testing.T) {
	store, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}
	if _, ok, err := store.Get("big", "co1"); ok || err != nil {
		t.Fatalf("Get(missing) = %v, %v", ok, err)
	}
	if err := store.Put("big", "co1", []byte("image")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	image, ok, err := store.Get("big", "co1")
	if err != nil || !ok || !bytes.Equal(image, []byte("image")) {
		t.Fatalf("Get = %q, %v, %v", image, ok, err)
	}
	if _, ok, _ := store.Get("small", "co1"); ok {
		t.Error("sizes share a file")
	}
}

func TestDiskStoreRejectsUnusableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskStore(file); err == nil {
		t.Fatal("NewDiskStore(file) error = nil")
	}
}
```

- [ ] **Step 2: Service tests** (`cover_test.go`):

```go
package cover

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeStore struct {
	mu      sync.Mutex
	images  map[string][]byte
	putErr  error
	onGet   func() // optional barrier hook, called before each Get reads
}

func (s *fakeStore) Get(size, id string) ([]byte, bool, error) {
	if s.onGet != nil {
		s.onGet()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	image, ok := s.images[size+"/"+id]
	return image, ok, nil
}

func (s *fakeStore) Put(size, id string, image []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images[size+"/"+id] = image
	return nil
}

type fakeCatalog struct {
	calls atomic.Int32
	known func(ctx context.Context, id string) (bool, error)
}

func (c *fakeCatalog) CoverImageKnown(ctx context.Context, id string) (bool, error) {
	c.calls.Add(1)
	if c.known == nil {
		return true, nil
	}
	return c.known(ctx, id)
}

type fakeFetcher struct {
	calls atomic.Int32
	fetch func(ctx context.Context, size, id string) ([]byte, error)
}

func (f *fakeFetcher) Fetch(ctx context.Context, size, id string) ([]byte, error) {
	f.calls.Add(1)
	if f.fetch == nil {
		return []byte("image-" + id), nil
	}
	return f.fetch(ctx, size, id)
}

func newTestService() (*Service, *fakeStore, *fakeCatalog, *fakeFetcher) {
	store := &fakeStore{images: map[string][]byte{}}
	catalog := &fakeCatalog{}
	fetcher := &fakeFetcher{}
	return NewService(store, fetcher, catalog, slog.New(slog.NewTextHandler(io.Discard, nil))), store, catalog, fetcher
}

func TestGetRejectsInvalidInputWithoutAccess(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	for _, input := range []struct{ size, id string }{
		{"big", "../x"}, {"big", "%2f"}, {"big", "CO1ABC"}, {"big", ""},
		{"big", strings.Repeat("a", 33)}, {"big", "co1.abc"}, {"medium", "co1abc"},
	} {
		if _, err := service.Get(t.Context(), input.size, input.id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q, %q) error = %v, want ErrNotFound", input.size, input.id, err)
		}
	}
	if catalog.calls.Load() != 0 || fetcher.calls.Load() != 0 {
		t.Errorf("catalog/fetch calls = %d/%d, want 0/0", catalog.calls.Load(), fetcher.calls.Load())
	}
}

func TestGetServesHitWithoutCatalogOrFetch(t *testing.T) {
	service, store, catalog, fetcher := newTestService()
	store.images["big/co1"] = []byte("cached")
	image, err := service.Get(t.Context(), "big", "co1")
	if err != nil || string(image) != "cached" {
		t.Fatalf("Get = %q, %v", image, err)
	}
	if catalog.calls.Load() != 0 || fetcher.calls.Load() != 0 {
		t.Error("cache hit reached catalog or fetcher")
	}
}

func TestGetFetchesAndStoresKnownMiss(t *testing.T) {
	service, store, _, fetcher := newTestService()
	image, err := service.Get(t.Context(), "big", "co1")
	if err != nil || string(image) != "image-co1" {
		t.Fatalf("Get = %q, %v", image, err)
	}
	if string(store.images["big/co1"]) != "image-co1" {
		t.Error("fetched image not stored")
	}
	if _, err := service.Get(t.Context(), "big", "co1"); err != nil || fetcher.calls.Load() != 1 {
		t.Errorf("second Get error = %v, fetches = %d, want nil/1", err, fetcher.calls.Load())
	}
}

func TestGetUnknownIDIsNotFetchedAndIsRemembered(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	catalog.known = func(context.Context, string) (bool, error) { return false, nil }
	for range 2 {
		if _, err := service.Get(t.Context(), "big", "co1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get error = %v, want ErrNotFound", err)
		}
	}
	if catalog.calls.Load() != 1 || fetcher.calls.Load() != 0 {
		t.Errorf("catalog/fetch calls = %d/%d, want 1/0", catalog.calls.Load(), fetcher.calls.Load())
	}
}

func TestGetStoreWriteErrorStillReturnsImage(t *testing.T) {
	service, store, _, _ := newTestService()
	store.putErr = errors.New("disk full")
	if image, err := service.Get(t.Context(), "big", "co1"); err != nil || string(image) != "image-co1" {
		t.Fatalf("Get = %q, %v", image, err)
	}
}

func TestGetCoalescesConcurrentMisses(t *testing.T) {
	service, _, _, fetcher := newTestService()
	release := make(chan struct{})
	fetcher.fetch = func(context.Context, string, string) ([]byte, error) {
		<-release
		return []byte("image"), nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Get(context.Background(), "big", "co1"); err != nil {
				t.Errorf("Get error = %v", err)
			}
		}()
	}
	for fetcher.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if fetcher.calls.Load() != 1 {
		t.Errorf("fetches = %d, want 1", fetcher.calls.Load())
	}
}

// A caller that missed the cache before another flight completed must use
// that flight's result from the in-flight recheck instead of fetching again.
func TestGetDelayedCallerUsesCompletedFlightResult(t *testing.T) {
	for name, known := range map[string]bool{"positive": true, "negative": false} {
		t.Run(name, func(t *testing.T) {
			service, store, catalog, fetcher := newTestService()
			catalog.known = func(context.Context, string) (bool, error) { return known, nil }
			paused, resume := make(chan struct{}), make(chan struct{})
			var first atomic.Bool
			store.onGet = func() {
				if first.CompareAndSwap(false, true) {
					close(paused)
					<-resume
				}
			}
			delayed := make(chan error, 1)
			go func() {
				_, err := service.Get(context.Background(), "big", "co1")
				delayed <- err
			}()
			<-paused
			_, _ = service.Get(t.Context(), "big", "co1") // completes a full flight
			close(resume)
			err := <-delayed
			if known && err != nil || !known && !errors.Is(err, ErrNotFound) {
				t.Fatalf("delayed Get error = %v", err)
			}
			if catalog.calls.Load() != 1 || fetcher.calls.Load() > 1 {
				t.Errorf("catalog/fetch calls = %d/%d, want 1/≤1", catalog.calls.Load(), fetcher.calls.Load())
			}
		})
	}
}

func TestGetRejectsNewColdKeysWhenSaturated(t *testing.T) {
	service, store, catalog, _ := newTestService()
	store.images["big/cached"] = []byte("cached")
	release := make(chan struct{})
	entered := make(chan struct{}, maxColdMisses)
	catalog.known = func(context.Context, string) (bool, error) {
		entered <- struct{}{}
		<-release
		return true, nil
	}
	var wg sync.WaitGroup
	for i := range maxColdMisses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Get(context.Background(), "big", fmt.Sprintf("co%d", i)); err != nil {
				t.Errorf("admitted Get error = %v", err)
			}
		}()
	}
	for range maxColdMisses {
		<-entered
	}
	if _, err := service.Get(t.Context(), "big", "coninth"); !errors.Is(err, ErrBusy) {
		t.Fatalf("ninth Get error = %v, want ErrBusy", err)
	}
	if catalog.calls.Load() != maxColdMisses {
		t.Errorf("catalog calls = %d, want %d", catalog.calls.Load(), maxColdMisses)
	}
	if _, err := service.Get(t.Context(), "big", "cached"); err != nil {
		t.Errorf("hit during saturation error = %v", err)
	}
	joined := make(chan error, 1)
	go func() { _, err := service.Get(context.Background(), "big", "co0"); joined <- err }()
	close(release)
	wg.Wait()
	if err := <-joined; err != nil {
		t.Errorf("joining caller error = %v", err)
	}
	catalog.known = nil
	if _, err := service.Get(t.Context(), "big", "coninth"); err != nil {
		t.Errorf("retry after release error = %v (busy must not be remembered)", err)
	}
}

func TestGetCanceledCallerDoesNotCancelSharedFetch(t *testing.T) {
	service, _, _, fetcher := newTestService()
	release := make(chan struct{})
	fetcher.fetch = func(ctx context.Context, _, _ string) ([]byte, error) {
		<-release
		return []byte("image"), ctx.Err()
	}
	canceled, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := service.Get(canceled, "big", "co1"); first <- err }()
	for fetcher.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { _, err := service.Get(context.Background(), "big", "co1"); second <- err }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller error = %v", err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatalf("waiting caller error = %v", err)
	}
	if fetcher.calls.Load() != 1 {
		t.Errorf("fetches = %d, want 1", fetcher.calls.Load())
	}
}

func TestGetSharesOneDeadlineAcrossCatalogAndFetch(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	service.timeout = 50 * time.Millisecond
	catalog.known = func(ctx context.Context, _ string) (bool, error) {
		<-ctx.Done()
		return false, ctx.Err()
	}
	_, err := service.Get(t.Context(), "big", "co1")
	if err == nil || errors.Is(err, ErrNotFound) || fetcher.calls.Load() != 0 {
		t.Fatalf("Get error = %v, fetches = %d; want internal error and no fetch", err, fetcher.calls.Load())
	}
	if service.failures.contains("big/co1") {
		t.Error("catalog deadline was negative-cached")
	}

	var catalogDeadline, fetchDeadline time.Time
	catalog.known = func(ctx context.Context, _ string) (bool, error) {
		catalogDeadline, _ = ctx.Deadline()
		return true, nil
	}
	fetcher.fetch = func(ctx context.Context, _, _ string) ([]byte, error) {
		fetchDeadline, _ = ctx.Deadline()
		return []byte("image"), nil
	}
	if _, err := service.Get(t.Context(), "big", "co2"); err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if catalogDeadline.IsZero() || !catalogDeadline.Equal(fetchDeadline) {
		t.Errorf("deadlines catalog=%v fetch=%v, want one shared deadline", catalogDeadline, fetchDeadline)
	}
	if _, err := service.Get(t.Context(), "big", "co3"); err != nil {
		t.Errorf("slot not released after deadline: %v", err)
	}
}

func TestGetRemembersFetchFailureForTenMinutes(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	now := time.Unix(0, 0)
	service.failures.now = func() time.Time { return now }
	fetcher.fetch = func(context.Context, string, string) ([]byte, error) { return nil, errors.New("offline") }
	for range 2 {
		if _, err := service.Get(t.Context(), "big", "co1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get error = %v, want ErrNotFound", err)
		}
	}
	if fetcher.calls.Load() != 1 {
		t.Fatalf("fetches = %d, want 1", fetcher.calls.Load())
	}
	now = now.Add(9 * time.Minute)
	_, _ = service.Get(t.Context(), "big", "co1") // read must not extend expiry
	now = now.Add(time.Minute)
	_, _ = service.Get(t.Context(), "big", "co1")
	if fetcher.calls.Load() != 2 {
		t.Errorf("fetches after 10 min = %d, want 2", fetcher.calls.Load())
	}

	catalog.known = func(context.Context, string) (bool, error) { return false, errors.New("db down") }
	_, _ = service.Get(t.Context(), "big", "co9")
	catalog.known = nil
	fetcher.fetch = nil
	if _, err := service.Get(t.Context(), "big", "co9"); err != nil {
		t.Errorf("catalog error was remembered: %v", err)
	}
}

func TestFailureCacheIsBounded(t *testing.T) {
	cache := newFailureCache(time.Now)
	for i := range maxFailures + 1 {
		cache.add(fmt.Sprintf("big/co%d", i))
	}
	if cache.order.Len() != maxFailures {
		t.Errorf("entries = %d, want %d", cache.order.Len(), maxFailures)
	}
	if cache.contains("big/co0") {
		t.Error("oldest entry not evicted")
	}
	if !cache.contains(fmt.Sprintf("big/co%d", maxFailures)) {
		t.Error("newest entry missing")
	}
}
```

- [ ] **Step 3: Run** `go test ./internal/game/cover` → compile failure.

- [ ] **Step 4: Implement** `cover.go`:

```go
// Package cover serves IGDB cover images from a local cache, fetching a
// cover LANsurfer already knows from the fixed IGDB origin on a miss, so
// player browsers never contact IGDB.
package cover

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"golang.org/x/sync/singleflight"
)

var (
	// ErrNotFound covers invalid input, unknown IDs, and failed fetches.
	ErrNotFound = errors.New("cover not found")
	// ErrBusy means every cold-miss slot is in use.
	ErrBusy = errors.New("cover fetch capacity exhausted")
)

const (
	// MemoryCapacity bounds the in-process store used without a cache directory.
	MemoryCapacity  = 64 << 20
	maxColdMisses   = 8
	coldMissTimeout = 5 * time.Second
)

var imageIDPattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

type Store interface {
	Get(size, id string) ([]byte, bool, error)
	Put(size, id string, image []byte) error
}

type Fetcher interface {
	Fetch(ctx context.Context, size, id string) ([]byte, error)
}

type Catalog interface {
	CoverImageKnown(ctx context.Context, id string) (bool, error)
}

type Service struct {
	store    Store
	fetcher  Fetcher
	catalog  Catalog
	logger   *slog.Logger
	flights  singleflight.Group
	slots    chan struct{}
	timeout  time.Duration
	failures *failureCache
}

func NewService(store Store, fetcher Fetcher, catalog Catalog, logger *slog.Logger) *Service {
	return &Service{
		store: store, fetcher: fetcher, catalog: catalog, logger: logger,
		slots:    make(chan struct{}, maxColdMisses),
		timeout:  coldMissTimeout,
		failures: newFailureCache(time.Now),
	}
}

func validSize(size string) bool { return size == "small" || size == "big" }

// Get returns the cover image bytes, ErrNotFound, ErrBusy, the caller's
// context error, or an internal error.
func (s *Service) Get(ctx context.Context, size, id string) ([]byte, error) {
	if !validSize(size) || !imageIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	key := size + "/" + id
	if image, done, err := s.lookup(size, id, key); done {
		return image, err
	}
	result := s.flights.DoChan(key, func() (any, error) { return s.fill(ctx, size, id, key) })
	select {
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]byte), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) lookup(size, id, key string) ([]byte, bool, error) {
	image, ok, err := s.store.Get(size, id)
	if err != nil {
		return nil, true, fmt.Errorf("read cached cover: %w", err)
	}
	if ok {
		return image, true, nil
	}
	if s.failures.contains(key) {
		return nil, true, ErrNotFound
	}
	return nil, false, nil
}

// fill runs once per key at a time. The shared work is detached from the
// first caller's cancellation and bounded by one deadline instead.
func (s *Service) fill(ctx context.Context, size, id, key string) ([]byte, error) {
	if image, done, err := s.lookup(size, id, key); done {
		return image, err
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()

	known, err := s.catalog.CoverImageKnown(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("look up cover %s: %w", id, err)
	}
	if !known {
		s.failures.add(key)
		return nil, ErrNotFound
	}
	image, err := s.fetcher.Fetch(ctx, size, id)
	if err != nil {
		s.failures.add(key)
		s.logger.Warn("fetch IGDB cover failed", "size", size, "image_id", id, "error", err)
		return nil, ErrNotFound
	}
	if err := s.store.Put(size, id, image); err != nil {
		s.logger.Error("cache IGDB cover failed", "size", size, "image_id", id, "error", err)
	}
	return image, nil
}
```

`failures.go`:

```go
package cover

import (
	"container/list"
	"sync"
	"time"
)

const (
	failureTTL  = 10 * time.Minute
	maxFailures = 4096
)

type failure struct {
	key     string
	addedAt time.Time
}

// failureCache remembers unknown IDs and failed fetches for failureTTL from
// insertion, bounded to maxFailures entries by evicting the oldest.
type failureCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*list.Element
	order   *list.List
}

func newFailureCache(now func() time.Time) *failureCache {
	return &failureCache{now: now, entries: map[string]*list.Element{}, order: list.New()}
}

func (c *failureCache) contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return false
	}
	if c.now().Sub(element.Value.(failure).addedAt) >= failureTTL {
		c.remove(element)
		return false
	}
	return true
}

func (c *failureCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.remove(element)
	}
	if c.order.Len() >= maxFailures {
		c.remove(c.order.Front())
	}
	c.entries[key] = c.order.PushBack(failure{key: key, addedAt: c.now()})
}

func (c *failureCache) remove(element *list.Element) {
	delete(c.entries, element.Value.(failure).key)
	c.order.Remove(element)
}
```

`memory.go`:

```go
package cover

import (
	"container/list"
	"sync"
)

type memoryEntry struct {
	key   string
	image []byte
}

// MemoryStore is a byte-bounded LRU used when no cache directory is set.
// Its contents are lost on restart.
type MemoryStore struct {
	mu       sync.Mutex
	capacity int
	used     int
	entries  map[string]*list.Element
	order    *list.List // front = most recently used
}

func NewMemoryStore(capacity int) *MemoryStore {
	return &MemoryStore{capacity: capacity, entries: map[string]*list.Element{}, order: list.New()}
}

func (m *MemoryStore) Get(size, id string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	element, ok := m.entries[size+"/"+id]
	if !ok {
		return nil, false, nil
	}
	m.order.MoveToFront(element)
	return element.Value.(memoryEntry).image, true, nil
}

func (m *MemoryStore) Put(size, id string, image []byte) error {
	if len(image) > m.capacity {
		return nil
	}
	key := size + "/" + id
	m.mu.Lock()
	defer m.mu.Unlock()
	if element, ok := m.entries[key]; ok {
		m.remove(element)
	}
	for m.used+len(image) > m.capacity {
		m.remove(m.order.Back())
	}
	m.entries[key] = m.order.PushFront(memoryEntry{key: key, image: image})
	m.used += len(image)
	return nil
}

func (m *MemoryStore) remove(element *list.Element) {
	entry := element.Value.(memoryEntry)
	delete(m.entries, entry.key)
	m.used -= len(entry.image)
	m.order.Remove(element)
}
```

`disk.go`:

```go
package cover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DiskStore keeps covers as {dir}/{size}/{id}. It never evicts; operators
// may delete the directory to reset it.
type DiskStore struct {
	dir string
}

func NewDiskStore(dir string) (*DiskStore, error) {
	for _, size := range []string{"small", "big"} {
		if err := os.MkdirAll(filepath.Join(dir, size), 0o750); err != nil {
			return nil, fmt.Errorf("create cover cache directory: %w", err)
		}
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return nil, fmt.Errorf("cover cache directory is not writable: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return &DiskStore{dir: dir}, nil
}

func (d *DiskStore) Get(size, id string) ([]byte, bool, error) {
	image, err := os.ReadFile(filepath.Join(d.dir, size, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return image, true, nil
}

// Put writes through a temp file and rename so a reader never sees a
// partial image. Temp names start with "." and cannot collide with IDs.
func (d *DiskStore) Put(size, id string, image []byte) error {
	temp, err := os.CreateTemp(filepath.Join(d.dir, size), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name()) // no-op after a successful rename
	if _, err := temp.Write(image); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filepath.Join(d.dir, size, id))
}
```

- [ ] **Step 5: Run** `go mod tidy && go test -race ./internal/game/cover` → PASS; `go.mod` lists `golang.org/x/sync` as direct.
- [ ] **Step 6: Commit** `feat(game): add bounded IGDB cover cache service`.

---

### Task 4: Config, HTTP route, wiring, templates, CSP

**Files:**
- Modify: `internal/platform/config/config.go`, `internal/platform/config/config_test.go`
- Create: `internal/web/handler/media.go`
- Modify: `internal/web/router.go`, `internal/web/router_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/web/pages/game_presentation.templ`, `internal/web/pages/games.templ`, `internal/web/pages/game_requests.templ` (+ regenerated `_templ.go`)
- Modify: `internal/web/pages/game_requests_test.go`, `internal/web/pages/games_test.go`
- Modify: `internal/web/middleware/security.go`, `internal/web/middleware/security_test.go`

**Interfaces:**
- Consumes: `cover.NewService`, `cover.NewDiskStore`, `cover.NewMemoryStore`, `cover.MemoryCapacity`, `cover.ErrNotFound`, `cover.ErrBusy` (Task 3); `igdb.NewCoverFetcher` (Task 2); `(*gamepostgres.Store).CoverImageKnown` (Task 1).
- Produces: `config.Config.CoverCacheDir string`; `handler.CoverImages` interface; `web.RouterDependencies.Covers`.

- [ ] **Step 1: Config test** (append to `config_test.go`):

```go
func TestLoadReadsCoverCacheDir(t *testing.T) {
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_COVER_CACHE_DIR", " /var/cache/lansurfer/covers ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CoverCacheDir != "/var/cache/lansurfer/covers" {
		t.Errorf("CoverCacheDir = %q", cfg.CoverCacheDir)
	}
}
```

Implement: add field to `Config` with a comment ("empty means the in-memory store"), read `strings.TrimSpace(os.Getenv("LANSURFER_COVER_CACHE_DIR"))` in `Load`, set it in the returned struct.

- [ ] **Step 2: Router tests** (append to `router_test.go`):

```go
type routerCovers struct {
	image []byte
	err   error
}

func (c routerCovers) Get(ctx context.Context, _, _ string) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.image, ctx.Err()
}

func TestIGDBCoverRoute(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}
	tests := []struct {
		name         string
		covers       routerCovers
		status       int
		cacheControl string
	}{
		{"hit", routerCovers{image: jpeg}, http.StatusOK, "public, max-age=31536000, immutable"},
		{"not found", routerCovers{err: cover.ErrNotFound}, http.StatusNotFound, "no-store"},
		{"busy", routerCovers{err: cover.ErrBusy}, http.StatusServiceUnavailable, "no-store"},
		{"internal", routerCovers{err: errors.New("db down")}, http.StatusInternalServerError, "no-store"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			// No session cookie: the route is public.
			NewRouter(RouterDependencies{Covers: test.covers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
				ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media/igdb/big/co1abc", nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, test.cacheControl)
			}
			if test.status == http.StatusOK && response.Header().Get("Content-Type") != "image/jpeg" {
				t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if test.status == http.StatusServiceUnavailable && response.Header().Get("Retry-After") != "1" {
				t.Errorf("Retry-After = %q, want 1", response.Header().Get("Retry-After"))
			}
		})
	}
}

func TestIGDBCoverRouteWritesNothingForCanceledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Covers: routerCovers{image: []byte("x")}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media/igdb/big/co1abc", nil).WithContext(ctx))
	if response.Body.Len() != 0 {
		t.Errorf("canceled caller received %d body bytes", response.Body.Len())
	}
}
```

Adjust the imports (`cover`, `errors`, `io`, `log/slog`) to what `router_test.go` already has. If existing tests construct the logger through a helper, use that helper instead.

- [ ] **Step 3: Handler** `media.go`:

```go
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/game/cover"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/go-chi/chi/v5"
)

type CoverImages interface {
	Get(ctx context.Context, size, imageID string) ([]byte, error)
}

type Media struct {
	covers CoverImages
}

func NewMedia(covers CoverImages) *Media {
	return &Media{covers: covers}
}

// IGDBCover serves one same-origin IGDB cover. Every failure is a plain
// status the page's existing image fallback handles.
func (h *Media) IGDBCover(w http.ResponseWriter, r *http.Request) {
	image, err := h.covers.Get(r.Context(), chi.URLParam(r, "size"), chi.URLParam(r, "imageID"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case errors.Is(err, cover.ErrNotFound):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, cover.ErrBusy):
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.Context().Err() != nil:
			// The browser is gone; there is nobody to answer.
		default:
			webmiddleware.RequestLogger(r).Error("serve IGDB cover failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(image))
	w.Header().Set("Content-Length", strconv.Itoa(len(image)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(image)
	}
}
```

- [ ] **Step 4: Router** — add `Covers handler.CoverImages` to `RouterDependencies` (comment: "public same-origin IGDB covers"), and register next to `/assets/*`:

```go
	media := handler.NewMedia(dependencies.Covers)
	router.Get("/media/igdb/{size}/{imageID}", media.IGDBCover)
	router.Head("/media/igdb/{size}/{imageID}", media.IGDBCover)
```

- [ ] **Step 5: App wiring** (`app.go`, after `gameStore` is built):

```go
	var coverStore cover.Store = cover.NewMemoryStore(cover.MemoryCapacity)
	if cfg.CoverCacheDir != "" {
		diskStore, err := cover.NewDiskStore(cfg.CoverCacheDir)
		if err != nil {
			return fmt.Errorf("open cover cache: %w", err)
		}
		coverStore = diskStore
	}
	covers := cover.NewService(coverStore, igdb.NewCoverFetcher(), gameStore, logger)
```

and `Covers: covers,` in `RouterDependencies`. Import `internal/game/cover` and `internal/game/igdb`.

- [ ] **Step 6: Templates** — in `game_presentation.templ`:

```go
// providerCoverURL is the same-origin cover proxy URL for the persisted
// cover_image_id (spec: "Covers"). LANsurfer fetches the image from IGDB
// server-side; browsers never contact images.igdb.com.
func providerCoverURL(coverImageID string) string {
	return "/media/igdb/small/" + coverImageID
}

// providerCoverLargeURL is providerCoverURL at the size the Games catalog
// card's cover band needs; t_cover_small visibly blurs when stretched.
func providerCoverLargeURL(coverImageID string) string {
	return "/media/igdb/big/" + coverImageID
}
```

Remove ` referrerpolicy="no-referrer"` from the three cover `<img>` tags (`game_presentation.templ`, `games.templ`, `CoverArt` in `game_requests.templ`). Run `go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate`.

- [ ] **Step 7: CSP** — in `security.go` change `img-src 'self' data: https://images.igdb.com;` to `img-src 'self' data:;`. In `TestSecurityHeaders` add:

```go
	if !strings.Contains(csp, "img-src 'self' data:;") || strings.Contains(csp, "images.igdb.com") {
		t.Errorf("Content-Security-Policy = %q, want same-origin images only", csp)
	}
```

- [ ] **Step 8: Page/router assertions** — replace every `https://images.igdb.com/igdb/image/upload/t_cover_big/co1abc.jpg` in `router_test.go`, `pages/game_requests_test.go` and `pages/games_test.go` with `/media/igdb/big/co1abc`. In `games_test.go`'s cover test add:

```go
	if strings.Contains(body, "images.igdb.com") {
		t.Errorf("rendered page references images.igdb.com: %s", body)
	}
```

- [ ] **Step 9: Run** `go build ./... && task test` → PASS. `grep -rn "images.igdb.com" internal/web --include=*.templ --include=*.go | grep -v _test.go` → no hits.
- [ ] **Step 10: Commit** `feat(web): serve IGDB covers same-origin`.

---

### Task 5: Deployment

**Files:**
- Modify: `Dockerfile`, `compose.yaml`

- [ ] **Step 1: Dockerfile** — in the build stage after `go build`: `RUN mkdir -p /out/covers`; in the runtime stage before `USER`: `COPY --from=build --chown=65532:65532 /out/covers /var/cache/lansurfer/covers`.
- [ ] **Step 2: compose.yaml** — under `lansurfer.environment`: `LANSURFER_COVER_CACHE_DIR: /var/cache/lansurfer/covers`; add `volumes: [cover-cache:/var/cache/lansurfer/covers]` to the service and `cover-cache:` under top-level `volumes`.
- [ ] **Step 3: Verify** `docker compose config -q` succeeds and `docker build -q .` succeeds (skip `docker build` if Docker is unavailable and say so).
- [ ] **Step 4: Commit** `build: persist IGDB cover cache in a compose volume`.

---

### Task 6: Final verification

- [ ] `task lint`
- [ ] `task test`
- [ ] `task test:integration`
- [ ] `task generate` then `git status --short` → no unexpected diff in generated files.
- [ ] Browser suite not required (fallback JS unchanged).
