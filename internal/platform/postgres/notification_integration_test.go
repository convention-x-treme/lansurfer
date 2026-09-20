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
