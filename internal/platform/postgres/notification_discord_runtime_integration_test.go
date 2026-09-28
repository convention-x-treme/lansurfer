//go:build integration

package postgres

import (
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

// TestDiscordRuntimeConstructedTransportDeliversWork proves E3-21's runtime
// registration through the exact production combination
// (notification.NewDiscordSendTransport feeding notification.NewWorker),
// which is all internal/app.newNotificationWorker/notificationTransports
// wire together: those functions add no delivery behavior of their own
// beyond this config-gated construction (proven separately, without a
// database, by internal/app's unit tests). A live Discord operator
// configuration must actually discover, materialize, and deliver new live
// activity end to end.
func TestDiscordRuntimeConstructedTransportDeliversWork(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "runtime-token")
	calls := make(chan discordSendHTTPCall, 1)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		discordSendReply(w, request, calls, "runtime-message")
	})
	// This is exactly what a configured internal/app.notificationTransports
	// registers for the worker when cfg.DiscordDelivery is enabled.
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Runtime Club", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Runtime")
	if err != nil {
		t.Fatal(err)
	}
	// newTestWorker is the same notification.NewWorker(store, transports,
	// logger, time.Now) call internal/app.newNotificationWorker makes.
	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	call := discordSendNextCall(t, calls)
	discordSendAssertCall(t, call, http.MethodPost, "runtime-token", "", request.ID)
	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || sync.MessageID != "runtime-message" {
		t.Fatalf("runtime-delivered sync = %+v, %v", sync, err)
	}
	var status string
	if err := pool.QueryRow(t.Context(), `
		SELECT d.status FROM notification_delivery d JOIN notification_work w ON w.id = d.work_id
		WHERE w.game_request_id = $1 AND d.transport = 'discord'
	`, request.ID).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("runtime delivery status = %q, %v; want succeeded", status, err)
	}
}

// TestDiscordRuntimeDisabledConfigurationNeverConstructsOrDelivers proves
// the other half of E3-21's gating: with no Discord transport registered
// (the disabled-configuration case internal/app's unit tests prove at the
// construction layer), the worker discovers and materializes live activity
// as always, but a Discord target never appears and no delivery is ever
// attempted, exactly like before E3 existed.
func TestDiscordRuntimeDisabledConfigurationNeverConstructsOrDelivers(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Disabled"); err != nil {
		t.Fatal(err)
	}
	// No Discord transport in the list: exactly what an unconfigured
	// cfg.DiscordDelivery produces.
	worker := newTestWorker(store, nil)
	worker.Tick(t.Context())
	var deliveries int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_delivery WHERE transport = 'discord'`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("discord deliveries = %d with no Discord transport registered, want none", deliveries)
	}
}

// TestDiscordDiagnosticsListingPerformsNoProviderCall proves listing
// problem deliveries (the CLI's `admin notifications problems` and its
// underlying Service.ListProblems, which internal/app.ListNotificationProblems
// calls with a nil transport list exactly like this) never reaches Discord,
// even while a real exhausted Discord delivery exists to list.
func TestDiscordDiagnosticsListingPerformsNoProviderCall(t *testing.T) {
	_, store, fixture, key := discordSendSetup(t)
	discordSendConnect(t, store, key, "diagnostics-token")
	var calls int
	client := discordSendTestClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Diagnostics"); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	if err := store.RecordExhausted(t.Context(), claims[0].DeliveryID, "simulated exhaustion for diagnostics listing"); err != nil {
		t.Fatal(err)
	}
	// Exactly like internal/app.ListNotificationProblems: no transports.
	service := notification.NewService(store, nil)
	problems, err := service.ListProblems(t.Context())
	if err != nil {
		t.Fatalf("ListProblems() error = %v", err)
	}
	if len(problems) != 1 || problems[0].DeliveryID != claims[0].DeliveryID {
		t.Fatalf("problems = %+v, want the one exhausted Discord delivery", problems)
	}
	if calls != 0 {
		t.Fatalf("listing diagnostics made %d HTTP calls, want zero", calls)
	}
}
