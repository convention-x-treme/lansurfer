# Track E1 — Durable Notification Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `notification` business module's durable foundation (Track E1): asynchronously consume Track A's `game_request_activity` handoff, persist two-level work/delivery state in PostgreSQL, claim and retry deliveries with crash-safe leases, and prove all of it with a fake in-test transport — with no Web Push, VAPID, or Discord code.

**Architecture:** A new `internal/notification` package defines the domain vocabulary (`Activity`, `Target`, `Transport`, `SendResult`, delivery states) and two pieces of behavior: a `Worker` that polls PostgreSQL to discover new activity, materialize `notification_work`/`notification_delivery` rows, claim due deliveries with `FOR UPDATE SKIP LOCKED` leases, and record outcomes with capped exponential backoff; and a `Service` that exposes authorized diagnostics and manual retry. `internal/notification/postgres` holds the sqlc-generated persistence. The composition root (`internal/app`) runs baseline initialization synchronously before the HTTP server and worker start, and ties the worker to the application's root context exactly like the existing IGDB sync service. A CLI `admin notifications` surface (mirroring the existing `admin users`/`admin promote` commands) gives operators diagnostics and manual retry without adding an HTTP admin page.

**Tech Stack:** Go 1.27, PostgreSQL via pgx/v5 + pgxpool, sqlc v1.31.1 (`sql_package: pgx/v5`), goose migrations, no new dependencies.

**Spec:** The user's implementation brief for Track E1 (given verbatim in this conversation) is the primary requirements source. It is grounded in `docs/superpowers/notifications/2026-09-20-track-e-notification-foundation-design.md` (settled E1 design), `docs/ARCHITECTURE.md` §5/§7, `docs/ROADMAP.md` (Track E, cross-track contract 1), and GitHub issue #28. `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md` (E2) is context only — nothing in this plan implements or documents it.

## Global Constraints

- E1 only. No Web Push, VAPID, browser permission UX, PushSubscription endpoints, service-worker push handlers, Discord OAuth, Discord webhook transport, or Discord UI in this plan.
- Never write a second transactional outbox into `gamerequest`'s tables or transactions. `game_request_activity` is read-only from `notification`'s perspective.
- `origin=backfill` rows must never be delivered.
- No global `sequence` high-watermark cursor. No timestamp cutoff for baseline.
- Two-level persistence: one `notification_work` row per consumed `game_request_activity` row; zero or more `notification_delivery` rows beneath it.
- Durable uniqueness must make repeated discovery harmless (no duplicate work/delivery rows).
- Baseline initialization is synchronous at startup, before the HTTP server and worker start, and must be idempotent and restart-safe.
- E1 keeps consuming/materializing work with zero transports configured; work with zero eligible targets completes with zero deliveries and is never replayed.
- Delivery states: pending/due, claimed/in-progress, succeeded, exhausted, permanent, uncertain (retryable failure is represented by `pending` with a future `next_attempt_at`, not a separate persisted state).
- Claiming uses PostgreSQL `FOR UPDATE SKIP LOCKED` with a durable lease; no in-process mutex, no Redis, no broker, no `LISTEN/NOTIFY`.
- Defaults: poll interval ≈2s, claim lease 60s, provider/network timeout shorter than the lease.
- Automatic retry budget: 6 total attempts. Backoff ≈5s → 30s → 2m → 5m → 5m, jittered, capped at 5 minutes per wait. HTTP 429/`Retry-After` is honored but still capped at 5 minutes. After the budget, the delivery becomes `exhausted`.
- Manual retry uses the existing `RequireAdmin` authorization concept but is exposed here as an admin CLI command (see Decisions below), not a new role model.
- Manual retry re-checks present-day eligibility before reopening. `uncertain` retry is a distinct action from ordinary manual retry.
- No automatic retention/cleanup of work or delivery rows in 1.0. No persistent in-app notification inbox for ordinary users.
- Transport-facing vocabulary is minimal: success, retryable failure, permanent failure, uncertain. `exhausted` is worker policy, not a transport outcome. No generic provider registry, plugin framework, event bus, job framework, ordered-stream abstraction, event sourcing, or CQRS infrastructure.
- Single application instance for 1.0; no HA coordination.
- Do not hand-edit generated sqlc files; run `task generate`.
- Do not guess at E2 (Web Push) design questions; do not touch `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md`.

## Decisions made in this plan (for the user to override before/while executing)

