# Structured Game Request Session Information Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement issue #102: participant-only structured key/value session information with Host inline CRUD and participant-authorized realtime refresh on Game Request details.

**Architecture:** Keep the feature inside the existing `gamerequest` module. Persist request-local entries in a dedicated child table, authorize reads from current active membership, serialize Host mutations under the existing Game Request row lock, and expose a server-rendered HTMX fragment integrated into the detail page. Reuse #104's content-free participant-authorized SSE pattern, generalized to one neutral detail-request stream scope. #103 secret presentation is explicitly not implemented by this plan.

**Tech Stack:** Go, PostgreSQL, pgx, sqlc, goose, chi, templ, HTMX, SSE, TypeScript, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-03-game-request-session-information-design.md`

## Global Constraints

- Scope is #102 only; do not add `is_secret`, masking, Reveal/Hide, clipboard behavior, or at-rest encryption.
- Session information is readable only by the Host/currently joined participants while the request is `open` or `playing`.
- Active non-participants receive no keys, values, entry count, or entry metadata; they see only “Join this Game Request to see session information.”
- `closed`/`cancelled` requests show no session-information section and reject mutations; stored rows remain until request deletion.
- Only the Host may create, edit, or delete entries.
- Maximum 12 entries per Game Request; key max 40 Unicode code points; value max 300 Unicode code points.
- Key/value are single-line plain text, trim surrounding whitespace, reject empty-after-trim and CR/LF.
- Keys are unique per Game Request case-insensitively while preserving display casing.
- Ordering is creation order via `sequence bigint GENERATED ALWAYS AS IDENTITY`; edits never reorder.
- Session-information CRUD creates no `game_request_activity`, notification work, Web Push, Discord synchronization, or Trending effect.
- A semantic no-op update performs no row update and publishes no realtime signal.
- Realtime signal contains only event type + request ID and is delivered only to currently authorized viewers of that detail request.
- Reuse the current Game Request row lock for mutation serialization; do not introduce another lock/mutex or runtime service.
- Follow existing templ/HTMX progressive-enhancement patterns; no JSON API, SPA state, or new dependency.
- Migration `00026_game_request_messages.sql` already exists on the approved base. Confirm the next free migration before implementation; on the current branch it is expected to be `00027`.
- Generated templ/sqlc output is regenerated with `task generate`; never hand-edit generated files.
- Temporary spec/plan files under `docs/superpowers/` are removed before the feature branch is merged after durable decisions have been harvested.

## Review Focus

- **Unicode/whitespace boundaries:** 40/300 Unicode-code-point values are accepted, 41/301 rejected, and whitespace-only or CR/LF input never persists; pin this in Task 1 domain tests and Task 2 database-constraint tests.
- **Cross-request/stale entry IDs:** an entry ID from another request or a deleted entry must never be updated/deleted through a different request; pin this in Task 3 integration tests and Task 4 handler tests.
- **Concurrent 12th/13th creates:** racing Host creates cannot produce 13 rows; pin this in Task 3 integration tests.
- **Membership/lifecycle races:** leaving or a terminal transition must stop future reads/signals, while a mutation that committed before the transition remains legal; pin this in Task 3 integration tests and Task 5 realtime tests.
- **Live refresh during inline editing:** dirty/rejected key/value state survives only while the same editor remains authorized and active, and is discarded when lifecycle/access removes that editor; pin this in Task 6 browser tests.

---

### Task 1: Define the session-information domain contract and validation

**Files:**
- Create: `internal/gamerequest/session_information.go`
- Create: `internal/gamerequest/session_information_test.go`
- Modify: `internal/gamerequest/gamerequest.go`
- Modify: `internal/gamerequest/gamerequest_test.go`

**Interfaces:**
- Consumes: existing `Service.repository`, `Service.publish`, `Event`, `ErrNotMember`, `ErrNotCreator`, `ErrNotActive`, and the request-level PostgreSQL locking contract implemented by the repository.
- Produces:
  - `type SessionInformationEntry struct { ID, RequestID, Key, Value string; Sequence int64; CreatedAt, UpdatedAt time.Time }`
  - `type SessionInformation struct { Entries []SessionInformationEntry; CanEdit bool }`
  - `type SessionInformationInput struct { Key, Value string }`
  - `const EventSessionInformationChanged EventType = "GameRequestSessionInformationChanged"`
  - errors `ErrInvalidSessionInformationKey`, `ErrInvalidSessionInformationValue`, `ErrSessionInformationKeyExists`, `ErrSessionInformationLimit`, `ErrSessionInformationEntryNotFound`
  - service methods:
    - `SessionInformation(ctx context.Context, requestID, userID string) (SessionInformation, error)`
    - `CreateSessionInformationEntry(ctx context.Context, requestID, userID string, input SessionInformationInput) error`
    - `UpdateSessionInformationEntry(ctx context.Context, requestID, entryID, userID string, input SessionInformationInput) error`
    - `DeleteSessionInformationEntry(ctx context.Context, requestID, entryID, userID string) error`
    - `CanSessionInformation(ctx context.Context, requestID, userID string) (bool, error)`
  - repository methods matching the above persistence needs, with `UpdateSessionInformationEntry(...)(bool, error)` returning whether a row actually changed and `SessionInformationAccess(ctx,... ) error` returning the existing access errors.

- [ ] **Step 1: Write failing validation and event-publication tests**

In `internal/gamerequest/session_information_test.go`, add focused tests with these assertions:

```go
func TestSessionInformationInputValidation(t *testing.T) {
    // "  Server  " / "  10.0.0.1  " reaches the repository as "Server" / "10.0.0.1".
    // empty/whitespace key => ErrInvalidSessionInformationKey.
    // empty/whitespace value => ErrInvalidSessionInformationValue.
    // key containing "\n" or "\r" => ErrInvalidSessionInformationKey.
    // value containing "\n" or "\r" => ErrInvalidSessionInformationValue.
    // strings.Repeat("ä", 40) accepted; 41 rejected.
    // strings.Repeat("界", 300) accepted; 301 rejected.
}

