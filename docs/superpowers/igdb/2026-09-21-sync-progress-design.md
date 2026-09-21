# IGDB synchronization progress design

**Date:** 2026-09-21  
**Issue:** #37 — Admin IGDB sync page gives no live progress signal while syncing  
**Status:** implementation-ready design

## Context

The IGDB administration page currently shows the state of the last persisted provider baseline. During a full synchronization, the page changes to `Syncing` but `Indexed records` continues to report the record count of the last successfully activated generation. The page also does not refresh itself.

That is semantically correct for the active catalog, but operationally ambiguous: an administrator cannot distinguish a healthy multi-minute import from a stalled one.

LANsurfer 1.0 is single-instance, and `IGDBSyncService` already owns the in-process exclusivity of a running synchronization. A running synchronization therefore does not need a new durable progress model.

## Goals

- Show visible progress while a synchronization is running.
- Keep `Indexed records` meaning “records in the last successfully activated catalog”.
- Stop live refresh automatically when the synchronization finishes or fails.
- Keep the implementation inside the existing IGDB service/admin presentation boundaries.
- Avoid new infrastructure, schema, dependencies, or a general job/progress framework.

## Non-goals

- Percentage-complete or ETA estimates; IGDB does not provide a reliable total in the current flow.
- Persisting per-batch progress across process restarts.
- Changing generation activation, failure, or archive semantics.
- Adding a general background-job subsystem or new realtime/SSE infrastructure.
- Changing player-facing discovery behavior.

## Decision 1: progress is service-owned, not database-counted

Add a dedicated in-progress count to the provider status exposed by `IGDBSyncService`.

The service already knows exactly when a batch has been durably staged: `StoreProviderGames` returns only after its transaction commits. After each successful call, increment an in-memory staged-record counter by the batch size.

Guard the running flag and staged count with the service's existing mutex.

Behavior:

- Reset the staged count to zero when a new sync starts.
- Increment only after a batch is successfully stored.
- While `running == true`, `Status` overlays:
  - `State = ProviderSyncing`
  - the current staged-record count.
- Do not overwrite `ProviderStatus.RecordCount`; it remains the last active generation's count.
- When the sync ends, the persisted repository status again becomes authoritative.

### Why not `COUNT(*)` on every poll?

The staged rows are already known to the service batch-by-batch. Counting the current generation repeatedly would scan an ever-growing provider index only to reconstruct information the sync loop already has. Process-local state is sufficient because the running sync itself is process-local in the approved single-instance architecture.

## Decision 2: expose a separate operator-facing field

Extend `game.ProviderStatus` with a clearly named in-progress field, for example:

`SyncRecordCount int64`

Do not reuse `RecordCount`, because the existing field is the count of the last successfully activated catalog and is also consumed outside the admin progress presentation.

The admin UI should label the new value **Staged records** (or equivalently explicit wording that means “current sync”), and render it only while `State == ProviderSyncing`.

A first sync therefore reads naturally:

- State: Syncing
- Indexed records: 0
- Staged records: 123456

A refresh with an existing baseline can show both the still-active catalog and the new staged generation without conflating them.

## Decision 3: poll a narrow status fragment with HTMX

Add a small authenticated-admin GET endpoint for the IGDB status fragment, adjacent to the existing `/admin/igdb` route.

Extract the existing status card into a reusable templ component. While the state is `ProviderSyncing`, the component polls the fragment endpoint every 2 seconds and swaps itself with the returned fragment.

When the next response is no longer syncing, the returned component has no polling trigger, so refresh stops automatically.

Prefer a narrow fragment endpoint over repeatedly fetching the full administration page because the full page also resolves navigation and layout state that do not change with sync progress.

A fragment-load failure should return a non-2xx response and leave the last successfully rendered status card in place; the server should log the error through the existing web error logging path.

## UX behavior

The status card continues to show:

- state;
- last attempt;
- last success;
- indexed records from the active generation;
- last error when present;
- `Sync now` only when synchronization is allowed and not already running.

While syncing it additionally shows:

- staged records for the current run;
- automatic 2-second refresh.

Do not display a progress bar or percentage.

## Compatibility and boundaries

This change:

- does not alter database schema or sqlc queries;
- does not add dependencies;
- does not change product scope or domain terminology;
- does not alter the active-generation contract;
- does not create a new module or cross-module write;
- stays compatible with the existing server-rendered templ/HTMX architecture.

## Acceptance criteria

1. While a sync is running, the admin page shows a staged-record count that increases after committed batches.
2. `Indexed records` continues to represent the last successful active generation.
3. The status fragment refreshes automatically while syncing and stops after completion/failure.
4. A first-ever sync can visibly progress from 0 staged records without an active baseline.
5. A failed batch does not falsely increment staged progress.
6. Starting a later sync resets staged progress to zero.
7. No database migration, new runtime service, or new direct dependency is introduced.