1. **No HTTP admin page in E1.** The SDD lists "admin diagnostic backend/handler integration" as needed "only as needed for E1," and Track G explicitly owns visual presentation of notification/diagnostic states (ROADMAP, issue #28). Building a templ admin page now, with no real transport and only a fake test transport, would be thrown away or fought over with Track G later. Instead, Task 9 adds `admin notifications problems|retry|retry-uncertain` CLI subcommands, exactly mirroring the existing `admin users`/`admin promote` pattern in `cmd/lansurfer/main.go`. This gives operators a real, testable diagnostics/retry surface without HTTP/templ scope creep. If the user wants an HTTP `RequireAdmin`-gated page instead, say so before or during Task 9 and it will be substituted.
2. **`WorkerRepository`, `DiagnosticsRepository`, and `BaselineInitializer` are three narrow interfaces**, not one wide `Repository`, each consumed by a different caller (`Worker`, `Service`, and the composition root respectively). `*notificationpostgres.Store` implements all three; the composition root wires the same concrete `Store` into each consumer through its narrow interface.
3. **`notification_work` snapshots `game_request_id`, `actor_id`, `kind`, and `occurred_at`** at materialization time rather than joining back to `game_request_activity` on every later read. This is a deliberate read-only snapshot (not a write to `gamerequest`'s tables) that keeps delivery processing and diagnostics self-sufficient after materialization.

---

### Task 1: Database schema and sqlc wiring for the notification module

**Files:**
- Create: `internal/platform/postgres/migrations/00010_notification_foundation.sql`
- Create: `internal/notification/postgres/queries/notification.sql`
- Modify: `sqlc.yaml`
- Generated (do not hand-edit): `internal/notification/postgres/sqlcgen/*.go`

**Interfaces:**
- Produces: the `notification_baseline`, `notification_work`, `notification_delivery` tables, and the sqlc-generated `Queries` type in package `sqlcgen` (import path `github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen`) that Task 3–6 build `Store` methods on top of.

- [ ] **Step 1: Write the migration**

Create `internal/platform/postgres/migrations/00010_notification_foundation.sql`:

```sql
-- +goose Up
-- A singleton marker: at most one row, and it can only ever be `true`. Its
-- presence means E1's one-time baseline materialization has already run, so
-- a later restart never re-scans and never risks delivering pre-upgrade
-- history. See internal/notification/postgres/store.go InitializeBaseline.
CREATE TABLE notification_baseline (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    initialized_at timestamptz NOT NULL DEFAULT now()
);

-- One row per game_request_activity row Track E has consumed. The UNIQUE
-- constraint on activity_sequence is what makes repeated discovery harmless:
-- INSERT ... ON CONFLICT (activity_sequence) DO NOTHING is the only
-- durability guarantee this module needs, in place of a global cursor.
-- game_request_id, actor_id, kind, and occurred_at are snapshotted from
-- game_request_activity at materialization time so later reads never need
-- to join back into gamerequest's tables.
CREATE TABLE notification_work (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    activity_sequence bigint NOT NULL UNIQUE
        REFERENCES game_request_activity (sequence) ON DELETE CASCADE,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('created', 'joined', 'left', 'closed', 'cancelled')),
    occurred_at timestamptz NOT NULL,
    baseline boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_work_game_request_idx ON notification_work (game_request_id);

-- Zero or more delivery targets per work item. UNIQUE (work_id, transport,
-- target_key) prevents a crash-and-retry of materialization from doubling up
-- deliveries for the same target.
CREATE TABLE notification_delivery (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    work_id bigint NOT NULL REFERENCES notification_work (id) ON DELETE CASCADE,
    transport text NOT NULL,
    target_key text NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain')),
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    claimed_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (work_id, transport, target_key)
);

-- Two narrow partial indexes back the claim query's two due-conditions
-- (never-claimed vs. lease-expired) instead of one index that would only
-- help one branch.
CREATE INDEX notification_delivery_pending_due_idx
    ON notification_delivery (next_attempt_at) WHERE status = 'pending';
CREATE INDEX notification_delivery_claimed_lease_idx
    ON notification_delivery (claimed_until) WHERE status = 'claimed';
-- Authorized diagnostics list exactly these three terminal problem states.
CREATE INDEX notification_delivery_problem_idx
    ON notification_delivery (status) WHERE status IN ('exhausted', 'permanent', 'uncertain');

-- +goose Down
DROP TABLE notification_delivery;
DROP TABLE notification_work;
DROP TABLE notification_baseline;
```

- [ ] **Step 2: Write the sqlc queries**

Create `internal/notification/postgres/queries/notification.sql`:

```sql
-- name: InsertBaselineMarker :execrows
INSERT INTO notification_baseline (id) VALUES (true)
ON CONFLICT (id) DO NOTHING;

-- name: MaterializeBaselineWork :exec
INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at, baseline)
SELECT sequence, game_request_id, actor_id, kind, occurred_at, true
FROM game_request_activity
WHERE origin = 'live'
ON CONFLICT (activity_sequence) DO NOTHING;

-- name: DiscoverNewActivity :many
SELECT a.sequence, a.game_request_id::text AS game_request_id, a.actor_id::text AS actor_id,
       a.kind, a.occurred_at
FROM game_request_activity a
WHERE a.origin = 'live'
  AND NOT EXISTS (SELECT 1 FROM notification_work w WHERE w.activity_sequence = a.sequence)
ORDER BY a.sequence
LIMIT sqlc.arg(row_limit);

-- name: InsertNotificationWork :one
INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at, baseline)
VALUES (
    sqlc.arg(activity_sequence),
    sqlc.arg(game_request_id)::uuid,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(kind),
    sqlc.arg(occurred_at),
    false
)
ON CONFLICT (activity_sequence) DO NOTHING
RETURNING id;

-- name: InsertNotificationDelivery :exec
INSERT INTO notification_delivery (work_id, transport, target_key)
VALUES (sqlc.arg(work_id), sqlc.arg(transport), sqlc.arg(target_key))
ON CONFLICT (work_id, transport, target_key) DO NOTHING;

-- name: ClaimDueDeliveries :many
WITH claimed AS (
    UPDATE notification_delivery
    SET status = 'claimed', claimed_until = sqlc.arg(claimed_until), updated_at = now()
    WHERE id IN (
        SELECT id FROM notification_delivery
        WHERE (status = 'pending' AND next_attempt_at <= now())
           OR (status = 'claimed' AND claimed_until <= now())
        ORDER BY next_attempt_at
        LIMIT sqlc.arg(row_limit)
        FOR UPDATE SKIP LOCKED
    )
    RETURNING id, work_id, transport, target_key, attempt_count
)
SELECT c.id, c.transport, c.target_key, c.attempt_count,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM claimed c
JOIN notification_work w ON w.id = c.work_id;

-- name: RecordDeliverySuccess :exec
UPDATE notification_delivery
SET status = 'succeeded', claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryRetry :exec
UPDATE notification_delivery
SET status = 'pending', attempt_count = attempt_count + 1, next_attempt_at = sqlc.arg(next_attempt_at),
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryExhausted :exec
UPDATE notification_delivery
SET status = 'exhausted', attempt_count = attempt_count + 1,
    claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryPermanent :exec
UPDATE notification_delivery
SET status = 'permanent', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: RecordDeliveryUncertain :exec
UPDATE notification_delivery
SET status = 'uncertain', claimed_until = NULL, last_error = sqlc.narg(last_error), updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ListProblemDeliveries :many
SELECT d.id, d.transport, d.target_key, d.status, d.attempt_count, d.last_error, d.updated_at,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.status IN ('exhausted', 'permanent', 'uncertain')
ORDER BY d.updated_at DESC;

-- name: GetDeliveryDetail :one
SELECT d.id, d.transport, d.target_key, d.status,
       w.activity_sequence, w.game_request_id::text AS game_request_id, w.actor_id::text AS actor_id,
       w.kind, w.occurred_at
FROM notification_delivery d
JOIN notification_work w ON w.id = d.work_id
WHERE d.id = sqlc.arg(id);

-- name: ReopenDeliveryForManualRetry :one
UPDATE notification_delivery
SET status = 'pending', attempt_count = 0, next_attempt_at = now(),
    claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status IN ('exhausted', 'permanent')
RETURNING id;

-- name: ReopenUncertainDeliveryForManualRetry :one
UPDATE notification_delivery
SET status = 'pending', attempt_count = 0, next_attempt_at = now(),
    claimed_until = NULL, last_error = NULL, updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'uncertain'
RETURNING id;
```

- [ ] **Step 3: Wire sqlc**

Edit `sqlc.yaml`, adding a new entry after the `internal/query/gamerequest/queries` block, following the exact shape of the existing entries:

```yaml
  - engine: postgresql
    schema: internal/platform/postgres/migrations
    queries: internal/notification/postgres/queries
    gen:
      go:
        package: sqlcgen
        out: internal/notification/postgres/sqlcgen
        sql_package: pgx/v5
        omit_unused_structs: true
```

- [ ] **Step 4: Generate and confirm the schema compiles**

Run: `task generate`

Expected: sqlc produces `internal/notification/postgres/sqlcgen/{db.go,models.go,notification.sql.go}` with no errors. If it does not, the most likely causes are a missing `sqlc.narg` on a nullable column or a typo in a table/column name — fix the SQL, not the generated output.

Then read `internal/notification/postgres/sqlcgen/notification.sql.go` and note the exact generated `Params`/`Row` struct field names and types (sqlc derives them from the query's argument/column names via PascalCase — e.g. `sqlc.arg(row_limit)` → `RowLimit`, `a.game_request_id::text AS game_request_id` → `GameRequestID string`). Tasks 3–6 assume the field names spelled out in this plan; if sqlc chose different names, use the actual generated names when writing `store.go` and note the discrepancy in that task's commit.

- [ ] **Step 5: Confirm the module builds**

Run: `go build ./...`

Expected: succeeds (there is nothing yet that imports the new `sqlcgen` package, so this mainly proves the generated code itself compiles).

- [ ] **Step 6: Commit**

```bash
git add internal/platform/postgres/migrations/00010_notification_foundation.sql \
        internal/notification/postgres/queries/notification.sql \
        internal/notification/postgres/sqlcgen \
        sqlc.yaml
git commit -m "feat(notification): add E1 schema and generated queries"
```

---

### Task 2: Domain types, errors, and retry backoff scheduling

**Files:**
- Create: `internal/notification/notification.go`
- Create: `internal/notification/backoff.go`
- Test: `internal/notification/backoff_test.go`

**Interfaces:**
- Consumes: nothing (pure domain package).
- Produces: `ActivityKind`, `Activity{Sequence int64, GameRequestID, ActorID string, Kind ActivityKind, OccurredAt time.Time}`, `Target{Transport, Key string}`, `Outcome` (`OutcomeSucceeded`, `OutcomeRetryable`, `OutcomePermanent`, `OutcomeUncertain`), `SendResult{Outcome Outcome, RetryAfter *time.Duration, Err error}`, `Transport` interface (`Name() string`, `Targets(context.Context, Activity) ([]Target, error)`, `Send(context.Context, Activity, Target) SendResult`), `DeliveryStatus` (`StatusPending`, `StatusClaimed`, `StatusSucceeded`, `StatusPermanent`, `StatusExhausted`, `StatusUncertain`), `ClaimedDelivery{DeliveryID int64, Target Target, AttemptCount int32, Activity Activity}`, `ProblemDelivery{DeliveryID int64, Target Target, Status DeliveryStatus, AttemptCount int32, LastError string, UpdatedAt time.Time, Activity Activity}`, `DeliveryDetail{DeliveryID int64, Target Target, Status DeliveryStatus, Activity Activity}`, sentinel errors `ErrDeliveryNotFound`, `ErrDeliveryNotRetryable`, `ErrDeliveryNotUncertain`, `ErrNoLongerEligible`, `ErrTransportNotConfigured`, and `computeBackoff`/`applyJitter`/`MaxAutomaticAttempts` used by Task 5's `Worker`.

- [ ] **Step 1: Write the domain types**

Create `internal/notification/notification.go`:

```go
// Package notification owns Track E1's durable notification foundation: it
// asynchronously consumes Track A's game_request_activity handoff and
// persists per-activity work and per-target delivery state, independent of
// any concrete transport. It does not own or write Game Requests.
package notification

import (
	"context"
	"errors"
	"time"
)

// ActivityKind mirrors gamerequest.ActivityKind's five durable kinds. It is
// redeclared here rather than imported so this module never depends on
// gamerequest's package, only on the read-only activity rows it discovers.
type ActivityKind string

const (
	ActivityCreated   ActivityKind = "created"
	ActivityJoined    ActivityKind = "joined"
	ActivityLeft      ActivityKind = "left"
	ActivityClosed    ActivityKind = "closed"
	ActivityCancelled ActivityKind = "cancelled"
)

// Activity is a read-only snapshot of one live game_request_activity row.
type Activity struct {
	Sequence      int64
	GameRequestID string
	ActorID       string
	Kind          ActivityKind
	OccurredAt    time.Time
}

// Target names one external delivery target within one transport, for
// example a Web Push subscription ID or "instance" for the single Discord
// destination. It is intentionally opaque outside the owning transport.
type Target struct {
	Transport string
	Key       string
}

// Outcome is the minimal transport-facing result vocabulary. Exhausted is
// deliberately not a member: it is worker policy computed from AttemptCount,
// never something a transport adapter decides.
type Outcome int

const (
	OutcomeSucceeded Outcome = iota
	OutcomeRetryable
	OutcomePermanent
	OutcomeUncertain
)

// SendResult is what a Transport reports for one delivery attempt.
// RetryAfter carries a provider-specified hint (for example HTTP 429's
// Retry-After) and is only meaningful when Outcome is OutcomeRetryable.
type SendResult struct {
	Outcome    Outcome
	RetryAfter *time.Duration
	Err        error
}

// Transport is implemented by concrete delivery mechanisms (Web Push,
// Discord in later tracks; a fake in this track's tests). Targets resolves
// which external targets are currently eligible for an activity; Send
// attempts delivery to one of them. Neither method may block past ctx.
type Transport interface {
	Name() string
	Targets(ctx context.Context, activity Activity) ([]Target, error)
	Send(ctx context.Context, activity Activity, target Target) SendResult
}

// DeliveryStatus is the durable state of one notification_delivery row.
// "Retryable failure" has no state of its own: it is StatusPending with a
// future NextAttemptAt.
type DeliveryStatus string

const (
	StatusPending   DeliveryStatus = "pending"
	StatusClaimed   DeliveryStatus = "claimed"
	StatusSucceeded DeliveryStatus = "succeeded"
	StatusPermanent DeliveryStatus = "permanent"
	StatusExhausted DeliveryStatus = "exhausted"
	StatusUncertain DeliveryStatus = "uncertain"
)

// ClaimedDelivery is one delivery row the Worker has just claimed, together
// with enough of its Activity snapshot to call a Transport.
type ClaimedDelivery struct {
	DeliveryID   int64
	Target       Target
	AttemptCount int32
	Activity     Activity
}

// ProblemDelivery is one row in a terminal problem state, for authorized
// diagnostics.
type ProblemDelivery struct {
	DeliveryID   int64
	Target       Target
	Status       DeliveryStatus
	AttemptCount int32
	LastError    string
	UpdatedAt    time.Time
	Activity     Activity
}

// DeliveryDetail is the minimal view Service needs to validate and reopen
// one delivery for manual retry.
type DeliveryDetail struct {
	DeliveryID int64
	Target     Target
	Status     DeliveryStatus
	Activity   Activity
}

var (
	ErrDeliveryNotFound       = errors.New("notification delivery not found")
	ErrDeliveryNotRetryable   = errors.New("notification delivery is not exhausted or permanent")
	ErrDeliveryNotUncertain   = errors.New("notification delivery is not uncertain")
	ErrNoLongerEligible       = errors.New("target is no longer eligible for delivery")
	ErrTransportNotConfigured = errors.New("transport is not configured")
)

// BaselineInitializer is consumed directly by the composition root: exactly
// one synchronous call before the HTTP server and Worker start.
type BaselineInitializer interface {
	InitializeBaseline(ctx context.Context) error
}

// WorkerRepository is the durable state Worker needs to discover activity,
// materialize work/delivery rows, claim due deliveries, and record outcomes.
type WorkerRepository interface {
	DiscoverActivity(ctx context.Context, limit int) ([]Activity, error)
	MaterializeWork(ctx context.Context, activity Activity, targets []Target) error
	ClaimDueDeliveries(ctx context.Context, limit int, leaseUntil time.Time) ([]ClaimedDelivery, error)
	RecordSuccess(ctx context.Context, deliveryID int64) error
	RecordRetry(ctx context.Context, deliveryID int64, nextAttemptAt time.Time, lastError string) error
	RecordExhausted(ctx context.Context, deliveryID int64, lastError string) error
	RecordPermanent(ctx context.Context, deliveryID int64, lastError string) error
	RecordUncertain(ctx context.Context, deliveryID int64, lastError string) error
}

// DiagnosticsRepository is the durable state Service needs for authorized
// diagnostics and manual retry.
type DiagnosticsRepository interface {
	ListProblemDeliveries(ctx context.Context) ([]ProblemDelivery, error)
	GetDeliveryDetail(ctx context.Context, deliveryID int64) (DeliveryDetail, error)
	ReopenDelivery(ctx context.Context, deliveryID int64) (bool, error)
	ReopenUncertainDelivery(ctx context.Context, deliveryID int64) (bool, error)
}
```

- [ ] **Step 2: Write the failing backoff test**

Create `internal/notification/backoff_test.go`:

```go
package notification

import (
	"testing"
	"time"
)

func TestComputeBackoffFollowsScheduleAndCapsAtFiveMinutes(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	want := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for attemptsMade := 1; attemptsMade <= len(want); attemptsMade++ {
		got := computeBackoff(attemptsMade, nil, noJitter)
		if got != want[attemptsMade-1] {
			t.Fatalf("computeBackoff(%d) = %v, want %v", attemptsMade, got, want[attemptsMade-1])
		}
	}
}

func TestComputeBackoffClampsAttemptsBeyondScheduleLength(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	if got := computeBackoff(9, nil, noJitter); got != 5*time.Minute {
		t.Fatalf("computeBackoff(9) = %v, want 5m (clamped to the last schedule entry)", got)
	}
}

func TestComputeBackoffHonorsRetryAfterCappedAtFiveMinutes(t *testing.T) {
	noJitter := func(d time.Duration) time.Duration { return d }
	hint := 45 * time.Second
	if got := computeBackoff(1, &hint, noJitter); got != hint {
		t.Fatalf("computeBackoff with Retry-After = %v, want %v", got, hint)
	}
	longHint := 20 * time.Minute
	if got := computeBackoff(1, &longHint, noJitter); got != 5*time.Minute {
		t.Fatalf("computeBackoff with a long Retry-After = %v, want the 5m cap", got)
	}
}

func TestComputeBackoffAppliesJitterOnlyToScheduledDelay(t *testing.T) {
	called := false
	jitter := func(d time.Duration) time.Duration { called = true; return d + time.Second }
	if got := computeBackoff(1, nil, jitter); !called || got != 6*time.Second {
		t.Fatalf("computeBackoff(1, nil, jitter) = %v, called = %v, want 6s and called", got, called)
	}

	called = false
	hint := 10 * time.Second
	if got := computeBackoff(1, &hint, jitter); called || got != hint {
		t.Fatalf("computeBackoff with Retry-After must not be jittered: got %v, called = %v", got, called)
	}
}

func TestApplyJitterStaysWithinHalfToOneAndHalfTimesBase(t *testing.T) {
	base := 10 * time.Second
	lower, upper := base/2, base/2+base // upper is exclusive
	for i := 0; i < 200; i++ {
		got := applyJitter(base)
		if got < lower || got >= upper {
			t.Fatalf("applyJitter(%v) = %v, want within [%v, %v)", base, got, lower, upper)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/notification/... -run TestComputeBackoff -v`
Expected: FAIL — `computeBackoff`/`applyJitter` undefined.

- [ ] **Step 4: Implement backoff.go**

Create `internal/notification/backoff.go`:

```go
package notification

import (
	"math/rand/v2"
	"time"
)

// MaxAutomaticAttempts is the total number of attempts (including the
// first) an automatic retry round makes before a delivery becomes
// exhausted.
const MaxAutomaticAttempts = 6

const maxAutomaticDelay = 5 * time.Minute

// backoffSchedule has one entry per gap between the six total attempts:
// 5s, 30s, 2m, 5m, 5m.
var backoffSchedule = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	5 * time.Minute,
}

// computeBackoff returns the delay before the next automatic attempt.
// attemptsMade is the number of attempts already made (1-based). When
// retryAfter is set (an HTTP 429 Retry-After or equivalent provider hint),
// it is honored as-is, without jitter, and only capped; otherwise the
// schedule entry for attemptsMade is used, jittered, then capped. jitter is
// injected so tests can be deterministic; production code passes
// applyJitter.
func computeBackoff(attemptsMade int, retryAfter *time.Duration, jitter func(time.Duration) time.Duration) time.Duration {
	var delay time.Duration
	if retryAfter != nil {
		delay = *retryAfter
	} else {
		index := attemptsMade - 1
		if index < 0 {
			index = 0
		}
		if index >= len(backoffSchedule) {
			index = len(backoffSchedule) - 1
		}
		delay = jitter(backoffSchedule[index])
	}
	if delay > maxAutomaticDelay {
		delay = maxAutomaticDelay
	}
	if delay < 0 {
		delay = 0
	}
	return delay
}

// applyJitter spreads a delay across roughly half to one-and-a-half times
// its base value, so many simultaneously failing deliveries do not all wake
// up on the same worker tick.
func applyJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return base/2 + time.Duration(rand.Int64N(int64(base)))
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/notification/... -run TestComputeBackoff -v` and `go test ./internal/notification/... -run TestApplyJitter -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/notification/notification.go internal/notification/backoff.go internal/notification/backoff_test.go
git commit -m "feat(notification): add domain vocabulary and retry backoff schedule"
```

---

### Task 3: Postgres store — baseline initialization and activity discovery/materialization

**Files:**
- Create: `internal/notification/postgres/store.go` (this task: `NewStore`, `InitializeBaseline`, `DiscoverActivity`, `MaterializeWork`, plus shared helpers `parseUUID`/`textString`/`nullableText` that later tasks extend)
- Test: `internal/platform/postgres/notification_integration_test.go` (new file, package `postgres`, `//go:build integration`)

**Interfaces:**
- Consumes: `sqlcgen.Queries` from Task 1; `notification.Activity`, `notification.Target`, `notification.ActivityKind` from Task 2.
- Produces: `Store` satisfying `notification.BaselineInitializer` and the discovery/materialization half of `notification.WorkerRepository`, used directly by this task's tests and by every later task.

- [ ] **Step 1: Write the failing integration test**

Create `internal/platform/postgres/notification_integration_test.go`:

```go
//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// notificationFixture creates one user, one game, and a Service the caller
// can use to produce live game_request_activity rows through the real
// gamerequest module, exactly as the running application would.
type notificationFixture struct {
	host     user.User
	guest    user.User
	gameID   string
	requests *gamerequest.Service
}

func newNotificationFixture(t *testing.T, pool *pgxpool.Pool) notificationFixture {
	t.Helper()
	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Notify Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Notify Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	created, err := catalog.Create(t.Context(), "Notification Game", "notification-game")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	return notificationFixture{
		host: host, guest: guest, gameID: created.ID,
		requests: gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil),
	}
}

func TestNotificationBaselineIgnoresBackfillAndMaterializesLiveHistoryWithoutDelivery(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)

	// A prototype-style backfill row: create a real request through the
	// module (which records 'live' activity), then mark that activity
	// 'backfill' directly, the way migration 00006 reconstructed history.
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Baseline round")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		"UPDATE game_request_activity SET origin = 'backfill' WHERE game_request_id = $1", created.ID,
	); err != nil {
		t.Fatalf("mark activity as backfill: %v", err)
	}

	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}

	var workCount, deliveryCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work").Scan(&workCount); err != nil {
		t.Fatalf("count notification_work: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveryCount); err != nil {
		t.Fatalf("count notification_delivery: %v", err)
	}
	if workCount != 0 {
		t.Fatalf("notification_work rows = %d, want 0 (the only activity is backfill)", workCount)
	}
	if deliveryCount != 0 {
		t.Fatalf("notification_delivery rows = %d, want 0", deliveryCount)
	}

	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("DiscoverActivity() = %#v, want none (backfill is never deliverable)", activities)
	}
}

func TestNotificationBaselineInitializationIsIdempotentAndDiscoversLaterActivity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)

	preExisting, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Pre-existing round")
	if err != nil {
		t.Fatalf("create pre-existing game request: %v", err)
	}

	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("first InitializeBaseline() error = %v", err)
	}
	var firstCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work WHERE baseline").Scan(&firstCount); err != nil {
		t.Fatalf("count baseline work: %v", err)
	}
	if firstCount != 1 {
		t.Fatalf("baseline work rows = %d, want 1 (the pre-existing request's created activity)", firstCount)
	}

	// A second call, as a restart would perform, must not re-scan or duplicate.
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("second InitializeBaseline() error = %v", err)
	}
	var secondCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work WHERE baseline").Scan(&secondCount); err != nil {
		t.Fatalf("count baseline work after second call: %v", err)
	}
	if secondCount != firstCount {
		t.Fatalf("baseline work rows after second InitializeBaseline = %d, want %d", secondCount, firstCount)
	}
	var deliveryCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveryCount); err != nil {
		t.Fatalf("count notification_delivery: %v", err)
	}
	if deliveryCount != 0 {
		t.Fatalf("notification_delivery rows = %d, want 0 after baseline", deliveryCount)
	}

	// Activity committed after baseline initialization is normal work, not baseline.
	afterBaseline, err := fixture.requests.Create(t.Context(), fixture.guest.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "After baseline")
	if err != nil {
		t.Fatalf("create game request after baseline: %v", err)
	}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	if len(activities) != 1 || activities[0].GameRequestID != afterBaseline.ID {
		t.Fatalf("DiscoverActivity() = %#v, want exactly the post-baseline request %s", activities, afterBaseline.ID)
	}
	_ = preExisting
}

func TestNotificationDiscoveryHasNoGlobalSequenceCursor(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}

	first, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatalf("create first game request: %v", err)
	}

	// Simulate a transaction that assigned a higher sequence value but
	// commits before "first"'s sequence has been consumed: open a second
	// connection, start a transaction, insert (reserving a sequence number),
	// hold it uncommitted, then let "first" be discovered and its work
	// materialized, and only then commit the held transaction. A
	// high-watermark cursor advanced past "first"'s sequence would then skip
	// the held row forever; the NOT EXISTS anti-join this store uses does not.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin held transaction: %v", err)
	}
	var heldRequestID string
	if err := tx.QueryRow(t.Context(),
		"INSERT INTO game_requests (game_id, creator_id, starts_at, description) VALUES ($1, $2, now() + interval '1 hour', 'Held') RETURNING id::text",
		fixture.gameID, fixture.host.ID,
	).Scan(&heldRequestID); err != nil {
		t.Fatalf("insert held game request: %v", err)
	}
	if _, err := tx.Exec(t.Context(),
		"INSERT INTO game_request_activity (game_request_id, actor_id, kind) VALUES ($1, $2, 'created')",
		heldRequestID, fixture.host.ID,
	); err != nil {
		t.Fatalf("insert held activity: %v", err)
	}

	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	if len(activities) != 1 || activities[0].GameRequestID != first.ID {
		t.Fatalf("DiscoverActivity() before the held commit = %#v, want only %s", activities, first.ID)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], nil); err != nil {
		t.Fatalf("MaterializeWork() error = %v", err)
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit held transaction: %v", err)
	}

	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() after the held commit error = %v", err)
	}
	if len(activities) != 1 || activities[0].GameRequestID != heldRequestID {
		t.Fatalf("DiscoverActivity() after the held commit = %#v, want the previously-uncommitted row %s", activities, heldRequestID)
	}
}

func TestNotificationMaterializationIsIdempotentUnderRepeatedDiscovery(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Repeat me"); err != nil {
		t.Fatalf("create game request: %v", err)
	}

	for i := 0; i < 3; i++ {
		activities, err := store.DiscoverActivity(t.Context(), 10)
		if err != nil {
			t.Fatalf("DiscoverActivity() iteration %d error = %v", i, err)
		}
		for _, activity := range activities {
			if err := store.MaterializeWork(t.Context(), activity, []notification.Target{{Transport: "test", Key: "target-1"}}); err != nil {
				t.Fatalf("MaterializeWork() iteration %d error = %v", i, err)
			}
		}
	}

	var workCount, deliveryCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work").Scan(&workCount); err != nil {
		t.Fatalf("count notification_work: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveryCount); err != nil {
		t.Fatalf("count notification_delivery: %v", err)
	}
	if workCount != 1 {
		t.Fatalf("notification_work rows after 3 repeated discovery passes = %d, want 1", workCount)
	}
	if deliveryCount != 1 {
		t.Fatalf("notification_delivery rows after 3 repeated discovery passes = %d, want 1", deliveryCount)
	}
}

func TestNotificationWorkWithNoEligibleTargetsCompletesWithoutFutureReplay(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "No transports yet"); err != nil {
		t.Fatalf("create game request: %v", err)
	}

	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v, want exactly one activity", activities, err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], nil); err != nil {
		t.Fatalf("MaterializeWork() with no targets error = %v", err)
	}

	var workCount, deliveryCount int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work").Scan(&workCount); err != nil {
		t.Fatalf("count notification_work: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveryCount); err != nil {
		t.Fatalf("count notification_delivery: %v", err)
	}
	if workCount != 1 || deliveryCount != 0 {
		t.Fatalf("work/delivery counts = %d/%d, want 1/0 (zero eligible targets)", workCount, deliveryCount)
	}

	// "Enabling a transport later" is simulated by discovering again after
	// the fact: the activity row is already materialized, so a later pass
	// (which would now have a transport to call) must not touch it again.
	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() second pass error = %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("DiscoverActivity() second pass = %#v, want none (no replay of already-materialized work)", activities)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Start the isolated test database once for this task and the rest of the plan:

Run: `docker compose -p lansurfer-test -f compose.test.yaml up -d --wait`

Then run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotification -v`

Expected: FAIL — `notificationpostgres` package/`Store` type/methods undefined.

- [ ] **Step 3: Implement the store**

Create `internal/notification/postgres/store.go`:

```go
// Package postgres provides PostgreSQL persistence for the notification module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: sqlcgen.New(pool)}
}

// InitializeBaseline runs once, ever, on the first successful call: it marks
// the singleton notification_baseline row and, in the same transaction,
// materializes every already-visible origin=live activity row as baseline
// (never-deliverable) work. Because the marker insert and the baseline scan
// share one transaction, a crash between them leaves neither committed, so a
// retried call after a crash safely redoes the whole thing rather than
// leaving some pre-existing history undiscovered and eligible for delivery.
// The composition root calls this exactly once, synchronously, before the
// HTTP server or the Worker starts, which also means no new live activity
// can be committed concurrently with this scan.
func (s *Store) InitializeBaseline(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin baseline transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	inserted, err := queries.InsertBaselineMarker(ctx)
	if err != nil {
		return fmt.Errorf("insert baseline marker: %w", err)
	}
	if inserted == 0 {
		return nil // an earlier successful call already initialized the baseline
	}
	if err := queries.MaterializeBaselineWork(ctx); err != nil {
		return fmt.Errorf("materialize baseline work: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit baseline transaction: %w", err)
	}
	return nil
}

func (s *Store) DiscoverActivity(ctx context.Context, limit int) ([]notification.Activity, error) {
	rows, err := s.queries.DiscoverNewActivity(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("discover new activity: %w", err)
	}
	activities := make([]notification.Activity, 0, len(rows))
	for _, row := range rows {
		activities = append(activities, notification.Activity{
			Sequence: row.Sequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
			Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
		})
	}
	return activities, nil
}

// MaterializeWork inserts one notification_work row for activity and zero or
// more notification_delivery rows beneath it, all in one transaction. If the
// work row already exists (a concurrent or repeated discovery pass), it
// leaves the existing row and its deliveries untouched rather than
// duplicating them.
func (s *Store) MaterializeWork(ctx context.Context, activity notification.Activity, targets []notification.Target) error {
	gameRequestID, err := parseUUID(activity.GameRequestID)
	if err != nil {
		return fmt.Errorf("parse game request ID: %w", err)
	}
	actorID, err := parseUUID(activity.ActorID)
	if err != nil {
		return fmt.Errorf("parse actor ID: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin materialize work transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	workID, err := queries.InsertNotificationWork(ctx, sqlcgen.InsertNotificationWorkParams{
		ActivitySequence: activity.Sequence, GameRequestID: gameRequestID, ActorID: actorID,
		Kind: string(activity.Kind), OccurredAt: pgtype.Timestamptz{Time: activity.OccurredAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // another materialization already handled this activity row
	}
	if err != nil {
		return fmt.Errorf("insert notification work: %w", err)
	}
	for _, target := range targets {
		if err := queries.InsertNotificationDelivery(ctx, sqlcgen.InsertNotificationDeliveryParams{
			WorkID: workID, Transport: target.Transport, TargetKey: target.Key,
		}); err != nil {
			return fmt.Errorf("insert notification delivery: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit materialize work transaction: %w", err)
	}
	return nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func nullableText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
```

Adjust field names (`ActivitySequence`, `GameRequestID`, `RowLimit`, etc.) to whatever Task 1 Step 4 actually generated if they differ.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotification -v`

Expected: PASS for all five tests written in Step 1.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/postgres/store.go internal/platform/postgres/notification_integration_test.go
git commit -m "feat(notification): add baseline initialization and activity discovery/materialization"
```

---

### Task 4: Postgres store — durable claiming and crash/lease recovery

**Files:**
- Modify: `internal/notification/postgres/store.go` (add `ClaimDueDeliveries`)
- Modify: `internal/platform/postgres/notification_integration_test.go` (add two tests)

**Interfaces:**
- Consumes: Task 3's `Store`, `sqlcgen.ClaimDueDeliveries`.
- Produces: the claiming half of `notification.WorkerRepository`, consumed by Task 5's `Worker`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/postgres/notification_integration_test.go`:

```go
func TestNotificationClaimDueDeliveriesIsExclusiveUnderConcurrency(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	for i := 0; i < 20; i++ {
		if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Concurrent"); err != nil {
			t.Fatalf("create game request %d: %v", i, err)
		}
	}
	activities, err := store.DiscoverActivity(t.Context(), 100)
	if err != nil || len(activities) != 20 {
		t.Fatalf("DiscoverActivity() = %#v, %v, want 20 activities", activities, err)
	}
	for _, activity := range activities {
		if err := store.MaterializeWork(t.Context(), activity, []notification.Target{{Transport: "test", Key: "target-1"}}); err != nil {
			t.Fatalf("MaterializeWork() error = %v", err)
		}
	}

	const racers = 5
	seen := make(chan int64, 20*racers)
	start := make(chan struct{})
	done := make(chan struct{})
	for i := 0; i < racers; i++ {
		go func() {
			<-start
			claimed, err := store.ClaimDueDeliveries(t.Context(), 20, time.Now().Add(time.Minute))
			if err != nil {
				t.Errorf("ClaimDueDeliveries() error = %v", err)
			}
			for _, delivery := range claimed {
				seen <- delivery.DeliveryID
			}
			done <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < racers; i++ {
		<-done
	}
	close(seen)

	counts := make(map[int64]int)
	for id := range seen {
		counts[id]++
	}
	if len(counts) != 20 {
		t.Fatalf("distinct claimed delivery IDs = %d, want 20", len(counts))
	}
	for id, count := range counts {
		if count != 1 {
			t.Fatalf("delivery %d was claimed %d times, want exactly 1 (SKIP LOCKED must prevent double-claiming)", id, count)
		}
	}
}

func TestNotificationExpiredClaimLeaseBecomesClaimableAgain(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Abandoned claim"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], []notification.Target{{Transport: "test", Key: "target-1"}}); err != nil {
		t.Fatalf("MaterializeWork() error = %v", err)
	}

	firstClaim, err := store.ClaimDueDeliveries(t.Context(), 10, time.Now().Add(time.Minute))
	if err != nil || len(firstClaim) != 1 {
		t.Fatalf("first ClaimDueDeliveries() = %#v, %v, want exactly one claim", firstClaim, err)
	}

	// A second claim attempt before the lease expires must find nothing: the
	// worker that made the first claim is (as far as this test can tell)
	// still working on it.
	stillLeased, err := store.ClaimDueDeliveries(t.Context(), 10, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimDueDeliveries() while leased error = %v", err)
	}
	if len(stillLeased) != 0 {
		t.Fatalf("ClaimDueDeliveries() while leased = %#v, want none", stillLeased)
	}

	// Simulate a crashed worker: back-date the lease directly, as no
	// application code path is expected to do this.
	if _, err := pool.Exec(t.Context(),
		"UPDATE notification_delivery SET claimed_until = now() - interval '1 second' WHERE id = $1",
		firstClaim[0].DeliveryID,
	); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	recovered, err := store.ClaimDueDeliveries(t.Context(), 10, time.Now().Add(time.Minute))
	if err != nil || len(recovered) != 1 || recovered[0].DeliveryID != firstClaim[0].DeliveryID {
		t.Fatalf("ClaimDueDeliveries() after lease expiry = %#v, %v, want to recover delivery %d", recovered, err, firstClaim[0].DeliveryID)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotificationClaim -v` and `-run TestNotificationExpiredClaimLease`

Expected: FAIL — `Store.ClaimDueDeliveries` undefined.

- [ ] **Step 3: Implement ClaimDueDeliveries**

Add to `internal/notification/postgres/store.go`:

```go
func (s *Store) ClaimDueDeliveries(ctx context.Context, limit int, leaseUntil time.Time) ([]notification.ClaimedDelivery, error) {
	rows, err := s.queries.ClaimDueDeliveries(ctx, sqlcgen.ClaimDueDeliveriesParams{
		RowLimit: int32(limit), ClaimedUntil: pgtype.Timestamptz{Time: leaseUntil, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("claim due deliveries: %w", err)
	}
	claimed := make([]notification.ClaimedDelivery, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, notification.ClaimedDelivery{
			DeliveryID:   row.ID,
			Target:       notification.Target{Transport: row.Transport, Key: row.TargetKey},
			AttemptCount: row.AttemptCount,
			Activity: notification.Activity{
				Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
				Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
			},
		})
	}
	return claimed, nil
}
```

Add `"time"` to the import block.

- [ ] **Step 4: Run the tests to verify they pass**

Run the same commands as Step 2.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/postgres/store.go internal/platform/postgres/notification_integration_test.go
git commit -m "feat(notification): add durable delivery claiming with lease-based crash recovery"
```

---

### Task 5: Worker — delivery processing, outcomes, retries, and end-to-end proof with a fake transport

**Files:**
- Modify: `internal/notification/postgres/store.go` (add `RecordSuccess`, `RecordRetry`, `RecordExhausted`, `RecordPermanent`, `RecordUncertain`)
- Create: `internal/notification/worker.go`
- Modify: `internal/platform/postgres/notification_integration_test.go` (add a `fakeTransport` type and seven tests)

**Interfaces:**
- Consumes: `notification.WorkerRepository` (now complete), `notification.Transport`, `computeBackoff`/`applyJitter`/`MaxAutomaticAttempts` from Task 2.
- Produces: `Worker{}`, `NewWorker(repo WorkerRepository, transports []Transport, logger *slog.Logger, now func() time.Time) *Worker`, `(*Worker).Run(ctx context.Context)`, `(*Worker).Wait(ctx context.Context)` — `Wait` is also exactly what Task 7 tests in isolation.

- [ ] **Step 1: Write the failing outcome-recording tests and the fake transport**

Append to `internal/platform/postgres/notification_integration_test.go`:

```go
// fakeTransport is a controllable notification.Transport for proving the
// worker's claim/send/record loop without a real Web Push or Discord
// adapter, which do not exist until later tracks.
type fakeTransport struct {
	name        string
	targets     []notification.Target
	targetsErr  error
	results     map[string]notification.SendResult // keyed by target.Key
	defaultSend notification.SendResult
	sendCalls   chan notification.Target
}

func newFakeTransport(name string, targets []notification.Target) *fakeTransport {
	return &fakeTransport{
		name: name, targets: targets, results: map[string]notification.SendResult{},
		defaultSend: notification.SendResult{Outcome: notification.OutcomeSucceeded},
		sendCalls:   make(chan notification.Target, 64),
	}
}

func (f *fakeTransport) Name() string { return f.name }

func (f *fakeTransport) Targets(context.Context, notification.Activity) ([]notification.Target, error) {
	if f.targetsErr != nil {
		return nil, f.targetsErr
	}
	return f.targets, nil
}

func (f *fakeTransport) Send(_ context.Context, _ notification.Activity, target notification.Target) notification.SendResult {
	select {
	case f.sendCalls <- target:
	default:
	}
	if result, ok := f.results[target.Key]; ok {
		return result
	}
	return f.defaultSend
}

func newTestWorker(repo *notificationpostgres.Store, transports []notification.Transport) *notification.Worker {
	return notification.NewWorker(repo, transports, testLogger(), time.Now)
}

func materializeOneActivityWithTransport(t *testing.T, store *notificationpostgres.Store, activity notification.Activity, transport *fakeTransport) {
	t.Helper()
	targets, err := transport.Targets(t.Context(), activity)
	if err != nil {
		t.Fatalf("transport.Targets() error = %v", err)
	}
	if err := store.MaterializeWork(t.Context(), activity, targets); err != nil {
		t.Fatalf("MaterializeWork() error = %v", err)
	}
}

func TestNotificationFakeTransportSuccessPathMarksDeliverySucceeded(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Success path")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())

	var status string
	if err := pool.QueryRow(t.Context(),
		"SELECT status FROM notification_delivery d JOIN notification_work w ON w.id = d.work_id WHERE w.game_request_id = $1",
		created.ID,
	).Scan(&status); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("delivery status = %q, want %q", status, "succeeded")
	}
}

func TestNotificationPermanentFailureIsTerminal(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Permanent failure"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.results["target-1"] = notification.SendResult{Outcome: notification.OutcomePermanent, Err: errors.New("target permanently invalid")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	worker.Tick(t.Context()) // a permanent delivery must never be reclaimed

	var status string
	var attempts int32
	if err := pool.QueryRow(t.Context(),
		"SELECT status, attempt_count FROM notification_delivery ORDER BY id DESC LIMIT 1",
	).Scan(&status, &attempts); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "permanent" {
		t.Fatalf("delivery status = %q, want %q", status, "permanent")
	}
	if attempts != 0 {
		t.Fatalf("attempt_count = %d, want 0 (permanent is not a retry outcome)", attempts)
	}
	if len(transport.sendCalls) != 1 {
		t.Fatalf("Send() was called %d times, want exactly 1 (a permanent delivery must not be reclaimed)", len(transport.sendCalls))
	}
}

func TestNotificationUncertainOutcomeRequiresManualRetryAndIsNeverAutomaticallyRetried(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Uncertain outcome"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.results["target-1"] = notification.SendResult{Outcome: notification.OutcomeUncertain, Err: errors.New("ambiguous provider response")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	for i := 0; i < 3; i++ {
		worker.Tick(t.Context())
	}

	var status string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&status); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "uncertain" {
		t.Fatalf("delivery status = %q, want %q", status, "uncertain")
	}
	if len(transport.sendCalls) != 1 {
		t.Fatalf("Send() was called %d times across 3 ticks, want exactly 1 (uncertain must never be automatically retried)", len(transport.sendCalls))
	}
}

