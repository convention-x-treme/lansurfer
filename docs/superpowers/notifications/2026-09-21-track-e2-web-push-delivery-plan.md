# Track E2 Delivery — Web Push Send Implementation Plan
> **Status:** Historical — E2 delivery implementation completed; remaining execution superseded by `2026-09-24-track-e2-remaining-web-push-plan.md`. Retained as implementation history.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Track E1's `Worker` actually send real Web Push notifications, by implementing `notification.Transport` (`Targets`/`Send`) against the subscriptions/preferences Track E2 Foundation persisted, using the approved `github.com/marknefedov/go-webpush/v2` adapter. This plan does not touch the browser: no service-worker `push`/`notificationclick` handler, no enable-flow JavaScript. That is a separate, later Client plan.

**Architecture:** A new `WebPushTransport` in package `notification` implements `Transport`. `Targets` resolves the eligible recipients for one activity (game followers for `created`, current participants for lifecycle kinds, both filtered by `web_push_enabled`) into one `Target` per eligible user's active subscription, using two new narrow cross-module read additions (`game.ListFollowers`, a small `query/gamerequest` summary query) and two new `notification`-owned Postgres reads. `Send` decrypts one subscription's material with the existing `internal/platform/crypto`, builds the versioned minimal payload, and calls `go-webpush/v2`'s `Client.Send`, mapping its `PushServiceError` fields to E1's outcome vocabulary. The composition root registers `WebPushTransport` in `notificationTransports()` only when `cfg.WebPush` is configured.

**Tech Stack:** Go 1.27, `github.com/marknefedov/go-webpush/v2` (new direct dependency, pre-approved by the settled design), everything else already in the repo.

**Spec:** `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md` (Approved), specifically: "Created recipient materialization," "Lifecycle recipient materialization," "Eligibility re-check," "Provider result mapping," "Payload contract," "Notification copy," "Recipient set semantics," "Suppression." `docs/superpowers/notifications/2026-09-21-track-e2-web-push-foundation-plan.md` for what already exists (`SubscriptionRepository`, `PreferenceRepository`, `SubscriptionService`, the `notification_push_subscription`/`notification_push_preference` schema). E1's `Worker.processOne` already re-checks eligibility via `Transport.Targets` before every send and marks `suppressed` on ineligibility — this plan's `Targets` implementation is what that mechanism calls.

## Global Constraints

- No browser/service-worker/JS work. No new HTTP routes. This plan is server-side send only.
- `Targets` must return one `Target{Transport: "webpush", Key: "<subscription id>"}` per eligible user's *active* subscription (a user with 3 devices gets 3 Targets) — never per user.
- Actor exclusion is universal: the activity's actor never receives a personal notification for their own activity, for every kind.
- `created`: eligible = (users following the Game, `web_push_enabled=true`) ∪ (users with `web_push_enabled=true` AND `new_request_audience="all"`), minus the actor, deduplicated by user.
- `joined`/`left`/`closed`/`cancelled`: eligible = current Game Request participants (resolved fresh, at `Targets`-call time — this is what makes a delayed/retried send only ever *suppress*, never add, a recipient) with `web_push_enabled=true`, minus the actor. The `new_request_audience` preference does not filter these.
- Payload is the versioned minimal contract: schema version, activity kind, Game name, planned start time, request deep link, minimum instance context, and the actor's public Game Nick only for `joined`/`left`. Never include description, roster, provider identity, or activity history.
- Notification copy is exactly (see design doc): `created` → "New \<Game\> Game Request"; `joined` → "\<Game Nick\> joined \<Game\>"; `left` → "\<Game Nick\> left \<Game\>"; `closed` → "\<Game\> Game Request closed"; `cancelled` → "\<Game\> Game Request cancelled".
- Provider result mapping: success → `OutcomeSucceeded`. Expired/invalid subscription → `OutcomePermanent`, and best-effort delete that subscription row. HTTP 429, temporary/5xx, and network/context errors → `OutcomeRetryable` (honor `Retry-After` when the library surfaces it). Other non-expiry 4xx → `OutcomePermanent`, but do **not** delete the subscription row solely because of the status class. `OutcomeUncertain` is not produced by this transport for any case in the settled mapping — it stays reserved vocabulary; do not invent a case for it.
- Push TTL is capped at 5 minutes (300 seconds) on every send, per the settled design, regardless of E1's own retry-backoff timing.
- `Store` (existing, from Foundation) still never sees plaintext; only `WebPushTransport` (via `crypto.Decrypt`) ever decrypts subscription material, and only at `Send` time, never at `Targets` time.
- Do not add a generic provider registry, plugin framework, or transport-neutral abstraction beyond the single `Transport` interface E1 already defines.
- Follow existing conventions: sqlc for all SQL, narrow interfaces defined in the consuming package (`notification`) with adapters wired in the composition root, `internal/platform/crypto` reused as-is (no changes).

## Decisions made in this plan

