package app

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type countingSyncStarter struct {
	calls chan struct{}
	count atomic.Int64
}

func TestNewIGDBSyncRequiresIGDBEnablement(t *testing.T) {
	if _, ok := newIGDBSync(t.Context(), igdbCredentials{}, nil, slog.Default()).(disabledIGDBSync); !ok {
		t.Fatal("Twitch authentication unexpectedly enabled IGDB synchronization")
	}
	if _, ok := newIGDBSync(t.Context(), igdbCredentials{Enabled: true, ClientID: "client", ClientSecret: "secret"}, nil, slog.Default()).(appBoundIGDBSync); !ok {
		t.Fatal("IGDB enablement did not create synchronization service")
	}
}

func (c *countingSyncStarter) TryStart(context.Context) bool {
	c.count.Add(1)
	select {
	case c.calls <- struct{}{}:
	default:
	}
	return true
}

func TestRunDailyIGDBSyncStartsImmediatelyAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	fake := &countingSyncStarter{calls: make(chan struct{}, 4)}
	done := make(chan struct{})
	go func() {
		runDailyIGDBSync(ctx, fake, 5*time.Millisecond)
		close(done)
	}()
	<-fake.calls
	cancel()
	<-done
	count := fake.count.Load()
	time.Sleep(15 * time.Millisecond)
	if fake.count.Load() != count {
		t.Fatal("sync continued after cancellation")
	}
}
