//go:build integration

package postgres

import (
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

// A known message edit that receives Discord 10008 Unknown Message
// compare-and-clears the stale reference under the real PostgreSQL
// destination-generation advisory lock, keeps the destination connected,
// creates exactly one replacement message from the latest current
// snapshot, persists it through the same monotonic sync semantics as a
// fresh create, and routes later activity to that replacement.
func TestDiscordSend10008RecoversWithOneReplacementMessage(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Original description")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 3)
	recovered := false
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && !recovered {
			recovered = true
			calls <- discordSendHTTPCall{method: r.Method, path: r.URL.RequestURI()}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":10008,"message":"Unknown Message"}`))
			return
		}
		discordSendReply(w, r, calls, "message-replacement")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	discordSendMaterialize(t, store, transport)

	first := discordSendClaim(t, store)
	if len(first) != 1 || transport.SendClaimed(t.Context(), first[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("initial create did not succeed")
	}
	discordSendNextCall(t, calls)
	if err := store.RecordSuccess(t.Context(), first[0].DeliveryID); err != nil {
		t.Fatal(err)
	}

	// Simulate Discord losing the message: the next activity's edit
	// attempt discovers 10008.
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	editAttempt := discordSendClaim(t, store)
	if len(editAttempt) != 1 {
		t.Fatalf("edit claim = %+v", editAttempt)
	}
	if result := transport.SendClaimed(t.Context(), editAttempt[0]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("recovery outcome = %v, want succeeded", result.Outcome)
	}
	// The failed edit, then the replacement create.
	discordSendNextCall(t, calls)
	created := discordSendNextCall(t, calls)
	discordSendAssertCall(t, created, http.MethodPost, "private-token", "", request.ID)
	if err := store.RecordSuccess(t.Context(), editAttempt[0].DeliveryID); err != nil {
		t.Fatal(err)
	}

	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || sync.MessageID != "message-replacement" {
		t.Fatalf("recovered sync = %+v, error = %v", sync, err)
	}
	if status, err := store.GetDiscordDestinationStatus(t.Context()); err != nil || status.State != notification.DiscordDestinationConnected {
		t.Fatalf("destination state = %+v, error = %v; want connected", status, err)
	}

	// Later activity edits the replacement message, never creates another.
	if err := fixture.requests.Cancel(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	later := discordSendClaim(t, store)
	if len(later) != 1 || transport.SendClaimed(t.Context(), later[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("post-recovery edit did not succeed")
	}
	discordSendAssertCall(t, discordSendNextCall(t, calls), http.MethodPatch, "private-token", "message-replacement", request.ID)

	var reservations int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_create_reservation WHERE destination_id = $1 AND game_request_id = $2`, destination.ID, request.ID).Scan(&reservations); err != nil || reservations != 0 {
		t.Fatalf("create reservations = %d, %v; want none retained after success", reservations, err)
	}
}

// A replacement create Discord accepted, followed by a lost sync
// transaction (simulated by terminating the backend holding the advisory
// lock), must retain the durable reservation exactly like an
// initial create: no automatic replay creates a second replacement even
// after the claim's lease expires.
func TestDiscordSend10008ReplacementCreateNeverReplaysAfterLostSync(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 4)
	var posts, terminations int
	// The original message stays permanently deleted for the rest of the
	// test: every edit against it returns 10008 again, exactly as real
	// Discord would. The very first replacement create's response is lost
	// (its sync transaction's backend is terminated); only that specific
	// create attempts the termination, matching the reservation the
	// pooled ReserveDiscordCreate call already committed durably.
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			calls <- discordSendHTTPCall{method: r.Method, path: r.URL.RequestURI()}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":10008,"message":"Unknown Message"}`))
			return
		}
		posts++
		if posts == 2 {
			// Kill the backend holding this destination/request sync lock
			// right after Discord accepts the replacement create, so the
			// message ID cannot be durably recorded.
			terminations++
			var terminated bool
			err := pool.QueryRow(r.Context(), `
				SELECT bool_or(pg_terminate_backend(l.pid))
				FROM pg_locks l, (SELECT hashtextextended($1::uuid::text || ':' || $2::uuid::text, 0) AS k) h
				WHERE l.locktype = 'advisory' AND l.granted AND l.objsubid = 1
				  AND l.classid = ((h.k >> 32) & 4294967295)::oid AND l.objid = (h.k & 4294967295)::oid`,
				destination.ID, request.ID).Scan(&terminated)
			if err != nil || !terminated {
				t.Errorf("terminate sync transaction = %v, %v", terminated, err)
			}
		}
		discordSendReply(w, r, calls, map[int]string{1: "message-first", 2: "message-second"}[posts])
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)

	first := discordSendClaim(t, store)
	if len(first) != 1 || transport.SendClaimed(t.Context(), first[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("initial create did not succeed")
	}
	discordSendNextCall(t, calls)
	if err := store.RecordSuccess(t.Context(), first[0].DeliveryID); err != nil {
		t.Fatal(err)
	}

	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	if result := transport.SendClaimed(t.Context(), claims[0]); result.Outcome != notification.OutcomeUncertain {
		t.Fatalf("accepted replacement create with lost sync outcome = %v, want uncertain", result.Outcome)
	}
	discordSendNextCall(t, calls) // the failed edit (10008)
	discordSendNextCall(t, calls) // the accepted-but-lost replacement create
	if terminations != 1 {
		t.Fatalf("terminations = %d, want exactly one", terminations)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET claimed_until = now() - interval '1 second' WHERE id = $1`, claims[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	// The sync transaction (including the recovery clear) rolled back with
	// the lost commit, so a replay legitimately rediscovers 10008 on the
	// still-referenced original message: exactly one more PATCH call is
	// expected. The durably committed reservation must still block a
	// second replacement create.
	discordSendNextCall(t, calls)
	if len(calls) != 0 {
		t.Fatal("lease expiry replayed a second Discord replacement create")
	}
	if posts != 2 {
		t.Fatalf("total create calls = %d, want exactly two: the initial create and one replacement create", posts)
	}
	if status := discordSendDeliveryStatus(t, pool, claims[0].DeliveryID); status != "uncertain" {
		t.Fatalf("replayed delivery status = %q, want uncertain", status)
	}
	var reservations int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_create_reservation WHERE destination_id = $1 AND game_request_id = $2`, destination.ID, request.ID).Scan(&reservations); err != nil || reservations != 1 {
		t.Fatalf("create reservations = %d, %v; want retained reservation", reservations, err)
	}
	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || sync.MessageID != "message-first" || sync.AppliedVersion != 1 {
		t.Fatalf("lost replacement create must leave the original recorded reference untouched: %+v, %v", sync, err)
	}
}
