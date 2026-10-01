# Game Request Host Edit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace PR #118's capacity-only Host editor with one inline Game Request editor for capacity, planned start, and description while preserving #101 concurrency and delivery guarantees.

**Architecture:** Keep one Host-only `UpdateDetails` command on the Game Request aggregate. It serializes through the existing `game_requests FOR UPDATE` boundary, writes at most one `request_updated` Activity row with typed before/after metadata, publishes one SSE event, and lets the existing Discord sync render the current snapshot. The Game and actual `started_at` never enter the update contract.

**Tech Stack:** Go, PostgreSQL, pgx, sqlc, goose, chi, templ/HTMX, SSE, Playwright, existing notification/Discord infrastructure.

**Spec:** `docs/superpowers/specs/2026-10-02-game-request-host-edit-design.md`

## Global Constraints

- Work only on `feat/issue-101-dynamic-capacity`.
- PR #118 is unmerged; rewrite its branch-only capacity update semantics rather than adding compatibility for an unshipped `capacity_changed` model.
- The current refined Capacity control must be moved into the shared editor, not visually redesigned or regressed.
- `open`: Host may edit Max players, planned start, and description.
- `playing`: Host may edit Max players and description; planned start is immutable.
- `closed` / `cancelled`: no editing.
- Game, creator, lifecycle status, and `started_at` are never update inputs.
- Blank/`0` capacity means unlimited; finite edited values are `2..999`; legacy stored `1` remains readable.
- Capacity may never be reduced below current membership.
- Description is trimmed and remains limited to 1000 runes.
- One Save is one atomic mutation and at most one Activity/SSE/Discord synchronization.
- Semantic no-op writes nothing.
- Replace `capacity_changed` with `request_updated`.
- Capacity before/after and planned-start before/after are typed Activity metadata; description records only a boolean changed marker.
- Request updates produce no personal Web Push and do not affect Trending.
- No generic JSON metadata, patch framework, second outbox, new broker, WebSocket, SPA, or runtime dependency.
- Generated sqlc/templ/frontend artifacts are regenerated with project tasks, not hand-edited.
- Temporary spec/plan are removed only after full verification and canonical docs are updated.

## Review Focus

- Merge with current `origin/main` before editing so migration numbering and current code are reconciled first.
- A manipulated playing-state request must not change `starts_at`, even if the browser omits/disables that control.
- Updating several fields in one Save must create exactly one `request_updated` Activity and one SSE event.
- Legacy `max_players = 1` remains readable but cannot be newly submitted.
- No-op updates must leave `updated_at`, Activity, notification work, and Discord untouched.

---

### Task 0: Synchronize the feature worktree with current main

**Files:** none intentionally; conflict resolution may touch files already changed by PR #118.

**Interfaces:** Produces the clean current baseline every later task assumes.

- [ ] **Step 1: Confirm isolated feature worktree and clean state**

Run:

```bash
git branch --show-current
git status --short
git worktree list --porcelain
```

Expected:
- current branch is `feat/issue-101-dynamic-capacity`;
- current worktree is the intended isolated worktree;
- working tree is clean.

If local uncommitted work exists, do not discard it. Stop and resolve/preserve it before continuing.

- [ ] **Step 2: Fetch and merge current main explicitly**

Run:

```bash
git fetch origin
git merge origin/main
```

Do **not** use a configuration-dependent plain `git pull`; the intended operation is an explicit merge of current `origin/main` into the feature branch.

If conflicts occur:
- resolve them preserving both current main behavior and PR #118's approved #101 behavior;
- pay particular attention to migrations, canonical docs, Game Request handlers/templates, generated files, and notification code;
- regenerate generated files rather than hand-merging generated output where practical.

- [ ] **Step 3: Reconcile migration numbering**

Inspect:

```bash
ls internal/platform/postgres/migrations
```

If main now owns the migration number currently used by PR #118's dynamic-capacity migration, rename that branch migration to the next free number and update tests expecting the latest schema version.

Because `capacity_changed` has never shipped on main, do not create a second migration solely to convert it to `request_updated`; edit the unmerged branch migration in place.

