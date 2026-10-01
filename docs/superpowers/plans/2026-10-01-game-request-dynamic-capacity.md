# Game Request Dynamic Participation and Capacity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Issue #101 so `playing` Game Requests continue to support safe Join/Leave and Host-controlled dynamic capacity, with durable before/after capacity history, Discord synchronization, no Web Push, and unchanged Trending semantics.

**Architecture:** Extend the existing Game Request row-lock transaction boundary instead of adding a new subsystem. Join, Leave, and Capacity changes serialize on the same locked `game_requests` row; a capacity change writes a typed `capacity_changed` activity row in the same transaction and returns whether a real change occurred so the service can suppress no-op SSE events.

**Tech Stack:** Go, PostgreSQL, pgx, sqlc, goose, chi, templ/HTMX, SSE, Playwright, existing notification/Discord infrastructure.

**Spec:** `docs/superpowers/specs/2026-10-01-game-request-dynamic-capacity-design.md`

## Global Constraints

- Work on branch `feat/issue-101-dynamic-capacity`.
- Implement #101 only. Do not implement session information (#102), secrets (#103), or chat (#104).
- Active participation statuses are exactly `open` and `playing`; Join and Leave remain rejected for `closed` and `cancelled`.
- Joinability is derived only from lifecycle state, current membership, and optional `max_players`. Do not add an accepting-players toggle, waitlist, approval flow, Host transfer, teams, slots, or roles.
- `max_players = NULL` means unlimited. Browser input `0` or blank maps to `NULL`.
- New or edited finite limits must be `2..999`. Preserve existing rows with legacy `max_players = 1`; do not backfill or normalize them.
- The Host may change capacity only while `open` or `playing`, and never below current membership.
- A semantically identical capacity submission is a no-op: no request write, no activity row, no SSE event, no Discord work.
- Add durable `capacity_changed` activity with typed nullable `previous_max_players` and `new_max_players`; `NULL` means unlimited.
- `capacity_changed` updates request Activity history and triggers the existing synchronized Discord message.
- `capacity_changed` creates no personal Web Push deliveries and is not added to the service-worker PushKind wire vocabulary.
- `capacity_changed` does not count toward Trending.
- Keep `game_request_activity` as both request-local history and the transactional notification handoff. Do not add a second outbox.
- Join, Leave, lifecycle transitions, and Capacity use the existing Game Request row lock as the consistency boundary.
- Do not introduce triggers, advisory locks, Redis, a broker, WebSockets, a SPA, a generic activity JSON payload, a capacity submodule, or new runtime dependencies.
- Generated sqlc, templ, and frontend asset files must be produced with `task generate`; never hand-edit generated files.
- Never modify the developer's `.env` or other gitignored local configuration.
- Keep each task green and commit it separately with the commit message listed in that task.
- Canonical docs record durable product/architecture truth; GitHub issues/PRs record status and validation evidence.
- Remove the temporary spec and plan only in the final cleanup task after all verification passes.

## Review Focus

- **Legacy finite limit `1`:** an existing row with `max_players = 1` remains readable and migratable; editing it to `1` is rejected, while editing it to unlimited or `>=2` succeeds.
- **Join vs capacity race:** concurrent Join and limit reduction can resolve in either lock order but must never commit member count above a finite maximum.
- **NULL metadata semantics:** `NULL` means unlimited for capacity history; unrelated activity kinds must keep both metadata columns `NULL`, and unlimited-to-unlimited must never create a `capacity_changed` row.
- **Unauthorized/inactive edits:** non-Host, `closed`, and `cancelled` capacity updates must leave request state, activity, SSE, and notification handoff unchanged.
- **Delivery split:** `capacity_changed` must materialize valid notification work/Discord delivery while producing zero Web Push targets; existing activity kinds must retain their current Push behavior.

---

### Task 1: Domain vocabulary, validation, and no-op-aware service API

**Files:**
- Modify: `internal/gamerequest/gamerequest.go`
- Modify: `internal/gamerequest/gamerequest_test.go`

**Interfaces:**
- Consumes: existing `Repository`, `EventPublisher`, `ValidateCreateInput`, lifecycle/status vocabulary.
- Produces:
  - `ErrNotActive = errors.New("game request is not active")`
  - `ErrCapacityBelowMembers = errors.New("maximum players is below current membership")`
  - `EventCapacityChanged EventType = "GameRequestCapacityChanged"`
  - `ActivityCapacityChanged ActivityKind = "capacity_changed"`
  - `Repository.UpdateCapacity(context.Context, string, string, *int32) (bool, error)`
  - `Service.UpdateCapacity(context.Context, string, string, *int32) error`