1. **`RequestParticipants`/`GameFollowers`/`GameRequestSummaryReader` are `notification`-owned interfaces with their own minimal return types** (`Participant{UserID string}`, `GameRequestSummary{GameID, GameName string, StartsAt time.Time}`), not interfaces returning `game`'s or `query/gamerequest`'s generated/exported row types. Composition-root adapters (`gameFollowersAdapter`, `requestSummaryAdapter`) map the real read-model calls into these shapes. This keeps `notification`'s interfaces self-contained, matching the existing `SubscriptionRepository`/`PreferenceRepository` style, at the cost of one small adapter type per dependency.
2. **`game.ListFollowers` is a new method on the existing `game` module** (query + store + a passthrough on `game.Service` or `game.DiscoveryService`, whichever already owns `game_subscriptions` reads — Task 1 determines which by reading the existing code first), not a new module or a generic reverse-lookup abstraction.
3. **`query/gamerequest` gets one new narrow read query**, `GetGameRequestSummary`, deliberately separate from the existing `GetGameRequest` (which requires a `viewer_id` for personalization this system-level caller has none of). `ListGameRequestMembers` (already exists) is reused as-is for participants, mapped down to `Participant` in the adapter.
4. **`go-webpush/v2`'s `Client` is constructed once in the composition root** (`internal/app/notification.go`) and injected into `WebPushTransport`, using `Config.HTTPClient` left as the library's default in production. Tests inject a fake `HTTPClient` (the library's own interface, one method: `Do(*http.Request) (*http.Response, error)`) to simulate provider responses without any real network call.

---

### Task 1: `game` module — list followers of a Game

**Files:**
- Modify: `internal/game/postgres/queries/games.sql`
- Modify: `internal/game/postgres/store.go` (or wherever the generated method needs a thin passthrough — read it first)
- Modify: `internal/game/discovery.go` (or `game/game.go` — read both first; add the method to whichever existing type already owns `game_subscriptions` reads, e.g. `DiscoveryService` or `Service`)
- Test: append to the appropriate existing `internal/game/*_test.go`, and an integration test appended to `internal/platform/postgres/postgres_integration_test.go` (this repo centralizes cross-module PostgreSQL integration tests there)

**Interfaces:**
- Produces: a method returning `([]string, error)` (user IDs following one Game) — read the existing `game` package first to name and place it consistently (likely `ListFollowers(ctx context.Context, gameID string) ([]string, error)` on whichever type already exposes `Follow`/`FollowLocal`/`ListSubscribedGames`). Consumed by Task 4's `gameFollowersAdapter`.

- [ ] **Step 1: Read the existing code first**

Read `internal/game/postgres/queries/games.sql` (in particular `ListSubscribedGames`, `FollowGame`, `UnfollowGame`), `internal/game/discovery.go`, and `internal/game/game.go` in full. Determine which existing type (`DiscoveryService`, `Service`, or a dedicated subscription type) already owns reads over `game_subscriptions`, and add the new method there — do not create a new type for one method.

- [ ] **Step 2: Write the failing test**

Add a query test. Follow this repo's existing pattern exactly (find and read a comparable existing test in `internal/game/*_test.go` or the integration suite first, then write in that style): register two users, create one Game, follow it with one user only, call the new method, assert it returns exactly that one user's ID and not the non-follower's.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/game/... ./internal/platform/postgres/... -run TestGameListFollowers -v` (adjust the test name to whatever you actually wrote)
Expected: FAIL — method undefined.

- [ ] **Step 4: Implement**

Add to `internal/game/postgres/queries/games.sql`:

```sql
-- name: ListGameFollowers :many
SELECT user_id::text FROM game_subscriptions WHERE game_id = sqlc.arg(game_id)::uuid;
```

Run `task generate`, then add the store passthrough and the service-level method, following the exact style of the neighboring `FollowGame`/`ListSubscribedGames` methods in the same files.

- [ ] **Step 5: Run the test to verify it passes**

Run the same command as Step 3.
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game/postgres/queries/games.sql internal/game/postgres/sqlcgen internal/game/discovery.go internal/platform/postgres/postgres_integration_test.go
git commit -m "feat(game): add ListFollowers for Track E2 recipient materialization"
```

---

### Task 2: `query/gamerequest` — system-level Game Request summary read

**Files:**
- Modify: `internal/query/gamerequest/queries/game_requests.sql`
- Modify: `internal/query/gamerequest/store.go` (read it first for the exact style)
- Test: append to `internal/platform/postgres/postgres_integration_test.go`

**Interfaces:**
- Produces: `GetGameRequestSummary(ctx, gameRequestID string) (Summary, error)` (name the returned type/fields whatever reads naturally in this package's existing style, but it must carry at minimum: `GameID`, `GameName`, `StartsAt`) — consumed by Task 4's `requestSummaryAdapter`.

- [ ] **Step 1: Read the existing code first**

Read `internal/query/gamerequest/queries/game_requests.sql` and `internal/query/gamerequest/store.go` in full — match the existing `GetGameRequest`/`ListActiveGameRequests` query and Go style exactly, including how `sqlc.arg`/`::text`/`::uuid` casts are used in this package.

- [ ] **Step 2: Write the failing test**

Append an integration test to `internal/platform/postgres/postgres_integration_test.go` (follow the file's existing fixture-building conventions, e.g. register a user, create a Game, create a Game Request) that calls the new method and asserts the returned `GameID`/`GameName`/`StartsAt` match what was created — and that it does NOT require or accept a viewer ID (unlike `GetGameRequest`).

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/platform/postgres/... -run TestGetGameRequestSummary -v` (adjust to your actual test name)
Expected: FAIL — undefined.

- [ ] **Step 4: Implement**

Add to `internal/query/gamerequest/queries/game_requests.sql`:

```sql
-- name: GetGameRequestSummary :one
SELECT gr.id::text AS request_id, g.id::text AS game_id, g.name AS game_name,
       gr.starts_at, gr.status
FROM game_requests gr
JOIN games g ON g.id = gr.game_id
WHERE gr.id = sqlc.arg(id)::uuid;
```

Run `task generate`, then add the store method wrapping this query, mapping to a small exported struct in this package (e.g. `type Summary struct { GameID, GameName string; StartsAt time.Time; Status string }` — match this package's existing naming; if a `Summary`-shaped type already exists for something else, pick a name that doesn't collide, e.g. `RequestSummary`).

- [ ] **Step 5: Run the test to verify it passes**

Run the same command as Step 3.
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/query/gamerequest/queries/game_requests.sql internal/query/gamerequest/sqlcgen internal/query/gamerequest/store.go internal/platform/postgres/postgres_integration_test.go
git commit -m "feat(query/gamerequest): add a viewer-independent Game Request summary read for Track E2 delivery"
```

---

### Task 3: `notification` Postgres store — subscription-by-ID read and audience-wide preference read

**Files:**
- Modify: `internal/notification/postgres/queries/push_subscriptions.sql`
- Modify: `internal/notification/postgres/store.go`
- Modify: `internal/notification/notification.go` (two new interface methods, or a new small interface — see Step 3)
- Test: append to `internal/platform/postgres/notification_push_subscription_integration_test.go`

**Interfaces:**
- Produces: `GetSubscriptionByID(ctx, id int64) (PushSubscriptionRecord, error)` (the full record, ciphertext included — unlike `ListSubscriptions`) and `ListUsersWithAudience(ctx, audience string) ([]string, error)`, both added to a new `DeliveryReadRepository` interface in `notification.go`. Consumed by Task 5's `WebPushTransport`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/postgres/notification_push_subscription_integration_test.go` (reuse the existing `newTestPool`/`testLogger` helpers already in that package):

```go
func TestGetSubscriptionByIDReturnsFullCiphertextRecord(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Subscription Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	created, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "This device", DeviceIDHash: "device-hash-x",
		EndpointFingerprint: "fingerprint-x", EndpointCiphertext: []byte("endpoint-cipher"),
		P256dhCiphertext: []byte("p256dh-cipher"), AuthCiphertext: []byte("auth-cipher"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() error = %v", err)
	}

	fetched, err := store.GetSubscriptionByID(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetSubscriptionByID() error = %v", err)
	}
	if string(fetched.EndpointCiphertext) != "endpoint-cipher" || string(fetched.P256dhCiphertext) != "p256dh-cipher" || string(fetched.AuthCiphertext) != "auth-cipher" {
		t.Fatalf("GetSubscriptionByID() = %#v, want the stored ciphertext back", fetched)
	}
	if fetched.UserID != owner.ID {
		t.Fatalf("GetSubscriptionByID() UserID = %q, want %q", fetched.UserID, owner.ID)
	}

	if _, err := store.GetSubscriptionByID(t.Context(), created.ID+9999); !errors.Is(err, notification.ErrDeliveryNotFound) {
		t.Fatalf("GetSubscriptionByID() for a missing ID error = %v, want %v", err, notification.ErrDeliveryNotFound)
	}
}

func TestListUsersWithAudienceReturnsOnlyMatchingEnabledUsers(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	allAudienceEnabled, err := users.Register(t.Context(), "All Audience Enabled")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	allAudienceDisabled, err := users.Register(t.Context(), "All Audience Disabled")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	followedOnly, err := users.Register(t.Context(), "Followed Only")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	if err := store.SetPreference(t.Context(), allAudienceEnabled.ID, notification.PushPreference{WebPushEnabled: true, NewRequestAudience: notification.AudienceAll}); err != nil {
		t.Fatalf("SetPreference() error = %v", err)
	}
	if err := store.SetPreference(t.Context(), allAudienceDisabled.ID, notification.PushPreference{WebPushEnabled: false, NewRequestAudience: notification.AudienceAll}); err != nil {
		t.Fatalf("SetPreference() error = %v", err)
	}
	if err := store.SetPreference(t.Context(), followedOnly.ID, notification.PushPreference{WebPushEnabled: true, NewRequestAudience: notification.AudienceFollowed}); err != nil {
		t.Fatalf("SetPreference() error = %v", err)
	}

	userIDs, err := store.ListUsersWithAudience(t.Context(), notification.AudienceAll)
	if err != nil {
		t.Fatalf("ListUsersWithAudience() error = %v", err)
	}
	if len(userIDs) != 1 || userIDs[0] != allAudienceEnabled.ID {
		t.Fatalf("ListUsersWithAudience(all) = %#v, want exactly [%s] (enabled-and-matching only)", userIDs, allAudienceEnabled.ID)
	}
}
```

Reuse `errors` (already imported for other tests in this file's package) and reuse `notification.ErrDeliveryNotFound` (already defined by Track E1 — a delivery/subscription lookup miss reuses this existing sentinel rather than adding a near-duplicate one).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run 'TestGetSubscriptionByID|TestListUsersWithAudience' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Add the queries and domain interface**

Add to `internal/notification/postgres/queries/push_subscriptions.sql`:

```sql
-- name: GetPushSubscriptionByID :one
SELECT id, user_id::text AS user_id, label, device_id_hash, endpoint_fingerprint,
       endpoint_ciphertext, p256dh_ciphertext, auth_ciphertext, created_at, updated_at
FROM notification_push_subscription
WHERE id = sqlc.arg(id);

-- name: ListUsersWithAudience :many
SELECT user_id::text FROM notification_push_preference
WHERE web_push_enabled = true AND new_request_audience = sqlc.arg(new_request_audience);
```

Run `task generate`, then add to `internal/notification/notification.go`:

```go
// DeliveryReadRepository is the durable state a Transport needs to resolve
// full subscription material for one send, and to find users who opted
// into the all-new-requests audience regardless of following.
type DeliveryReadRepository interface {
	GetSubscriptionByID(ctx context.Context, id int64) (PushSubscriptionRecord, error)
	ListUsersWithAudience(ctx context.Context, audience string) ([]string, error)
}
```

Add to `internal/notification/postgres/store.go`:

```go
func (s *Store) GetSubscriptionByID(ctx context.Context, id int64) (notification.PushSubscriptionRecord, error) {
	row, err := s.queries.GetPushSubscriptionByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.PushSubscriptionRecord{}, notification.ErrDeliveryNotFound
	}
	if err != nil {
		return notification.PushSubscriptionRecord{}, fmt.Errorf("get push subscription: %w", err)
	}
	return notification.PushSubscriptionRecord{
		ID: row.ID, UserID: row.UserID, Label: row.Label, DeviceIDHash: row.DeviceIDHash,
		EndpointFingerprint: row.EndpointFingerprint, EndpointCiphertext: row.EndpointCiphertext,
		P256dhCiphertext: row.P256dhCiphertext, AuthCiphertext: row.AuthCiphertext,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func (s *Store) ListUsersWithAudience(ctx context.Context, audience string) ([]string, error) {
	userIDs, err := s.queries.ListUsersWithAudience(ctx, audience)
	if err != nil {
		return nil, fmt.Errorf("list users with audience: %w", err)
	}
	return userIDs, nil
}
```

Adjust field/method names to whatever `task generate` actually produced if they differ.

- [ ] **Step 4: Run the tests to verify they pass**

Run the same command as Step 2, then run the full existing notification integration suite (`-run 'TestNotification|TestPushSubscription|TestPushPreference|TestGetSubscriptionByID|TestListUsersWithAudience'`) to confirm nothing regressed.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/postgres/queries/push_subscriptions.sql internal/notification/postgres/sqlcgen internal/notification/notification.go internal/notification/postgres/store.go internal/platform/postgres/notification_push_subscription_integration_test.go
git commit -m "feat(notification): add subscription-by-ID and audience-wide reads for delivery"
```

---

### Task 4: `WebPushTransport.Targets` — recipient materialization and eligibility

**Files:**
- Create: `internal/notification/webpush.go`
- Create: `internal/notification/webpush_test.go`
- Modify: `internal/app/notification.go` (the two adapter types — see Global Constraints Decision 1)

**Interfaces:**
- Consumes: `SubscriptionRepository.ListSubscriptions`, `PreferenceRepository.GetPreference`, `DeliveryReadRepository.ListUsersWithAudience` (Task 3), plus two new interfaces this task defines and Task 1/2's real implementations adapt into.
- Produces: `WebPushTransport{}`, `NewWebPushTransport(...) *WebPushTransport`, `(*WebPushTransport).Name() string` (`"webpush"`), `(*WebPushTransport).Targets(ctx, Activity) ([]Target, error)` — consumed by Task 5's `Send` (same type) and Task 6's composition-root wiring.

- [ ] **Step 1: Write the failing tests**

Create `internal/notification/webpush_test.go`:

```go
package notification

import (
	"context"
	"testing"
	"time"
)

type fakeGameFollowers struct{ followers map[string][]string }

func (f *fakeGameFollowers) ListFollowers(ctx context.Context, gameID string) ([]string, error) {
	return f.followers[gameID], nil
}

type fakeRequestParticipants struct{ participants map[string][]string }

func (f *fakeRequestParticipants) ListParticipants(ctx context.Context, gameRequestID string) ([]string, error) {
	return f.participants[gameRequestID], nil
}

type fakeGameRequestSummaryReader struct{ summaries map[string]GameRequestSummary }

func (f *fakeGameRequestSummaryReader) GetGameRequestSummary(ctx context.Context, gameRequestID string) (GameRequestSummary, error) {
	return f.summaries[gameRequestID], nil
}

type fakeWebPushSubscriptionSource struct {
	subsByUser map[string][]PushSubscriptionRecord
	preferences map[string]PushPreference
	audienceAll []string
}

func (f *fakeWebPushSubscriptionSource) ListSubscriptions(ctx context.Context, userID string) ([]PushSubscriptionRecord, error) {
	return f.subsByUser[userID], nil
}
func (f *fakeWebPushSubscriptionSource) GetPreference(ctx context.Context, userID string) (PushPreference, error) {
	if pref, ok := f.preferences[userID]; ok {
		return pref, nil
	}
	return PushPreference{NewRequestAudience: AudienceFollowed}, nil
}
func (f *fakeWebPushSubscriptionSource) ListUsersWithAudience(ctx context.Context, audience string) ([]string, error) {
	if audience == AudienceAll {
		return f.audienceAll, nil
	}
	return nil, nil
}

func TestWebPushTargetsForCreatedUnionsFollowersAndAllAudienceExcludingActor(t *testing.T) {
	followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {"follower-1", "actor-1"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {GameID: "game-1", GameName: "Warcraft III"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser: map[string][]PushSubscriptionRecord{
			"follower-1":       {{ID: 10}},
			"all-audience-1":   {{ID: 20}},
		},
		preferences: map[string]PushPreference{
			"follower-1": {WebPushEnabled: true, NewRequestAudience: AudienceFollowed},
			"actor-1":    {WebPushEnabled: true, NewRequestAudience: AudienceFollowed},
		},
		audienceAll: []string{"all-audience-1"},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 1, GameRequestID: "req-1", ActorID: "actor-1", Kind: ActivityCreated})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	got := map[string]bool{}
	for _, target := range targets {
		if target.Transport != "webpush" {
			t.Fatalf("target.Transport = %q, want %q", target.Transport, "webpush")
		}
		got[target.Key] = true
	}
	if len(got) != 2 || !got["10"] || !got["20"] {
		t.Fatalf("Targets() = %#v, want exactly subscription IDs 10 and 20 (follower's device + all-audience user's device, actor excluded)", targets)
	}
}

func TestWebPushTargetsForLifecycleUsesCurrentParticipantsRegardlessOfAudience(t *testing.T) {
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"participant-1", "leaving-actor"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"participant-1": {{ID: 30}}},
		preferences: map[string]PushPreference{"participant-1": {WebPushEnabled: true, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, &fakeGameFollowers{}, participants, &fakeGameRequestSummaryReader{}, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 2, GameRequestID: "req-1", ActorID: "leaving-actor", Kind: ActivityLeft})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Key != "30" {
		t.Fatalf("Targets() for left = %#v, want exactly subscription 30 (leaving actor excluded)", targets)
	}
}

func TestWebPushTargetsExcludesDisabledUsers(t *testing.T) {
	followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {"disabled-follower"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {GameID: "game-1"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"disabled-follower": {{ID: 40}}},
		preferences: map[string]PushPreference{"disabled-follower": {WebPushEnabled: false, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 3, GameRequestID: "req-1", ActorID: "someone-else", Kind: ActivityCreated})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("Targets() = %#v, want none (the only follower has web_push_enabled=false)", targets)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notification/... -run TestWebPushTargets -v`
Expected: FAIL to compile — `NewWebPushTransport` undefined.

- [ ] **Step 3: Implement the domain types and Targets**

Create `internal/notification/webpush.go`:

```go
package notification

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// GameRequestSummary is the minimal, viewer-independent Game Request context
// WebPushTransport needs: which Game (for follower resolution and payload
// copy) and when it starts (for the payload). Resolved fresh at both Targets
// and Send time, never cached across a delayed retry.
type GameRequestSummary struct {
	GameID   string
	GameName string
	StartsAt time.Time
}

// GameFollowers resolves which users follow one Game, for `created`
// recipient materialization.
type GameFollowers interface {
	ListFollowers(ctx context.Context, gameID string) ([]string, error)
}

// RequestParticipants resolves current Game Request membership, for
// lifecycle recipient materialization. It is always called fresh, so a
// delayed/retried send only ever loses recipients who have since left, never
// gains ones who joined afterward.
type RequestParticipants interface {
	ListParticipants(ctx context.Context, gameRequestID string) ([]string, error)
}

// GameRequestSummaryReader resolves GameRequestSummary for one Game Request.
type GameRequestSummaryReader interface {
	GetGameRequestSummary(ctx context.Context, gameRequestID string) (GameRequestSummary, error)
}

// WebPushTransport implements notification.Transport for personal Web Push.
// It never persists anything itself: recipient/eligibility state lives in
// SubscriptionRepository/PreferenceRepository/DeliveryReadRepository, all
// owned by Track E2 Foundation.
type WebPushTransport struct {
	subscriptions SubscriptionRepository
	preferences   PreferenceRepository
	delivery      DeliveryReadRepository
	followers     GameFollowers
	participants  RequestParticipants
	summaries     GameRequestSummaryReader
	baseURL       string
	sender        webPushSender // set in Task 5; nil is valid here, Send is not implemented yet
}

func NewWebPushTransport(
	subscriptions SubscriptionRepository, preferences PreferenceRepository, delivery DeliveryReadRepository,
	followers GameFollowers, participants RequestParticipants, summaries GameRequestSummaryReader,
	baseURL string, sender webPushSender,
) *WebPushTransport {
	return &WebPushTransport{
		subscriptions: subscriptions, preferences: preferences, delivery: delivery,
		followers: followers, participants: participants, summaries: summaries,
		baseURL: baseURL, sender: sender,
	}
}

func (t *WebPushTransport) Name() string { return "webpush" }

func (t *WebPushTransport) Targets(ctx context.Context, activity Activity) ([]Target, error) {
	var candidates []string
	switch activity.Kind {
	case ActivityCreated:
		summary, err := t.summaries.GetGameRequestSummary(ctx, activity.GameRequestID)
		if err != nil {
			return nil, fmt.Errorf("get game request summary: %w", err)
		}
		followers, err := t.followers.ListFollowers(ctx, summary.GameID)
		if err != nil {
			return nil, fmt.Errorf("list game followers: %w", err)
		}
		allAudience, err := t.delivery.ListUsersWithAudience(ctx, AudienceAll)
		if err != nil {
			return nil, fmt.Errorf("list all-audience users: %w", err)
		}
		candidates = dedupeExcept(append(followers, allAudience...), activity.ActorID)
	case ActivityJoined, ActivityLeft, ActivityClosed, ActivityCancelled:
		participants, err := t.participants.ListParticipants(ctx, activity.GameRequestID)
		if err != nil {
			return nil, fmt.Errorf("list game request participants: %w", err)
		}
		candidates = dedupeExcept(participants, activity.ActorID)
	default:
		return nil, nil
	}

	var targets []Target
	for _, userID := range candidates {
		preference, err := t.preferences.GetPreference(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("get push preference for %s: %w", userID, err)
		}
		if !preference.WebPushEnabled {
			continue
		}
		subs, err := t.subscriptions.ListSubscriptions(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list subscriptions for %s: %w", userID, err)
		}
		for _, sub := range subs {
			targets = append(targets, Target{Transport: "webpush", Key: strconv.FormatInt(sub.ID, 10)})
		}
	}
	return targets, nil
}

func dedupeExcept(userIDs []string, exclude string) []string {
	seen := make(map[string]bool, len(userIDs))
	result := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id == exclude || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}
```

`webPushSender` is a placeholder type Task 5 defines; for this task, add a minimal forward declaration so the file compiles on its own:

```go
// webPushSender is implemented in webpush_send.go (Task 5). Defined here so
// WebPushTransport can hold one before Send exists.
type webPushSender interface {
	send(ctx context.Context, endpoint, p256dh, auth string, payload []byte, ttlSeconds int) sendOutcome
}

type sendOutcome struct {
	Outcome    Outcome
	RetryAfter *time.Duration
	Err        error
	Expired    bool
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/notification/... -v`
Expected: PASS for every test in the package, old and new.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/webpush.go internal/notification/webpush_test.go
git commit -m "feat(notification): add WebPushTransport recipient materialization (Targets)"
```

---

### Task 5: `WebPushTransport.Send` — payload, notification copy, and the go-webpush/v2 adapter

**Files:**
- Create: `internal/notification/webpush_send.go`
- Create: `internal/notification/webpush_send_test.go`
- Modify: `go.mod`, `go.sum` (new dependency)

**Interfaces:**
- Consumes: `crypto.Decrypt`, `DeliveryReadRepository.GetSubscriptionByID`, `SubscriptionRepository.DeleteSubscription` (for expired-subscription cleanup), `GameRequestSummaryReader`.
- Produces: `(*WebPushTransport).Send(ctx, Activity, Target) SendResult` (completing the `Transport` interface), a concrete `webPushSender` implementation wired in Task 6.

- [ ] **Step 1: Write the failing tests**

First run: `go get github.com/marknefedov/go-webpush/v2` (this adds it to `go.mod`/`go.sum`; it will show as `// indirect` until this task's code actually imports it, which it will by the end of this step).

Create `internal/notification/webpush_send_test.go`:

```go
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	webpush "github.com/marknefedov/go-webpush/v2"
)

// fakeHTTPClient implements webpush.HTTPClient without any real network call.
type fakeHTTPClient struct {
	response *http.Response
	err      error
	lastBody []byte
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		f.lastBody, _ = io.ReadAll(req.Body)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func newTestWebPushSender(t *testing.T, statusCode int, headers http.Header) (*webPushClientSender, *fakeHTTPClient) {
	t.Helper()
	vapidKeys, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys() error = %v", err)
	}
	fake := &fakeHTTPClient{response: &http.Response{StatusCode: statusCode, Header: headers, Body: io.NopCloser(bytes.NewReader(nil))}}
	client := webpush.NewClient(webpush.Config{HTTPClient: fake})
	return &webPushClientSender{client: client, vapidKeys: vapidKeys, subject: "mailto:ops@example.test"}, fake
}

func TestWebPushSendMapsSuccessResponse(t *testing.T) {
	sender, _ := newTestWebPushSender(t, http.StatusCreated, nil)
	keys, err := webpush.DecodeSubscriptionKeys("AAAAAAAAAAAAAAAAAAAAAA==", validTestP256dhKey)
	if err != nil {
		t.Fatalf("DecodeSubscriptionKeys() error = %v", err)
	}
	_ = keys
	result := sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("send() outcome = %v, want OutcomeSucceeded (result: %#v)", result.Outcome, result)
	}
}

func TestWebPushSendMapsExpiredSubscriptionToPermanentAndExpired(t *testing.T) {
	sender, _ := newTestWebPushSender(t, http.StatusGone, nil)
	result := sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	if result.Outcome != OutcomePermanent || !result.Expired {
		t.Fatalf("send() on 410 Gone = %#v, want Outcome=Permanent, Expired=true", result)
	}
}

func TestWebPushSendMapsRateLimitToRetryableWithRetryAfter(t *testing.T) {
	headers := http.Header{"Retry-After": []string{"30"}}
	sender, _ := newTestWebPushSender(t, http.StatusTooManyRequests, headers)
	result := sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	if result.Outcome != OutcomeRetryable {
		t.Fatalf("send() on 429 outcome = %v, want OutcomeRetryable", result.Outcome)
	}
	if result.RetryAfter == nil {
		t.Fatal("send() on 429 with a Retry-After header did not surface a RetryAfter hint")
	}
}

func TestWebPushSendMapsOtherClientErrorToPermanentWithoutExpired(t *testing.T) {
	sender, _ := newTestWebPushSender(t, http.StatusBadRequest, nil)
	result := sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	if result.Outcome != OutcomePermanent || result.Expired {
		t.Fatalf("send() on 400 = %#v, want Outcome=Permanent, Expired=false (row must not be deleted solely for status class)", result)
	}
}

func TestWebPushSendMapsNetworkErrorToRetryable(t *testing.T) {
	sender, _ := newTestWebPushSender(t, 0, nil)
	sender.client = webpush.NewClient(webpush.Config{HTTPClient: &fakeHTTPClient{err: errors.New("connection refused")}})
	result := sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	if result.Outcome != OutcomeRetryable {
		t.Fatalf("send() on network error outcome = %v, want OutcomeRetryable", result.Outcome)
	}
}

func TestWebPushSendCapsTTLAtFiveMinutes(t *testing.T) {
	sender, fake := newTestWebPushSender(t, http.StatusCreated, nil)
	_ = fake
	sender.send(t.Context(), "https://push.example/endpoint", validTestP256dhKey, "AAAAAAAAAAAAAAAAAAAAAA==", []byte(`{"v":1}`), 300)
	// The library sets the TTL header itself from SendOptions.TTL; this test
	// documents the caller-side contract (300 seconds is what webPushClientSender
	// always passes to SendOptions.TTL) rather than re-deriving header parsing.
	// If webPushClientSender ever accepts a caller-supplied TTL greater than
	// 300, that is the regression this test exists to catch — implement it by
	// asserting on whatever field/method your webPushClientSender.send
	// actually threads the ttlSeconds parameter into, once written.
}

func TestPayloadForCreatedOmitsActorGameNick(t *testing.T) {
	payload := buildPushPayload(Activity{Kind: ActivityCreated, GameRequestID: "req-1"}, GameRequestSummary{GameName: "Warcraft III", StartsAt: time.Date(2026, 9, 21, 20, 0, 0, 0, time.UTC)}, "https://lansurfer.example", "")
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if _, present := decoded["actorGameNick"]; present {
		t.Fatalf("payload for created included actorGameNick, want it omitted: %s", payload)
	}
	if decoded["game"] != "Warcraft III" {
		t.Fatalf("payload game = %v, want %q", decoded["game"], "Warcraft III")
	}
}

func TestPayloadForJoinedIncludesActorGameNick(t *testing.T) {
	payload := buildPushPayload(Activity{Kind: ActivityJoined, GameRequestID: "req-1"}, GameRequestSummary{GameName: "Warcraft III"}, "https://lansurfer.example", "PlayerOne")
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if decoded["actorGameNick"] != "PlayerOne" {
		t.Fatalf("payload actorGameNick = %v, want %q", decoded["actorGameNick"], "PlayerOne")
	}
}

func TestNotificationCopyMatchesSettledWording(t *testing.T) {
	cases := []struct {
		kind ActivityKind
		want string
	}{
		{ActivityCreated, "New Warcraft III Game Request"},
		{ActivityJoined, "PlayerOne joined Warcraft III"},
		{ActivityLeft, "PlayerOne left Warcraft III"},
		{ActivityClosed, "Warcraft III Game Request closed"},
		{ActivityCancelled, "Warcraft III Game Request cancelled"},
	}
	for _, c := range cases {
		got := notificationTitle(c.kind, "Warcraft III", "PlayerOne")
		if got != c.want {
			t.Errorf("notificationTitle(%v) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// validTestP256dhKey is a real base64url-encoded uncompressed P-256 public
// key point (65 raw bytes), required for webpush.DecodeSubscriptionKeys to
// accept it. Generate one exactly as Task 1 of the Foundation plan did (a
// short throwaway ecdh.P256().GenerateKey(rand.Reader) program), hardcode
// the printed public key string here, and delete the scratch file.
const validTestP256dhKey = "<fill in with a real base64url-encoded 65-byte P-256 public key>"
```

Adjust the network-error test's status-code/fake wiring if `webpush.Client.Send`'s actual error-returning behavior for a transport-level `Do` error differs from what's assumed above — read `go doc -all github.com/marknefedov/go-webpush/v2` yourself first (specifically `Client.Send`, `PushServiceError`, `SendResult`) to confirm exact field names and error-wrapping behavior before writing `webpush_send.go`, and adjust these tests to match reality rather than forcing them to match this plan's guess.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notification/... -run 'TestWebPushSend|TestPayload|TestNotificationCopy' -v`
Expected: FAIL — `webPushClientSender`, `buildPushPayload`, `notificationTitle` undefined.

- [ ] **Step 3: Implement**

Create `internal/notification/webpush_send.go`:

```go
package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	webpush "github.com/marknefedov/go-webpush/v2"
)

const pushTTLSeconds = 300 // 5 minutes, per the settled design; never exceeded regardless of caller input.

// webPushClientSender adapts github.com/marknefedov/go-webpush/v2 to
// webPushSender. It holds no per-request state; vapidKeys/subject are fixed
// for the process lifetime (one operator-configured VAPID keypair).
type webPushClientSender struct {
	client    *webpush.Client
	vapidKeys *webpush.VAPIDKeys
	subject   string
}

func newWebPushClientSender(vapidKeys *webpush.VAPIDKeys, subject string) *webPushClientSender {
	return &webPushClientSender{client: webpush.NewClient(webpush.Config{}), vapidKeys: vapidKeys, subject: subject}
}

func (s *webPushClientSender) send(ctx context.Context, endpoint, p256dh, auth string, payload []byte, _ int) sendOutcome {
	keys, err := webpush.DecodeSubscriptionKeys(auth, p256dh)
	if err != nil {
		return sendOutcome{Outcome: OutcomePermanent, Err: fmt.Errorf("decode subscription keys: %w", err)}
	}
	sub := &webpush.Subscription{Endpoint: endpoint, Keys: keys}
	_, err = s.client.Send(ctx, payload, sub, webpush.SendOptions{
		Subject: s.subject, TTL: pushTTLSeconds, VAPIDKeys: s.vapidKeys,
	})
	if err == nil {
		return sendOutcome{Outcome: OutcomeSucceeded}
	}
	var svcErr *webpush.PushServiceError
	if errors.As(err, &svcErr) {
		if svcErr.SubscriptionExpired {
			return sendOutcome{Outcome: OutcomePermanent, Err: err, Expired: true}
		}
		if svcErr.Temporary {
			var retryAfter *time.Duration
			if !svcErr.RetryAfter.IsZero() {
				delay := time.Until(svcErr.RetryAfter)
				retryAfter = &delay
			}
			return sendOutcome{Outcome: OutcomeRetryable, Err: err, RetryAfter: retryAfter}
		}
		return sendOutcome{Outcome: OutcomePermanent, Err: err}
	}
	return sendOutcome{Outcome: OutcomeRetryable, Err: err}
}

func (t *WebPushTransport) Send(ctx context.Context, activity Activity, target Target) SendResult {
	subscriptionID, err := parseSubscriptionID(target.Key)
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}
	record, err := t.delivery.GetSubscriptionByID(ctx, subscriptionID)
	if errors.Is(err, ErrDeliveryNotFound) {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: err}
	}

	endpoint, p256dh, auth, err := t.decryptSubscription(record)
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}

	summary, err := t.summaries.GetGameRequestSummary(ctx, activity.GameRequestID)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get game request summary: %w", err)}
	}
	actorGameNick := "" // Task 6/composition-root wires the real Game Nick lookup if needed; empty is valid for closed/cancelled and any kind where it's unavailable.
	payload := buildPushPayload(activity, summary, t.baseURL, actorGameNick)

	outcome := t.sender.send(ctx, endpoint, p256dh, auth, payload, pushTTLSeconds)
	if outcome.Expired {
		if _, deleteErr := t.subscriptions.DeleteSubscription(ctx, record.ID, record.UserID); deleteErr != nil {
			// Best-effort: the delivery is already terminal (permanent) either way.
			_ = deleteErr
		}
	}
	return SendResult{Outcome: outcome.Outcome, RetryAfter: outcome.RetryAfter, Err: outcome.Err}
}