func TestNotificationRetryBackoffProgressesToExhaustionAfterSixAttempts(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Always retryable"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomeRetryable, Err: errors.New("temporary provider error")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	for attempt := 1; attempt <= notification.MaxAutomaticAttempts; attempt++ {
		if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET next_attempt_at = now()"); err != nil {
			t.Fatalf("force delivery due: %v", err)
		}
		worker.Tick(t.Context())

		var status string
		var attemptCount int32
		if err := pool.QueryRow(t.Context(),
			"SELECT status, attempt_count FROM notification_delivery ORDER BY id DESC LIMIT 1",
		).Scan(&status, &attemptCount); err != nil {
			t.Fatalf("read delivery after attempt %d: %v", attempt, err)
		}
		if int(attemptCount) != attempt {
			t.Fatalf("attempt %d: attempt_count = %d, want %d", attempt, attemptCount, attempt)
		}
		if attempt < notification.MaxAutomaticAttempts {
			if status != "pending" {
				t.Fatalf("attempt %d: status = %q, want %q", attempt, status, "pending")
			}
		} else {
			if status != "exhausted" {
				t.Fatalf("final attempt: status = %q, want %q", status, "exhausted")
			}
		}
	}

	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET next_attempt_at = now()"); err != nil {
		t.Fatalf("force delivery due after exhaustion: %v", err)
	}
	worker.Tick(t.Context())
	sendsBeforeExtraTick := len(transport.sendCalls)
	var attemptCountAfterExtraTick int32
	if err := pool.QueryRow(t.Context(), "SELECT attempt_count FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&attemptCountAfterExtraTick); err != nil {
		t.Fatalf("read attempt_count after exhaustion tick: %v", err)
	}
	if int(attemptCountAfterExtraTick) != notification.MaxAutomaticAttempts {
		t.Fatalf("attempt_count after exhaustion tick = %d, want unchanged at %d (exhausted deliveries are never reclaimed)", attemptCountAfterExtraTick, notification.MaxAutomaticAttempts)
	}
	_ = sendsBeforeExtraTick
}

