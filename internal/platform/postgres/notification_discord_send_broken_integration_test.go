//go:build integration

package postgres

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A definitive 10015 Unknown Webhook during an edit transitions the
// destination to Broken, clears its encrypted credential, terminates the
// exact current delivery as Permanent (never ambiguous/suppressed), and
// suppresses only other still-pending/claimed Discord deliveries for that
// exact destination generation. A delivery on an unrelated generation is
// left completely untouched.
func TestDiscordSend10015BreaksDestinationAndSuppressesSameGenerationOnly(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	requestA, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Request A")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 3)
	broken := false
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && !broken {
			broken = true
			calls <- discordSendHTTPCall{method: r.Method, path: r.URL.RequestURI()}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":10015,"message":"Unknown Webhook"}`))
			return
		}
		discordSendReply(w, r, calls, "message-a")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)

	claimedA := discordSendClaim(t, store)
	if len(claimedA) != 1 || transport.SendClaimed(t.Context(), claimedA[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("initial create for request A did not succeed")
	}
	discordSendNextCall(t, calls)
	if err := store.RecordSuccess(t.Context(), claimedA[0].DeliveryID); err != nil {
		t.Fatal(err)
	}

	// Request B materializes and claims a delivery to the same generation,
	// but is deliberately never sent: it represents other still-claimed
	// work for this exact destination generation.
	requestB, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(2*time.Hour), "Request B")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claimedB := discordSendClaim(t, store)
	if len(claimedB) != 1 || claimedB[0].Activity.GameRequestID != requestB.ID {
		t.Fatalf("claimed B = %+v", claimedB)
	}
	// Request A's next activity discovers the destination is definitively
	// gone (10015).
	if err := fixture.requests.Join(t.Context(), requestA.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	var editAttempt notification.ClaimedDelivery
	for _, claim := range discordSendClaim(t, store) {
		if claim.Activity.GameRequestID == requestA.ID {
			editAttempt = claim
		}
	}
	if editAttempt.DeliveryID == 0 {
		t.Fatal("edit claim for request A not found")
	}
	result := transport.SendClaimed(t.Context(), editAttempt)
	if result.Outcome != notification.OutcomePermanent || result.Err == nil {
		t.Fatalf("10015 edit outcome = %v, err = %v; want terminal permanent", result.Outcome, result.Err)
	}
	discordSendNextCall(t, calls)
	if len(calls) != 0 {
		t.Fatal("10015 must not retry the invalid webhook")
	}
	// A worker can stop here: the current claim must already be terminal.
	if status := discordSendDeliveryStatus(t, pool, editAttempt.DeliveryID); status != "permanent" {
		t.Fatalf("current 10015 delivery before worker recording = %q, want permanent", status)
	}
	if err := store.RecordPermanent(t.Context(), editAttempt.DeliveryID, "discord webhook no longer exists"); err != nil {
		t.Fatalf("worker post-result RecordPermanent() error = %v", err)
	}

	// A delivery on an unrelated destination generation, seeded only now
	// (after the break already committed) so it can never be swept up by
	// an earlier claim pass, must never be touched by this break either.
	otherGenerationTarget := "d1:cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	discordLifecycleDeliveryFixture(t, pool, store, fixture, []notification.Target{{Transport: "discord", Key: otherGenerationTarget}}, []string{"pending"})

	status, err := store.GetDiscordDestinationStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != notification.DiscordDestinationBroken || status.ID != destination.ID {
		t.Fatalf("destination status = %+v, want broken and same identity", status)
	}
	if _, ciphertext, err := store.GetDiscordDestination(t.Context()); err != nil || ciphertext != nil {
		t.Fatalf("broken destination ciphertext = %v, error = %v; want cleared", ciphertext, err)
	}
	if status.BrokenReason == "" || strings.Contains(status.BrokenReason, "private-token") {
		t.Fatalf("broken reason = %q, want a safe non-empty reason", status.BrokenReason)
	}

	if got := discordSendDeliveryStatus(t, pool, claimedB[0].DeliveryID); got != "suppressed" {
		t.Fatalf("other same-generation claimed delivery status = %q, want suppressed", got)
	}
	if got := deliveryStatus(t, pool, "discord", otherGenerationTarget); got != "pending" {
		t.Fatalf("different-generation delivery status = %q, want untouched (pending)", got)
	}

	// No further target materialization while broken.
	activities, err := store.DiscoverActivity(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, activity := range activities {
		targets, err := transport.Targets(t.Context(), activity)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 0 {
			t.Fatalf("Targets() after broken = %v, want none", targets)
		}
	}
	if err := store.DeleteDiscordDestination(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := discordSendDeliveryStatus(t, pool, editAttempt.DeliveryID); got != "permanent" {
		t.Fatalf("disconnect changed 10015 delivery = %q", got)
	}
}

// A 10015 during an initial create also breaks the destination and
// terminates that exact delivery as Permanent, consistently with an edit.
func TestDiscordSend10015OnCreateBreaksDestination(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	discordSendConnect(t, store, key, "private-token")
	if _, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First"); err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 2)
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls <- discordSendHTTPCall{method: r.Method, path: r.URL.RequestURI()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":10015,"message":"Unknown Webhook"}`))
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	result := transport.SendClaimed(t.Context(), claims[0])
	if result.Outcome != notification.OutcomePermanent {
		t.Fatalf("10015 create outcome = %v, want permanent", result.Outcome)
	}
	discordSendNextCall(t, calls)
	if status := discordSendDeliveryStatus(t, pool, claims[0].DeliveryID); status != "permanent" {
		t.Fatalf("10015 create claim before worker recording = %q, want permanent", status)
	}
	if err := store.RecordPermanent(t.Context(), claims[0].DeliveryID, "discord webhook no longer exists"); err != nil {
		t.Fatalf("RecordPermanent() error = %v, want the claim still available", err)
	}
	status, err := store.GetDiscordDestinationStatus(t.Context())
	if err != nil || status.State != notification.DiscordDestinationBroken {
		t.Fatalf("destination status = %+v, error = %v; want broken", status, err)
	}
	var reservations int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_create_reservation`).Scan(&reservations); err == nil && reservations != 0 {
		t.Fatalf("create reservations = %d, want none retained after a proven-rejected create", reservations)
	}
}

// Create reservations are scoped by destination generation via their
// composite primary key: a reservation left behind for an old, broken
// generation must never block a create for a later, independent
// replacement generation, and cleaning up old state never touches the
// new generation's own (absent) reservation.
func TestDiscordCreateReservationDoesNotBlockLaterReplacementGeneration(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	old := discordSendConnect(t, store, key, "old-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Reservation isolation")
	if err != nil {
		t.Fatal(err)
	}
	if reserved, err := store.ReserveDiscordCreate(t.Context(), old.ID, request.ID); err != nil || !reserved {
		t.Fatalf("reserve old generation create = %v, %v", reserved, err)
	}

	replacement := discordSendConnect(t, store, key, "new-token")
	if replacement.ID == old.ID {
		t.Fatal("replacement reused destination identity")
	}
	calls := make(chan discordSendHTTPCall, 2)
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		discordSendReply(w, r, calls, "new-message")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 || claims[0].Target.Key != "d1:"+replacement.ID {
		t.Fatalf("claims = %+v, want the new generation's target", claims)
	}
	if result := transport.SendClaimed(t.Context(), claims[0]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("new generation create outcome = %v, want succeeded despite the old generation's reservation", result.Outcome)
	}
	discordSendAssertCall(t, discordSendNextCall(t, calls), http.MethodPost, "new-token", "", request.ID)
	if err := store.RecordSuccess(t.Context(), claims[0].DeliveryID); err != nil {
		t.Fatal(err)
	}

	if count := discordSendReservationCount(t, pool, old.ID, request.ID); count != 1 {
		t.Fatalf("old generation reservation count = %d, want the untouched retained row", count)
	}
	if count := discordSendReservationCount(t, pool, replacement.ID, request.ID); count != 0 {
		t.Fatalf("new generation reservation count = %d, want none (create succeeded and released it)", count)
	}
}

func discordSendReservationCount(t *testing.T, pool *pgxpool.Pool, destinationID, gameRequestID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM notification_discord_create_reservation
		WHERE destination_id = $1::uuid AND game_request_id = $2::uuid
	`, destinationID, gameRequestID).Scan(&count); err != nil {
		t.Fatalf("count discord create reservations: %v", err)
	}
	return count
}