func parseSubscriptionID(key string) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(key, "%d", &id); err != nil {
		return 0, fmt.Errorf("parse subscription ID from target key %q: %w", key, err)
	}
	return id, nil
}

// pushPayload is the versioned minimal Web Push payload contract.
// ActorGameNick is present only for joined/left, per the settled design.
type pushPayload struct {
	Version       int    `json:"v"`
	Kind          string `json:"kind"`
	Game          string `json:"game"`
	StartsAt      string `json:"startsAt,omitempty"`
	DeepLink      string `json:"deepLink"`
	Title         string `json:"title"`
	ActorGameNick string `json:"actorGameNick,omitempty"`
}

func buildPushPayload(activity Activity, summary GameRequestSummary, baseURL, actorGameNick string) []byte {
	payload := pushPayload{
		Version: 1, Kind: string(activity.Kind), Game: summary.GameName,
		DeepLink: baseURL + "/game-requests/" + activity.GameRequestID,
		Title:    notificationTitle(activity.Kind, summary.GameName, actorGameNick),
	}
	if !summary.StartsAt.IsZero() {
		payload.StartsAt = summary.StartsAt.Format(time.RFC3339)
	}
	if activity.Kind == ActivityJoined || activity.Kind == ActivityLeft {
		payload.ActorGameNick = actorGameNick
	}
	encoded, _ := json.Marshal(payload) // pushPayload has no cyclic/unsupported fields; Marshal cannot fail here.
	return encoded
}