func TestSessionInformationEventsOnlyPublishForRealMutations(t *testing.T) {
    // create => exactly one EventSessionInformationChanged with request ID.
    // update repository changed=false => no event.
    // update changed=true => exactly one event.
    // delete => exactly one event.
    // repository error => no event.
}
```

Also add `CanSessionInformation` tests proving `ErrNotFound`, `ErrNotMember`, and `ErrNotActive` become `false, nil`, while unexpected repository errors are returned.

- [ ] **Step 2: Run the focused domain tests and verify they fail**

Run:

```bash
go test ./internal/gamerequest -run 'TestSessionInformation' -count=1
```

Expected: FAIL because the session-information types/methods/errors do not exist.

- [ ] **Step 3: Implement the domain types, validation, service methods, and repository interface**

In `internal/gamerequest/session_information.go`:

- trim key/value with `strings.TrimSpace`;
- use `utf8.RuneCountInString` for the 40/300 limits;
- reject `\r` and `\n`;
- validate before repository calls;
- publish `EventSessionInformationChanged` only after successful create/delete and changed update;
- make `CanSessionInformation` mirror chat's fail-closed access helper for expected access errors.

Extend `Repository` in `gamerequest.go` with the exact methods from the Interfaces block. Add no secret field.

Update `recordingRepository` in `gamerequest_test.go` with minimal stubs/recording fields needed to satisfy the expanded interface and the new tests.

- [ ] **Step 4: Run the focused domain tests and verify they pass**

Run:

```bash
go test ./internal/gamerequest -run 'TestSessionInformation' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run all gamerequest unit tests**

Run:

```bash
go test ./internal/gamerequest -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/gamerequest/session_information.go internal/gamerequest/session_information_test.go internal/gamerequest/gamerequest.go internal/gamerequest/gamerequest_test.go
git commit -m "feat(gamerequest): define session information domain"
```

---

### Task 2: Add the session-entry schema and authorized PostgreSQL read/create path

**Files:**
- Create: `internal/platform/postgres/migrations/00027_game_request_session_entries.sql` (or the actual next free migration if `main` advanced)
- Create: `internal/gamerequest/postgres/queries/session_information.sql`
- Create: `internal/gamerequest/postgres/session_information.go`
- Create: `internal/platform/postgres/game_request_session_information_integration_test.go`
- Regenerate: `internal/gamerequest/postgres/sqlcgen/*` via `task generate`

**Interfaces:**
- Consumes: Task 1's `SessionInformation`, `SessionInformationEntry`, `SessionInformationInput`, access errors, and the existing `Store.withLockedRequest`.
- Produces: PostgreSQL implementation of `SessionInformation`, `SessionInformationAccess`, and `CreateSessionInformationEntry`.

