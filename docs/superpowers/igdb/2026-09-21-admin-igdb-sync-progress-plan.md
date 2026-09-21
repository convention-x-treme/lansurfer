# Admin IGDB Sync Progress Signal

Status: Implementation plan
Date: 2026-09-21
Issue: [#37](https://github.com/convention-x-treme/lansurfer/issues/37)
Track: C follow-up (operator UX). Does not change provider/index architecture.

## Problem

While a sync is `syncing`, `/admin/igdb` shows only `RecordCount`, which is
`igdb_sync_generations.record_count` of the **active** generation and is
therefore written only by `ActivateIGDBGeneration`. During a full baseline the
page sits at "Indexed records: 0" for minutes even though `igdb_game_index` for
the staging generation is growing, and nothing on the page ever refreshes. An
admin cannot distinguish progress from a hang.

Two gaps, one fix each:

1. No read model for the in-progress generation's staged row count.
2. No automatic refresh while `State == ProviderSyncing`.

## Design

### 1. Staged count on the existing status read model

Add `StagedRecords int64` to `game.ProviderStatus`: rows currently staged into
the in-progress generation, `0` whenever no generation is in progress. It is
deliberately a separate field from `RecordCount` so `RecordCount` keeps its
existing product meaning ("records in the last successful baseline").

Extend the existing `IGDBProviderStatus` query instead of adding a second one, so
one round trip still serves the whole admin status:

```sql
(SELECT count(*)
 FROM igdb_sync_generations attempt
 JOIN igdb_game_index staged ON staged.generation_id = attempt.id
 WHERE attempt.id = state.latest_attempt_generation_id
   AND attempt.status = 'syncing') AS staged_record_count
```

- `latest_attempt_generation_id` is the only pointer to the in-progress
  generation; `BeginIGDBGeneration` sets it and `ActivateIGDBGeneration`/
  `FailIGDBGeneration` make the row terminal, so a terminal attempt yields `0`.
- Cost is a `count(*)` over one generation's rows, served by the existing
  `igdb_game_index` primary key `(generation_id, igdb_id)`. At ~400k rows this is
  a bounded index scan, read-only and MVCC-safe against the concurrent inserts
  (no lock, no partial-transaction visibility problem: staged rows are committed
  per batch).
- If this ever measures hot, the upgrade path is a counter column maintained in
  the same statement as `StoreIGDBProviderGame` (`ponytail:` comment on the
  query), not a second source of truth.

`game.ProviderStatus.StagedRecords` is DB truth and is *not* gated on the
service's in-process `running` flag. `IGDBSyncService.Status` keeps only its
existing `ProviderSyncing` overlay. The page renders `StagedRecords` only while
`State == ProviderSyncing`, so the DB value and the displayed value cannot
disagree in a user-visible way.

### 2. Self-terminating htmx polling on the status card

Extract the existing status `<section class="detail-card">` from `AdminIGDB`
into a component `AdminIGDBStatus(status game.ProviderStatus) templ` with a
stable `id="igdb-status"`. When `status.State == game.ProviderSyncing` that
component carries:

```html
hx-get="/admin/igdb/status" hx-trigger="every 2s" hx-swap="outerHTML"
```

New fragment route `GET /admin/igdb/status` (inside the existing
`RequireAdmin` + `RequireGameNick` admin group, same `dependencies.ProviderSync
!= nil` guard as `/admin/igdb`) returns that component with no layout.

Why this stops by itself: the response replaces the element carrying the
polling attributes. Once the sync ends, the re-rendered component has no
`hx-trigger`, so htmx drops the poll. No JS, no new asset, no broker change.

Rendering during `syncing` (replaces the current always-on "Indexed records"
row set only for that state):

- Status badge, Last attempt, Last success, Indexed records: unchanged.
- New row **Staged in this sync**: `strconv.FormatInt(status.StagedRecords, 10)`.
- `aria-live="polite"` on the card while syncing so the count change is
  announced where the platform supports live-region insertion; the page remains
  fully correct without it.

`Sync now` keeps its current condition (absent while `syncing`), so the fragment
never needs to re-render the button.

### Rejected alternatives

- **SSE stream like `/events/game-requests`.** Correct but disproportionate:
  it needs a broker, a new app event kind, a TypeScript handler, and browser
  regression coverage for a single-admin status card. Polling is the pattern the
  issue itself lists as acceptable.
- **Poll `/admin/igdb` with `hx-select="#igdb-status"`.** No new route, but each
  2s tick re-runs the full page render including the sidebar's navigation
  queries. A dedicated fragment is smaller than the load it avoids.
- **JS timer / `setTimeout` in `app.ts`.** More code than one attribute, and
  `app.ts` is scoped to progressive enhancement of player surfaces.
- **Adding a `record_count`-style counter column to `igdb_sync_generations`.** A
  second write path for a value `count(*)` already derives correctly.

## Changes

| File | Change |
| --- | --- |
| `internal/game/postgres/queries/igdb.sql` | Add `staged_record_count` to `IGDBProviderStatus`, with a comment stating the generation it reads and the counter-column upgrade path. |
| `internal/game/postgres/sqlcgen/igdb.sql.go` | Regenerated (`task generate`). Do not hand-edit. |
| `internal/game/provider.go` | Add `StagedRecords int64` to `ProviderStatus` with its meaning in the comment. |
| `internal/game/postgres/igdb_store.go` | Map `row.StagedRecordCount` in `Status`. |
| `internal/web/pages/admin.templ` | Extract `AdminIGDBStatus(status)` from `AdminIGDB`; render the staged row and polling attributes while syncing. |
| `internal/web/handler/administration.go` | Add `IGDBStatus` (fragment render) next to `IGDB`. |
| `internal/web/router.go` | Add `admin.Get("/igdb/status", administrationHandler.IGDBStatus)` beside the existing `/igdb` routes. |

No interface, composition-root, schema, or module-boundary change.
`ProviderSyncAdministration` (`Status`, `TryStart`) already carries everything
needed.

## Tests

1. `internal/platform/postgres/postgres_integration_test.go` — new
   `TestIGDBStatusReportsStagedRecords` (integration tag):
   - after `BeginGeneration` + `StoreProviderGames` of N records:
     `StagedRecords == N` and `RecordCount` still reflects the previously
     activated generation (0 on a first sync);
   - after `ActivateGeneration`: `StagedRecords == 0`, `RecordCount == N`;
   - after a second `BeginGeneration` with M staged rows while generation one
     stays active: `StagedRecords == M`, `RecordCount == N`.
   This is the real invariant — staged count reads the in-progress generation,
   never the active one, and is not confused with the last completed baseline.
2. `internal/web/router_test.go` — extend the existing
   `routerProviderSync`-based admin tests:
   - `State: ProviderSyncing` with `StagedRecords: 1234` renders the count,
     `hx-get="/admin/igdb/status"`, and `hx-trigger="every 2s"`;
   - `State: ProviderCurrent` renders no polling attributes and no staged row;
   - `GET /admin/igdb/status` as a non-admin is still 404/redirect as the
     existing `TestAdminIGDBRoutesRequireAdmin` asserts for the group, and as an
     admin returns the fragment without the layout (assert absence of the
     topbar/sidebar markup).

No browser test: the polling attribute is declarative htmx and the new fragment
route is a plain GET; `web/tests` stays scoped to interaction behavior Go tests
cannot reach.

## Docs

- `docs/PRODUCT.md` §9 Synchronized provider index: add one sentence — while a
  synchronization is running, administrators see how many records have been
  staged so far, refreshed automatically, and the last successful baseline's
  record count keeps being reported separately.
- `docs/ARCHITECTURE.md` §17: extend the administrator/operator surface
  paragraph with the staged-count read (`latest_attempt_generation_id`, joined
  to `igdb_game_index` for the `syncing` generation) and the fragment route the
  admin page polls while `syncing`.
- `docs/ROADMAP.md` Track C row: drop "issue #37 is an operator-UX follow-up"
  once merged.

## Verification

```sh
task generate
task lint
task test
task test:integration
```

Manual proof against the Compose stack: start a sync from `/admin/igdb` with a
clean database, leave the page open, and observe "Staged in this sync" climb
without a reload, then freeze at the activated value while "Indexed records"
switches to the same number and the polling stops.

## Risks and trade-offs

- **Polling cadence vs DB load.** One `count(*)` per open admin page per 2s,
  bounded by the generation's index scan. Only while `syncing`.
- **No percentage.** The total is unknown until the upstream scan finishes, so
  the signal is a monotonic count, not a progress bar. This matches what the
  issue asks for and avoids fabricating a denominator from a stale record count.
- **`aria-live` on a swapped node** is announced inconsistently across screen
  readers; the count is supplementary and the page stays correct without it.