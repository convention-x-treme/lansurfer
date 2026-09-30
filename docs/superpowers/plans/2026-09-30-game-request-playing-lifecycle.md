# Game Request Playing Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement issue #100 so a Host can explicitly transition a Game Request through `open -> playing -> closed`, with a durable actual start timestamp, atomic `started` activity, active discovery/realtime presentation, synchronized Discord state, and participant Web Push.

**Architecture:** Extend the existing `gamerequest` lifecycle instead of introducing a state-machine framework. PostgreSQL remains authoritative: the Host transition and `game_request_activity` handoff commit in one locked transaction, SSE remains best-effort browser refresh, and the existing notification worker consumes the new durable activity kind. Read models treat `open` and `playing` as active while mutation eligibility remains deliberately `open`-only until #101.

**Tech Stack:** Go modular monolith, PostgreSQL + goose + sqlc, templ/HTMX, in-process SSE, TypeScript service worker, Playwright.

**Spec:** [2026-09-30-game-request-playing-lifecycle-design.md](../specs/2026-09-30-game-request-playing-lifecycle-design.md)

## Global Constraints

- Work on branch `feat/issue-100-playing-lifecycle` / PR #112. Do not implement #101–#104 in this plan.
- Do not introduce a generic state machine, new module, second activity/outbox table, message broker, WebSocket path, SPA state, or new direct dependency.
- Keep `gamerequest.Repository.SetStatus(..., Status)` as the persistence interface. Add the explicit `Service.Start` use case, but do not fan out new repository methods through the domain interface.
- Keep Join and Leave restricted to `open` in #100. A `playing` request is visible but not joinable/leavable until #101.
- `game_request_activity` remains both durable history and the transactional notification handoff. The `started` row must be inserted in the same PostgreSQL transaction as `status='playing'` and `started_at`.
- Use PostgreSQL `now()` for both the Start update and activity insert in the same transaction. PostgreSQL's transaction timestamp makes the two values identical without application clock plumbing.
- Never invent historical actual-start data. Existing `closed` rows may remain `started_at IS NULL`.
- Web Push wire version remains `3`. `started` is actor-neutral in the payload; the Host is excluded as a recipient and is not added as `actor_game_nick`.
- A `started` Web Push is sent to current participants using the existing preference/device eligibility path. Existing visible-window suppression remains authoritative.
- Discord continues to maintain one synchronized message per request/destination generation. A Start activity causes the existing message to render current `playing` state; do not create an event-specific Discord message.
- Treat `playing` as active in discovery, active navigation counts, and “Your next games”.
- Keep the Free places filter's existing capacity arithmetic for `playing`; do not turn that into Join eligibility in this issue.
- Trending keeps its existing simple activity-count model. Add `started` beside the existing scored kinds; do not add weights.
- Do not hand-edit generated files. Run `task generate` after sqlc or templ/frontend source changes and commit the generated output it produces.
- Never create, overwrite, or delete the developer's local `.env` or other gitignored local configuration.
- Keep every task independently green before moving to the next task. Make the listed commit after each task.
- When a listed test fails for an unrelated reason, fix the current task only. Do not use the failure as permission to refactor neighboring architecture.
- Canonical docs in this branch are already prepared for the approved design. Do not redesign them during implementation; only correct a factual mismatch discovered by implementation.

## Review Focus

1. **Historical integrity:** migration #23 must not backfill `started_at` for legacy `closed` requests.
2. **Transition integrity:** only `open -> playing`, `open -> cancelled`, and `playing -> closed` may succeed.
3. **Atomicity:** Start must produce exactly one `started` activity with the same transaction timestamp as `started_at`.
4. **Concurrency:** double Start and Start-vs-Cancel must serialize on the existing request row lock and never duplicate start state/activity.
5. **#101 boundary:** Join/Leave must still reject `playing`.
6. **Active reads:** `playing` must stay in discovery, active count, Joined scope, and “Your next games”.
7. **Notification handoff:** both `game_request_activity.kind` and `notification_work.kind` constraints must accept `started`.
8. **Recipient privacy:** Host/actor receives no personal Start push; Start payload carries no actor Game Nick.
9. **Presentation:** Playing is visibly labeled and uses the approved Sunset semantic without being styled as closed/muted.
10. **No regression of legacy closed history:** use generic “closed the request” activity copy rather than claiming historical requests had a played session.

---

### Task 1: Add the playing vocabulary and explicit Start use case