func TestNotificationRetryAfterHintOverridesScheduledBackoffCappedAtFiveMinutes(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Retry-After"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	retryAfter := 20 * time.Minute // deliberately far beyond the 5m cap
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.results["target-1"] = notification.SendResult{Outcome: notification.OutcomeRetryable, RetryAfter: &retryAfter, Err: errors.New("rate limited")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	before := time.Now()
	worker.Tick(t.Context())

	var nextAttemptAt time.Time
	if err := pool.QueryRow(t.Context(), "SELECT next_attempt_at FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&nextAttemptAt); err != nil {
		t.Fatalf("read next_attempt_at: %v", err)
	}
	delay := nextAttemptAt.Sub(before)
	if delay < 4*time.Minute+30*time.Second || delay > 5*time.Minute+30*time.Second {
		t.Fatalf("next_attempt_at delay = %v, want capped at approximately 5m (Retry-After of 20m must not be honored past the cap)", delay)
	}
}

func TestNotificationRestartRedeliversPendingAndRetryScheduledWorkAfterProcessRestart(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}

	// One request whose delivery will be scheduled for a future retry before
	// the restart, and a second, created after the first Tick, whose
	// delivery is still untouched (pending, immediately due) at restart time
	// — the two required "restart with pending work" and "restart with
	// retry-scheduled work" cases, proven together.
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Restart retry-scheduled"); err != nil {
		t.Fatalf("create retry-scheduled game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}, {Transport: "test", Key: "target-2"}})
	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomeRetryable, Err: errors.New("temporary")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], []notification.Target{{Transport: "test", Key: "target-1"}}); err != nil {
		t.Fatalf("MaterializeWork() for the retry-scheduled request error = %v", err)
	}

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context()) // one failure on target-1 schedules a future retry

	var scheduledAt time.Time
	var scheduledDeliveryID int64
	if err := pool.QueryRow(t.Context(), "SELECT id, next_attempt_at FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&scheduledDeliveryID, &scheduledAt); err != nil {
		t.Fatalf("read scheduled retry: %v", err)
	}
	if !scheduledAt.After(time.Now()) {
		t.Fatalf("next_attempt_at = %v, want a future time after one retryable failure", scheduledAt)
	}

	// A second request materialized after that Tick, whose delivery has
	// never been attempted and is still pending/due when the restart happens.
	if _, err := fixture.requests.Create(t.Context(), fixture.guest.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Restart still pending"); err != nil {
		t.Fatalf("create still-pending game request: %v", err)
	}
	pendingActivities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(pendingActivities) != 1 {
		t.Fatalf("DiscoverActivity() for the still-pending request = %#v, %v", pendingActivities, err)
	}
	if err := store.MaterializeWork(t.Context(), pendingActivities[0], []notification.Target{{Transport: "test", Key: "target-2"}}); err != nil {
		t.Fatalf("MaterializeWork() for the still-pending request error = %v", err)
	}
	var pendingDeliveryID int64
	var pendingStatus string
	if err := pool.QueryRow(t.Context(),
		"SELECT id, status FROM notification_delivery WHERE target_key = 'target-2'",
	).Scan(&pendingDeliveryID, &pendingStatus); err != nil {
		t.Fatalf("read still-pending delivery: %v", err)
	}
	if pendingStatus != "pending" {
		t.Fatalf("still-pending delivery status = %q, want %q (never attempted before restart)", pendingStatus, "pending")
	}

	// "Restart" the process: close this pool, open a fresh one against the
	// same schema, and build a new Store/Worker, exactly as
	// TestCommittedActivitySurvivesRestartBeforeDelivery does for Track A.
	pool.Close()
	restartedPool := newTestPool(t)
	restartedStore := notificationpostgres.NewStore(restartedPool)
	restartedTransport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}, {Transport: "test", Key: "target-2"}})
	restartedWorker := newTestWorker(restartedStore, []notification.Transport{restartedTransport})

	// The still-pending delivery (target-2) is due now and must be picked up
	// immediately; the retry-scheduled delivery (target-1) is not yet due
	// and must not be jumped.
	restartedWorker.Tick(t.Context())
	var statusAfterFirstRestartTick string
	if err := restartedPool.QueryRow(t.Context(), "SELECT status FROM notification_delivery WHERE id = $1", pendingDeliveryID).Scan(&statusAfterFirstRestartTick); err != nil {
		t.Fatalf("read still-pending delivery status after restart: %v", err)
	}
	if statusAfterFirstRestartTick != "succeeded" {
		t.Fatalf("still-pending delivery status after restart = %q, want %q", statusAfterFirstRestartTick, "succeeded")
	}
	sawTarget1 := false
	for target := range drainTargets(restartedTransport.sendCalls) {
		if target.Key == "target-1" {
			sawTarget1 = true
		}
	}
	if sawTarget1 {
		t.Fatal("the retry-scheduled delivery (target-1) was attempted before it was due")
	}

	if _, err := restartedPool.Exec(t.Context(), "UPDATE notification_delivery SET next_attempt_at = now() WHERE id = $1", scheduledDeliveryID); err != nil {
		t.Fatalf("advance clock to the scheduled retry: %v", err)
	}
	restartedTransport.defaultSend = notification.SendResult{Outcome: notification.OutcomeSucceeded}
	restartedWorker.Tick(t.Context())

	var finalStatus string
	if err := restartedPool.QueryRow(t.Context(), "SELECT status FROM notification_delivery WHERE id = $1", scheduledDeliveryID).Scan(&finalStatus); err != nil {
		t.Fatalf("read final status: %v", err)
	}
	if finalStatus != "succeeded" {
		t.Fatalf("final status after restart = %q, want %q", finalStatus, "succeeded")
	}
}

