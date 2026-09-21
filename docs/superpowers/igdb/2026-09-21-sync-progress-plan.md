# IGDB synchronization progress implementation plan

**Date:** 2026-09-21  
**Issue:** #37  
**Design:** [2026-09-21-sync-progress-design.md](2026-09-21-sync-progress-design.md)  
**Status:** ready for coding-agent execution

## Execution handoff

This plan is intentionally the boundary between Webchat planning and coding-agent execution.

For the coding agent:

> Read `AGENTS.md`, the relevant IGDB implementation, and the linked design. Implement this plan end-to-end on the current branch. Do not broaden scope or replace the design with a general progress/job abstraction. Run the listed verification and report any repository evidence that makes a planned step invalid before changing the agreed behavior.

There are no unresolved product or architecture decisions in this plan.

## Task 1 — add service-owned staged progress

**Files**
- `internal/game/provider.go`
- `internal/game/igdb_sync.go`
- `internal/game/igdb_sync_test.go`

**Changes**
1. Add a separate in-progress count to `game.ProviderStatus`; use `SyncRecordCount int64` unless existing naming conventions discovered during implementation strongly require another explicit name.
2. Extend `IGDBSyncService` with a staged-record counter guarded by the existing mutex.
3. Reset the counter when `TryStart` successfully claims a new run.
4. In the catalog callback, increment the counter only after `StoreProviderGames` returns success. The increment must therefore reflect committed staged batches, not merely fetched provider rows.
5. In `Status`, read `running` and the staged count under the same lock. When running, overlay `ProviderSyncing` and the staged count without modifying the repository-provided `RecordCount`.
6. Preserve current completion/failure behavior; no persistence or repository interface change is required.

**Tests**
- status reports staged count 0 at the beginning of a run;
- after one successful batch, status reports that batch size;
- multiple successful batches accumulate;
- a store failure does not add the failed batch;
- a subsequent run begins again at 0;
- repository `RecordCount` remains unchanged while sync progress is overlaid.

Use existing fake catalog/repository patterns in `igdb_sync_test.go` rather than introducing a new mocking framework.

## Task 2 — extract and test the admin status fragment

**Files**
- `internal/web/pages/admin.templ`
- generated templ output produced by `task generate`
- add or extend focused page tests under `internal/web/pages/`

**Changes**
1. Extract the status card from `AdminIGDB` into a reusable component, e.g. `AdminIGDBStatus(status game.ProviderStatus)`.
2. Keep the current `Indexed records` field unchanged.
3. While `status.State == game.ProviderSyncing`, render a distinct **Staged records** field using `status.SyncRecordCount`.
4. While syncing, put HTMX polling attributes on the outer status-card element:
   - GET the dedicated status-fragment route;
   - trigger every 2 seconds;
   - replace the outer element.
5. Do not render those polling attributes when the state is not syncing. This is what terminates polling after the final fragment swap.
6. Preserve the current error box and `Sync now` visibility rules.

**Page tests**
Render the status component directly and assert:
- syncing state contains `Staged records` and the expected numeric value;
- syncing state contains the polling endpoint/trigger;
- non-syncing state omits staged progress and polling attributes;
- `Indexed records` is still rendered independently from staged count.

Do not add browser/E2E coverage for behavior that is fully represented by static HTMX attributes and service tests.

## Task 3 — add the narrow admin fragment endpoint

**Files**
- `internal/web/handler/administration.go`
- `internal/web/router.go`
- focused handler/router tests if the existing test structure provides a low-cost pattern

**Changes**
1. Add an admin-only GET route adjacent to the existing IGDB admin routes, recommended path:
   `/admin/igdb/status`.
2. Add a handler method that:
   - calls `h.providerSync.Status(r.Context())`;
   - renders only the extracted status component on success;
   - on error, logs through the existing web error logger and returns a non-2xx response without returning the full administration layout.
3. Reuse the same `RequireAdmin` and `RequireGameNick` route group as `/admin/igdb`; do not add a second authorization mechanism.

**Tests**
If an established lightweight handler/router pattern exists, cover:
- successful fragment response;
- status error returns non-2xx;
- route remains behind the existing admin middleware.

If adding such a test would require building a broad new harness, rely on the page/service tests plus existing router structure and record that omission in the PR verification notes.

## Task 4 — regenerate and verify

Because `admin.templ` changes, generated output must be committed.

Run, in order:

```sh
task generate
task lint
task test
task test:integration
task build
```

Run `task test:browser` only if implementation changes shared frontend TypeScript or browser interaction code beyond declarative HTMX markup; this design does not require such a change.

Review the generated diff before committing and ensure no unrelated generated changes are included.

## Task 5 — final self-review and PR update

Before marking implementation complete:

- compare the result against every acceptance criterion in the design;
- confirm `RecordCount` still means the active catalog count everywhere;
- confirm no database query/schema/dependency was added solely for progress;
- confirm polling is present only while syncing;
- confirm failed store batches cannot inflate progress;
- update the draft PR with the exact verification commands and results.

After implementation, follow `docs/superpowers/README.md` for artifact lifecycle. If these documents are retained, mark the execution plan historical once the work is merged; otherwise remove the transient plan if it no longer adds durable value.

## Done means

The coding agent can stop when the implementation, generated files, focused tests, and required verification are complete and the draft PR contains no unresolved implementation questions.
