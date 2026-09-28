//go:build integration

package postgres

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

func TestDiscordWorkerSuppressesGenerationChangedAfterEligibilityCheck(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	old := discordSendConnect(t, store, key, "old-private-token")
	var calls atomic.Int32
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) { calls.Add(1) })
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Original")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	locked, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	lockerDone := make(chan error, 1)
	go func() {
		lockerDone <- store.WithDiscordMessageSyncLock(ctx, old.ID, request.ID, func(notification.DiscordMessageSyncRepository) error {
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
		t.Fatal("lock not acquired")
	}
	finished := make(chan struct{})
	go func() {
		newTestWorker(store, []notification.Transport{transport}).Tick(ctx)
		close(finished)
	}()
	for {
		var waiters int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if waiters > 0 {
			break
		}
		select {
		case <-finished:
			t.Fatal("worker escaped lock")
		case <-ctx.Done():
			t.Fatal("worker never reached lock")
		default:
			runtime.Gosched()
		}
	}
	if replacement := discordSendConnect(t, store, key, "new-private-token"); replacement.ID == old.ID {
		t.Fatal("replacement reused destination generation")
	}
	released.Do(func() { close(release) })
	if err := <-lockerDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("worker did not finish")
	}
	if calls.Load() != 0 {
		t.Fatalf("replaced generation received %d provider calls", calls.Load())
	}
	var status string
	if err := pool.QueryRow(t.Context(), `SELECT d.status FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1`, request.ID).Scan(&status); err != nil || status != "suppressed" {
		t.Fatalf("stale delivery status = %q, %v; want suppressed, not provider failure", status, err)
	}
}