// drainTargets non-blockingly reads every currently buffered target off a
// fakeTransport's sendCalls channel, for asserting what was (or was not)
// attempted so far without racing the channel's producer.
func drainTargets(calls chan notification.Target) <-chan notification.Target {
	drained := make(chan notification.Target, cap(calls))
	for {
		select {
		case target := <-calls:
			drained <- target
		default:
			close(drained)
			return drained
		}
	}
}

func TestNotificationTransportFailureNeverAffectsOriginatingGameRequestWrites(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Never affected")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	if err := fixture.requests.Join(t.Context(), created.ID, fixture.guest.ID); err != nil {
		t.Fatalf("join game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomePermanent, Err: errors.New("provider rejected")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	for _, activity := range activities {
		materializeOneActivityWithTransport(t, store, activity, transport)
	}

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())

	var status string
	var memberCount int
	if err := pool.QueryRow(t.Context(), "SELECT status FROM game_requests WHERE id = $1", created.ID).Scan(&status); err != nil {
		t.Fatalf("read game request status: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM game_request_members WHERE game_request_id = $1", created.ID).Scan(&memberCount); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if status != "open" || memberCount != 2 {
		t.Fatalf("game request state after a permanent transport failure = status=%q members=%d, want status=%q members=2 (unaffected)", status, memberCount, "open")
	}
}
```

Add `"context"` and `"errors"` to the file's import block if not already present from Task 3.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotificationFakeTransport -v`

Expected: FAIL — `Store.RecordSuccess` etc. and `notification.Worker`/`NewWorker`/`Tick` undefined.

- [ ] **Step 3: Implement the remaining Store outcome methods**

Add to `internal/notification/postgres/store.go`:

```go
func (s *Store) RecordSuccess(ctx context.Context, deliveryID int64) error {
	if err := s.queries.RecordDeliverySuccess(ctx, deliveryID); err != nil {
		return fmt.Errorf("record delivery success: %w", err)
	}
	return nil
}

func (s *Store) RecordRetry(ctx context.Context, deliveryID int64, nextAttemptAt time.Time, lastError string) error {
	if err := s.queries.RecordDeliveryRetry(ctx, sqlcgen.RecordDeliveryRetryParams{
		ID: deliveryID, NextAttemptAt: pgtype.Timestamptz{Time: nextAttemptAt, Valid: true}, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery retry: %w", err)
	}
	return nil
}

func (s *Store) RecordExhausted(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryExhausted(ctx, sqlcgen.RecordDeliveryExhaustedParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery exhausted: %w", err)
	}
	return nil
}

func (s *Store) RecordPermanent(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryPermanent(ctx, sqlcgen.RecordDeliveryPermanentParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery permanent: %w", err)
	}
	return nil
}

func (s *Store) RecordUncertain(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryUncertain(ctx, sqlcgen.RecordDeliveryUncertainParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery uncertain: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Implement the Worker**

Create `internal/notification/worker.go`:

```go
package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	DefaultPollInterval   = 2 * time.Second
	DefaultClaimLease     = 60 * time.Second
	DefaultSendTimeout    = 20 * time.Second
	DefaultDiscoveryBatch = 200
	DefaultClaimBatch     = 20
)

// Worker discovers new activity, materializes work/delivery rows, claims due
// deliveries, and attempts them against registered transports on a fixed
// poll interval. It runs entirely against WorkerRepository and never touches
// gamerequest's tables directly.
type Worker struct {
	repo             WorkerRepository
	transports       []Transport
	transportsByName map[string]Transport
	logger           *slog.Logger
	now              func() time.Time
	jitter           func(time.Duration) time.Duration
	pollInterval     time.Duration
	claimLease       time.Duration
	sendTimeout      time.Duration
	discoveryBatch   int
	claimBatch       int
	done             chan struct{}
}

func NewWorker(repo WorkerRepository, transports []Transport, logger *slog.Logger, now func() time.Time) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	byName := make(map[string]Transport, len(transports))
	for _, transport := range transports {
		byName[transport.Name()] = transport
	}
	return &Worker{
		repo: repo, transports: transports, transportsByName: byName, logger: logger, now: now, jitter: applyJitter,
		pollInterval: DefaultPollInterval, claimLease: DefaultClaimLease, sendTimeout: DefaultSendTimeout,
		discoveryBatch: DefaultDiscoveryBatch, claimBatch: DefaultClaimBatch, done: make(chan struct{}),
	}
}

// Run polls until ctx is done, then closes the channel Wait blocks on. It
// ties every database operation to ctx, so application shutdown (which
// cancels ctx) stops new work without requiring a separate stop signal.
func (w *Worker) Run(ctx context.Context) {
	defer close(w.done)
	w.Tick(ctx)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Wait blocks until Run has returned (so no further database operations are
// in flight) or ctx is done, whichever comes first. The composition root
// uses a bounded ctx during shutdown so it never waits forever, mirroring
// IGDBSyncService.Wait.
func (w *Worker) Wait(ctx context.Context) {
	select {
	case <-w.done:
	case <-ctx.Done():
	}
}

// Tick runs one discovery-and-delivery pass. It is exported so integration
// tests can drive the worker deterministically instead of waiting on its
// internal poll ticker.
func (w *Worker) Tick(ctx context.Context) {
	w.materialize(ctx)
	w.processDue(ctx)
}

func (w *Worker) materialize(ctx context.Context) {
	activities, err := w.repo.DiscoverActivity(ctx, w.discoveryBatch)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("discover new notification activity", "error", err)
		}
		return
	}
	for _, activity := range activities {
		var targets []Target
		for _, transport := range w.transports {
			transportTargets, err := transport.Targets(ctx, activity)
			if err != nil {
				w.logger.Error("resolve transport targets", "transport", transport.Name(), "error", err)
				continue
			}
			targets = append(targets, transportTargets...)
		}
		if err := w.repo.MaterializeWork(ctx, activity, targets); err != nil && ctx.Err() == nil {
			w.logger.Error("materialize notification work", "activity_sequence", activity.Sequence, "error", err)
		}
	}
}

