//go:build integration

package postgres

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Discord accepts the create, then PostgreSQL loses the sync transaction
// before the message ID or uncertain outcome is recorded. Lease expiry must
// not replay the create.
func TestDiscordSendLeaseExpiryNeverReplaysAcceptedCreate(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 2)
	client := discordSendTestClient(t, func(w http.ResponseWriter, httpRequest *http.Request) {
		// Kill the backend holding this destination/request sync lock, so
		// the accepted create cannot be recorded.
		var terminated bool
		err := pool.QueryRow(httpRequest.Context(), `
			SELECT bool_or(pg_terminate_backend(l.pid))
			FROM pg_locks l, (SELECT hashtextextended($1::uuid::text || ':' || $2::uuid::text, 0) AS k) h
			WHERE l.locktype = 'advisory' AND l.granted AND l.objsubid = 1
			  AND l.classid = ((h.k >> 32) & 4294967295)::oid AND l.objid = (h.k & 4294967295)::oid`,
			destination.ID, request.ID).Scan(&terminated)
		if err != nil || !terminated {
			t.Errorf("terminate sync transaction = %v, %v", terminated, err)
		}
		discordSendReply(w, httpRequest, calls, "accepted-message")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	// Called directly so no outcome is recorded, as if PostgreSQL stayed
	// unavailable for the worker's RecordUncertain too.
	if result := transport.SendClaimed(t.Context(), claims[0]); result.Outcome != notification.OutcomeUncertain {
		t.Fatalf("accepted create with lost sync outcome = %v", result.Outcome)
	}
	discordSendAssertCall(t, discordSendNextCall(t, calls), http.MethodPost, "private-token", "", request.ID)

	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET claimed_until = now() - interval '1 second' WHERE id = $1`, claims[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	if len(calls) != 0 {
		t.Fatal("lease expiry replayed an accepted Discord create")
	}
	if status := discordSendDeliveryStatus(t, pool, claims[0].DeliveryID); status != "uncertain" {
		t.Fatalf("replayed delivery status = %q, want uncertain", status)
	}
	var reservations int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_create_reservation WHERE destination_id = $1 AND game_request_id = $2`, destination.ID, request.ID).Scan(&reservations); err != nil || reservations != 1 {
		t.Fatalf("create reservations = %d, %v; want retained reservation", reservations, err)
	}
	if _, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID); err != notification.ErrDiscordMessageSyncNotFound {
		t.Fatalf("lost create recorded sync state: %v", err)
	}
}

