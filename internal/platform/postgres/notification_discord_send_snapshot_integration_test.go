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

func TestDiscordSendReadsCurrentSnapshotOnlyAfterLock(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	calls := make(chan discordSendHTTPCall, 1)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		discordSendReply(w, request, calls, "latest-message")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Old description")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	if len(claims) != 1 {
		t.Fatalf("claims = %+v", claims)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	locked, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	lockerDone := make(chan error, 1)
	go func() {
		lockerDone <- store.WithDiscordMessageSyncLock(ctx, destination.ID, request.ID, func(notification.DiscordMessageSyncRepository) error {
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return ctx.Err()
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("first transaction never acquired advisory lock")
	}
	result := make(chan notification.SendResult, 1)
	go func() { result <- transport.SendClaimed(ctx, claims[0]) }()
	for {
		var waiters int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if waiters > 0 {
			break
		}
		select {
		case <-result:
			t.Fatal("send escaped transaction lock")
		case <-ctx.Done():
			t.Fatal("send never waited for PostgreSQL lock")
		default:
			runtime.Gosched()
		}
	}
	if err := fixture.requests.Join(ctx, request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	// Join activity is deliberately NOT materialized. The original create
	// must itself read the joined state after waiting for the lock.
	released.Do(func() { close(release) })
	if err := <-lockerDone; err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if sent := <-result; sent.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("send outcome = %v", sent.Outcome)
	}
	call := discordSendNextCall(t, calls)
	discordSendAssertCall(t, call, http.MethodPost, "private-token", "", request.ID)
	if call.field("Players") != "2 players (unlimited)" || len(calls) != 0 {
		t.Fatal("create used snapshot read before PostgreSQL lock")
	}
	state, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || state.MessageID != "latest-message" || state.AppliedVersion != claims[0].Activity.Sequence {
		t.Fatalf("create marker = %+v, %v", state, err)
	}
}