func notificationTitle(kind ActivityKind, game, actorGameNick string) string {
	switch kind {
	case ActivityCreated:
		return "New " + game + " Game Request"
	case ActivityJoined:
		return actorGameNick + " joined " + game
	case ActivityLeft:
		return actorGameNick + " left " + game
	case ActivityClosed:
		return game + " Game Request closed"
	case ActivityCancelled:
		return game + " Game Request cancelled"
	default:
		return game + " Game Request updated"
	}
}

func (t *WebPushTransport) decryptSubscription(record PushSubscriptionRecord) (endpoint, p256dh, auth string, err error) {
	endpoint, err = platformcrypto.Decrypt(t.encryptionKey, record.EndpointCiphertext)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt endpoint: %w", err)
	}
	p256dh, err = platformcrypto.Decrypt(t.encryptionKey, record.P256dhCiphertext)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt p256dh: %w", err)
	}
	auth, err = platformcrypto.Decrypt(t.encryptionKey, record.AuthCiphertext)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt auth: %w", err)
	}
	return endpoint, p256dh, auth, nil
}
```

This requires adding an `encryptionKey []byte` field to `WebPushTransport` and threading it through `NewWebPushTransport`'s parameter list (append it, update Task 4's constructor signature and its call sites in `webpush_test.go`) and importing `platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"` in `webpush_send.go`. Note: subscription material is encrypted with the raw `encryptionKey`, not a derived subkey (only the *fingerprint*/*device-hash* HMACs use derived subkeys, per the Foundation plan's Finding 2 fix) — `WebPushTransport` decrypting with the raw key is correct and matches how `Encrypt` was originally called in `SubscriptionService.Enable`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/notification/... -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Run `go mod tidy` and confirm the dependency is no longer indirect**