// discordSendMakeUncertainWithReservation drives one Discord create to the
// exact ambiguous-outcome state E3-18 leaves behind: Discord accepted the
// create, the sync transaction was lost before recording it, and an
// automatic lease-expiry reclaim already proved it cannot replay the create
// while the reservation stands. It returns the transport/service ready for
// a caller to exercise the explicit administrator retry-uncertain override.
func discordSendMakeUncertainWithReservation(t *testing.T) (pgxPool *pgxpool.Pool, store *notificationpostgres.Store, transport *notification.DiscordTransport, destination notification.DiscordDestination, requestID string, deliveryID int64, key []byte) {
	t.Helper()
	pool, store, fixture, key := discordSendSetup(t)
	destination = discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Ambiguous")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 2)
	killedLock := false
	client := discordSendTestClient(t, func(w http.ResponseWriter, httpRequest *http.Request) {
		// Only the ambiguous first create must lose its sync transaction;
		// a later administrator-triggered retry attempt must complete
		// normally, or this helper could never prove the retry itself
		// succeeds.
		if !killedLock {
			killedLock = true
			var terminated bool
			err := pool.QueryRow(httpRequest.Context(), `
				SELECT bool_or(pg_terminate_backend(l.pid))
				FROM pg_locks l, (SELECT hashtextextended($1::uuid::text || ':' || $2::uuid::text, 0) AS k) h
				WHERE l.locktype = 'advisory' AND l.granted AND l.objsubid = 1
				  AND l.classid = ((h.k >> 32) & 4294967295)::oid AND l.objid = (h.k & 4294967295)::oid`,
				destination.ID, request.ID).Scan(&terminated)
			if err != nil || !terminated {
				t.Errorf("terminate sync transaction = %v, %v", terminated, err)
			}
			discordSendReply(w, httpRequest, calls, "accepted-message")
			return
		}
		discordSendReply(w, httpRequest, calls, "retried-message")
	})
	transport = notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	if result := transport.SendClaimed(t.Context(), claims[0]); result.Outcome != notification.OutcomeUncertain {
		t.Fatalf("accepted create with lost sync outcome = %v", result.Outcome)
	}
	discordSendNextCall(t, calls)
	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET claimed_until = now() - interval '1 second' WHERE id = $1`, claims[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	if len(calls) != 0 {
		t.Fatal("automatic lease-expiry reclaim replayed the ambiguous create")
	}
	if status := discordSendDeliveryStatus(t, pool, claims[0].DeliveryID); status != "uncertain" {
		t.Fatalf("delivery status = %q, want uncertain", status)
	}
	if reservations := discordSendReservationCount(t, pool, destination.ID, request.ID); reservations != 1 {
		t.Fatalf("create reservations = %d, want the retained reservation", reservations)
	}
	return pool, store, transport, destination, request.ID, claims[0].DeliveryID, key
}

// This is the explicit administrator override E1 defines as
// ManualRetryUncertain: it knowingly risks a duplicate externally visible
// Discord message, unlike every automatic path (lease expiry, worker
// reclaim, restart), which must never clear this reservation. Proving that
// atomicity requires a real PostgreSQL statement, not a fake repository.
func TestDiscordManualRetryUncertainReservationAwareOverrideRetriesAfterAmbiguousCreate(t *testing.T) {
	pool, store, transport, destination, requestID, deliveryID, _ := discordSendMakeUncertainWithReservation(t)
	service := notification.NewService(store, []notification.Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), deliveryID); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v, want nil", err)
	}
	if reservations := discordSendReservationCount(t, pool, destination.ID, requestID); reservations != 0 {
		t.Fatalf("create reservations = %d after override, want the exact reservation cleared", reservations)
	}
	if status := discordSendDeliveryStatus(t, pool, deliveryID); status != "pending" {
		t.Fatalf("delivery status after override = %q, want pending", status)
	}
	// The reopened delivery can now attempt another create: this may
	// duplicate the first, externally-uncertain message, which is exactly
	// the risk this explicit administrator action accepts.
	claims := discordSendClaim(t, store)
	if len(claims) != 1 || claims[0].DeliveryID != deliveryID {
		t.Fatalf("reopened claim = %+v", claims)
	}
	if result := transport.SendClaimed(t.Context(), claims[0]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("retried create outcome = %v, want succeeded", result.Outcome)
	}
	if err := store.RecordSuccess(t.Context(), deliveryID); err != nil {
		t.Fatal(err)
	}
	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, requestID)
	if err != nil || sync.MessageID == "" {
		t.Fatalf("post-override sync = %+v, %v", sync, err)
	}
}

// A destination replacement between the ambiguous create and the
// administrator's retry-uncertain call must never let the override clear
// the old generation's reservation or reopen a delivery that targets a
// generation which no longer exists.
func TestDiscordManualRetryUncertainGenerationMismatchFailsAndRetainsReservation(t *testing.T) {
	pool, store, transport, destination, requestID, deliveryID, key := discordSendMakeUncertainWithReservation(t)
	if replacement := discordSendConnect(t, store, key, "new-private-token"); replacement.ID == destination.ID {
		t.Fatal("replacement reused destination generation")
	}
	service := notification.NewService(store, []notification.Transport{transport})
	err := service.ManualRetryUncertain(t.Context(), deliveryID)
	if !errors.Is(err, notification.ErrNoLongerEligible) {
		t.Fatalf("ManualRetryUncertain() across a destination replacement = %v, want %v", err, notification.ErrNoLongerEligible)
	}
	if reservations := discordSendReservationCount(t, pool, destination.ID, requestID); reservations != 1 {
		t.Fatalf("create reservations = %d, want the stale generation's reservation retained untouched", reservations)
	}
	if status := discordSendDeliveryStatus(t, pool, deliveryID); status != "uncertain" {
		t.Fatalf("delivery status = %q, want uncertain (never reopened across a generation mismatch)", status)
	}
}
