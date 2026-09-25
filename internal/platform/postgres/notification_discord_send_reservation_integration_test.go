//go:build integration

package postgres

import (
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
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