func (w *Worker) processDue(ctx context.Context) {
	claimed, err := w.repo.ClaimDueDeliveries(ctx, w.claimBatch, w.now().Add(w.claimLease))
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("claim due notification deliveries", "error", err)
		}
		return
	}
	for _, delivery := range claimed {
		w.processOne(ctx, delivery)
	}
}

func (w *Worker) processOne(ctx context.Context, delivery ClaimedDelivery) {
	transport, ok := w.transportsByName[delivery.Target.Transport]
	if !ok {
		w.recordOutcome(ctx, delivery, SendResult{
			Outcome: OutcomePermanent, Err: fmt.Errorf("transport %q is not configured", delivery.Target.Transport),
		})
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
	result := transport.Send(sendCtx, delivery.Activity, delivery.Target)
	cancel()
	w.recordOutcome(ctx, delivery, result)
}

func (w *Worker) recordOutcome(ctx context.Context, delivery ClaimedDelivery, result SendResult) {
	var recordErr error
	switch result.Outcome {
	case OutcomeSucceeded:
		recordErr = w.repo.RecordSuccess(ctx, delivery.DeliveryID)
	case OutcomePermanent:
		recordErr = w.repo.RecordPermanent(ctx, delivery.DeliveryID, errorMessage(result.Err))
	case OutcomeUncertain:
		recordErr = w.repo.RecordUncertain(ctx, delivery.DeliveryID, errorMessage(result.Err))
	default: // OutcomeRetryable
		attemptsMade := int(delivery.AttemptCount) + 1
		if attemptsMade >= MaxAutomaticAttempts {
			recordErr = w.repo.RecordExhausted(ctx, delivery.DeliveryID, errorMessage(result.Err))
		} else {
			delay := computeBackoff(attemptsMade, result.RetryAfter, w.jitter)
			recordErr = w.repo.RecordRetry(ctx, delivery.DeliveryID, w.now().Add(delay), errorMessage(result.Err))
		}
	}
	if recordErr != nil && ctx.Err() == nil {
		w.logger.Error("record notification delivery outcome", "delivery_id", delivery.DeliveryID, "error", recordErr)
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotification -v`

Expected: PASS for every test added in Tasks 3–5.

- [ ] **Step 6: Commit**

```bash
git add internal/notification/postgres/store.go internal/notification/worker.go internal/platform/postgres/notification_integration_test.go
git commit -m "feat(notification): add the delivery worker with retry, backoff, and Retry-After handling"
```

---

### Task 6: Diagnostics and manual retry — Service, remaining Store methods

**Files:**
- Modify: `internal/notification/postgres/store.go` (add `ListProblemDeliveries`, `GetDeliveryDetail`, `ReopenDelivery`, `ReopenUncertainDelivery`)
- Create: `internal/notification/service.go`
- Create: `internal/notification/service_test.go` (unit tests with fakes, no DB)
- Modify: `internal/platform/postgres/notification_integration_test.go` (add two end-to-end tests)

**Interfaces:**
- Consumes: `notification.DiagnosticsRepository`, `notification.Transport`.
- Produces: `Service{}`, `NewService(repo DiagnosticsRepository, transports []Transport) *Service`, `(*Service).ListProblems`, `(*Service).ManualRetry`, `(*Service).ManualRetryUncertain` — consumed by Task 9's CLI commands.

- [ ] **Step 1: Write the failing Service unit tests**

Create `internal/notification/service_test.go`:

```go
package notification

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDiagnosticsRepository struct {
	problems       []ProblemDelivery
	detail         DeliveryDetail
	detailErr      error
	reopened       bool
	reopenedErr    error
	uncertainErr   error
	reopenedUncCal bool
	reopenedCal    bool
}

func (f *fakeDiagnosticsRepository) ListProblemDeliveries(context.Context) ([]ProblemDelivery, error) {
	return f.problems, nil
}

func (f *fakeDiagnosticsRepository) GetDeliveryDetail(context.Context, int64) (DeliveryDetail, error) {
	return f.detail, f.detailErr
}

func (f *fakeDiagnosticsRepository) ReopenDelivery(context.Context, int64) (bool, error) {
	f.reopenedCal = true
	return f.reopened, f.reopenedErr
}

func (f *fakeDiagnosticsRepository) ReopenUncertainDelivery(context.Context, int64) (bool, error) {
	f.reopenedUncCal = true
	return f.reopened, f.uncertainErr
}

type fakeManualRetryTransport struct {
	name    string
	targets []Target
}

func (f fakeManualRetryTransport) Name() string { return f.name }
func (f fakeManualRetryTransport) Targets(context.Context, Activity) ([]Target, error) {
	return f.targets, nil
}
func (f fakeManualRetryTransport) Send(context.Context, Activity, Target) SendResult {
	return SendResult{Outcome: OutcomeSucceeded}
}

func TestServiceManualRetryRejectsDeliveryNotInExhaustedOrPermanentState(t *testing.T) {
	repo := &fakeDiagnosticsRepository{detail: DeliveryDetail{DeliveryID: 1, Status: StatusUncertain}}
	service := NewService(repo, nil)
	if err := service.ManualRetry(t.Context(), 1); !errors.Is(err, ErrDeliveryNotRetryable) {
		t.Fatalf("ManualRetry() on an uncertain delivery = %v, want %v", err, ErrDeliveryNotRetryable)
	}
	if repo.reopenedCal {
		t.Fatal("ManualRetry() must not reopen a delivery in the wrong state")
	}
}

func TestServiceManualRetryUncertainRejectsDeliveryNotUncertain(t *testing.T) {
	repo := &fakeDiagnosticsRepository{detail: DeliveryDetail{DeliveryID: 1, Status: StatusExhausted}}
	service := NewService(repo, nil)
	if err := service.ManualRetryUncertain(t.Context(), 1); !errors.Is(err, ErrDeliveryNotUncertain) {
		t.Fatalf("ManualRetryUncertain() on an exhausted delivery = %v, want %v", err, ErrDeliveryNotUncertain)
	}
	if repo.reopenedUncCal {
		t.Fatal("ManualRetryUncertain() must not reopen a delivery that is not uncertain")
	}
}

func TestServiceManualRetryRejectsWhenTargetNoLongerEligible(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail: DeliveryDetail{DeliveryID: 1, Status: StatusExhausted, Target: target, Activity: Activity{Sequence: 1}},
	}
	transport := fakeManualRetryTransport{name: "test", targets: nil} // recheck finds no eligible targets
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetry(t.Context(), 1); !errors.Is(err, ErrNoLongerEligible) {
		t.Fatalf("ManualRetry() with no eligible targets = %v, want %v", err, ErrNoLongerEligible)
	}
	if repo.reopenedCal {
		t.Fatal("ManualRetry() must not reopen a delivery that failed its eligibility recheck")
	}
}

func TestServiceManualRetrySucceedsAndReopensDelivery(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail:   DeliveryDetail{DeliveryID: 1, Status: StatusExhausted, Target: target, Activity: Activity{Sequence: 1}},
		reopened: true,
	}
	transport := fakeManualRetryTransport{name: "test", targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetry(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetry() error = %v, want nil", err)
	}
	if !repo.reopenedCal {
		t.Fatal("ManualRetry() must call ReopenDelivery once eligibility is confirmed")
	}
}

func TestServiceManualRetryUncertainIsDistinctFromOrdinaryManualRetry(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail:   DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
		reopened: true,
	}
	transport := fakeManualRetryTransport{name: "test", targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v, want nil", err)
	}
	if !repo.reopenedUncCal || repo.reopenedCal {
		t.Fatal("ManualRetryUncertain() must call ReopenUncertainDelivery, not ReopenDelivery")
	}
}

func TestServiceListProblemsReturnsRepositoryResult(t *testing.T) {
	want := []ProblemDelivery{{DeliveryID: 1, Status: StatusExhausted, UpdatedAt: time.Now()}}
	repo := &fakeDiagnosticsRepository{problems: want}
	service := NewService(repo, nil)
	got, err := service.ListProblems(t.Context())
	if err != nil || len(got) != 1 || got[0].DeliveryID != want[0].DeliveryID {
		t.Fatalf("ListProblems() = %#v, %v, want %#v", got, err, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notification/... -run TestServiceManual -v`
Expected: FAIL — `NewService`/`Service` undefined.

- [ ] **Step 3: Implement Service**

Create `internal/notification/service.go`:

```go
package notification

import (
	"context"
	"fmt"
)

// Service exposes authorized diagnostics and manual retry over durable
// notification state. It performs no authorization itself: the caller
// (currently the admin CLI in internal/app, see Task 9) is responsible for
// only reaching Service through an already-authorized path.
type Service struct {
	repo             DiagnosticsRepository
	transportsByName map[string]Transport
}

func NewService(repo DiagnosticsRepository, transports []Transport) *Service {
	byName := make(map[string]Transport, len(transports))
	for _, transport := range transports {
		byName[transport.Name()] = transport
	}
	return &Service{repo: repo, transportsByName: byName}
}

func (s *Service) ListProblems(ctx context.Context) ([]ProblemDelivery, error) {
	return s.repo.ListProblemDeliveries(ctx)
}

// ManualRetry reopens one exhausted or permanent delivery for a fresh
// automatic retry budget, after confirming the target is still eligible.
func (s *Service) ManualRetry(ctx context.Context, deliveryID int64) error {
	return s.reopen(ctx, deliveryID, false)
}

// ManualRetryUncertain reopens one uncertain delivery despite the
// possibility of a duplicate externally visible effect. It is a distinct
// action from ManualRetry and only applies to deliveries already in the
// uncertain state.
func (s *Service) ManualRetryUncertain(ctx context.Context, deliveryID int64) error {
	return s.reopen(ctx, deliveryID, true)
}

func (s *Service) reopen(ctx context.Context, deliveryID int64, uncertainOnly bool) error {
	detail, err := s.repo.GetDeliveryDetail(ctx, deliveryID)
	if err != nil {
		return err
	}
	if uncertainOnly {
		if detail.Status != StatusUncertain {
			return ErrDeliveryNotUncertain
		}
	} else if detail.Status != StatusExhausted && detail.Status != StatusPermanent {
		return ErrDeliveryNotRetryable
	}

	transport, ok := s.transportsByName[detail.Target.Transport]
	if !ok {
		return ErrTransportNotConfigured
	}
	targets, err := transport.Targets(ctx, detail.Activity)
	if err != nil {
		return fmt.Errorf("recheck delivery eligibility: %w", err)
	}
	if !containsTarget(targets, detail.Target) {
		return ErrNoLongerEligible
	}

	var reopened bool
	if uncertainOnly {
		reopened, err = s.repo.ReopenUncertainDelivery(ctx, deliveryID)
	} else {
		reopened, err = s.repo.ReopenDelivery(ctx, deliveryID)
	}
	if err != nil {
		return err
	}
	if !reopened {
		if uncertainOnly {
			return ErrDeliveryNotUncertain
		}
		return ErrDeliveryNotRetryable
	}
	return nil
}

func containsTarget(targets []Target, want Target) bool {
	for _, target := range targets {
		if target == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `go test ./internal/notification/... -v`
Expected: PASS for every test in `backoff_test.go` and `service_test.go`.

- [ ] **Step 5: Write the failing end-to-end integration tests**

Append to `internal/platform/postgres/notification_integration_test.go`:

```go
func TestNotificationManualRetryReopensExhaustedDeliveryEndToEnd(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Exhaust then retry"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomeRetryable, Err: errors.New("temporary")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	var deliveryID int64
	for attempt := 1; attempt <= notification.MaxAutomaticAttempts; attempt++ {
		if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET next_attempt_at = now()"); err != nil {
			t.Fatalf("force delivery due: %v", err)
		}
		worker.Tick(t.Context())
	}
	if err := pool.QueryRow(t.Context(), "SELECT id FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&deliveryID); err != nil {
		t.Fatalf("read exhausted delivery ID: %v", err)
	}
	var statusBeforeRetry string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM notification_delivery WHERE id = $1", deliveryID).Scan(&statusBeforeRetry); err != nil {
		t.Fatalf("read status before manual retry: %v", err)
	}
	if statusBeforeRetry != "exhausted" {
		t.Fatalf("status before manual retry = %q, want %q", statusBeforeRetry, "exhausted")
	}

	service := notification.NewService(store, []notification.Transport{transport})
	if err := service.ManualRetry(t.Context(), deliveryID); err != nil {
		t.Fatalf("ManualRetry() error = %v", err)
	}
	var statusAfterRetry string
	var attemptCountAfterRetry int32
	if err := pool.QueryRow(t.Context(),
		"SELECT status, attempt_count FROM notification_delivery WHERE id = $1", deliveryID,
	).Scan(&statusAfterRetry, &attemptCountAfterRetry); err != nil {
		t.Fatalf("read status after manual retry: %v", err)
	}
	if statusAfterRetry != "pending" || attemptCountAfterRetry != 0 {
		t.Fatalf("status/attempt_count after ManualRetry() = %q/%d, want pending/0 (a fresh retry budget)", statusAfterRetry, attemptCountAfterRetry)
	}

	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomeSucceeded}
	worker.Tick(t.Context())
	var finalStatus string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM notification_delivery WHERE id = $1", deliveryID).Scan(&finalStatus); err != nil {
		t.Fatalf("read final status: %v", err)
	}
	if finalStatus != "succeeded" {
		t.Fatalf("final status after the manually retried attempt = %q, want %q", finalStatus, "succeeded")
	}
}

func TestNotificationManualRetryUncertainReopensUncertainDeliveryEndToEnd(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Uncertain then retry"); err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	transport.defaultSend = notification.SendResult{Outcome: notification.OutcomeUncertain, Err: errors.New("ambiguous")}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	var deliveryID int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&deliveryID); err != nil {
		t.Fatalf("read uncertain delivery ID: %v", err)
	}

	service := notification.NewService(store, []notification.Transport{transport})
	if err := service.ManualRetry(t.Context(), deliveryID); err == nil {
		t.Fatal("ordinary ManualRetry() on an uncertain delivery must fail; use ManualRetryUncertain")
	}
	if err := service.ManualRetryUncertain(t.Context(), deliveryID); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v", err)
	}
	var statusAfterRetry string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM notification_delivery WHERE id = $1", deliveryID).Scan(&statusAfterRetry); err != nil {
		t.Fatalf("read status after ManualRetryUncertain(): %v", err)
	}
	if statusAfterRetry != "pending" {
		t.Fatalf("status after ManualRetryUncertain() = %q, want %q", statusAfterRetry, "pending")
	}
}
```

- [ ] **Step 6: Implement the remaining Store methods**

Add to `internal/notification/postgres/store.go`:

```go
func (s *Store) ListProblemDeliveries(ctx context.Context) ([]notification.ProblemDelivery, error) {
	rows, err := s.queries.ListProblemDeliveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list problem deliveries: %w", err)
	}
	problems := make([]notification.ProblemDelivery, 0, len(rows))
	for _, row := range rows {
		problems = append(problems, notification.ProblemDelivery{
			DeliveryID: row.ID, Target: notification.Target{Transport: row.Transport, Key: row.TargetKey},
			Status: notification.DeliveryStatus(row.Status), AttemptCount: row.AttemptCount,
			LastError: textString(row.LastError), UpdatedAt: row.UpdatedAt.Time,
			Activity: notification.Activity{
				Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
				Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
			},
		})
	}
	return problems, nil
}

func (s *Store) GetDeliveryDetail(ctx context.Context, deliveryID int64) (notification.DeliveryDetail, error) {
	row, err := s.queries.GetDeliveryDetail(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.DeliveryDetail{}, notification.ErrDeliveryNotFound
	}
	if err != nil {
		return notification.DeliveryDetail{}, fmt.Errorf("get delivery detail: %w", err)
	}
	return notification.DeliveryDetail{
		DeliveryID: row.ID, Target: notification.Target{Transport: row.Transport, Key: row.TargetKey},
		Status: notification.DeliveryStatus(row.Status),
		Activity: notification.Activity{
			Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
			Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
		},
	}, nil
}

func (s *Store) ReopenDelivery(ctx context.Context, deliveryID int64) (bool, error) {
	_, err := s.queries.ReopenDeliveryForManualRetry(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reopen delivery for manual retry: %w", err)
	}
	return true, nil
}

func (s *Store) ReopenUncertainDelivery(ctx context.Context, deliveryID int64) (bool, error) {
	_, err := s.queries.ReopenUncertainDeliveryForManualRetry(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reopen uncertain delivery for manual retry: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestNotificationManualRetry -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/notification/postgres/store.go internal/notification/service.go internal/notification/service_test.go internal/platform/postgres/notification_integration_test.go
git commit -m "feat(notification): add authorized diagnostics and manual retry"
```

---

### Task 7: Worker lifecycle — Run/Wait/shutdown without a real database

**Files:**
- Create: `internal/notification/worker_test.go`

**Interfaces:**
- Consumes: `notification.Worker`, `notification.WorkerRepository` (implemented by a local fake).

- [ ] **Step 1: Write the failing test**

Create `internal/notification/worker_test.go`:

```go
package notification

import (
	"context"
	"testing"
	"time"
)

// blockingWorkerRepository blocks its first DiscoverActivity call until
// unblock is closed or ctx is done, so tests can control exactly when the
// Worker's first tick finishes.
type blockingWorkerRepository struct {
	unblock chan struct{}
}

func (r *blockingWorkerRepository) DiscoverActivity(ctx context.Context, limit int) ([]Activity, error) {
	select {
	case <-r.unblock:
	case <-ctx.Done():
	}
	return nil, ctx.Err()
}

func (r *blockingWorkerRepository) MaterializeWork(context.Context, Activity, []Target) error { return nil }
func (r *blockingWorkerRepository) ClaimDueDeliveries(context.Context, int, time.Time) ([]ClaimedDelivery, error) {
	return nil, nil
}
func (r *blockingWorkerRepository) RecordSuccess(context.Context, int64) error                  { return nil }
func (r *blockingWorkerRepository) RecordRetry(context.Context, int64, time.Time, string) error { return nil }
func (r *blockingWorkerRepository) RecordExhausted(context.Context, int64, string) error        { return nil }
func (r *blockingWorkerRepository) RecordPermanent(context.Context, int64, string) error         { return nil }
func (r *blockingWorkerRepository) RecordUncertain(context.Context, int64, string) error         { return nil }

func TestWorkerWaitBlocksUntilRunReturnsAfterShutdown(t *testing.T) {
	repo := &blockingWorkerRepository{unblock: make(chan struct{})}
	worker := NewWorker(repo, nil, nil, time.Now)
	runCtx, cancelRun := context.WithCancel(context.Background())

	runReturned := make(chan struct{})
	go func() {
		worker.Run(runCtx)
		close(runReturned)
	}()

	// Run's first tick is blocked inside DiscoverActivity. A Wait bounded by
	// a short timeout must time out rather than observe Run finishing.
	earlyWaitCtx, cancelEarly := context.WithTimeout(t.Context(), 20*time.Millisecond)
	worker.Wait(earlyWaitCtx)
	cancelEarly()
	select {
	case <-runReturned:
		t.Fatal("Run returned before shutdown was requested and the first tick was released")
	default:
	}

	cancelRun()
	close(repo.unblock)

	waitCtx, cancelWait := context.WithTimeout(t.Context(), time.Second)
	defer cancelWait()
	worker.Wait(waitCtx)
	select {
	case <-runReturned:
	default:
		t.Fatal("Wait returned before Run had actually finished")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/notification/... -run TestWorkerWait -v`
Expected: FAIL to compile — `blockingWorkerRepository` does not satisfy `WorkerRepository` if `Worker`/`NewWorker` do not yet exist. (They already do from Task 5; if this task is executed independently, expect it to instead PASS immediately, in which case skip to Step 4 without changes — there is nothing left to implement.)

- [ ] **Step 3: No implementation change needed**

`Worker`/`NewWorker`/`Run`/`Wait` were implemented in Task 5. This task only adds coverage for their shutdown-ordering contract.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/notification/... -run TestWorkerWait -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/worker_test.go
git commit -m "test(notification): cover worker shutdown ordering without a real database"
```

---

### Task 8: Composition root — startup ordering and worker lifecycle

**Files:**
- Create: `internal/app/notification.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `notificationpostgres.NewStore`, `notification.NewWorker`, `notification.NewService`.
- Produces: the running application's notification worker, wired exactly like `internal/app/igdb.go`'s `appBoundIGDBSync`.

- [ ] **Step 1: Add the composition helper**

Create `internal/app/notification.go`:

```go
package app

import (
	"log/slog"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

// newNotificationWorker ties the worker to the caller's root context by
// convention (the caller starts it with `go worker.Run(ctx)` using the
// application's own ctx), matching appBoundIGDBSync's rationale: a
// background worker must keep running, and be cancellable only by
// application shutdown, independent of any single request.
//
// No transports are registered yet: E1 has none. E2/E3 append their
// transports to this slice as they land.
func newNotificationWorker(store *notificationpostgres.Store, logger *slog.Logger) *notification.Worker {
	return notification.NewWorker(store, nil, logger, time.Now)
}
```

- [ ] **Step 2: Wire baseline initialization, the worker, and shutdown ordering into Run**

Edit `internal/app/app.go`. First, add the import:

```go
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
```

(alongside the existing `gamerequestpostgres` import, keeping the import block alphabetically grouped as it already is).

Then, immediately after the existing migration block:

```go
	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = postgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}
```

insert:

```go
	notificationStore := notificationpostgres.NewStore(pool)
	if err := notificationStore.InitializeBaseline(ctx); err != nil {
		return fmt.Errorf("initialize notification baseline: %w", err)
	}
```

This runs before any other service is constructed, satisfying "migrations, then baseline, then everything else, including the HTTP server."

Next, alongside the other service construction (near `gameRequestQueries := gamerequestquery.NewStore(pool)`), add:

```go
	notificationWorker := newNotificationWorker(notificationStore, logger)
```

Next, alongside the existing `go runDailyIGDBSync(ctx, igdbSyncService, igdbSyncInterval)` line, add:

```go
	go notificationWorker.Run(ctx)
```

Finally, immediately after the existing igdb shutdown-ordering comment and `defer func() { ... igdbSyncService.Wait(waitCtx) ... }()` block, add a second one for the notification worker, so it also finishes before the pool closes:

```go
	defer func() {
		waitCtx, cancelWait := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelWait()
		notificationWorker.Wait(waitCtx)
	}()
```

- [ ] **Step 3: Confirm the application builds and starts**

Run: `go build ./...`
Expected: succeeds.

Run: `task build`
Expected: succeeds (this also runs `frontend:build`, proving nothing in the frontend pipeline broke).

- [ ] **Step 4: Smoke-test startup ordering manually**

Run: `task dev` (or, if Docker Compose is unavailable in the execution environment, `LANSURFER_DATABASE_URL=<a running local PostgreSQL> LANSURFER_BASE_URL=http://localhost:8080 go run ./cmd/lansurfer` against any reachable PostgreSQL instance).

Expected: the server logs migrations, then starts listening, with no error about the notification baseline. Stop it with Ctrl-C and confirm it shuts down cleanly (no panic, no "use of closed pool" error in the logs) — this is the manual proof that `Worker.Wait` is correctly ordered before `pool.Close()`. If Compose/PostgreSQL is not reachable in this environment, skip this step and rely on Task 11's `task test:integration` run plus code review of the ordering instead; say so explicitly in the final summary.

- [ ] **Step 5: Commit**

```bash
git add internal/app/notification.go internal/app/app.go
git commit -m "feat(notification): wire baseline initialization and the worker into the composition root"
```

---

### Task 9: Admin CLI diagnostics and manual retry

**Files:**
- Modify: `internal/app/notification.go` (add `ListNotificationProblems`, `RetryNotificationDelivery`, `RetryUncertainNotificationDelivery`)
- Modify: `cmd/lansurfer/main.go` (add `admin notifications problems|retry|retry-uncertain`)
- Modify: `cmd/lansurfer/main_test.go` (add flag-parsing test)

**Interfaces:**
- Consumes: `notification.NewService`, `notificationpostgres.NewStore`, `platformpostgres.Connect`/`Migrate`, `config.Config`.
- Produces: three CLI-callable functions and one flag-parsing helper `notificationDeliveryID`.

See "Decisions made in this plan" item 1 above for why this is a CLI surface rather than an HTTP `/admin/notifications` page.

- [ ] **Step 1: Write the failing CLI flag-parsing test**

Append to `cmd/lansurfer/main_test.go`:

```go
func TestNotificationDeliveryID(t *testing.T) {
	deliveryID, err := notificationDeliveryID([]string{"--delivery-id", "42"})
	if err != nil || deliveryID != "42" {
		t.Fatalf("notificationDeliveryID() = %q, %v", deliveryID, err)
	}

	for _, arguments := range [][]string{nil, {"--delivery-id", ""}, {"unexpected"}} {
		if _, err := notificationDeliveryID(arguments); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("notificationDeliveryID(%q) error = %v, want usage error", arguments, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/lansurfer/... -run TestNotificationDeliveryID -v`
Expected: FAIL — `notificationDeliveryID` undefined.

- [ ] **Step 3: Implement the app-layer CLI functions**

Append to `internal/app/notification.go`:

```go
import (
	"context"
	"fmt"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
)
```

(merge with the existing import block from Task 8, keeping one `import (...)` group)

```go
// ListNotificationProblems opens its own standalone database connection and
// reports every exhausted, permanent, or uncertain delivery for operator
// diagnosis, following the same one-shot pattern as ListUsers.
func ListNotificationProblems(ctx context.Context, cfg config.Config, logger *slog.Logger) ([]notification.ProblemDelivery, error) {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}

	service := notification.NewService(notificationpostgres.NewStore(pool), nil)
	problems, err := service.ListProblems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notification problems: %w", err)
	}
	return problems, nil
}

// RetryNotificationDelivery reopens one exhausted or permanent delivery for
// a fresh automatic retry budget, after re-checking present-day eligibility.
func RetryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, false)
}

// RetryUncertainNotificationDelivery reopens one uncertain delivery despite
// the possibility of a duplicate externally visible effect. It is a
// distinct action from RetryNotificationDelivery.
func RetryUncertainNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, true)
}

func retryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger, uncertain bool) error {
	id, err := strconv.ParseInt(deliveryID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse delivery ID: %w", err)
	}

	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	service := notification.NewService(notificationpostgres.NewStore(pool), nil)
	if uncertain {
		return service.ManualRetryUncertain(ctx, id)
	}
	return service.ManualRetry(ctx, id)
}
```

- [ ] **Step 4: Implement the CLI subcommands**

Edit `cmd/lansurfer/main.go`. Add, after the existing `admin promote` block and before `return usageError()`:

```go
	if len(arguments) == 3 && arguments[0] == "admin" && arguments[1] == "notifications" && arguments[2] == "problems" {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		problems, err := app.ListNotificationProblems(ctx, cfg, logger)
		if err != nil {
			return err
		}
		if len(problems) == 0 {
			_, err = fmt.Fprintln(output, "No exhausted, permanent, or uncertain notification deliveries.")
			return err
		}
		if _, err := fmt.Fprintln(output, "DELIVERY ID\tSTATUS\tTRANSPORT\tGAME REQUEST\tLAST ERROR"); err != nil {
			return err
		}
		for _, problem := range problems {
			if _, err := fmt.Fprintf(output, "%d\t%s\t%s\t%s\t%s\n",
				problem.DeliveryID, problem.Status, problem.Target.Transport, problem.Activity.GameRequestID, problem.LastError,
			); err != nil {
				return err
			}
		}
		return nil
	}

	if len(arguments) >= 3 && arguments[0] == "admin" && arguments[1] == "notifications" &&
		(arguments[2] == "retry" || arguments[2] == "retry-uncertain") {
		deliveryID, err := notificationDeliveryID(arguments[3:])
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		if arguments[2] == "retry-uncertain" {
			err = app.RetryUncertainNotificationDelivery(ctx, cfg, deliveryID, logger)
		} else {
			err = app.RetryNotificationDelivery(ctx, cfg, deliveryID, logger)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Reopened notification delivery %s for retry.\n", deliveryID)
		return err
	}
```

Add the flag-parsing helper, near `promoteUserID`:

```go
func notificationDeliveryID(arguments []string) (string, error) {
	flags := flag.NewFlagSet("admin notifications retry", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	deliveryID := flags.String("delivery-id", "", "notification delivery ID")
	if err := flags.Parse(arguments); err != nil {
		return "", usageError()
	}
	if *deliveryID == "" || flags.NArg() != 0 {
		return "", usageError()
	}
	return *deliveryID, nil
}
```

Update `usageError`:

```go
func usageError() error {
	return errors.New("usage: lansurfer [admin users | admin promote --user-id <UUID> | " +
		"admin notifications problems | admin notifications retry --delivery-id <ID> | " +
		"admin notifications retry-uncertain --delivery-id <ID>]")
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/lansurfer/... -v` and `go build ./...`
Expected: PASS / succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/app/notification.go cmd/lansurfer/main.go cmd/lansurfer/main_test.go
git commit -m "feat(notification): add admin CLI diagnostics and manual retry commands"
```

---

### Task 10: Canonical documentation reconciliation

**Files:**
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/ROADMAP.md`
- Modify: `docs/superpowers/README.md`
- Modify: `docs/superpowers/notifications/2026-09-20-track-e-notification-foundation-design.md`

Do not modify `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md` — E2 remains open and undocumented in canonical docs.

- [ ] **Step 1: Update ARCHITECTURE.md's `gamerequest` section**

In `docs/ARCHITECTURE.md`, find the paragraph beginning "One table, `game_request_activity`, currently serves both of those responsibilities... This remains provisional until Track E consumes it..." (§7, under `gamerequest`). Replace the final sentence ("This remains provisional until Track E consumes it; if delivery turns out to need state that does not belong to `gamerequest`, splitting the notification record out is the expected change rather than a reversal.") with:

"Track E1's implementation confirmed this design: the `notification` module consumes this table asynchronously and keeps its own `notification_work`/`notification_delivery` state, with no second write into `gamerequest`'s tables or transactions. Splitting the notification record out remains the expected change only if a future track demonstrates delivery-specific state that does not belong here."

- [ ] **Step 2: Update ARCHITECTURE.md's `notification` section**

Find the `### \`notification\` (Track E)` section (§7). Replace its body with:

```markdown
Owns:

- `notification_work`: one durable row per consumed live `game_request_activity` row, snapshotting the fields (`game_request_id`, `actor_id`, `kind`, `occurred_at`) delivery processing and diagnostics need, so neither has to join back into `gamerequest`'s tables afterward.
- `notification_delivery`: zero or more rows per work item, one per external target that was eligible for that activity at materialization time. Owns delivery status (`pending`, `claimed`, `succeeded`, `permanent`, `exhausted`, `uncertain`), attempts, retry scheduling, and (in later tracks) transport-specific external references.
- Web Push subscriptions and audience preferences, and the single instance-level Discord destination (Tracks E2/E3, not yet implemented).

The notification module consumes durable Game Request outbox work asynchronously; it does not own or write Game Requests, and Track A never writes into `notification`'s tables. Discovery uses a `NOT EXISTS` anti-join against `game_request_activity.sequence` plus a unique constraint on `notification_work.activity_sequence`, not a high-watermark cursor, so a row committed late is never silently skipped. A one-time, idempotent baseline pass materializes all pre-existing `origin=live` activity as never-deliverable "baseline" work before the worker starts; `origin=backfill` rows are excluded from discovery entirely. Delivery claiming uses `FOR UPDATE SKIP LOCKED` with a 60-second lease so a crashed worker's claim becomes eligible again automatically, with no in-process mutex and no additional required runtime service.

E1 (this durable foundation) is implemented. E2 (Web Push) and E3 (Discord delivery) are not yet implemented; see `docs/ROADMAP.md`.
```

A module must not directly update another module's tables merely because they are in the same database. (keep this closing sentence as-is)

- [ ] **Step 3: Update ROADMAP.md's Track E status**

In `docs/ROADMAP.md`, in the tracks table, change the Track E row's status cell from:

`**Prioritized** — not implemented on \`main\``

to:

`**In progress** — E1 (durable foundation) implemented; E2 (Web Push) and E3 (Discord) not yet implemented`

In the "Planning status and delivery evidence" section, replace the bullet:

`- **Track E — Planned.** No \`notification\` module exists on \`main\`; durable Web Push/Discord delivery and delivery persistence remain outstanding.`

with:

`- **Track E — In progress.** The \`notification\` module's durable foundation (E1) is implemented: asynchronous consumption of Track A's \`game_request_activity\` handoff, two-level \`notification_work\`/\`notification_delivery\` persistence, PostgreSQL-backed claiming with crash-safe leases, capped exponential backoff with jitter, and authorized diagnostics/manual retry, proven with a fake transport. Personal Web Push (E2) and Discord delivery (E3) remain outstanding and unimplemented; no transport-specific behavior or UI exists yet.`

- [ ] **Step 4: Update docs/superpowers/README.md**

Add a new section after "## IGDB" and before any following section:

```markdown
## Notifications

Track E1 (the durable notification foundation) is implemented. Its retained historical record is:

1. `notifications/2026-09-20-track-e-notification-foundation-design.md` — design rationale for the two-level work/delivery persistence model, baseline initialization, claiming/lease recovery, and retry/backoff policy.
2. `notifications/2026-09-20-track-e1-notification-foundation-plan.md` — historical execution plan for E1.

Track E2 (Web Push) remains open design work; `notifications/2026-09-20-track-e-web-push-design.md` is still an active draft, not a historical record, and is unaffected by E1's implementation.
```

- [ ] **Step 5: Mark the E1 design doc historical**

In `docs/superpowers/notifications/2026-09-20-track-e-notification-foundation-design.md`, change line 3 from:

`Status: Draft — interactive design in progress`

to:

`Status: Historical — E1 implemented; see docs/ARCHITECTURE.md §7 (notification) for current architecture`

- [ ] **Step 6: Note GitHub issue #28 as a manual follow-up, not an automated step**

Do not edit issue #28 as part of this plan (editing a shared GitHub issue is a visible action on shared state and outside what this plan should do automatically). In the final summary delivered at the end of Task 11, tell the user that issue #28's "Notification architecture" checklist items now reflect implemented behavior and can be checked off, but leave that edit to them or to an explicit follow-up request.

- [ ] **Step 7: Commit**

```bash
git add docs/ARCHITECTURE.md docs/ROADMAP.md docs/superpowers/README.md \
        docs/superpowers/notifications/2026-09-20-track-e-notification-foundation-design.md
git commit -m "docs(notification): reconcile E1 into canonical architecture and roadmap"
```

---

### Task 11: Full verification pass

**Files:** none (verification only).

- [ ] **Step 1: Regenerate everything**

Run: `task generate`
Expected: succeeds with no diff-worthy surprises in `internal/notification/postgres/sqlcgen`.

- [ ] **Step 2: Run unit tests with the race detector**

Run: `task test`
Expected: PASS, including every test added in Tasks 2, 5 (`worker_test.go`... note: `internal/notification` unit tests run here too), 6, and 7, and `cmd/lansurfer`'s new test.

- [ ] **Step 3: Run PostgreSQL integration tests**

Run: `task test:integration`
Expected: PASS, including every test added to `internal/platform/postgres/notification_integration_test.go` across Tasks 3–6, alongside the full pre-existing integration suite (this also re-proves Track A/B/C/D integration tests still pass, i.e. nothing in this change broke an existing invariant).

- [ ] **Step 4: Lint**

Run: `task lint`
Expected: succeeds (formatting, `go vet`/module tidiness, and TypeScript — this change touches no TypeScript, so that portion should be a no-op).

- [ ] **Step 5: Build**

Run: `task build`
Expected: succeeds and produces `bin/lansurfer`.

- [ ] **Step 6: Tear down the test database**

Run: `docker compose -p lansurfer-test -f compose.test.yaml down --volumes --remove-orphans`

(If Task 11 Step 3 was run via `task test:integration` directly, this already happened automatically via its `defer` cleanup command; only run this manually if Steps were run piecemeal with the compose stack left up from earlier tasks.)

- [ ] **Step 7: Final summary**

Report to the user:
- What was implemented (E1: schema, domain package, Postgres store, worker, service, composition-root wiring, admin CLI).
- That E2 (Web Push) and E3 (Discord) remain fully unimplemented and untouched, including the E2 SDD document.
- The two scope decisions made in this plan (CLI diagnostics instead of an HTTP admin page; three narrow repository interfaces instead of one) and that they are open to revision.
- Verification performed (Steps 1–5 above) and any step that could not be run in this environment (for example, Task 8 Step 4's manual `task dev` smoke test, if Docker/Compose was unavailable).
- That issue #28's checklist can now be partially checked off, left for the user to do.