- [ ] **Step 4: Restore a green baseline before feature edits**

Run:

```bash
task generate
task lint
task test
task test:integration
```

Expected: all exit 0 before starting Task 1.

- [ ] **Step 5: Commit merge/conflict-resolution only if Git did not already create the merge commit**

Keep synchronization changes separate from the feature rewrite. Do not mix Task 1 implementation into the merge/conflict commit.

---

### Task 1: Replace capacity-only domain API with atomic details update

**Files:**
- Modify: `internal/gamerequest/gamerequest.go`
- Modify: `internal/gamerequest/gamerequest_test.go`

**Produces:**
- `UpdateDetailsInput`
- `Repository.UpdateDetails(context.Context, string, string, UpdateDetailsInput) (bool, error)`
- `Service.UpdateDetails(context.Context, string, string, UpdateDetailsInput) error`
- `EventUpdated = "GameRequestUpdated"`
- `ActivityUpdated = "request_updated"`
- remove/replace branch-only `UpdateCapacity`, `EventCapacityChanged`, and `ActivityCapacityChanged`.

- [ ] **Step 1: Write failing service tests**

Cover:
- capacity-only change;
- start-only change;
- description-only change;
- multiple fields in one input;
- repository `changed=false` publishes no event;
- repository error publishes no event;
- finite max `1` rejected;
- invalid/zero start rejected;
- description >1000 runes rejected and valid description trimmed.

- [ ] **Step 2: Implement the domain input and service**

Use:

```go
type UpdateDetailsInput struct {
    MaxPlayers  *int32
    StartsAt    time.Time
    Description string
}
```

Share validation helpers with Create where doing so keeps rules single-sourced; do not force Create and Update into one command.

Publish `GameRequestUpdated` only when repository returns `changed == true`.

- [ ] **Step 3: Run unit tests**

Run:

