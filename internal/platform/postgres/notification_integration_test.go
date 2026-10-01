//go:build integration

package postgres

import (
	"context"
	"errors"
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

	// The adversarial race this store must tolerate is a row that claims a
	// LOWER sequence number (because its INSERT executes first) but commits
	// LATER, after some other row with a HIGHER sequence number has already
	// been discovered and materialized. Postgres identity columns assign
	// their value at INSERT-statement time via a non-transactional nextval(),
	// not at commit time, so two transactions can commit in the opposite
	// order to the numeric order of the sequence values they reserved.
	//
	// Construct that ordering explicitly: open a second connection, start a
	// transaction, and insert the "held" game request and its activity row
	// directly via raw SQL first -- this claims the LOWER sequence number --
	// then leave it uncommitted. Only after that does the fixture's own
	// requests.Create (which auto-commits) insert a second, independent game
	// request; because its activity INSERT executes after the held
	// transaction's insert, it claims a HIGHER sequence number and commits
	// immediately, so it alone is visible to a query issued right now.
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

	afterHeld, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "After held")
	if err != nil {
		t.Fatalf("create higher-sequence game request: %v", err)
	}

	// Only the higher-sequence, already-committed row is visible: the held
	// transaction's lower-sequence row is still uncommitted.
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	if len(activities) != 1 || activities[0].GameRequestID != afterHeld.ID {
		t.Fatalf("DiscoverActivity() before the held commit = %#v, want only %s", activities, afterHeld.ID)
	}
	// This is the step where a naive high-watermark cursor design would
	// advance its watermark to this higher sequence value, having now
	// "processed" it.
	if err := store.MaterializeWork(t.Context(), activities[0], nil); err != nil {
		t.Fatalf("MaterializeWork() error = %v", err)
	}

	// The held transaction commits now, making its lower-sequence row
	// visible for the first time -- after the higher-sequence row was
	// already discovered and materialized.
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit held transaction: %v", err)
	}

	// A high-watermark cursor advanced past the higher sequence in the
	// MaterializeWork step above would now permanently skip this
	// lower-sequence row: its sequence value is <= that watermark forever,
	// even though it only just became visible. The NOT EXISTS anti-join
	// this store uses does not care about relative sequence ordering at
	// all, so it correctly finds and returns it.
	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() after the held commit error = %v", err)
	}
	if len(activities) != 1 || activities[0].GameRequestID != heldRequestID {
		t.Fatalf("DiscoverActivity() after the held commit = %#v, want the previously-uncommitted lower-sequence row %s", activities, heldRequestID)
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

func TestNotificationTargetResolutionFailsClosed(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline(): %v", err)
	}
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Retry resolution")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	push := newFakeTransport("push", []notification.Target{{Transport: "push", Key: "push-one"}})
	discord := newFakeTransport("discord", []notification.Target{{Transport: "discord", Key: "discord-one"}})
	push.targetsErr = errors.New("temporary read failure")
	worker := newTestWorker(store, []notification.Transport{push, discord})
	worker.Tick(t.Context())
	var work, deliveries int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work WHERE game_request_id = $1", created.ID).Scan(&work); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	discovered, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || work != 0 || deliveries != 0 || len(discovered) != 1 || discovered[0].GameRequestID != created.ID {
		t.Fatalf("failed resolution: work=%d deliveries=%d rediscovered=%v error=%v", work, deliveries, discovered, err)
	}
	push.targetsErr = nil
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work WHERE game_request_id = $1", created.ID).Scan(&work); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery").Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if work != 1 || deliveries != 2 {
		t.Fatalf("retry work/deliveries = %d/%d, want 1/2", work, deliveries)
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

func TestNotificationEligibilityReadErrorRetriesWithoutSend(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline(): %v", err)
	}
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Unknown eligibility")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)
	transport.targetsErr = errors.New("eligibility read unavailable")
	before := time.Now()
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())

	var status string
	var attempts int
	var next time.Time
	if err := pool.QueryRow(t.Context(),
		"SELECT d.status, d.attempt_count, d.next_attempt_at FROM notification_delivery d JOIN notification_work w ON w.id = d.work_id WHERE w.game_request_id = $1",
		created.ID,
	).Scan(&status, &attempts, &next); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 || !next.After(before) || len(transport.sendCalls) != 0 {
		t.Fatalf("unknown eligibility: status=%s attempts=%d next=%v sends=%d", status, attempts, next, len(transport.sendCalls))
	}
}

