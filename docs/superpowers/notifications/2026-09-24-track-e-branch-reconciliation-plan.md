# Track E — Branch Reconciliation and E1 Follow-up Plan

Status: Current execution prerequisite
Date: 2026-09-24
Tracking issue: #28
Branch: `docs/track-e-notifications-sdd`

## Authority

This plan does not change Track E product or architecture scope. The source of truth remains:

- `docs/PRODUCT.md`
- `docs/ARCHITECTURE.md`
- `docs/ROADMAP.md`
- `docs/superpowers/notifications/2026-09-20-track-e-notification-foundation-design.md`
- `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md`
- `docs/superpowers/notifications/2026-09-20-track-e-discord-delivery-design.md`

The tasks below exist only to reconcile the long-lived Track E branch with current `main` and to close the one documented E1 implementation-alignment follow-up before E2/E3 rely on provider retry hints.

## Observed repository state on 2026-09-24

- The Track E branch is substantially diverged from `main`; re-check the exact ahead/behind counts immediately before executing Task R1.
- Current `main` has migrations through `00013_igdb_cover_index.sql`.
- The Track E branch currently carries `00010_notification_foundation.sql`, `00011_notification_suppressed_delivery.sql`, and `00012_notification_push_subscriptions.sql`, so migration numbers collide with work already merged to `main`.
- Track E touches files that also changed on `main`, especially `internal/app/app.go`, config, game discovery/query code, web routing/UI, canonical docs, generated sqlc output, and migration tests.
- E1 is implemented, but `computeBackoff` still caps an explicit provider `Retry-After` at five minutes. The approved E1 design says only LANsurfer-computed backoff is capped; provider `Retry-After` must be honored in full.

Do not begin new E2/E3 implementation until R1-R4 are complete.

---

## Task R1 — Merge current `main` into the Track E branch

**Goal**

Bring the published Track E branch onto the current repository baseline without rewriting its existing public history and without changing Track E behavior.

**Affected files/components**

Potentially every file changed on both branches. Expect conflicts particularly in:

- `docs/ARCHITECTURE.md`
- `docs/PRODUCT.md`
- `docs/ROADMAP.md`
- `internal/app/app.go`
- `internal/platform/config/*`
- `internal/game/*`
- `internal/query/gamerequest/*`
- `internal/web/*`
- generated sqlc/template/frontend output

**Concrete changes**

1. Fetch current `main`.
2. Merge current `main` into `docs/track-e-notifications-sdd`; do not rebase/force-push the already-published Track E history.
3. Resolve conflicts by preserving both already-merged `main` behavior and the approved Track E behavior.
4. Do not renumber notification migrations in this task; leave that isolated for R2.
5. Do not perform opportunistic refactors or feature work while resolving conflicts.
6. Generated files may be temporarily inconsistent after conflict resolution; R3 owns regeneration.

**Dependencies**

None.

**Acceptance / verification**

- Current `main` is an ancestor of the resulting Track E HEAD.
- No unresolved conflict markers remain.
- No Track E source/design file or already-implemented E1/E2-foundation behavior was dropped merely to resolve a conflict.
- `git diff --check` passes.
- The commit contains merge/integration changes only.

---

## Task R2 — Renumber the Track E migrations after the merged `main` sequence

**Goal**

Remove migration-number collisions while preserving Track E migration order and semantics.

**Affected files/components**

- `internal/platform/postgres/migrations/00010_notification_foundation.sql`
- `internal/platform/postgres/migrations/00011_notification_suppressed_delivery.sql`
- `internal/platform/postgres/migrations/00012_notification_push_subscriptions.sql`
- migration-version assertions in `internal/platform/postgres/postgres_integration_test.go`
- any documentation/tests that refer to those filenames explicitly

**Concrete changes**

1. After R1, inspect the highest migration number on the merged branch.
2. Rename the three Track E migrations to the next three free consecutive numbers, preserving this order:
   1. notification foundation;
   2. suppressed delivery state;
   3. Web Push subscriptions/preferences.