`go test -race ./internal/gamerequest -count=1`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/gamerequest
git commit -m "feat(gamerequest): generalize host request editing"
```

---

### Task 2: Rewrite branch migration and PostgreSQL mutation

**Files:**
- Modify/rename: PR #118 dynamic-capacity migration (currently `internal/platform/postgres/migrations/00025_game_request_dynamic_capacity.sql`, subject to Task 0 renumbering)
- Modify: `internal/gamerequest/postgres/queries/game_requests.sql`
- Modify: `internal/gamerequest/postgres/store.go`
- Modify: `internal/platform/postgres/game_request_dynamic_capacity_integration_test.go` (rename file/test vocabulary if useful)
- Modify: schema-version tests
- Generate: gamerequest sqlc output

**Produces:** one locked atomic `UpdateDetails` transaction and `request_updated` typed metadata.

- [ ] **Step 1: Rewrite migration tests first**

The unmerged migration must define `request_updated`, not `capacity_changed`.

Activity columns:
- `previous_max_players integer NULL`
- `new_max_players integer NULL`
- `previous_starts_at timestamptz NULL`
- `new_starts_at timestamptz NULL`
- `description_changed boolean NOT NULL DEFAULT false`

Constraint rules:
- non-`request_updated`: all four nullable metadata columns NULL and `description_changed = false`;
- `request_updated`: at least one dimension changed;
- capacity pair is either both omitted for unchanged capacity or `IS DISTINCT FROM`;
- previous finite capacity may be 1 for legacy history;
- new finite capacity is >=2;
- start pair is either both omitted for unchanged time or both non-null and distinct.

Extend `notification_work.kind` with `request_updated`.

No deployed-data conversion for `capacity_changed`.

- [ ] **Step 2: Write failing Store integration tests**

Cover:
- open Host updates each field separately;
- open Host updates all three in one call;
- playing Host updates capacity + description;
- playing attempted `starts_at` change fails with no partial write;
- non-Host/inactive edits fail;
- Game unchanged;
- `started_at` unchanged;
- capacity-below-members fails;
- complete no-op returns `changed=false`, leaves `updated_at` unchanged, and writes no Activity;
- one multi-field Save writes exactly one `request_updated` row;
- Activity metadata is populated only for changed dimensions;
- description text itself is absent from Activity metadata.

- [ ] **Step 3: Replace SQL mutations**

Replace capacity-only SQL with a targeted details update whose columns are exactly:
- `max_players`;
- `starts_at`;
- `description`;
- `updated_at`.

Do not update `game_id`, `creator_id`, `status`, or `started_at`.

Add a targeted `RecordRequestUpdatedActivity` insert.

- [ ] **Step 4: Implement `Store.UpdateDetails` under the existing request row lock**

Under lock:
1. Host-only;
2. active-state-only;
3. normalize/compare the requested editable state;
4. while playing, reject any planned-start difference;
5. count members only when needed for a finite capacity change;
6. reject finite max below membership;
7. derive changed dimensions;
8. return false for complete no-op;
9. update request and write one Activity in the same transaction.

Keep Join/Leave/update serialized on the same Game Request row.

- [ ] **Step 5: Preserve race coverage**

Retarget existing capacity-race tests to `UpdateDetails`. Assert Join vs capacity edit can never commit member count above finite max.

- [ ] **Step 6: Generate and verify**

Run:

```bash
task generate
task test:integration
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/gamerequest/postgres internal/platform/postgres
git commit -m "feat(gamerequest): persist atomic request edits"
```

---

### Task 3: Generalize Activity read model and notification semantics

**Files:**
- Modify: `internal/query/gamerequest/query.go`
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Modify: `internal/notification/notification.go`
- Modify: `internal/notification/webpush.go`
- Modify: notification/read-model tests
- Generate: query sqlc output

**Produces:** typed `request_updated` history projection; Discord-only external delivery; unchanged Trending.

- [ ] **Step 1: Extend read-model Activity**

Expose:
- `PreviousMaxPlayers *int32`
- `NewMaxPlayers *int32`
- `PreviousStartsAt *time.Time`
- `NewStartsAt *time.Time`
- `DescriptionChanged bool`

Map the new SQL columns from `ListGameRequestActivity`.

- [ ] **Step 2: Replace notification kind**

Replace branch-only `ActivityCapacityChanged` with `ActivityUpdated`.

Web Push must explicitly return zero targets for `request_updated` without querying participants/preferences/subscriptions.

Do not change service-worker PushKind.

- [ ] **Step 3: Pin Trending**

Keep Trending activity kinds exactly as before; `request_updated` must not be added.

- [ ] **Step 4: Verify notification work + Discord**

Integration tests must prove:
- one real update yields one `request_updated` work item;
- Discord edits the existing synchronized message using current capacity/start/description;
- Web Push creates no delivery;
- multi-field Save still causes only one Discord synchronization work item.

- [ ] **Step 5: Commit**

```bash
git add internal/query/gamerequest internal/notification internal/platform/postgres
git commit -m "feat(notification): deliver request updates to discord"
```

---

### Task 4: Replace Capacity card with inline Edit Game Request flow

**Files:**
- Modify: `internal/web/router.go`
- Modify: `internal/web/handler/game_requests.go`
- Modify: `internal/web/handler/game_requests_test.go`
- Modify: `internal/web/pages/game_requests.templ`
- Modify: `internal/web/pages/game_requests_test.go`
- Modify: `internal/web/handler/realtime_test.go`
- Generate: templ output

**Produces:** `?edit=1` inline editor and one details-save endpoint.

- [ ] **Step 1: Preserve the current Capacity UX with tests before moving it**

Pin the current PR #118 behavior:
- blank/`0` => unlimited;
- `2..999`;
- existing hint/copy unless intentionally generalized;
- field-local errors;
- rejected raw input remains visible.

These tests protect the user's recently refined Capacity box while it is moved.

- [ ] **Step 2: Add edit-mode detail rendering**

Normal detail gets Host-only **Edit** for `open|playing`.

`GET /game-requests/{id}?edit=1`:
- Host + open: editable Capacity + planned start + description;
- Host + playing: editable Capacity + description; planned start displayed read-only;
- non-Host: no editor;
- closed/cancelled: no editor.

Game remains visible but has no input/select/hidden field that can change identity.

Cancel links back to `/game-requests/{id}`.

- [ ] **Step 3: Reuse existing Create input conventions**

Reuse the proven browser-local planned-time parsing model:
- local date/time + IANA timezone;
- parse to absolute instant server-side;
- preserve submitted values on validation failure.

Reuse description length/count behavior where practical.

Do not reuse the Game selector portion of the composer.

- [ ] **Step 4: Replace route/handler**

Replace the capacity-only command route with one details endpoint, e.g.:

`POST /game-requests/{requestID}/details`

Handler parses:
- `max_players`;
- planned start only when open;
- description.

For playing requests, the server must still pass/validate the stored `starts_at` or otherwise ensure a forged request cannot alter it; do not rely only on a disabled/absent browser input.

On validation/business errors, remain in edit mode and preserve submitted editable values.

- [ ] **Step 5: Replace SSE event coverage**

Assert successful real Save streams `GameRequestUpdated`; no-op emits none.

Existing generic SSE refresh remains unchanged.

- [ ] **Step 6: Generate/test**

Run:

```bash
task generate
go test -race ./internal/web/... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/web
git commit -m "feat(web): add inline game request editing"
```

---

### Task 5: Browser regression and canonical documentation

**Files:**
- Modify: `internal/web/browsertest/main.go`
- Modify: relevant `web/tests/*.spec.ts`
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/TERMINOLOGY.md`
- Modify: `docs/ui-kit/README.md`

- [ ] **Step 1: Update browser fixture command semantics**

Replace capacity-only fixture update with the exact handler-facing `UpdateDetails` command behavior. Keep it in-memory; do not emulate DB locking.

- [ ] **Step 2: Add Host edit browser flow**

Cover:
1. Host opens Edit on an open request;
2. edits capacity, planned start, and description;
3. Save returns to normal detail and all values update;
4. Start game;
5. Edit remains available while Playing;
6. Capacity + description remain editable;
7. planned start is no longer editable;
8. Cancel exits edit mode without saving.

- [ ] **Step 3: Preserve #101 participant flow**

Retain Playing Join/Leave/Full browser coverage so moving the editor cannot regress dynamic participation.

- [ ] **Step 4: Update canonical docs**

Ensure the four docs describe the generalized Host edit behavior and `request_updated`, not a standalone Capacity card/event.

- [ ] **Step 5: Run browser tests**

Run:

`task test:browser`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/browsertest web/tests docs/PRODUCT.md docs/ARCHITECTURE.md docs/TERMINOLOGY.md docs/ui-kit/README.md
git commit -m "docs: define host game request editing"
```

---

### Task 6: Final verification and temporary-artifact cleanup

- [ ] **Step 1: Full generation and static/unit verification**

Run:

```bash
task generate
task lint
task test
```

Expected: all exit 0.

- [ ] **Step 2: Integration and browser/PWA verification**

Run:

```bash
task test:integration
task test:browser
task test:pwa
```

Expected: all exit 0.

- [ ] **Step 3: Build**

Run:

`task build`

Expected: exit 0.

- [ ] **Step 4: Manual invariant review**

Confirm:
- Game cannot change;
- creator cannot change;
- `started_at` cannot change;
- open edits all three approved fields;
- playing edits only capacity + description;
- no-op truly writes/delivers nothing;
- multi-field Save creates one Activity/SSE/Discord work item;
- request update never creates Web Push;
- request update never affects Trending;
- current Capacity UX survived relocation;
- #102–#104 remain untouched.

- [ ] **Step 5: Remove temporary working artifacts**

Delete:
- `docs/superpowers/specs/2026-10-02-game-request-host-edit-design.md`
- `docs/superpowers/plans/2026-10-02-game-request-host-edit.md`

Only do this after all verification succeeds and canonical docs contain the durable truth.

- [ ] **Step 6: Final status and PR update**

Run:

`git status --short`

Expected: clean after cleanup commit.

Update PR #118 description so its summary/test evidence reflects the generalized Host Edit flow rather than Capacity-only editing.
