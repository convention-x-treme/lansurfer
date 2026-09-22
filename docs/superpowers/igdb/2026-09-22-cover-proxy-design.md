# IGDB Cover Proxy — Design

Issue: [#45](https://github.com/convention-x-treme/lansurfer/issues/45)
Branch: `feat/issue-45-local-igdb-covers`
Status: approved design, 2026-09-22

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
| Scope | Reverses the "no local cover proxy/cache" decision. Stable 1.0 scope. PRODUCT, ARCHITECTURE, ROADMAP, README are updated in the same PR. |
| Storage | `LANSURFER_COVER_CACHE_DIR` set → disk cache in that directory. Unset → in-process LRU with a fixed 64 MiB byte cap. No two-tier cache (the OS page cache already covers hot disk files). Memory cap is not configurable. |
| Allowed IDs | Strict format check **and** the ID must exist in `igdb_game_index.cover_image_id` or `game_igdb_enrichment.cover_image_id`. Lookup runs only on a cache miss. |
| Auth | Public, no session required, like `/assets/*`. A planned anonymous product homepage will show covers. Abuse is bounded by the known-ID check, negative cache, and `singleflight`. |
| Sizes | Two: `small` → `t_cover_small`, `big` → `t_cover_big`. Cache key is `(size, imageID)`. |
| Failures | 5 s upstream timeout. Failed fetches are remembered in memory for 10 min per key and answered `404` immediately. Failures are never persisted. Concurrent misses for one key share one upstream fetch (`singleflight`). |
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
 └─ singleflight(size/id), detached ctx:
      Catalog.Known(ctx,id)       false → remember failure, ErrNotFound
      Fetcher.Fetch(ctx,size,id)  error → remember failure, ErrNotFound
      Store.Put(size,id,bytes)    error → log, still return bytes
```

### `internal/game/cover` (new package)

- `cover.go`
  - `Service` with `Get(ctx, size, id string) ([]byte, error)` returning image bytes or `ErrNotFound`; other errors are internal failures.
  - Consumer-owned interfaces:
    - `Store { Get(size, id string) ([]byte, bool, error); Put(size, id string, image []byte) error }`
    - `Fetcher { Fetch(ctx, size, id string) ([]byte, error) }`
    - `Catalog { Known(ctx, id string) (bool, error) }`
  - Validation: `size` is exactly `small` or `big`; `id` matches `^[a-z0-9]{1,32}$`. Validation runs before any store, catalog, or fetch access, so no disk path or upstream URL is ever built from an unvalidated value.
  - Negative cache: `map[key]time.Time` behind a mutex with an injectable clock; entries older than 10 min are ignored and overwritten. Expired entries are deleted lazily on lookup. `ponytail:` comment names the ceiling (unbounded between lookups; bounded in practice by known IDs × 2 sizes).
  - `singleflight.Group` (`golang.org/x/sync`, already an indirect dependency, becomes direct). The shared fetch runs with `context.WithoutCancel(ctx)` so one client disconnect does not fail the other waiters; the fetcher's 5 s timeout bounds it.
  - `Catalog.Known` errors are returned as internal errors and are **not** negative-cached (a DB blip must not hide covers for 10 min).
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
| catalog or store read error | `500` (logged) | `Cache-Control: no-store` |

`immutable` assumes IGDB image IDs are content-stable (a new cover gets a new ID). No ETag/conditional handling; add it only if a need appears. The global `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer` headers stay.

The handler contains only transport concerns: path params → `Service.Get` → error translation → write bytes.

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
- **ARCHITECTURE §17**: bullet "do not mirror … cover image bytes" → on-demand cache only; replace the remote-content-exception paragraph with the bounded proxy contract (fixed origin, size enum, ID regex, known-ID check, 5 s / 1 MiB / image-type limits, no redirects, negative cache, disk-or-memory store, no generic remote-asset proxy).
- **ARCHITECTURE §19**: "the IGDB cover host stays the only remote-content exception" → "there is no remote-content exception in CSP".
- **ARCHITECTURE §21**: "fixed CSP allowlisting for the IGDB cover exception" → "server-constructed IGDB cover fetch; no third-party requests from player browsers".
- **ROADMAP Track C "Covers"** and its verification evidence: proxy limits, cache hit without upstream request, CSP without IGDB, fallback.
- **README**: document `LANSURFER_COVER_CACHE_DIR`, the compose volume, and that deleting the directory resets the cache.

## 8. Testing

Close to owning code; fakes only at the three `cover` interfaces and `httptest` for upstream.

- **`internal/game/cover`**
  - Validation table: `../x`, `%2f`, `CO1ABC`, `""`, 33 chars, `co1.abc`, size `medium` → `ErrNotFound` with zero store/catalog/fetch calls.
  - Hit: no catalog or fetch call.
  - Miss: known → fetched → stored → returned; unknown → `404`, no fetch.
  - Concurrency: N goroutines on one cold key → exactly one fetch.
  - Negative cache: failure → next call no fetch; after 10 min (fake clock) → refetch. Catalog error is not remembered.
  - Store write error → bytes still returned.
  - Memory store evicts least-recently-used entries at the byte cap; oversize image not stored.
  - Disk store round-trip; missing file → miss; unwritable directory → constructor error.
- **`internal/game/igdb`** (`httptest.Server`): exact request path per size; `302` rejected; non-`200` rejected; HTML body rejected; >1 MiB rejected; slow handler times out.
- **Integration (PostgreSQL)**: `CoverImageKnown` true for an index ID, true for an enrichment-only ID, false for unknown.
- **HTTP (`router_test.go`)**: `200` with headers for a cached cover; `404` + `no-store` for invalid/unknown; anonymous request (no session cookie) gets `200`; CSP header has no `images.igdb.com`.
- **Pages**: existing assertions switch to `/media/igdb/{small|big}/{id}`; add an assertion that rendered player-facing pages contain no `images.igdb.com`.
- No browser test: fallback JS is unchanged.

## 9. Out of scope

- A generic remote-asset proxy or other providers.
- Rate limiting of upstream fetches (deferred pending team discussion; a global token bucket on cache misses was the proposed option).
- Disk eviction or size caps, cache stats/admin UI, prefetching/bulk warm-up.
- ETag/conditional requests.
- Configurable memory cap, timeout, TTL, or size limit.