**Files:**
- Modify: `internal/gamerequest/gamerequest.go`
- Modify: `internal/gamerequest/gamerequest_test.go`

**Interfaces and fixed decisions:**

Keep the repository interface unchanged:

```go
type Repository interface {
    Create(context.Context, GameRequest) (GameRequest, error)
    Join(context.Context, string, string) error
    Leave(context.Context, string, string) error
    SetStatus(context.Context, string, string, Status) error
    RecordView(context.Context, string, string) error
}
```

Add exactly these domain values:

```go
ErrNotPlaying = errors.New("game request is not playing")

StatusPlaying Status = "playing"

EventStarted EventType = "GameRequestStarted"

ActivityStarted ActivityKind = "started"
```

Add this service method; transition-source validation remains in the PostgreSQL repository where the locked current state is available:

```go
func (s *Service) Start(ctx context.Context, requestID, userID string) error {
    return s.setStatus(ctx, requestID, userID, StatusPlaying, EventStarted)
}
```

- [ ] **Step 1: Write failing domain tests.**

Extend `TestPublishesSuccessfulLifecycleChangesOnly` so the successful sequence includes `Start` before `Close` and expects `EventStarted`.

Add a focused failure assertion proving a repository error from Start publishes no event. Extend `recordingRepository` only enough to capture the requested target status and inject a `SetStatus` error.

Expected event order for the lifecycle portion:

```go
[]Event{
    {Type: EventJoined, RequestID: "request-1"},
    {Type: EventLeft, RequestID: "request-1"},
    {Type: EventStarted, RequestID: "request-1"},
    {Type: EventClosed, RequestID: "request-1"},
    {Type: EventCancelled, RequestID: "request-2"},
}
```

- [ ] **Step 2: Run the focused test and confirm it fails before implementation.**

Run:

```bash
go test ./internal/gamerequest -run TestPublishesSuccessfulLifecycleChangesOnly -count=1
```

Expected: compile/test failure because the new status/event/activity/Start method does not exist yet.

- [ ] **Step 3: Implement only the domain vocabulary and Start method.**

In `gamerequest.go`:
- add `ErrNotPlaying`;
- add `StatusPlaying`;
- add `EventStarted`;
- add `ActivityStarted`;
- add `Service.Start`;
- update the ActivityKind comment so it describes the notification handoff without introducing new historical “Track” terminology.

Do **not** add a generic state-machine abstraction and do **not** add `StartedAt` to domain/read structs merely because the database will gain the column; #100 has no current consumer that needs that field in those projections.

- [ ] **Step 4: Run the package tests.**

Run:

```bash
go test ./internal/gamerequest -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/gamerequest/gamerequest.go internal/gamerequest/gamerequest_test.go
git commit -m "feat(gamerequest): define playing lifecycle"
```

---

### Task 2: Persist Start atomically and enforce the lifecycle in PostgreSQL

**Files:**
- Create: `internal/platform/postgres/migrations/00023_game_request_playing.sql`
- Create: `internal/platform/postgres/game_request_playing_integration_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Modify: `internal/platform/postgres/gamerequest_summary_integration_test.go`
- Modify: `internal/gamerequest/postgres/queries/game_requests.sql`
- Modify: `internal/gamerequest/postgres/store.go`
- Generated by `task generate`, do not hand-edit:
  - `internal/gamerequest/postgres/sqlcgen/game_requests.sql.go`
  - `internal/gamerequest/postgres/sqlcgen/models.go` if sqlc changes it
  - `internal/gamerequest/postgres/sqlcgen/db.go` if sqlc changes it

**Migration contract:**

Migration 23 must:
- add nullable `game_requests.started_at timestamptz`;
- extend request status to `open|playing|closed|cancelled`;
- enforce `open/cancelled => started_at NULL`;
- enforce `playing => started_at NOT NULL`;
- deliberately allow legacy `closed + started_at NULL`;
- replace the partial open index with an active `open|playing` index;
- extend **both** `game_request_activity.kind` **and** `notification_work.kind` checks with `started`;
- perform no start-time backfill.

Use this Up shape (constraint names come from the existing PostgreSQL-created checks):

```sql
-- +goose Up
ALTER TABLE game_requests ADD COLUMN started_at timestamptz NULL;

ALTER TABLE game_requests DROP CONSTRAINT game_requests_status_check;
ALTER TABLE game_requests
    ADD CONSTRAINT game_requests_status_check
    CHECK (status IN ('open', 'playing', 'closed', 'cancelled'));