Run: `go mod tidy` then `grep go-webpush go.mod` — expect the `// indirect` comment gone now that `webpush_send.go` imports it directly.

- [ ] **Step 6: Commit**

```bash
git add internal/notification/webpush_send.go internal/notification/webpush_send_test.go internal/notification/webpush.go go.mod go.sum
git commit -m "feat(notification): add WebPushTransport.Send with the go-webpush/v2 adapter"
```

---

### Task 6: Composition-root wiring

**Files:**
- Modify: `internal/app/notification.go`

**Interfaces:**
- Consumes: everything from Tasks 1-5.
- Produces: `WebPushTransport` registered in `notificationTransports()` when `cfg.WebPush != nil`.

- [ ] **Step 1: Read the existing composition root first**

Read `internal/app/notification.go` and `internal/app/app.go` in full — in particular `notificationTransports()` (added by the Foundation plan's final fix wave) and how `cfg.WebPush`/`cfg.DataEncryptionKey` currently flow into `app.Run`.

- [ ] **Step 2: Implement the adapters and wiring**

Add to `internal/app/notification.go`:

```go
type gameFollowersAdapter struct{ discovery *game.DiscoveryService } // or whatever concrete type Task 1 actually added the method to

func (a gameFollowersAdapter) ListFollowers(ctx context.Context, gameID string) ([]string, error) {
	return a.discovery.ListFollowers(ctx, gameID) // exact method name per Task 1
}

type requestParticipantsAdapter struct{ queries *gamerequestquery.Store }

func (a requestParticipantsAdapter) ListParticipants(ctx context.Context, gameRequestID string) ([]string, error) {
	members, err := a.queries.ListGameRequestMembers(ctx, gameRequestID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID) // exact field name per the existing ListGameRequestMembers row type
	}
	return userIDs, nil
}

type requestSummaryAdapter struct{ queries *gamerequestquery.Store }

func (a requestSummaryAdapter) GetGameRequestSummary(ctx context.Context, gameRequestID string) (notification.GameRequestSummary, error) {
	summary, err := a.queries.GetGameRequestSummary(ctx, gameRequestID) // exact method/type name per Task 2
	if err != nil {
		return notification.GameRequestSummary{}, err
	}
	return notification.GameRequestSummary{GameID: summary.GameID, GameName: summary.GameName, StartsAt: summary.StartsAt}, nil
}
```

Change `notificationTransports()`'s signature to accept what it now needs to construct `WebPushTransport` (the current signature, `notificationTransports() []notification.Transport`, takes nothing — it will need the config, the store, and the two query stores). Read how `newNotificationWorker`/`newSubscriptionService` already receive their dependencies in `app.go`'s `Run` and follow the same explicit-parameter style; do not reach for a service locator or global state.

```go
func notificationTransports(
	store *notificationpostgres.Store, gameDiscovery *game.DiscoveryService, gameRequestQueries *gamerequestquery.Store,
	cfg config.Config, baseURL string,
) ([]notification.Transport, error) {
	if cfg.WebPush == nil {
		return nil, nil
	}
	vapidKeys, err := loadVAPIDKeys(cfg.WebPush.PublicKey, cfg.WebPush.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("load VAPID keys: %w", err)
	}
	sender := newWebPushClientSender(vapidKeys, cfg.WebPush.Subject)
	transport := notification.NewWebPushTransport(
		store, store, store, gameFollowersAdapter{discovery: gameDiscovery}, requestParticipantsAdapter{queries: gameRequestQueries},
		requestSummaryAdapter{queries: gameRequestQueries}, baseURL, sender,
	)
	return []notification.Transport{transport}, nil
}

func loadVAPIDKeys(publicKey, privateKey string) (*webpush.VAPIDKeys, error) {
	encoded, err := json.Marshal(struct {
		PublicKey  string `json:"publicKey"`
		PrivateKey string `json:"privateKey"`
	}{PublicKey: publicKey, PrivateKey: privateKey})
	if err != nil {
		return nil, err
	}
	var vapidKeys webpush.VAPIDKeys
	if err := json.Unmarshal(encoded, &vapidKeys); err != nil {
		return nil, fmt.Errorf("parse VAPID keys: %w", err)
	}
	return &vapidKeys, nil
}
```

Update `newNotificationWorker`'s caller in `app.go` to call the new `notificationTransports(...)` with real arguments (it already has `notificationStore`, and will need `gameDiscovery`/`gameRequestQueries` in scope — both already constructed earlier in `Run` for other purposes; read `app.go` to find their exact existing local variable names) and handle the returned error by failing startup (`return fmt.Errorf("configure notification transports: %w", err)`), matching how every other fallible startup step in `Run` already behaves.

Read `go doc -all github.com/marknefedov/go-webpush/v2` yourself to confirm `VAPIDKeys`'s actual `UnmarshalJSON` field-name expectations (`publicKey`/`privateKey`, lowercase per the package doc) before finalizing `loadVAPIDKeys` — adjust if reality differs from this plan's guess.

- [ ] **Step 3: Verify**

Run: `go build ./...`, `go vet ./...`, `go test ./internal/app/... -race`.
Expected: succeeds.

Manually smoke-test if a local PostgreSQL/Docker environment is reachable (same as the Foundation plan's Task 8): set real `LANSURFER_WEBPUSH_PUBLIC_KEY`/`LANSURFER_WEBPUSH_PRIVATE_KEY`/`LANSURFER_WEBPUSH_SUBJECT`/`LANSURFER_DATA_ENCRYPTION_KEY` (generate a throwaway VAPID pair the same way Task 1 of the Foundation plan did), start the app, confirm it logs no startup error. Do not attempt a real send to a real browser — there is still no browser-side code to create a real subscription (that's the Client plan).

- [ ] **Step 4: Commit**

```bash
git add internal/app/notification.go internal/app/app.go
git commit -m "feat(notification): wire WebPushTransport into the composition root"
```

---

### Task 7: Documentation reconciliation and full verification

**Files:**
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/ROADMAP.md`
- Modify: `docs/superpowers/README.md`

- [ ] **Step 1: Update ARCHITECTURE.md**

In the `### \`notification\` (Track E)` section, add a paragraph stating that E2 now sends real personal Web Push notifications via `WebPushTransport`/`github.com/marknefedov/go-webpush/v2`, following the settled recipient-eligibility and provider-result-mapping rules, and that this is still server-side only — no browser-side enable flow or service-worker push handler exists yet (that remains the Client plan). Update the closing status line accordingly.

- [ ] **Step 2: Update ROADMAP.md**

Update Track E's status line/bullet: E2 now has both its subscription/preference foundation and real server-side delivery implemented; only the browser client (service-worker push/notificationclick, enable-flow JavaScript) and E3 (Discord) remain outstanding.

- [ ] **Step 3: Update docs/superpowers/README.md**

Note in the Notifications section that this plan (link it) completes E2's server-side delivery; the Client plan (not yet written) remains the only piece of E2 left.

- [ ] **Step 4: Full verification**

Run, in order: `task generate`, `task test`, `task test:integration`, `task lint`, `task build`. Fix any pre-existing hardcoded assertion (a migration-count check has needed updating after every schema-adding task so far in this branch's history) directly rather than treating it as a new finding.

- [ ] **Step 5: Commit**

```bash
git add docs/ARCHITECTURE.md docs/ROADMAP.md docs/superpowers/README.md
git commit -m "docs(notification): document Web Push server-side delivery"
```

- [ ] **Step 6: Final summary**

Report: what was implemented, that the browser Client plan is the only remaining E2 piece, any deviation from this plan's guesses about `go-webpush/v2`'s exact API discovered during implementation, and verification performed/omitted.