- [ ] **Step 1: Write failing domain tests for the new finite minimum**

Add tests proving:
- `ValidateCreateInput` rejects finite `1` with `ErrInvalidMaxPlayers`.
- `ValidateCreateInput` still accepts `nil`.
- `ValidateCreateInput` accepts finite `2`.
- A direct finite `0` pointer remains invalid; only transport parsing maps browser `0` to `nil`.

Run: `go test -race ./internal/gamerequest -run 'TestCreate|TestValidate' -count=1`  
Expected: FAIL because finite `1` is currently accepted.

- [ ] **Step 2: Change the domain finite-limit rule**

In `ValidateCreateInput`, reject any non-nil `maxPlayers < 2`. Do not add a `999` domain maximum; `999` remains the web input bound, preserving older/imported domain values.

Run: `go test -race ./internal/gamerequest -run 'TestCreate|TestValidate' -count=1`  
Expected: PASS.

- [ ] **Step 3: Write failing tests for `Service.UpdateCapacity`**

Extend `recordingRepository` with:
- requested capacity storage;
- `capacityChanged bool`;
- `capacityErr error`;
- `UpdateCapacity(...)(bool,error)`.

Add tests proving:
- changed=true publishes exactly `EventCapacityChanged`;
- changed=false returns nil and publishes nothing;
- repository error returns the same error and publishes nothing;
- finite `1` is rejected before repository call;
- `nil` and finite `2` reach the repository.

Run: `go test -race ./internal/gamerequest -run 'TestUpdateCapacity' -count=1`  
Expected: FAIL because the API/event do not exist.

- [ ] **Step 4: Add exact domain API and vocabulary**

Add the errors, event kind, activity kind, repository method, and:

`func (s *Service) UpdateCapacity(ctx context.Context, requestID, userID string, maxPlayers *int32) error`

Behavior:
1. reject non-nil values below `2` with `ErrInvalidMaxPlayers`;
2. call repository `UpdateCapacity`;
3. publish `EventCapacityChanged` only when repository returns `changed == true`;
4. return nil for `changed == false, err == nil`.

Run: `go test -race ./internal/gamerequest -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gamerequest/gamerequest.go internal/gamerequest/gamerequest_test.go
git commit -m "feat(gamerequest): define dynamic capacity"
```

---

### Task 2: PostgreSQL migration, active membership writes, and capacity concurrency