ALTER TABLE game_requests
    ADD CONSTRAINT game_requests_started_at_check
    CHECK (
        (status IN ('open', 'cancelled') AND started_at IS NULL)
        OR (status = 'playing' AND started_at IS NOT NULL)
        OR status = 'closed'
    );

DROP INDEX game_requests_open_idx;
CREATE INDEX game_requests_active_idx
    ON game_requests (starts_at, created_at)
    WHERE status IN ('open', 'playing');

ALTER TABLE game_request_activity DROP CONSTRAINT game_request_activity_kind_check;
ALTER TABLE game_request_activity
    ADD CONSTRAINT game_request_activity_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));

ALTER TABLE notification_work DROP CONSTRAINT notification_work_kind_check;
ALTER TABLE notification_work
    ADD CONSTRAINT notification_work_kind_check
    CHECK (kind IN ('created', 'joined', 'left', 'started', 'closed', 'cancelled'));
```

For the developer-only Down path, remove `started` notification work/activity, map any remaining `playing` rows to `closed`, restore the old checks/index, then drop `started_at`. Production never runs DOWN automatically.

**Persistence SQL contract:**

Replace the generic `UpdateGameRequestStatus` command with exactly three explicit queries:

```sql
-- name: StartGameRequest :execrows
UPDATE game_requests
SET status = 'playing', started_at = now(), updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'open';

-- name: CancelGameRequest :execrows
UPDATE game_requests
SET status = 'cancelled', updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'open';

-- name: CloseGameRequest :execrows
UPDATE game_requests
SET status = 'closed', updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND creator_id = sqlc.arg(creator_id)::uuid
  AND status = 'playing';
```

Keep `RecordGameRequestActivity` using its existing `occurred_at DEFAULT now()`. Because Start update + activity insert share one transaction, `started_at` and `occurred_at` are the same PostgreSQL transaction timestamp.

- [ ] **Step 1: Add failing integration coverage for lifecycle, timestamps, atomicity, legacy migration, and concurrency.**

Create `game_request_playing_integration_test.go` with `//go:build integration`.

Cover these cases with real PostgreSQL:

1. Host starts an `open` request with no additional participant and with a planned time still in the future.
2. Read DB state directly and assert:
   - status = `playing`;
   - `started_at IS NOT NULL`;
   - exactly one `started` activity exists;
   - its `occurred_at == started_at`.