3. As of 2026-09-24, if `main` is still at `00013`, the expected result is `00014`, `00015`, `00016`. If `main` advanced, use the actual next free numbers instead.
4. Update only hardcoded migration-version/filename expectations that necessarily follow from the renumbering.
5. Do not alter the SQL behavior while renaming.

**Dependencies**

R1.

**Acceptance / verification**

- There is exactly one migration for every version number.
- Notification migration order is unchanged.
- A fresh database migrates successfully through the new Track E versions.
- The existing populated migration/integration fixtures still migrate successfully.
- No SQL behavior change is mixed into this task.

---

## Task R3 — Regenerate derived code/assets and fix merge-only fallout

**Goal**

Restore repository-generated artifacts to the merged source state before feature work continues.

**Affected files/components**

Generated outputs only where their source changed, including:

- `internal/**/sqlcgen/*`
- `internal/web/**/*_templ.go`
- `internal/web/assets/dist/*`
- generated branding assets when required by the normal generation pipeline

**Concrete changes**

1. Run `task generate`.
2. Review the generated diff; keep only output implied by merged source/template/query changes.
3. Fix compile/type errors that are direct merge fallout.
4. Do not change product behavior, notification behavior, or UI scope to make generation pass.
5. If generated output reveals a real source conflict, fix the source and regenerate rather than hand-editing generated files.

**Dependencies**

R2.

**Acceptance / verification**

- `task generate` completes cleanly.
- A second `task generate` produces no diff.
- `go test ./internal/notification/... ./internal/app/... ./internal/web/... ./internal/query/gamerequest/... ./internal/game/...` passes before proceeding to R4.
- Generated files are not manually edited.

---

## Task R4 — Establish a clean post-merge verification baseline

**Goal**

Prove that the reconciled branch is healthy before new notification implementation is added.

**Affected files/components**

No planned feature files. Only direct merge/regeneration fallout may be fixed.

**Concrete changes**

Run the repository verification commands in this order:

1. `task test`
2. `task test:integration`
3. `task test:pwa`
4. `task lint`
5. `task build`

If a failure is caused by the merge, migration renumbering, or regeneration, fix only that failure and rerun the failing command. Do not fold E2/E3 implementation into this task.

**Dependencies**

R3.

**Acceptance / verification**

- All five commands pass.
- The branch has a known-green baseline before R5 or any E2/E3 task starts.
- Any omitted environment-dependent check is explicitly recorded instead of being silently skipped.

---

## Task R5 — Honor provider `Retry-After` without LANsurfer's five-minute cap

**Goal**

Align the implemented E1 backoff helper with the approved E1 design before Web Push or Discord rely on provider rate-limit hints.

**Affected files/components**

- `internal/notification/backoff.go`
- `internal/notification/backoff_test.go`

**Concrete changes**

1. Keep the existing automatic schedule and jitter behavior unchanged.
2. Keep the five-minute cap for LANsurfer-computed/jittered backoff.
3. When `retryAfter != nil`, return the provider duration without applying jitter and without applying the five-minute cap.
4. Continue clamping a negative provider duration to zero.
5. Rename/update the existing test that currently expects a 20-minute hint to be capped.
6. Add/retain tests proving:
   - a normal provider hint is returned unchanged;
   - a provider hint greater than five minutes is returned unchanged;
   - provider hints are not jittered;
   - LANsurfer-computed delay is still capped at five minutes;
   - negative delay becomes zero.

**Dependencies**

R4.

**Acceptance / verification**

- `go test ./internal/notification/... -run 'TestComputeBackoff|TestApplyJitter' -count=1` passes.
- `task test` passes.
- No retry-budget, attempt-count, polling, or lease behavior changes.

---

## Exit criteria

This prerequisite plan is complete when:

- the Track E branch contains current `main`;
- migration numbering no longer collides;
- generation is stable;
- the merged branch is green;
- E1's provider `Retry-After` behavior matches the approved design.

Only then execute the current E2 remaining-work plan and, after E2, the E3 implementation plan.