**Files:**
- Create: `internal/platform/postgres/migrations/00024_game_request_dynamic_capacity.sql`
- Create: `internal/platform/postgres/game_request_dynamic_capacity_integration_test.go`
- Modify: `internal/gamerequest/postgres/queries/game_requests.sql`
- Modify: `internal/gamerequest/postgres/store.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Modify: existing #100 playing integration assertions that intentionally expected Join/Leave to fail while playing
- Generate: `internal/gamerequest/postgres/sqlcgen/*`

**Interfaces:**
- Consumes: Task 1 `ErrNotActive`, `ErrCapacityBelowMembers`, `ActivityCapacityChanged`, and `Repository.UpdateCapacity(...)(bool,error)`.
- Produces: transactionally safe Join/Leave for `open|playing`, capacity mutation under the same request row lock, typed before/after activity persistence, schema version 24.

- [ ] **Step 1: Write failing integration tests for active Join/Leave**

Add:
- `TestPlayingRequestAllowsJoinAndLeave`
- `TestInactiveRequestRejectsJoinAndLeave`

Assertions:
- Join succeeds while `playing` when capacity permits.
- Leave succeeds while `playing`.
- `closed` and `cancelled` return `ErrNotActive`.
- Host still cannot leave.
- finite capacity still returns `ErrFull`.

Run: `task test:integration`  
Expected: FAIL because Join/Leave still require `open`.

- [ ] **Step 2: Make Join and Leave accept exactly active statuses**

In `store.go`, replace the `status == open` guards in Join/Leave with a small local predicate that accepts exactly `open` or `playing` and returns `ErrNotActive` otherwise.

Keep:
- the same `FOR UPDATE` request lock;
- duplicate membership check;
- capacity count/check;
- Host-leave prohibition;
- same-transaction joined/left activity writes.

Run: `task test:integration`  
Expected: PASS after updating obsolete #100 assertions.

- [ ] **Step 3: Write migration 24 and schema tests**

Migration Up must:
1. add `previous_max_players integer NULL`;
2. add `new_max_players integer NULL`;
3. replace `game_request_activity_kind_check` so it includes `capacity_changed`;
4. add `game_request_activity_capacity_metadata_check` enforcing:
   - unrelated kinds => both metadata fields are `NULL`;
   - `capacity_changed` => `previous_max_players IS DISTINCT FROM new_max_players`;
   - previous finite value, when present, is `>= 1` so a legacy `1` can be recorded as the old value;
   - new finite value, when present, is `>= 2`;
5. replace `notification_work_kind_check` so it includes `capacity_changed`;
6. perform no update/backfill of `game_requests.max_players`.

Migration Down:
- delete `notification_work` rows for `capacity_changed`;
- delete `game_request_activity` rows for `capacity_changed`;
- restore previous kind constraints;
- drop the metadata constraint;
- drop the two metadata columns.

Tests:
- migration version is `24`;
- upgrade from version 23 preserves a request whose `max_players = 1`;
- unrelated activity with capacity metadata violates the check;
- equal old/new values violate the check;
- `capacity_changed` accepts `1 -> 2`, finite -> unlimited, and unlimited -> finite.

Run: `task test:integration`  
Expected: PASS once migration and expected version are updated.

- [ ] **Step 4: Add focused SQL for capacity mutation and activity**

In `internal/gamerequest/postgres/queries/game_requests.sql`, add:

- `UpdateGameRequestCapacity :execrows`
  - update `max_players` and `updated_at = now()`;
  - require request id, Host creator id, and `status IN ('open','playing')`;
  - use `IS DISTINCT FROM` to refuse a semantic no-op.

- `RecordCapacityChangedActivity :exec`
  - insert `game_request_id`, `actor_id`, kind `capacity_changed`, `previous_max_players`, `new_max_players`.

Do not modify generic `RecordGameRequestActivity` to carry metadata for unrelated kinds.

Run: `task generate`  
Expected: sqlc generation succeeds.

- [ ] **Step 5: Write failing repository tests for Capacity**

Add:
- `TestHostUpdatesCapacityWhileOpenAndPlaying`
- `TestCapacityCanReturnToUnlimited`
- `TestCapacityRejectsNonHostAndInactiveRequests`
- `TestCapacityCannotDropBelowCurrentMembership`
- `TestCapacityNoOpWritesNothing`
- `TestCapacityChangeStoresBeforeAndAfterValues`
- `TestLegacyCapacityOneCanBeChangedButNotResubmitted`

Assertions:
- changed update returns success and exactly one live `capacity_changed` row;
- no-op produces zero new activity rows and leaves `updated_at` unchanged;
- legacy `1` may be changed to nil or `>=2`, while Task 1 rejects a newly submitted `1`.

Run: `task test:integration`  
Expected: FAIL because Store.UpdateCapacity is not implemented.

- [ ] **Step 6: Implement `Store.UpdateCapacity` under the existing row lock**

Implement:

`func (s *Store) UpdateCapacity(ctx context.Context, requestID, userID string, maxPlayers *int32) (bool, error)`

Inside `withLockedRequest`:
1. verify actor is creator, else `ErrNotCreator`;
2. verify status is `open|playing`, else `ErrNotActive`;
3. if requested value is semantically equal to locked `max_players`, return `false, nil` before counting members;
4. count current members;
5. if requested finite maximum < member count, return `ErrCapacityBelowMembers`;
6. execute `UpdateGameRequestCapacity`;
7. require exactly one updated row;
8. insert `RecordCapacityChangedActivity` with locked old value and requested new value;
9. return `true, nil` after transaction commit.

Use a small conversion/equality helper for `pgtype.Int4` versus `*int32`; do not introduce a generic nullable-value abstraction.

Run: `task test:integration`  
Expected: PASS.

- [ ] **Step 7: Add concurrency tests**

Add:
- `TestConcurrentJoinsCannotExceedPlayingCapacity`
- `TestConcurrentJoinAndCapacityReductionPreserveInvariant`
- `TestConcurrentLeaveAndCapacityReductionPreserveInvariant`

For every race, assert:

`max_players IS NULL OR member_count <= max_players`

Join/reduction may legally end in either lock-order result, but never over capacity.

Run: `task test:integration`  
Expected: PASS under `-race`.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/postgres/migrations/00024_game_request_dynamic_capacity.sql   internal/platform/postgres/game_request_dynamic_capacity_integration_test.go   internal/platform/postgres/postgres_integration_test.go   internal/platform/postgres/game_request_playing_integration_test.go   internal/gamerequest/postgres/queries/game_requests.sql   internal/gamerequest/postgres/store.go   internal/gamerequest/postgres/sqlcgen
git commit -m "feat(gamerequest): persist dynamic capacity"
```

---

### Task 3: Read-model capacity history and active participation presentation

**Files:**
- Modify: `internal/query/gamerequest/query.go`
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Modify: `internal/platform/postgres/game_request_dynamic_capacity_integration_test.go`
- Generate: `internal/query/gamerequest/sqlcgen/*`
- Generate: `internal/web/pages/game_requests_templ.go`

**Interfaces:**
- Consumes: Task 2 activity metadata and active Join/Leave persistence.
- Produces:
  - `requestquery.Activity.PreviousMaxPlayers *int32`
  - `requestquery.Activity.NewMaxPlayers *int32`
  - list/detail Join/Leave/Full behavior for both active statuses;
  - capacity-history copy with old/new values.

- [ ] **Step 1: Write failing activity projection tests**

Prove `ByID` maps:
- finite -> finite;
- finite -> unlimited;
- unlimited -> finite

into `Activity.PreviousMaxPlayers` / `NewMaxPlayers`.

Run: `task test:integration`  
Expected: FAIL because the read model does not select/map metadata.

- [ ] **Step 2: Extend the Activity projection**

Add pointer fields to `requestquery.Activity`.

Update `ListGameRequestActivity` to select both metadata columns and map them via the existing nullable-int helper.

Do not add these values to notification activity structs; Discord reads the current request summary.

Run: `task generate && task test:integration`  
Expected: PASS.

- [ ] **Step 3: Write failing template tests for playing Join/Leave/Full**

Add tests proving:
- playing + non-host + joined => Leave;
- playing + non-host + capacity available => Join;
- playing + non-host + full => Full/no Join;
- playing + last finite place communicates the available place;
- closed/cancelled remain static;
- Host never gets Join/Leave.

Run: `go test -race ./internal/web/pages -run 'GameRequest.*Playing|Capacity|Activity' -count=1`  
Expected: FAIL because playing rows/detail are currently non-actionable.

- [ ] **Step 4: Add a single active-state helper and reuse it**

Add:

`func requestActive(request requestquery.Request) bool`

returning true exactly for `open|playing`.

Use it to:
- allow row Join/Leave in both active states;
- allow detail Join/Leave in both active states;
- keep Host actions separate;
- keep closed/cancelled static;
- make `lastPlace` valid for both active states;
- make capacity color/availability helpers treat playing as active rather than muted solely because it is not open.

Do not change the status chip: playing keeps its own Playing state.

Run: `task generate && go test -race ./internal/web/pages -count=1`  
Expected: PASS.

- [ ] **Step 5: Render capacity history with exact before/after semantics**

Extend `activityLabel` so `ActivityCapacityChanged` renders:
- `<nick> changed capacity from 4 to 6`
- `<nick> changed capacity from 6 to unlimited`
- `<nick> changed capacity from unlimited to 8`

Add a small formatting helper for `*int32` that returns `"unlimited"` for nil.

Run: `go test -race ./internal/web/pages -run 'Activity|Capacity' -count=1`  
Expected: PASS.

- [ ] **Step 6: Pin Free places and Trending semantics**

In integration tests assert:
- Free places includes joinable playing requests;
- excludes full playing requests;
- a successful `capacity_changed` does not increase Trending activity count.

Do not change the `TrendingGames` activity-kind filter; it must remain `created|joined|started`.

Run: `task test:integration`  
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/query/gamerequest/query.go   internal/query/gamerequest/queries/game_requests.sql   internal/query/gamerequest/sqlcgen   internal/web/pages/game_requests.templ   internal/web/pages/game_requests_templ.go   internal/web/pages/game_requests_test.go   internal/platform/postgres/game_request_dynamic_capacity_integration_test.go
git commit -m "feat(web): keep playing participation active"
```

---

### Task 4: Capacity endpoint, Host editor, and SSE refresh

**Files:**
- Modify: `internal/web/router.go`
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/handler/realtime_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Generate: `internal/web/pages/game_requests_templ.go`

**Interfaces:**
- Consumes: Task 1 `Service.UpdateCapacity`; Task 3 detail/read projection.
- Produces:
  - `POST /game-requests/{requestID}/capacity`
  - Host-only detail capacity editor;
  - field-local capacity errors;
  - SSE event `GameRequestCapacityChanged`.

- [ ] **Step 1: Write failing handler parsing tests**

Add a dedicated parser for the capacity form, reusing the create semantics:
- blank => nil;
- `0` => nil;
- `2` and `999` => finite;
- `1`, negative, non-integer, `1000` => `ErrInvalidMaxPlayers`.

Do not route the update form through `parseCreateForm`.

Run: `go test -race ./internal/web/handler -run 'Capacity' -count=1`  
Expected: FAIL.

- [ ] **Step 2: Add command interface, route, and handler**

Extend `GameRequestCommands`:

`UpdateCapacity(context.Context, string, string, *int32) error`

Register:

`POST /game-requests/{requestID}/capacity`

Add `GameRequests.UpdateCapacity`:
1. parse capacity form;
2. call command;
3. on success redirect to the request detail;
4. on validation/business error re-read the detail and render it with a capacity-field error;
5. do not support list-origin editing because the editor exists only on detail.

Map:
- `ErrInvalidMaxPlayers` => 422;
- `ErrCapacityBelowMembers` => 409;
- `ErrNotActive` => 409;
- `ErrNotCreator` => 403.

Use user-facing copy:
- invalid: `Maximum players must be 0 for unlimited or a whole number from 2 to 999.`
- below members: `Maximum players cannot be lower than the current player count.`
- inactive: `Capacity can only be changed while the Game Request is active.`

Run: `go test -race ./internal/web/handler -run 'Capacity' -count=1`  
Expected: PASS.

- [ ] **Step 3: Add a capacity-specific detail message model**

Do not overload the Create composer's `ComposerMessage`.

Add a small detail message type or explicit arguments so the detail Capacity editor can mark `max_players` invalid while existing page-level lifecycle errors remain unchanged.

The rendered input must preserve the submitted value after an error rather than silently replacing it with the database value.

Run: `go test -race ./internal/web/pages ./internal/web/handler -run 'Capacity' -count=1`  
Expected: PASS.

- [ ] **Step 4: Add the Host capacity editor to the detail page**

Only for creator + `open|playing`, render a compact form containing:
- number input `name="max_players"`;
- `min="0"`, `max="999"`, `step="1"`;
- current finite value, or `0`/blank according to one consistent template convention;
- hint: `0 means unlimited; finite limits start at 2.`;
- Save button with a normal non-destructive action style;
- no confirmation dialog.

Keep Start/Cancel/Close Host actions as they are; Capacity may live in the same Host card or a dedicated compact Host Capacity card, but it must remain visually separate from destructive lifecycle actions.

Run: `task generate && go test -race ./internal/web/pages -run 'Capacity' -count=1`  
Expected: PASS.

- [ ] **Step 5: Add realtime event coverage**

Change `TestRealtimeStreamsGameRequestEvents` or add a focused test proving the SSE payload carries:

`"type":"GameRequestCapacityChanged"`

with the request id.

No realtime protocol changes are required; it is another existing `gamerequest.Event`.

Run: `go test -race ./internal/web/handler -run 'Realtime.*Capacity|RealtimeStreams' -count=1`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/router.go   internal/web/handler/game_requests.go   internal/web/handler/game_requests_test.go   internal/web/handler/realtime_test.go   internal/web/pages/game_requests.templ   internal/web/pages/game_requests_templ.go   internal/web/pages/game_requests_test.go
git commit -m "feat(web): add host capacity editor"
```

---

### Task 5: Notification handoff and Discord-only delivery

**Files:**
- Modify: `internal/notification/notification.go`
- Modify: `internal/notification/webpush.go`
- Modify: `internal/notification/webpush_test.go`
- Modify: `internal/platform/postgres/notification_integration_test.go`
- Modify: relevant Discord notification integration tests if they assert the activity-kind matrix
- Generate: notification sqlc files only if generation changes them

**Interfaces:**
- Consumes: Task 2 schema accepts `capacity_changed`; existing Discord transport targets every valid activity and renders current request summary.
- Produces:
  - `notification.ActivityCapacityChanged = "capacity_changed"`
  - durable notification work for capacity activity;
  - Discord target/materialization;
  - explicitly zero Web Push targets.

- [ ] **Step 1: Add the notification kind mirror**

Add `ActivityCapacityChanged` to `internal/notification/notification.go`.

Update comments that currently say six durable kinds.

Do not add old/new capacity metadata to `notification.Activity`; delivery does not need it.

Run: `go test -race ./internal/notification -count=1`  
Expected: PASS after enum/test updates.

- [ ] **Step 2: Write failing Web Push target test**

Add a test that passes `ActivityCapacityChanged` to `WebPushTransport.Targets` and asserts:
- returned targets length is zero;
- participant repository is not queried;
- preference/subscription repositories are not queried.

Run: `go test -race ./internal/notification -run 'WebPush.*Capacity' -count=1`  
Expected: FAIL until kind handling is explicit.

- [ ] **Step 3: Explicitly suppress capacity Web Push**

Add a switch branch:

`case ActivityCapacityChanged: return nil, nil`

Keep existing `created/joined/left/started/closed/cancelled` behavior unchanged.

Do not modify `web/src/service-worker.ts`; `capacity_changed` is not a Push wire kind.

Run: `go test -race ./internal/notification -count=1`  
Expected: PASS.

- [ ] **Step 4: Add notification integration coverage**

Create a request, initialize/baseline notification state as the existing tests require, perform a capacity change, then assert:
- `DiscoverActivity` returns exactly one `capacity_changed` activity;
- `MaterializeWork` creates a `notification_work.kind = 'capacity_changed'`;
- with Discord transport target resolution, a Discord delivery is materialized;
- no Web Push delivery is materialized.

Use existing worker/transport integration fixtures rather than inserting fake notification rows directly.

Run: `task test:integration`  
Expected: PASS.

- [ ] **Step 5: Verify Discord snapshot reflects the new capacity**

Extend a Discord send/sync integration test:
- materialize/send a request;
- change capacity;
- process the capacity activity;
- assert the existing Discord message is edited, not recreated;
- assert rendered player summary uses the new finite/unlimited state.

No new Discord message format or event-specific content is introduced.

Run: `task test:integration`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/notification/notification.go   internal/notification/webpush.go   internal/notification/webpush_test.go   internal/platform/postgres/notification_integration_test.go   internal/platform/postgres/notification_discord_send_integration_test.go
git commit -m "feat(notification): sync capacity changes to discord"
```

---

### Task 6: Browser fixture, end-to-end interaction, and canonical docs

**Files:**
- Modify: `internal/web/browsertest/main.go`
- Modify: `web/tests/rapid-interaction.spec.ts`
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/TERMINOLOGY.md`
- Modify: `docs/ui-kit/README.md`
- Generate: templ/frontend artifacts as required by `task generate`

**Interfaces:**
- Consumes: Tasks 1–5 production behavior.
- Produces: browser fixture matching #101 only, visible browser regression coverage, canonical durable documentation.

- [ ] **Step 1: Upgrade the browser fixture to #101 semantics**

In the in-memory fixture:
- Join/Leave accept `open|playing`;
- Host cannot leave;
- capacity remains enforced;
- implement `UpdateCapacity(...)(bool,error)`-equivalent command surface expected by the handler;
- reject non-Host/inactive/below-members;
- record `ActivityCapacityChanged` with old/new pointers;
- publish `EventCapacityChanged` only for a real change;
- preserve no-op behavior.

Keep the fixture in-memory. Do not emulate PostgreSQL locking; database concurrency belongs in integration tests.

Run: `go test -race ./internal/web/browsertest -count=1` or the package-level command used by the repo.  
Expected: PASS.

- [ ] **Step 2: Add browser coverage for the complete visible flow**

Add one focused Playwright flow that:
1. creates a request with finite capacity;
2. changes capacity on the detail page;
3. starts the request;
4. confirms a participant can Join while Playing;
5. reaches the finite maximum and shows Full;
6. participant Leaves and the place becomes available;
7. Host switches capacity to unlimited;
8. another Join becomes available without a page reload after realtime refresh.

Also assert the Host editor is absent for a non-Host viewer.

Run: `task test:browser`  
Expected: PASS.

- [ ] **Step 3: Update PRODUCT**

Record implemented durable truth:
- Join/Leave while `playing`;
- Host capacity editing while `open|playing`;
- unlimited semantics;
- finite values for new/edited requests start at 2;
- capacity history is durable;
- capacity changes do not create personal Push and do not alter Trending.

Do not add implementation status prose.

- [ ] **Step 4: Update ARCHITECTURE**

Record:
- Join/Leave/Capacity share the Game Request row-lock consistency boundary;
- `capacity_changed` is a durable activity/notification-handoff kind with typed old/new columns;
- notification path uses it for Discord sync but Web Push has zero targets;
- no generic metadata/event payload was introduced.

- [ ] **Step 5: Update TERMINOLOGY and UI kit**

Define or align:
- **Capacity / Max players**;
- **Unlimited**;
- **Full**;
- **Free place(s)**.

Document:
- Playing keeps normal Join/Leave/Full states;
- Host capacity editor exists only on detail;
- finite input is `2..999`, `0`/blank is unlimited.

Run: `git diff --check`  
Expected: no whitespace errors.

- [ ] **Step 6: Commit**

```bash
git add internal/web/browsertest/main.go   web/tests/rapid-interaction.spec.ts   docs/PRODUCT.md docs/ARCHITECTURE.md docs/TERMINOLOGY.md docs/ui-kit/README.md
git commit -m "docs: align dynamic capacity behavior"
```

---

### Task 7: Full verification, working-artifact cleanup, and PR handoff

**Files:**
- Delete only after verification:
  - `docs/superpowers/specs/2026-10-01-game-request-dynamic-capacity-design.md`
  - `docs/superpowers/plans/2026-10-01-game-request-dynamic-capacity.md`
- Update: PR description for #101 implementation branch once a PR exists

**Interfaces:**
- Consumes: all prior tasks.
- Produces: fully verified #101 branch with temporary planning artifacts retired and canonical docs retained.

- [ ] **Step 1: Regenerate from source**

Run:

`task generate`

Expected:
- succeeds;
- generated sqlc/templ/frontend artifacts are consistent;
- no unrelated generated churn.

- [ ] **Step 2: Run static/unit verification**

Run:

`task lint`

`task test`

Expected: both exit 0.

- [ ] **Step 3: Run PostgreSQL integration verification**

Run:

`task test:integration`

Expected: exit 0, including migration 24, concurrency, notification, legacy-`1`, and capacity-history tests.

- [ ] **Step 4: Run browser/PWA verification**

Run:

`task test:browser`

`task test:pwa`

Expected:
- browser suite exits 0;
- PWA suite remains green even though no `capacity_changed` PushKind was added.

- [ ] **Step 5: Build**

Run:

`task build`

Expected: exit 0.

- [ ] **Step 6: Perform manual invariant review**

Check the diff against this exact list:
- Join and Leave allow only `open|playing`.
- Host still cannot Leave.
- Finite new/edit input is `2..999`; blank/0 => unlimited.
- Legacy database rows with max `1` are untouched.
- Capacity edit is Host-only and active-only.
- Capacity can never commit below current membership.
- Join and Capacity use the same locked request row.
- Same capacity is a true no-op: no `updated_at`, activity, SSE, or notification work.
- `capacity_changed` stores typed before/after values.
- Unrelated activity cannot carry capacity metadata.
- Trending still includes only `created|joined|started`.
- Web Push returns zero targets for `capacity_changed`.
- Service worker PushKind is unchanged.
- Discord sync edits the existing synchronized request message using current state.
- No new dependency/module/transport/outbox/generic metadata system was introduced.

- [ ] **Step 7: Retire temporary planning artifacts**

Only after Steps 1–6 pass, delete:

`docs/superpowers/specs/2026-10-01-game-request-dynamic-capacity-design.md`

`docs/superpowers/plans/2026-10-01-game-request-dynamic-capacity.md`

Keep all canonical documentation changes.

Run:

`task lint`

`git status --short`

Expected: lint passes and only intended cleanup changes remain.

- [ ] **Step 8: Commit cleanup**

```bash
git add -A docs/superpowers
git commit -m "docs: retire dynamic capacity working artifacts"
```

- [ ] **Step 9: Prepare PR for review**

Create or update the #101 PR description with:
- implemented behavior;
- migration/legacy-`1` rule;
- activity/Discord/no-Push behavior;
- verification commands and results;
- explicit statement that #102–#104 remain out of scope.

Do not mark #101 complete until the implementation PR is merged.