// TestNotificationSuppressesDeliveryWhenTargetBecomesIneligibleBeforeSend
// proves the eligibility recheck the worker performs before every send: a
// delivery materialized when a target was eligible must not be sent once
// that target stops being eligible, and must be marked `suppressed` rather
// than attempted, retried, or silently dropped.
func TestNotificationSuppressesDeliveryWhenTargetBecomesIneligibleBeforeSend(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Becomes ineligible")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	transport := newFakeTransport("test", []notification.Target{{Transport: "test", Key: "target-1"}})
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("DiscoverActivity() = %#v, %v", activities, err)
	}
	materializeOneActivityWithTransport(t, store, activities[0], transport)

	// The target was eligible at materialization time; simulate it becoming
	// ineligible before the worker gets to send (e.g. a subscription removed,
	// a user leaving the request) by having the transport's next Targets()
	// call return an empty set.
	transport.targets = nil

	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	worker.Tick(t.Context()) // a suppressed delivery must never be reclaimed

	var status string
	var lastError string
	if err := pool.QueryRow(t.Context(),
		"SELECT status, last_error FROM notification_delivery d JOIN notification_work w ON w.id = d.work_id WHERE w.game_request_id = $1",
		created.ID,
	).Scan(&status, &lastError); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "suppressed" {
		t.Fatalf("delivery status = %q, want %q", status, "suppressed")
	}
	if lastError == "" {
		t.Fatal("suppressed delivery has no reason recorded")
	}
	if len(transport.sendCalls) != 0 {
		t.Fatalf("Send() was called %d times, want 0 (an ineligible target must never be sent to)", len(transport.sendCalls))
	}

	problems, err := store.ListProblemDeliveries(t.Context())
	if err != nil {
		t.Fatalf("ListProblemDeliveries() error = %v", err)
	}
	if len(problems) != 1 || problems[0].Status != notification.StatusSuppressed {
		t.Fatalf("ListProblemDeliveries() = %#v, want exactly one suppressed entry", problems)
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
	var attemptCountAfterExtraTick int32
	if err := pool.QueryRow(t.Context(), "SELECT attempt_count FROM notification_delivery ORDER BY id DESC LIMIT 1").Scan(&attemptCountAfterExtraTick); err != nil {
		t.Fatalf("read attempt_count after exhaustion tick: %v", err)
	}
	if int(attemptCountAfterExtraTick) != notification.MaxAutomaticAttempts {
		t.Fatalf("attempt_count after exhaustion tick = %d, want unchanged at %d (exhausted deliveries are never reclaimed)", attemptCountAfterExtraTick, notification.MaxAutomaticAttempts)
	}
}

func TestNotificationProviderRetryAfterIsHonoredWithoutFiveMinuteCap(t *testing.T) {
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
	if delay < 19*time.Minute+30*time.Second || delay > 20*time.Minute+30*time.Second {
		t.Fatalf("next_attempt_at delay = %v, want approximately 20m (provider Retry-After is neither capped nor jittered)", delay)
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
	schemaName := currentSchema(t, pool)
	pool.Close()
	restartedPool := reopenTestPool(t, schemaName)
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

func TestNotificationStartedActivityMaterializesWork(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Start me")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatalf("InitializeBaseline() error = %v", err)
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 || activities[0].Kind != notification.ActivityStarted || activities[0].GameRequestID != request.ID {
		t.Fatalf("DiscoverActivity() = %#v, %v, want exactly one started activity for %s", activities, err, request.ID)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], nil); err != nil {
		t.Fatalf("MaterializeWork() for started error = %v", err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_work WHERE kind = 'started' AND game_request_id = $1", request.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("started notification_work rows = %d, %v, want 1", count, err)
	}
}