- [ ] **Step 1: Confirm the migration number before writing the migration**

Run:

```bash
ls internal/platform/postgres/migrations | tail
```

Expected on the approved base: `00026_game_request_messages.sql` is the highest migration, so use `00027_game_request_session_entries.sql`. If a newer migration exists, use the next free number and update this plan's filename while preserving the schema below.

- [ ] **Step 2: Write failing integration tests for schema, read authorization, validation constraints, and create**

In `internal/platform/postgres/game_request_session_information_integration_test.go`, use `newPlayingFixture(t)` and add:

```go
func TestGameRequestSessionInformationReadAccess(t *testing.T) {
    // Host creates "Server" -> "10.20.30.40:27015".
    // Joined guest reads the same value.
    // Outsider gets ErrNotMember and no values.
    // After guest Leave, guest gets ErrNotMember.
    // After rejoin, guest reads the current value again.
    // While Playing, Host/guest still read.
    // After Closed, Host/guest get ErrNotActive.
}

func TestGameRequestSessionInformationCreateValidationAndDatabaseConstraints(t *testing.T) {
    // Domain accepts exact 40-rune key and 300-rune value.
    // Direct SQL attempts with 41/301 chars, CR/LF, or empty text fail a CHECK constraint.
    // "Server" then "server" for one request returns ErrSessionInformationKeyExists.
    // Same key on another request succeeds.
    // Stored display casing and trimmed values are preserved.
}
```

The direct-SQL checks deliberately prove the DB guardrails independently of the domain validator.

- [ ] **Step 3: Run the focused integration tests and verify they fail**

Start/use the isolated test database and run:

```bash
docker compose -p lansurfer-test -f compose.test.yaml up -d --wait
LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable'   go test -count=1 -tags=integration ./internal/platform/postgres -run 'TestGameRequestSessionInformation(ReadAccess|CreateValidationAndDatabaseConstraints)'
```

Expected: FAIL because the migration/store/query methods do not exist.

- [ ] **Step 4: Add the migration**

Create the table with:

- UUID primary key default `gen_random_uuid()`;
- `game_request_id` FK `ON DELETE CASCADE`;
- `key text NOT NULL`, `value text NOT NULL`;
- `sequence bigint GENERATED ALWAYS AS IDENTITY`;
- `created_at` / `updated_at timestamptz NOT NULL DEFAULT now()`;
- CHECKs for key 1..40, value 1..300, and absence of CR/LF;
- unique index on `(game_request_id, lower(key))`;
- ordering index on `(game_request_id, sequence)`.

Do not add `is_secret`.

- [ ] **Step 5: Add sqlc queries for a single-snapshot authorized read and create**

In `internal/gamerequest/postgres/queries/session_information.sql`, define:

- `ReadGameRequestSessionInformation :many`: start from `game_requests`, compute current membership for `viewer_id`, and LEFT JOIN entries only when status is active and membership is true. Return request status, membership, `creator_id = viewer_id AS can_edit`, plus nullable entry columns ordered by sequence. This one statement must return one request row even for an authorized empty list while never returning entry values to an unauthorized viewer.
- `GameRequestSessionInformationAccess :one`: request status + current membership for SSE authorization.
- `CountGameRequestSessionEntries :one`.
- `CreateGameRequestSessionEntry :one`.

- [ ] **Step 6: Regenerate sqlc code**

Run:

```bash
task generate
```

Expected: generated gamerequest sqlc code compiles; inspect the generated nullable fields from the LEFT JOIN rather than hand-editing them.

- [ ] **Step 7: Implement the PostgreSQL read/access/create methods**

In `internal/gamerequest/postgres/session_information.go`:

- parse request/user IDs using existing helpers;
- map no request row to `ErrNotFound`;
- map inactive status to `ErrNotActive`;
- map non-member to `ErrNotMember`;
- map the authorized empty sentinel row to `SessionInformation{CanEdit: ..., Entries: nil}`;
- map entry rows to domain values in sequence order;
- create under `withLockedRequest`, checking creator, active status, and count < 12 before INSERT;
- map PostgreSQL unique violation `23505` to `ErrSessionInformationKeyExists`;
- map FK/missing request cases to the existing not-found behavior.

