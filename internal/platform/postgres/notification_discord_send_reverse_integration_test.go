//go:build integration

package postgres

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

// The older worker must not carry its snapshot across a wait for a newer
// worker's PostgreSQL advisory lock and then restore obsolete external state.
func TestDiscordSendOlderWaiterCannotOverwriteNewerCreate(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	calls := make(chan discordSendHTTPCall, 2)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		discordSendReply(w, request, calls, "message-newest")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 2 {
		t.Fatalf("claims = %+v", claims)
	}
	var old, newer notification.ClaimedDelivery
	for _, claim := range claims {
		if claim.Activity.Kind == notification.ActivityCreated {
			old = claim
		} else {
			newer = claim
		}
	}
	if old.DeliveryID == 0 || newer.DeliveryID == 0 {
		t.Fatalf("missing old/new claim: %+v", claims)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	newDone, oldDone := make(chan notification.SendResult, 1), make(chan notification.SendResult, 1)
	go func() { newDone <- transport.SendClaimed(ctx, newer) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("newer worker did not enter provider call")
	}
	go func() { oldDone <- transport.SendClaimed(ctx, old) }()
	for {
		var waiters int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if waiters > 0 {
			break
		}
		select {
		case result := <-oldDone:
			t.Fatalf("older worker escaped lock: %v", result.Outcome)
		case <-ctx.Done():
			t.Fatal("older worker did not wait on PostgreSQL lock")
		default:
			runtime.Gosched()
		}
	}
	released.Do(func() { close(release) })
	if result := <-newDone; result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("newer outcome = %v", result.Outcome)
	}
	if result := <-oldDone; result.Outcome != notification.OutcomeAlreadyRecorded {
		t.Fatalf("older outcome = %v", result.Outcome)
	}
	call := discordSendNextCall(t, calls)
	discordSendAssertCall(t, call, http.MethodPost, "private-token", "", request.ID)
	if call.field("Players") != "2 players (unlimited)" || len(calls) != 0 {
		t.Fatal("older worker restored stale representation or created duplicate")
	}
	if status := discordSendDeliveryStatus(t, pool, old.DeliveryID); status != "superseded" {
		t.Fatalf("older delivery = %q", status)
	}
	state, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || state.MessageID != "message-newest" || state.AppliedVersion != newer.Activity.Sequence {
		t.Fatalf("highest applied marker = %+v, %v", state, err)
	}
}
