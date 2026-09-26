# IGDB Cover Proxy — Design

Issue: [#45](https://github.com/convention-x-treme/lansurfer/issues/45)
Branch: `feat/issue-45-local-igdb-covers`
Status: approved design, 2026-09-22. The proxy core has since moved to `internal/platform/mediaproxy` and is shared with provider avatars (#64).

## 1. Goal

Player browsers must never contact `images.igdb.com`. LANsurfer serves IGDB covers same-origin:

```
browser -> LANsurfer (/media/igdb/{size}/{imageID}) -> images.igdb.com
```

Success criteria (from #45):

- Player-facing HTML contains no `images.igdb.com` URL.
- Covers are served same-origin.
- Request input cannot influence the upstream host or escape the expected path.
- A cache hit makes no upstream request.
- Missing or failed covers keep the existing fallback UI.
- CSP `img-src` no longer allows `https://images.igdb.com`.

## 2. Approved decisions

| # | Decision |
|---|---|
| Scope | Reverses the "no local cover proxy/cache" decision. Stable 1.0 scope. PRODUCT, ARCHITECTURE, ROADMAP, README, AGENTS.md are updated in the same PR. |
| Storage | `LANSURFER_COVER_CACHE_DIR` set → disk cache in that directory. Unset → in-process LRU with a fixed 64 MiB byte cap. No two-tier cache (the OS page cache already covers hot disk files). Memory cap is not configurable. |
| Allowed IDs | Strict format check **and** the ID must exist in `igdb_game_index.cover_image_id` or `game_igdb_enrichment.cover_image_id`. Lookup runs only on a cache miss. |
| Auth | Public, no session required, like `/assets/*`. A planned anonymous product homepage will show covers. Known-ID checks restrict upstream targets; the negative cache has a fixed entry cap, and concurrent cold-miss work has a fixed limit. These are not a request-rate limit. |
| Sizes | Two: `small` → `t_cover_small`, `big` → `t_cover_big`. Cache key is `(size, imageID)`. |
| Failures | 5 s combined catalog-and-fetch deadline, plus a 5 s upstream client timeout. Unknown IDs and failed fetches are remembered in memory for up to 10 min per key, subject to a 4,096-entry cap, and answered `404` immediately. Failures are never persisted. Concurrent misses for one key share one operation (`singleflight`). |
| Concurrency | At most 8 shared cold-miss operations per process, across both sizes. Each slot covers catalog lookup, fetch, and cache write. When all slots are occupied, a new cold key gets `503` without queuing or negative caching. Cache hits and callers joining an existing flight need no additional slot. |
| IGDB disabled | Proxy keeps working: hits served, misses fetched. Covers are catalog content; no credentials are needed. |
| Disk eviction | None. Operators may delete the directory at any time to reset it. |
| Fallback UI | Unchanged. The existing abbreviation box is shown when the image fails. |

## 3. Components

```
GET /media/igdb/{size}/{imageID}
        │  web/handler/media.go  (public)
        ▼
internal/game/cover.Service.Get(ctx, size, id)
 ├─ validate size ∈ {small,big}, id ∈ ^[a-z0-9]{1,32}$   → ErrNotFound
 ├─ Store.Get(size,id)                                  → hit: return
 ├─ negative cache hit (≤10 min old)                    → ErrNotFound
 └─ singleflight(size/id):
      recheck store and negative cache → return cached result if present
      try to acquire one of 8 slots   → unavailable: ErrBusy
      detached ctx with 5 s deadline (catalog + fetch)
      Catalog.CoverImageKnown(ctx,id) false → remember failure, ErrNotFound
      Fetcher.Fetch(ctx,size,id)  error → remember failure, ErrNotFound
      Store.Put(size,id,bytes)    error → log, still return bytes
      release slot on every exit
```

### `internal/game/cover` (new package)

- `cover.go`
  - `Service` with `Get(ctx, size, id string) ([]byte, error)` returning image bytes, `ErrNotFound`, `ErrBusy`, or the caller's context error; other errors are internal failures.
  - Consumer-owned interfaces:
    - `Store { Get(size, id string) ([]byte, bool, error); Put(size, id string, image []byte) error }`
    - `Fetcher { Fetch(ctx, size, id string) ([]byte, error) }`
    - `Catalog { CoverImageKnown(ctx, id string) (bool, error) }`
  - Validation: `size` is exactly `small` or `big`; `id` matches `^[a-z0-9]{1,32}$`. Validation runs before any store, catalog, or fetch access, so no disk path or upstream URL is ever built from an unvalidated value.
  - Negative cache: a mutex-guarded map plus insertion-order list, with an injectable clock and a fixed cap of 4,096 entries, including unknown IDs. Entries expire 10 min after insertion; reads do not extend expiry. Remove expired entries on lookup, and evict the oldest entry when inserting at capacity. An evicted key may be retried before 10 min. No cleanup goroutine or configurable cap is needed.
  - `singleflight.Group` (`golang.org/x/sync`, already an indirect dependency, becomes direct). Use `DoChan` so each caller can select between the shared result and its own `ctx.Done()`; caller cancellation returns its context error without canceling the shared work or adding a negative entry.
  - Recheck both the store and negative cache inside the shared function before admission or catalog access. A caller that observed an earlier miss but enters after a completed flight must use that flight's cached result instead of fetching again.
  - Admit shared cold-miss work through a nonblocking buffered-channel semaphore with 8 slots. Acquire only inside the shared function after the cache rechecks; release with `defer`, including on errors. A full semaphore returns `ErrBusy` immediately, without catalog/fetch access or negative caching. This bounds active cold-miss work without building a waiting queue; it does not enforce requests per second.
  - After admission, derive `context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)` and defer cancellation. Pass this context to both catalog lookup and fetch, so database pool waits and queries share the deadline with the upstream request. A client disconnect does not fail other waiters. The fetcher's own 5 s timeout remains a backstop. Ordinary filesystem operations have no context cancellation; the deadline bounds catalog and network work, not disk I/O.
  - `Catalog.CoverImageKnown` errors, including deadline expiry during lookup, are returned as internal errors and are **not** negative-cached (a DB blip must not hide covers for 10 min). Fetch failures, including deadline expiry during fetch, use the bounded negative cache.
- `memory.go`: LRU over `container/list` + map, byte-bounded at 64 MiB, mutex-guarded. An image larger than the cap is not stored.
- `disk.go`: files at `{dir}/{size}/{id}`. `Put` writes to a temp file in the same directory and renames it (atomic). Size directories are created on construction. `NewDiskStore` verifies the directory is writable and returns an error otherwise.

The content type is not stored. The handler sniffs it with `http.DetectContentType` (JPEG, PNG and WebP are recognized).

### `internal/game/igdb/cover.go`

Upstream fetcher, provider-adapter code next to the IGDB API client.

- URL built only from constants plus validated input: `https://images.igdb.com/igdb/image/upload/t_cover_{small|big}/{id}.jpg`. The size maps through a fixed table; an unknown size is an error.
- Own `http.Client`: `Timeout: 5 * time.Second`, `CheckRedirect` returns `http.ErrUseLastResponse` (so a redirect is a non-200 failure).
- Accept only status `200`.
- Read through `io.LimitReader(body, 1<<20 + 1)`; more than 1 MiB is a failure.
- Accept only when `http.DetectContentType` of the body is `image/jpeg`, `image/png`, or `image/webp` (the upstream `Content-Type` header is not trusted alone).
- The origin is a package constant. Tests use an unexported constructor that takes the base URL.

### `internal/game/postgres`

- New sqlc query `CoverImageKnown(id) bool`:
  ```sql
  SELECT EXISTS (SELECT 1 FROM igdb_game_index WHERE cover_image_id = $1)
      OR EXISTS (SELECT 1 FROM game_igdb_enrichment WHERE cover_image_id = $1);
  ```
- Store method `CoverImageKnown(ctx, id) (bool, error)` satisfies `cover.Catalog`.

### Migration `00013_igdb_cover_index.sql`

```sql
-- +goose Up
CREATE INDEX igdb_game_index_cover_image_id_idx
    ON igdb_game_index (cover_image_id) WHERE cover_image_id IS NOT NULL;
-- +goose Down
DROP INDEX igdb_game_index_cover_image_id_idx;
```

`game_igdb_enrichment` holds only materialized Games (small); no index needed.

### `internal/platform/config`

- `CoverCacheDir string` from `LANSURFER_COVER_CACHE_DIR`, trimmed. Empty means memory store.

### `internal/app` (composition root)

- `CoverCacheDir != ""` → `cover.NewDiskStore(dir)`; a failure is fatal at startup. Otherwise `cover.NewMemoryStore(64 << 20)`.
- Build `igdb.NewCoverFetcher()`, the postgres store as `Catalog`, and `cover.NewService(store, fetcher, catalog)`; pass it to the router dependencies.

## 4. HTTP

Public routes registered next to `/assets/*`, outside the authenticated group (no session lookup):

```
GET  /media/igdb/{size}/{imageID}
HEAD /media/igdb/{size}/{imageID}
```

| Case | Status | Headers |
|---|---|---|
| cache hit or successful fetch | `200` | `Content-Type` (sniffed), `Cache-Control: public, max-age=31536000, immutable` |
| invalid size/ID, unknown ID, negative-cache hit, upstream failure | `404` | `Cache-Control: no-store` |
| cold-miss concurrency limit reached | `503` | `Cache-Control: no-store`, `Retry-After: 1` |
| catalog or store read error | `500` (logged) | `Cache-Control: no-store` |

`immutable` assumes IGDB image IDs are content-stable (a new cover gets a new ID). No ETag/conditional handling; add it only if a need appears. The global `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer` headers stay.

The handler contains only transport concerns: path params → `Service.Get` → error translation → write bytes. If the caller's request context ends while waiting, return without writing an image or treating the cancellation as a catalog failure. A `503` triggers the existing image fallback; no automatic browser retry is introduced.

## 5. Templates and CSP

- `providerCoverURL(id)` → `/media/igdb/small/{id}`; `providerCoverLargeURL(id)` → `/media/igdb/big/{id}`. Update their doc comments.
- Remove `referrerpolicy="no-referrer"` from cover `<img>` tags (same-origin now; global header remains).
- Keep `data-cover-fallback="true"` and the existing fallback markup/JS (`web/src/app.ts`) unchanged. A `404` triggers the same `error` handler.
- CSP (`internal/web/middleware/security.go`): `img-src 'self' data:`.
- The service worker has no fetch handler; no change.

## 6. Deployment

- `compose.yaml`: add a named volume `cover-cache` mounted at `/var/cache/lansurfer/covers` and `LANSURFER_COVER_CACHE_DIR: /var/cache/lansurfer/covers`.
- `Dockerfile`: the runtime image is `scratch` as UID `65532`. Create the empty directory in the build stage and `COPY --from=build --chown=65532:65532` it to `/var/cache/lansurfer/covers`, so Docker seeds a new named volume with the correct owner.

## 7. Canonical document changes

- **PRODUCT §5 "Explicitly not planned"**: remove "local caching or proxying of remote covers".
- **PRODUCT §9 "Synchronized provider index"**: replace "Do not mirror cover image bytes or the full provider graph." with "Do not mirror the full provider graph or bulk-download cover images; cover bytes are cached on demand only for cover IDs LANsurfer already knows."
- **PRODUCT §9 "Covers and attribution"**: covers are served same-origin through LANsurfer; player browsers never contact IGDB. The server contacts `images.igdb.com` on a cache miss. Replace the viewer-IP disclosure with that server-side disclosure. Keep attribution and the local placeholder.
- **PRODUCT §14**: "remote IGDB-cover exception" → "IGDB cover proxy".
- **ARCHITECTURE §15**: add `LANSURFER_COVER_CACHE_DIR` and one sentence on disk vs memory.
- **ARCHITECTURE §17**: bullet "do not mirror … cover image bytes" → on-demand cache only; replace the remote-content-exception paragraph with the bounded proxy contract (fixed origin, size enum, ID regex, known-ID check, combined 5 s catalog-and-fetch deadline, 1 MiB / image-type limits, no redirects, 4,096-entry negative cache, 8 concurrent cold-miss operations with `503` on saturation, disk-or-memory store, no generic remote-asset proxy).
- **ARCHITECTURE §19**: "the IGDB cover host stays the only remote-content exception" → "there is no remote-content exception in CSP".
- **ARCHITECTURE §21**: "fixed CSP allowlisting for the IGDB cover exception" → "server-constructed IGDB cover fetch; no third-party requests from player browsers".
- **ROADMAP Track C "Covers"** and its verification evidence: proxy limits, cache hit without upstream request, CSP without IGDB, fallback.
- **ROADMAP Track C provider-index storage**: replace the separate "Do not mirror the full provider graph or cover image bytes" statement with the same on-demand-only cover cache rule as PRODUCT §9.
- **ROADMAP Track G "Shared UI platform"**: replace the "remote-cover exception" wording with the same-origin catalog proxy and retain the prohibition on remote brand assets or a generic remote-asset proxy.
- **ROADMAP 1.0 Integration Gate**: replace the "remote-cover exception" check with verification that the same-origin IGDB cover proxy coexists with the manifest/service worker and that CSP has no third-party IGDB image allowance.
- **ROADMAP "Explicitly not planned"**: remove "local caching/proxying of remote covers" because the bounded IGDB cover proxy/cache is now approved stable-1.0 scope.
- **README**: document `LANSURFER_COVER_CACHE_DIR`, the compose volume, and that deleting the directory resets the cache.
- **AGENTS.md architecture guardrails**: replace the IGDB cover host's remote-content exception with same-origin covers fetched server-side from the fixed IGDB origin; retain embedded production assets and the prohibition on a generic remote-asset proxy.

## 8. Testing

Close to owning code; fakes only at the three `cover` interfaces and `httptest` for upstream.

- **`internal/game/cover`**
  - Validation table: `../x`, `%2f`, `CO1ABC`, `""`, 33 chars, `co1.abc`, size `medium` → `ErrNotFound` with zero store/catalog/fetch calls.
  - Hit: no catalog or fetch call.
  - Miss: known → fetched → stored → returned; unknown → `404`, no fetch.
  - Concurrency: N goroutines on one cold key → exactly one fetch.
  - Delayed caller: pause after the initial cache miss, let another flight finish, then resume → inner recheck returns the positive or negative result without a second catalog lookup or fetch. Use synchronization barriers rather than sleeps.
  - Admission: block 8 distinct cold keys in catalog/fetch work; a ninth gets `ErrBusy` with zero catalog/fetch calls. Cache hits and callers joining an admitted key still succeed. Completion or failure releases the slot; retrying a rejected key is not negative-cached.
  - Cancellation: cancel one caller while another waits on the same key → canceled caller returns promptly, other caller receives the image, exactly one fetch occurs.
  - Deadline: a catalog fake blocked on `ctx.Done()` exits at the shared deadline without fetching or negative caching; a fetch fake observes the same deadline after catalog work has consumed part of the budget. Use an unexported test timeout override for fast tests, not a runtime setting.
  - Negative cache: failure → next call no fetch; after 10 min (fake clock) → refetch. Catalog error is not remembered. More than 4,096 distinct unknown IDs never exceed the entry cap; oldest entries are evicted and may be retried, and expiry works without extending TTL on reads.
  - Store write error → bytes still returned.
  - Memory store evicts least-recently-used entries at the byte cap; oversize image not stored.
  - Disk store round-trip; missing file → miss; unwritable directory → constructor error.
- **`internal/game/igdb`** (`httptest.Server`): exact request path per size; `302` rejected; non-`200` rejected; HTML body rejected; >1 MiB rejected; slow handler times out.
- **Integration (PostgreSQL)**: `CoverImageKnown` true for an index ID, true for an enrichment-only ID, false for unknown.
- **HTTP (`router_test.go`)**: `200` with headers for a cached cover; `404` + `no-store` for invalid/unknown; `503` + `no-store` + `Retry-After: 1` on saturation; canceled waiter writes no image; anonymous request (no session cookie) gets `200`; CSP header has no `images.igdb.com`.
- **Pages**: existing assertions switch to `/media/igdb/{small|big}/{id}`; add an assertion that rendered player-facing pages contain no `images.igdb.com`.
- No browser test: fallback JS is unchanged.

## 9. Out of scope

- A generic remote-asset proxy or other providers.
- Requests-per-second rate limiting of upstream fetches (deferred pending team discussion; a global token bucket on cache misses was the proposed option). The fixed concurrency limit is in scope.
- Disk eviction or size caps, cache stats/admin UI, prefetching/bulk warm-up.
- ETag/conditional requests.
- Configurable memory cap, negative-cache entry cap, concurrency limit, timeout, TTL, or size limit.