3. A second Start returns `ErrNotOpen`, does not change `started_at`, and does not add a second `started` row.
4. Non-Host Start returns `ErrNotCreator`.
5. Close on an `open` request returns `ErrNotPlaying`.
6. Cancel after Start returns `ErrNotOpen`.
7. Join and Leave while `playing` still return `ErrNotOpen` (#101 boundary).
8. Close after Start succeeds and preserves the original `started_at`.
9. Start-vs-Cancel launched concurrently on the same `open` request yields exactly one successful mutation; final state is either `playing` with one start activity/timestamp or `cancelled` with no start timestamp/activity.
10. Direct invalid DB state is rejected:
    - `playing` with null `started_at`;
    - `open` or `cancelled` with non-null `started_at`.

Add a migration-specific test using the existing `migrateToVersion(..., 22)` helper:
- migrate only through version 22;
- create an ordinary request (Create does not depend on `started_at`);
- directly update it to legacy `closed`;
- run the normal full migration;
- assert the row is still `closed` and `started_at IS NULL`.

- [ ] **Step 2: Update existing tests that encode the old direct-open-to-close behavior.**

In `TestGameRequestWorkflowAndConcurrentCapacity`, call Start before the successful Host Close and add an explicit assertion that direct Close while open is rejected.

In `gamerequest_summary_integration_test.go`, change the `closed` fixture from:

```text
create -> close
```

to:

```text
create -> start -> close
```

The summary still expects final status `closed`.

Update `TestMigrateAndReadyCheck` expected migration version from `22` to `23`.

- [ ] **Step 3: Run integration tests and confirm the new tests fail.**

Run:

```bash
task test:integration
```

Expected before implementation: failures around unsupported `playing`, missing `started_at`, and old Close semantics.

- [ ] **Step 4: Add migration 23 and explicit lifecycle SQL.**

Implement the migration contract above.

In `internal/gamerequest/postgres/queries/game_requests.sql`:
- delete `UpdateGameRequestStatus`;
- add `StartGameRequest`, `CancelGameRequest`, `CloseGameRequest`;
- leave Join/Leave SQL unchanged;
- leave `RecordGameRequestActivity` default timestamp behavior unchanged.

- [ ] **Step 5: Make Store.SetStatus dispatch explicit transitions under the existing row lock.**

Inside the existing `withLockedRequest` callback, keep the creator check first, then use a small target-status switch:

```go
var (
    updated  int64
    activity gamerequest.ActivityKind
)

switch status {
case gamerequest.StatusPlaying:
    if locked.Status != string(gamerequest.StatusOpen) {
        return gamerequest.ErrNotOpen
    }
    updated, err = queries.StartGameRequest(ctx, ...)
    activity = gamerequest.ActivityStarted

case gamerequest.StatusCancelled:
    if locked.Status != string(gamerequest.StatusOpen) {
        return gamerequest.ErrNotOpen
    }
    updated, err = queries.CancelGameRequest(ctx, ...)
    activity = gamerequest.ActivityCancelled

case gamerequest.StatusClosed:
    if locked.Status != string(gamerequest.StatusPlaying) {
        return gamerequest.ErrNotPlaying
    }
    updated, err = queries.CloseGameRequest(ctx, ...)
    activity = gamerequest.ActivityClosed

default:
    return fmt.Errorf("unsupported game request target status %q", status)
}
```

Then:
- translate SQL errors with the existing style;
- require `updated == 1`;
- record exactly one selected activity with `recordActivity`;
- remove the old `statusActivity` helper.

Do not add separate transaction code: keep using `withLockedRequest` so Start-vs-Cancel and double Start serialize exactly like Join/Leave already do.

- [ ] **Step 6: Regenerate sqlc and templates/assets.**

Run:

```bash
task generate
```

Do not manually modify generated files.

- [ ] **Step 7: Run focused Go and integration tests.**

Run:

```bash
go test ./internal/gamerequest/... -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 8: Commit.**

```bash
git add internal/platform/postgres/migrations/00023_game_request_playing.sql   internal/platform/postgres/game_request_playing_integration_test.go   internal/platform/postgres/postgres_integration_test.go   internal/platform/postgres/gamerequest_summary_integration_test.go   internal/gamerequest/postgres
git commit -m "feat(gamerequest): persist playing lifecycle"
```

---

### Task 3: Make playing requests active in discovery, navigation, and Trending

**Files:**
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Modify: `internal/query/gamerequest/query.go`
- Modify: `internal/platform/postgres/game_request_playing_integration_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go` only where existing Trending assertions need the additional `started` signal
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/navigation.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/web/components/layout.templ`
- Modify: `internal/web/components/layout_test.go`
- Modify: `internal/web/components/navigation_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Generated by `task generate`, do not hand-edit:
  - `internal/query/gamerequest/sqlcgen/game_requests.sql.go`
  - `internal/query/gamerequest/sqlcgen/models.go` if changed
  - `internal/web/components/layout_templ.go`
  - `internal/web/pages/game_requests_templ.go`

**Read-model contract:**

Change active discovery from:

```sql
WHERE gr.status = 'open'
```

to:

```sql
WHERE gr.status IN ('open', 'playing')
```

Change Trending's scored activity kinds from:

```sql
a.kind IN ('created', 'joined')
```

to:

```sql
a.kind IN ('created', 'joined', 'started')
```

Rename the count query/method to active semantics:

```sql
-- name: CountActiveGameRequests :one
SELECT count(*)
FROM game_requests
WHERE status IN ('open', 'playing');
```

```go
func (s *Store) CountActive(ctx context.Context) (int64, error)
```

Propagate the same naming through web interfaces/state:
- `GameRequestQueries.CountActive`;
- `NavigationQueries.CountActive`;
- `NavigationState.ActiveRequests`;
- list parameter/helper names `activeRequests` / `navigationWithActiveCount`.

Do not reinterpret this as Join eligibility. The row/detail action checks remain `StatusOpen` until #101.

- [ ] **Step 1: Add failing read-model integration assertions.**

Extend the playing integration fixture so, after Start:
- `ListActive(... ScopeAll)` still contains the request with status `playing`;
- `ListActive(... ScopeJoined)` still contains it for a joined participant;
- `CountActive()` includes it;
- a `playing` request with spare capacity remains visible under the existing Free places capacity filter;
- Trending counts the new `started` activity.

For Trending, assert the exact activity-count delta rather than relying only on ordering.

- [ ] **Step 2: Add failing navigation/component tests for active naming/behavior.**

Update component/navigation tests so a nonzero active count renders beside “Game Requests” using `ActiveRequests`.

Update page tests/copy expectations from “open requests” to “active requests” where the text refers to the whole discovery set. Do not rename the user-facing **Free places** filter.

- [ ] **Step 3: Run the focused tests and confirm failure.**

Run:

```bash
task test:integration
go test ./internal/web/components ./internal/web/pages ./internal/web/handler -count=1
```

Expected before implementation: playing omitted from reads, Trending delta missing, and CountActive symbols absent.

- [ ] **Step 4: Implement SQL and Go read-model changes.**

Make only the changes fixed in the Read-model contract above.

Keep `GetGameRequestSummary`, `RecentlyViewed`, and current membership reads unchanged.

- [ ] **Step 5: Propagate CountActive/ActiveRequests through web navigation.**

Update:
- handler interfaces and list rendering local variable;
- shared Navigation builder;
- router/handler fakes;
- `NavigationState`;
- `NavigationLinks`;
- `GameRequestList` parameter and `navigationWithActiveCount`.

Update comments to say “active (`open` or `playing`)”, not “open”.

Do not rename CSS utility classes such as `count-open`; those are presentation tokens, not domain APIs.

- [ ] **Step 6: Regenerate and test.**

Run:

```bash
task generate
go test ./internal/query/gamerequest ./internal/web/components ./internal/web/pages ./internal/web/handler -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add internal/query/gamerequest   internal/platform/postgres/game_request_playing_integration_test.go   internal/platform/postgres/postgres_integration_test.go   internal/web/handler   internal/web/router_test.go   internal/web/components   internal/web/pages
git commit -m "feat(query): keep playing requests active"
```

---

### Task 4: Add the Start route, Host controls, Playing presentation, and realtime proof

**Files:**
- Modify: `internal/web/router.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/handler/realtime_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Modify: `web/src/app.css`
- Modify: `internal/web/browsertest/main.go`
- Modify: `web/tests/rapid-interaction.spec.ts`
- Generated by `task generate`, do not hand-edit:
  - `internal/web/pages/game_requests_templ.go`
  - `internal/web/assets/dist/app.css`
  - `internal/web/assets/dist/app.js`

**HTTP contract:**

Add:

```text
POST /game-requests/{requestID}/start
```

to the same authenticated + Game Nick + CSRF-protected request router as Join/Leave/Close/Cancel.

Add `Start(context.Context, string, string) error` to `GameRequestCommands`.

Use the existing post-command rendering/redirect behavior. Rename `changeMembership` to `changeRequest` because it now also handles lifecycle mutations.

Add `ErrNotPlaying` to HTTP 409 handling.

Update user-facing domain messages:
- `ErrNotCreator`: “Only the Host can start, close, or cancel this Game Request.”
- `ErrNotOpen`: keep “This Game Request is no longer open.”
- `ErrNotPlaying`: “This Game Request is not currently playing.”
- `ErrCreatorCannotLeave`: use a generic Host message that does not tell an open Host to Close directly, e.g. “The Host cannot leave their own Game Request.”

**UI contract:**

Open Host:
- **Start game**;
- **Cancel Game Request**;
- no Close action.

Start form must include an explicit `data-confirm` message communicating:
- the request becomes Playing;
- it cannot be cancelled afterward;
- it must be closed when finished.

Use a primary/commit treatment for **Start game**, not Coral. Cancellation remains destructive.

Playing Host:
- **Close game** only;
- no Start;
- no Cancel.

Non-Host playing viewer:
- no Join/Leave in #100;
- visible Playing state only.

Presentation:
- Playing chip uses Sunset;
- Playing row stays visually active, not `request-row-closed`;
- Playing hero gets a Sunset active-now accent;
- list title connector becomes “is playing” for `playing`;
- `requestActionLabel(playing)` returns “Playing”;
- `activityLabel(started)` returns “<Game Nick> started the game”;
- change the old `closed` activity copy to the historically safe generic “<Game Nick> closed the request”.

- [ ] **Step 1: Write failing handler/router/page tests.**

Add tests proving:
- Start handler calls `commands.Start`;
- `ErrNotPlaying` maps to 409 and the defined message;
- router exposes POST `/{requestID}/start` under normal auth/CSRF middleware;
- open Host detail renders Start + Cancel and not Close;
- Start form posts to `/start` and contains `data-confirm`;
- playing Host detail renders Close game and neither Start nor Cancel;
- playing non-Host detail renders no Join/Leave;
- playing row says Playing / “is playing” and does not get the closed-row class;
- Started Activity renders the chosen copy;
- legacy/new Closed Activity uses generic “closed the request”.

- [ ] **Step 2: Make realtime serialization explicitly cover EventStarted.**

Change `TestRealtimeStreamsGameRequestEvents` into a small table/subtest or use `EventStarted` as the event under test and assert the stream contains:

```json
"type":"GameRequestStarted"
```

No production realtime code should be necessary; this test pins that the existing generic SSE path carries the new event.

- [ ] **Step 3: Run focused Go tests and confirm failure.**

Run:

```bash
go test ./internal/web/handler ./internal/web/pages ./internal/web -count=1
```

Expected before implementation: missing Start command/route and missing Playing rendering.

- [ ] **Step 4: Implement handler and route changes.**

Add `Start` beside Join/Leave/Close/Cancel and route it through `changeRequest`.

Do not put Host/status checks in the handler. The service/repository path remains authoritative.

- [ ] **Step 5: Implement the templ Playing state and Host action matrix.**

Use explicit status checks. Do not create a generic frontend state machine.

Recommended helpers:

```go
func requestActionLabel(request requestquery.Request) string {
    switch request.Status {
    case gamerequest.StatusPlaying:
        return "Playing"
    case gamerequest.StatusCancelled:
        return "Cancelled"
    default:
        return "Closed"
    }
}

func requestRowConnector(request requestquery.Request) string {
    if request.Status == gamerequest.StatusPlaying {
        return "is playing"
    }
    return "is looking for"
}
```

For `gameRequestRowClass`, test `StatusPlaying` before terminal/full muting so a full Playing request is still visibly Playing rather than looking closed.

Keep `playerCountClass` neutral/muted for Playing because #100 does not make the remaining capacity actionable yet.

- [ ] **Step 6: Add Playing CSS using existing brand tokens.**

In `web/src/app.css`:
- include `.status-chip-playing` in the bold state-chip group;
- give it a Sunset tint using `var(--brand-sunset)` and the same accessible dark ink already used for Sunset/last-place chips;
- add `.request-row-playing` with a Sunset border/accent analogous to active-now semantics;
- add `.detail-hero-playing` with the same semantic accent.

Do not hardcode a new palette color.

- [ ] **Step 7: Update the browser fixture to model only #100 behavior.**

In `internal/web/browsertest/main.go`:
- add `Start` to the fake commands;
- make Start require open and publish `EventStarted`;
- make Close require playing;
- make Cancel require open;
- keep Join/Leave open-only;
- make `ListActive` and the active count include open + playing;
- rename the fixture's CountOpen method/state to CountActive as required by Task 3 interfaces;
- add the `started` activity to the request when Start succeeds.

Do not implement #101 capacity mutation or playing Join/Leave in the fixture.

- [ ] **Step 8: Add one real-browser confirmation test.**

In `web/tests/rapid-interaction.spec.ts`:
1. create a request through the existing composer so the fixture viewer is its Host;
2. on the detail page, intercept the browser `dialog`;
3. dismiss the first **Start game** confirmation and assert the request remains Open / Start is still present;
4. click Start again, accept the confirmation;
5. assert the detail page shows Playing and **Close game**, with Start and Cancel gone.

This test is specifically for browser confirmation/interaction. Keep lifecycle correctness in Go/PostgreSQL tests.

- [ ] **Step 9: Regenerate and run web/browser tests.**

Run:

```bash
task generate
go test ./internal/web/... -count=1
task test:browser
```

Expected: PASS.

- [ ] **Step 10: Commit.**

```bash
git add internal/web web/src/app.css web/tests/rapid-interaction.spec.ts
git commit -m "feat(web): add start game playing flow"
```

---

### Task 5: Deliver started activity through the existing notification pipeline

**Files:**
- Modify: `internal/notification/notification.go`
- Modify: `internal/notification/webpush.go`
- Modify: `internal/notification/webpush_test.go`
- Modify: `internal/notification/webpush_payload.go`
- Modify: `internal/notification/webpush_payload_test.go`
- Modify: `internal/notification/discord_message.go`
- Modify: `internal/notification/discord_message_test.go`
- Modify: `internal/platform/postgres/notification_integration_test.go`

**Notification contract:**

Add:

```go
ActivityStarted ActivityKind = "started"
```

to the notification module's local mirror.

Web Push target selection:
- treat `ActivityStarted` like Join/Leave/Close/Cancel for audience selection: current participants;
- continue calling `dedupeExcept(..., activity.ActorID)`, excluding the Host;
- do **not** make `actorGameNick` look up Started.

Payload:
- accept Started in the actor-neutral case;
- keep version 3;
- no `actor_game_nick` for Started.

Discord:
- no target/materialization change is needed; Discord already targets every activity;
- add `playing` to current-snapshot rendering with Sunset `0xFF6B35`.

- [ ] **Step 1: Write failing notification unit tests.**

In `webpush_test.go`, add/extend tests so `ActivityStarted`:
- resolves current request participants;
- excludes the activity actor/Host;
- honors existing `WebPushEnabled` eligibility;
- performs no actor Game Nick lookup.

In `webpush_payload_test.go` add `ActivityStarted` with `actor: false` to the kind/privacy table.

In `discord_message_test.go` add:

```go
{"playing", "playing", "", "2/4 players (2 free)", 2, &four, 0xFF6B35}
```

or the equivalent existing table shape.

- [ ] **Step 2: Add a failing PostgreSQL notification-handoff test.**

In `notification_integration_test.go`:
1. create a Game Request;
2. initialize the notification baseline so its Created activity is historical to the worker;
3. Start the request;
4. call `DiscoverActivity`;
5. assert exactly one activity with kind `started`;
6. call `MaterializeWork` (zero targets is sufficient);
7. assert the corresponding `notification_work.kind = 'started'`.

This specifically protects the migration's `notification_work_kind_check`; without that constraint update the materialization step must fail.

- [ ] **Step 3: Run focused tests and confirm failure.**

Run:

```bash
go test ./internal/notification -count=1
task test:integration
```

Expected before implementation: Started is unsupported by notification Go switches/materialization.

- [ ] **Step 4: Implement the minimal notification changes.**

In `notification.go`, add `ActivityStarted` and update the comment from five to six durable kinds.

In `webpush.go`:

```go
case ActivityJoined, ActivityLeft, ActivityStarted, ActivityClosed, ActivityCancelled:
    participants, err := t.participants.ListParticipants(...)
    ...
    candidates = dedupeExcept(participants, activity.ActorID)
```

Leave `actorGameNick` restricted to Joined/Left.

In `webpush_payload.go`:

```go
case ActivityStarted, ActivityClosed, ActivityCancelled:
    // actor-neutral payload
```

In `discord_message.go`:

```go
case "playing":
    embed.Color = 0xFF6B35
```

Do not add a Started-specific Discord message.

- [ ] **Step 5: Run notification and integration tests.**

Run:

```bash
go test ./internal/notification -count=1
task test:integration
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/notification internal/platform/postgres/notification_integration_test.go
git commit -m "feat(notification): deliver game started activity"
```

---

### Task 6: Accept and render started Web Push in the service worker

**Files:**
- Modify: `web/src/service-worker.ts`
- Modify: `web/tests-pwa/service-worker.spec.ts`
- Generated by `task generate`, do not hand-edit:
  - `internal/web/assets/dist/service-worker.js`

**Service-worker contract:**

Extend:

```ts
type PushKind = "created" | "joined" | "left" | "started" | "closed" | "cancelled";
```

and `isPushKind` accordingly.

Started remains actor-neutral under the existing validation rule that only Joined/Left require `actor_game_nick`.

Title:
- Started: `payload.game`.
- Created retains the creator-based “is looking for” title.
- Other existing kinds retain current behavior.

Body prefix:
- Started: `Game started · `
- Closed/Cancelled: existing `Request ... · `
- Joined/Left: existing actor prefix.

A resulting Started notification should read conceptually:

```text
Title: Warcraft III
Body: Game started · <localized planned time> · 3/4 players · Friday Club
```

Visible same-origin LANsurfer window suppression remains unchanged and applies before rendering any kind.

- [ ] **Step 1: Extend PWA tests first.**

In the existing real service-worker push delivery test:
- deliver a valid `started` payload;
- assert it creates a notification with the Game title;
- assert the body contains `Game started ·`;
- assert no actor Game Nick is required.

In the visible-window suppression test, use or add a `started` payload and assert notification count remains unchanged while LANsurfer is visible.

Keep malformed/unknown-kind rejection coverage intact.

- [ ] **Step 2: Run the PWA suite and confirm failure.**

Run:

```bash
task test:pwa
```

Expected before implementation: Started payload is rejected by `isPushKind` / no Started notification appears.

- [ ] **Step 3: Implement the service-worker changes.**

Use explicit Started branches; do not rewrite the payload parser or introduce a notification framework.

A safe title/prefix shape is:

```ts
const title = payload.kind === "started"
  ? payload.game
  : payload.creator_game_nick
    ? `${payload.creator_game_nick} is looking for ${payload.game}`
    : payload.game;

const prefix = payload.kind === "joined" || payload.kind === "left"
  ? `${payload.actor_game_nick} ${payload.kind} · `
  : payload.kind === "started"
    ? "Game started · "
    : payload.kind === "closed" || payload.kind === "cancelled"
      ? `Request ${payload.kind} · `
      : "";
```

- [ ] **Step 4: Regenerate and test.**

Run:

```bash
task generate
task test:pwa
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add web/src/service-worker.ts web/tests-pwa/service-worker.spec.ts internal/web/assets/dist/service-worker.js
git commit -m "feat(pwa): render game started push"
```

---

### Task 7: Full regression verification and retire temporary planning artifacts

**Files:**
- Verify all implementation files changed in Tasks 1–6
- Delete only after every verification passes:
  - `docs/superpowers/specs/2026-09-30-game-request-playing-lifecycle-design.md`
  - `docs/superpowers/plans/2026-09-30-game-request-playing-lifecycle.md`
- Canonical docs already prepared on this branch and must remain:
  - `docs/PRODUCT.md`
  - `docs/ARCHITECTURE.md`
  - `docs/TERMINOLOGY.md`
  - `docs/ROADMAP.md`
  - `docs/BRANDING.md`
  - `docs/ui-kit/README.md`

- [ ] **Step 1: Regenerate from a clean source state.**

Run:

```bash
task generate
```

Inspect `git status`. Generated diffs must correspond only to changed templ/sqlc/frontend sources. Do not accept unrelated generated churn.

- [ ] **Step 2: Run the complete repository verification required by the spec.**

Run in this order:

```bash
task lint
task test
task test:integration
task test:browser
task test:pwa
task build
```

Expected: every command exits 0.

- [ ] **Step 3: Manually inspect the final lifecycle invariants in the diff.**

Confirm all of the following from code/tests, not from assumption:

- exactly one `StatusPlaying`, `EventStarted`, and each module's `ActivityStarted`;
- no direct open-to-closed code path remains;
- Close store path requires locked `playing`;
- Cancel and Start require locked `open`;
- Join/Leave still require locked `open`;
- migration has no `started_at` backfill;
- both activity-kind DB constraints include `started`;
- ListActive and CountActive use `open|playing`;
- Trending includes `started`;
- Started Push audience is participants minus actor;
- service worker Started payload is actor-neutral and visible-window suppression is unchanged;
- Discord has a Playing/Sunset current-state branch;
- no new runtime dependency/module/transport exists.

- [ ] **Step 4: Remove the temporary Superpowers spec and plan.**

Only now, after canonical docs contain the durable decisions and all tests pass:

```bash
git rm docs/superpowers/specs/2026-09-30-game-request-playing-lifecycle-design.md
git rm docs/superpowers/plans/2026-09-30-game-request-playing-lifecycle.md
```

This follows `AGENTS.md` / `docs/superpowers/README.md`: working artifacts guide the branch but normally do not survive the final merge once durable truth is harvested.

- [ ] **Step 5: Re-run the cheap final checks after cleanup.**

Run:

```bash
task lint
git status --short
```

Expected: lint exits 0; only the intended staged/unstaged changes for the final cleanup remain.

- [ ] **Step 6: Commit cleanup.**

```bash
git add -A
git commit -m "docs: retire playing lifecycle working artifacts"
```

- [ ] **Step 7: Update PR #112 before requesting final review.**

Update the PR description so it no longer says “implementation not started”. Record:
- implementation summary;
- migration/backward-compatibility rule;
- tests actually run and their results;
- any omitted manual validation;
- explicit statement that #101–#104 remain follow-up slices and were not implemented here.

Do not mark #100 complete until the implementation and required verification above are actually present on the PR branch.