Add a small `isUniqueViolation` helper next to the existing PostgreSQL error helpers if needed.

- [ ] **Step 8: Run the focused integration tests and verify they pass**

Run the command from Step 3 again.

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/platform/postgres/migrations internal/gamerequest/postgres internal/platform/postgres/game_request_session_information_integration_test.go
git commit -m "feat(gamerequest): persist participant session information"
```

---

### Task 3: Complete mutation invariants, ordering, no-op behavior, and concurrency

**Files:**
- Modify: `internal/gamerequest/postgres/queries/session_information.sql`
- Modify: `internal/gamerequest/postgres/session_information.go`
- Modify: `internal/platform/postgres/game_request_session_information_integration_test.go`
- Regenerate: `internal/gamerequest/postgres/sqlcgen/*`

**Interfaces:**
- Consumes: Task 2's table/read/create path and Task 1's update/delete service contract.
- Produces: PostgreSQL `UpdateSessionInformationEntry(...)(bool,error)` and `DeleteSessionInformationEntry(... ) error` with all request-local invariants serialized under the Game Request row lock.

- [ ] **Step 1: Add failing integration tests for update/delete/no-op/ordering**

Add:

```go
func TestGameRequestSessionInformationUpdateDeleteAndOrdering(t *testing.T) {
    // Create Server, Lobby, Meeting; capture IDs and sequence order.
    // Rename Lobby -> Room and change value; ID and sequence stay unchanged.
    // Re-submit normalized identical Room/value => success with no updated_at change and no event-side DB effect.
    // Delete Server; remaining order is Room then Meeting.
    // Update/delete with an entry ID belonging to another request => ErrSessionInformationEntryNotFound.
    // Repeating delete on removed entry => ErrSessionInformationEntryNotFound.
}

func TestGameRequestSessionInformationCreatesNoActivity(t *testing.T) {
    // Count game_request_activity before create/update/delete.
    // Count is unchanged after all three session-info mutations.
}
```

- [ ] **Step 2: Add failing integration tests for limit, lifecycle, and races**

Add:

```go
func TestGameRequestSessionInformationLimitSerializesConcurrentCreates(t *testing.T) {
    // Seed 11 unique entries.
    // Race two unique creates with runConcurrently.
    // Exactly one succeeds and the loser is ErrSessionInformationLimit.
    // Final row count is 12.
}

func TestGameRequestSessionInformationSerializesWithLifecycle(t *testing.T) {
    // On a playing request, race entry update with Close.
    // Legal outcomes: update commits before Close, or update returns ErrNotActive after Close wins.
    // Never observe a session mutation that begins after terminal state and still succeeds.
}

func TestGameRequestSessionInformationCascadesWithRequest(t *testing.T) {
    // Create entry, delete owning game_requests row, assert child count = 0.
}
```

Also add direct database assertions that the per-request case-insensitive unique index rejects a collision even when bypassing the service.

- [ ] **Step 3: Run the focused integration tests and verify they fail**

Run:

```bash
LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable'   go test -count=1 -tags=integration ./internal/platform/postgres -run 'TestGameRequestSessionInformation'
```

Expected: new update/delete/concurrency tests FAIL.

- [ ] **Step 4: Add update/delete sqlc queries and implement the store methods**

Queries:

- fetch one entry by `game_request_id + entry_id` while the request row lock is already held;
- update key/value and `updated_at = now()`;
- delete by `game_request_id + entry_id`.

Store behavior:

- take `withLockedRequest` first;
- require Host and active lifecycle;
- reject cross-request/missing entry with `ErrSessionInformationEntryNotFound`;
- compare current normalized key/value before UPDATE and return `changed=false` for semantic no-op;
- map case-insensitive rename collision to `ErrSessionInformationKeyExists`;
- delete exactly one matching row.

- [ ] **Step 5: Regenerate sqlc code and rerun all session-information integration tests**

Run:

```bash
task generate
LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable'   go test -count=1 -tags=integration ./internal/platform/postgres -run 'TestGameRequestSessionInformation'
```

Expected: PASS.

- [ ] **Step 6: Stop the focused test database**

Run:

```bash
docker compose -p lansurfer-test -f compose.test.yaml down --volumes --remove-orphans
```

Expected: test containers/volumes removed.

- [ ] **Step 7: Commit**

```bash
git add internal/gamerequest/postgres internal/platform/postgres/game_request_session_information_integration_test.go
git commit -m "feat(gamerequest): enforce session information invariants"
```

---

### Task 4: Add server-rendered inline CRUD to the Game Request detail page

**Files:**
- Create: `internal/web/handler/session_information.go`
- Create: `internal/web/handler/session_information_test.go`
- Create: `internal/web/pages/session_information.templ`
- Create: `internal/web/pages/session_information_test.go`
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/router.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/app/app.go`
- Regenerate: `internal/web/pages/session_information_templ.go`, `internal/web/pages/game_requests_templ.go`

**Interfaces:**
- Consumes: Task 1's service methods and Task 2/3's authorization/errors.
- Produces:
  - handler interface `GameRequestSessionInformation` with the five Task 1 service methods used by web/realtime;
  - `pages.SessionInformationView` and `pages.SessionInformationForm`;
  - Game Request routes:
    - `GET /game-requests/{requestID}/session-information`
    - `POST /game-requests/{requestID}/session-information`
    - `POST /game-requests/{requestID}/session-information/{entryID}`
    - `POST /game-requests/{requestID}/session-information/{entryID}/delete`
  - full-detail query state `session_add=1` or `session_edit=<entryID>` for no-JS fallback and refresh preservation.

- [ ] **Step 1: Write failing page-component tests**

In `internal/web/pages/session_information_test.go`, render the new component and assert:

- authorized participant sees keys/values but no Add/Edit/Delete;
- Host sees Add plus per-entry Edit;
- active non-participant view contains exactly the neutral join hint and none of supplied key/value/count metadata;
- Host add mode renders Key/Value fields with max lengths 40/300;
- Host edit mode pre-fills the selected entry and keeps a stable entry ID;
- error form marks the correct field and retains submitted values;
- section markup precedes `Lobby chat` when both are rendered by `GameRequestDetailContent`;
- terminal detail rendering contains no `Session information` section.

- [ ] **Step 2: Write failing handler tests for read + create/update/delete**

In `internal/web/handler/session_information_test.go`, create a focused fake implementing `GameRequestSessionInformation` and assert:

- GET authorized -> 200 fragment with values;
- GET `ErrNotMember` -> 200 join hint with no values;
- GET `ErrNotActive` -> 200 empty/omitted section fragment;
- Host create/update/delete call the exact service method with request/entry/viewer IDs;
- successful HTMX mutation replaces `#session-information` and clears editor URL state via `HX-Replace-Url`;
- successful non-HTMX mutation redirects to `/game-requests/{id}`;
- invalid key/value -> inline form with retained raw submission;
- duplicate key -> key-local “already exists” error;
- 13th entry -> form-level limit error;
- non-Host/inactive/missing-entry map to 403/409/404 as appropriate;
- a foreign/stale `entryID` is passed only to the scoped service update/delete and never treated as an upsert.

- [ ] **Step 3: Run focused web tests and verify they fail**

Run:

```bash
go test ./internal/web/handler ./internal/web/pages -run 'SessionInformation' -count=1
```

Expected: FAIL because the web interfaces/components/routes do not exist.

- [ ] **Step 4: Implement the focused web view/form types and templates**

In `session_information.templ` define:

- `SessionInformationView`: request ID, entries, `CanEdit`, `JoinRequired`, `Visible`, current form;
- `SessionInformationForm`: mode (`add`/`edit`), entry ID, key, value, error text, error field;
- `GameRequestSessionInformation(view SessionInformationView)`.

UI behavior:

- compact `detail-card` in the main column immediately after Players and before Lobby chat;
- Add/Edit are normal links to full-detail URLs (`?session_add=1`, `?session_edit=<id>`) enhanced with `hx-get`, `hx-target="#session-information"`, `hx-swap="outerHTML"`, and URL replacement;
- only one form is represented by `SessionInformationForm`;
- Save posts to create/update endpoint and targets the section;
- Cancel links to plain detail URL and HTMX-loads the base section;
- Delete is a POST with the existing `data-confirm` behavior;
- no manual ordering UI.

- [ ] **Step 5: Integrate session information into full detail rendering**

In `handler/session_information.go`, add helpers that:

- call the service for current viewer;
- map `ErrNotMember` to `JoinRequired`;
- map `ErrNotActive` to `Visible=false`;
- determine add/edit mode only when `CanEdit`;
- for edit mode, find the requested entry in the authorized current entries; missing entry => `ErrSessionInformationEntryNotFound`.

Modify `GameRequests` to carry a separate narrow `sessionInformation GameRequestSessionInformation` dependency. `View` and `RefreshView` load session info separately from `queries.ByID` and pass it to `GameRequestDetail` / `GameRequestDetailContent`.

Extend `detailRefreshURL` so current general Host edit and session add/edit query state survive a full-detail refresh.

Keep the existing Game Request list/read model unchanged.

- [ ] **Step 6: Implement mutation handlers and error mapping**

Implement methods on `*GameRequests` in `handler/session_information.go` for fragment/create/update/delete.

For invalid submissions:

- key error copy: “Key is required, must be one line, and can be at most 40 characters.”
- value error copy: “Value is required, must be one line, and can be at most 300 characters.”
- duplicate key: “This key already exists in this Game Request.”
- limit: “A Game Request can have at most 12 session-information entries.”

HTMX validation/conflict responses render the section so the form is swapped; non-HTMX errors render the full detail page with the same retained form state. Do not put business validation in the handler; it only maps domain errors.

- [ ] **Step 7: Wire routes and dependencies**

In `RouterDependencies`, add a narrow `SessionInformation handler.GameRequestSessionInformation` dependency and pass it to `NewGameRequests`. Register the four routes only when the dependency is non-nil, mirroring optional chat wiring in tests.

In `internal/app/app.go`, set `SessionInformation: gameRequests`.

Update existing constructor call sites/fakes in handler/router tests.

- [ ] **Step 8: Regenerate templ output and run focused tests**

Run:

```bash
task frontend:build
go test ./internal/web/handler ./internal/web/pages ./internal/web -run 'SessionInformation' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/web internal/app/app.go
git commit -m "feat(web): add inline Game Request session information"
```

---

### Task 5: Add participant-authorized session-information realtime delivery

**Files:**
- Modify: `internal/web/handler/realtime.go`
- Modify: `internal/web/handler/realtime_test.go`
- Modify: `internal/web/router.go`
- Modify: `internal/app/app.go`
- Modify: `web/src/app.ts`

**Interfaces:**
- Consumes: Task 1's `EventSessionInformationChanged` and `CanSessionInformation`; #104's `ChatAccess` pattern.
- Produces:
  - `SessionInformationAccess interface { CanSessionInformation(context.Context, string, string) (bool, error) }`
  - neutral SSE detail scope query `detailRequestId` replacing the chat-specific `chatRequestId`;
  - SSE event name `game-request-session-information`.

- [ ] **Step 1: Write failing realtime-handler tests**

Extend `realtime_test.go`:

```go
func TestRealtimeSessionInformationSignalsOnlyReachParticipants(t *testing.T) {
    // member watching detailRequestId=request-1 receives event: game-request-session-information.
    // outsider receives no session-information event.
    // ordinary GameRequestStarted still reaches both.
}

func TestRealtimeSkipsSessionInformationAuthorizationForUnwatchedRequest(t *testing.T) {
    // signal request-2 while stream watches detailRequestId=request-1.
    // CanSessionInformation is never called and no dedicated event is sent.
}

func TestRealtimeUsesNeutralDetailScopeForChatAndSessionInformation(t *testing.T) {
    // chat and session signals for the selected request both work with detailRequestId.
    // old chatRequestId is no longer the production contract.
}
```

Also update existing chat tests to use `detailRequestId`.

- [ ] **Step 2: Run focused realtime tests and verify they fail**

Run:

```bash
go test ./internal/web/handler -run 'TestRealtime(Chat|Session|UsesNeutral)' -count=1
```

Expected: FAIL until the new dependency/event routing exists.

- [ ] **Step 3: Generalize the realtime handler**

Change `Realtime` to hold both `ChatAccess` and `SessionInformationAccess`.

For each dedicated event:

1. compare `event.RequestID` with `r.URL.Query().Get("detailRequestId")` before any authorization query;
2. read current user;
3. call the matching access method;
4. fail closed on nil dependency, empty viewer, error, or false;
5. set event name to `game-request-chat` or `game-request-session-information`;
6. marshal the existing content-free `gamerequest.Event`.

Do not include entry/key/value data.

- [ ] **Step 4: Wire the realtime session-information dependency**

Add `RealtimeSessionInformation handler.SessionInformationAccess` to `RouterDependencies`.

Construct:

```go
handler.NewRealtime(dependencies.Realtime, dependencies.RealtimeChat, dependencies.RealtimeSessionInformation)
```

Set both `RealtimeChat: gameRequests` and `RealtimeSessionInformation: gameRequests` in `internal/app/app.go`.

- [ ] **Step 5: Update the browser EventSource contract**

In `web/src/app.ts`:

- replace `chatRequestId` with `detailRequestId`;
- include `GameRequestSessionInformationChanged` in the `GameRequestEvent` type;
- listen for `game-request-session-information`;
- if the current detail request matches and `#session-information` exists, GET the section's own `data-refresh-url` and outer-swap only `#session-information`;
- leave general `game-request` handling unchanged.

- [ ] **Step 6: Run realtime Go tests and TypeScript typecheck**

Run:

```bash
go test ./internal/web/handler -run 'TestRealtime' -count=1
npm run typecheck
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web/handler/realtime.go internal/web/handler/realtime_test.go internal/web/router.go internal/app/app.go web/src/app.ts
git commit -m "feat(web): stream participant session information updates"
```

---

### Task 6: Preserve inline drafts across live refreshes and exercise browser-only behavior

**Files:**
- Modify: `web/src/app.ts`
- Modify: `internal/web/browsertest/main.go`
- Modify: `web/tests/live-updates.spec.ts`

**Interfaces:**
- Consumes: Task 4's `data-session-information-form` / section refresh URL contract and Task 5's dedicated SSE event.
- Produces: field-level session-information draft preservation across section/full-detail HTMX swaps.

- [ ] **Step 1: Add failing browser tests for dedicated refresh + draft preservation**

In `web/tests/live-updates.spec.ts`, add:

```ts
test("a session-information signal refreshes only the section and preserves a dirty inline draft", async ({ page, request }) => {
  // Create a request as Host, open Add information, type unsaved key/value.
  // Publish GameRequestSessionInformationChanged for this request.
  // Assert the section refreshed and both dirty values/focus remain.
  // Assert the rest of #game-request-detail was not replaced by this dedicated event.
});

test("a terminal detail refresh discards a session-information draft", async ({ page }) => {
  // Open Add information and type a draft.
  // Cancel the open Game Request through a real request mutation.
  // General Game Request realtime refresh makes status Cancelled.
  // Assert session-information editor/section is gone; draft is not resurrected.
});
```

These are browser tests because they pin HTMX swap/focus/draft behavior, not CRUD business behavior.

- [ ] **Step 2: Extend the browser fixture just enough for session-information interaction**

In `internal/web/browsertest/main.go`:

- add `sessionInformation []gamerequest.SessionInformationEntry` per fixture request and a simple next-entry counter/ID;
- implement the Task 1 service interface methods in-memory with the same active/member/Host rules needed by the tests;
- publish `EventSessionInformationChanged` after real mutations;
- implement `CanSessionInformation`;
- wire `SessionInformation: state` and `RealtimeSessionInformation: state`;
- set creator access explicitly even if the fixture's historical `members` map does not contain the creator.

Do not turn the fixture into a duplicate database implementation; it exists only to exercise browser interaction.

- [ ] **Step 3: Implement field-level draft capture/restore**

In `web/src/app.ts`, parallel the existing Host-detail draft strategy:

- before a swap targeting either `#session-information` or `#game-request-detail`, capture current session form mode, entry ID, dirty/rejected key/value values, focus, and inline error text;
- after swap, restore only if the server rendered the same legal form mode/entry ID;
- never restore a form that disappeared because lifecycle/access changed;
- preserve server-rendered structure and untouched fields rather than preserving the old whole form DOM.

Use a small typed draft structure and data attributes from Task 4; do not use a frontend state framework.

- [ ] **Step 4: Build frontend and run the two focused browser tests**

Run:

```bash
task frontend:build
npx playwright test web/tests/live-updates.spec.ts --grep 'session-information'
```

Expected: PASS.

- [ ] **Step 5: Run the existing chat-draft regression too**

Run:

```bash
npx playwright test web/tests/live-updates.spec.ts --grep 'chat draft|session-information'
```

Expected: PASS; the neutral detail stream scope and new draft logic must not regress #104 chat preservation.

- [ ] **Step 6: Commit**

```bash
git add web/src/app.ts internal/web/browsertest/main.go web/tests/live-updates.spec.ts
git commit -m "test(web): preserve session information drafts on live refresh"
```

---

### Task 7: Harvest durable truth, align issues, clean working artifacts, and verify the branch

**Files:**
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/TERMINOLOGY.md`
- Delete before merge: `docs/superpowers/specs/2026-10-03-game-request-session-information-design.md`
- Delete before merge: `docs/superpowers/plans/2026-10-03-structured-session-information.md`
- Regenerate/verify generated files affected by earlier tasks.

**Interfaces:**
- Consumes: completed #102 behavior.
- Produces: canonical documentation matching the implementation and updated GitHub issue text for #102/#103.

- [ ] **Step 1: Update PRODUCT.md**

In the Game Request coordination section, make the durable product behavior explicit:

- generic session information is participant-only for Host + currently joined players while active;
- Host alone mutates it;
- terminal requests do not expose it;
- session-information CRUD is not Game Request activity/notification behavior;
- secret handling remains scheduled under #103 and changes visual privacy rather than audience.

Do not claim #103 is implemented.

- [ ] **Step 2: Update ARCHITECTURE.md**

Document implemented #102 invariants:

- `game_request_session_entries` ownership under `gamerequest`;
- UUID child rows, stable identity `sequence`, case-insensitive per-request keys;
- active-membership authorized reads;
- Host mutation under the existing Game Request row lock;
- max 12 enforced under that lock;
- no activity/notification handoff;
- content-free participant-authorized `GameRequestSessionInformationChanged` realtime event;
- neutral `detailRequestId` stream scope shared by chat/session info.

Do not document #103 plaintext-secret implementation as current architecture yet.

- [ ] **Step 3: Update TERMINOLOGY.md**

Add:

- **Session information**: request-local generic key/value coordination data, readable only by Host/current participants while active and editable only by Host.
- **Secret session information**: scheduled #103 presentation-privacy extension with the same participant audience; do not describe it as implemented until #103 lands.

- [ ] **Step 4: Align GitHub issues #102 and #103**

Update #102 acceptance criteria to include the approved participant-only audience, active lifecycle, limits, case-insensitive key uniqueness, inline CRUD, no activity/notification side effects, and participant-authorized realtime.

Update #103 wording so it no longer implies that only secret entries are participant-only: all session information already has that audience; #103 adds masked/copy/reveal presentation privacy. Keep #103 open.

- [ ] **Step 5: Run generation and inspect the diff**

Run:

```bash
task generate
git status --short
git diff --check
```

Expected: generated templ/sqlc/frontend output is current; no whitespace errors; no unexpected files.

- [ ] **Step 6: Run unit/lint/build verification**

Run:

```bash
task lint
task test
task build
```

Expected: all PASS.

- [ ] **Step 7: Run PostgreSQL integration verification**

Run:

```bash
task test:integration
```

Expected: PASS, including session-information concurrency/access tests and existing chat/capacity/lifecycle tests.

- [ ] **Step 8: Run browser verification**

Run:

```bash
task test:browser
```

Expected: PASS, including existing chat draft/realtime tests and the new session-information live-draft tests.

- [ ] **Step 9: Confirm #102 adds no activity/notification/Discord coupling**

Inspect the final diff:

```bash
git diff main...HEAD -- internal/gamerequest internal/notification docs/ARCHITECTURE.md
```

Expected: session-information mutations contain no `recordActivity`, notification-store write, Web Push target, or Discord rendering path.

- [ ] **Step 10: Remove temporary Superpowers artifacts before the final feature commit**

After the canonical docs and GitHub issues carry the durable decisions:

```bash
git rm docs/superpowers/specs/2026-10-03-game-request-session-information-design.md
git rm docs/superpowers/plans/2026-10-03-structured-session-information.md
```

Expected: no #102 working spec/plan remains in the final tree.

- [ ] **Step 11: Commit durable docs and cleanup**

```bash
git add docs internal web
git commit -m "docs: document Game Request session information"
```

- [ ] **Step 12: Verify clean final state**

Run:

```bash
git status --short
git diff --check main...HEAD
```

Expected: clean worktree and no diff-check errors.

At implementation completion, use the normal Superpowers verification/code-review/branch-finishing flow before opening or merging the PR for #102. #103 remains a separate follow-up with its own implementation plan after #102 is merged.
