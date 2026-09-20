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
