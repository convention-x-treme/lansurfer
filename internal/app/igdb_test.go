package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingSyncStarter struct {
	calls chan struct{}
	count atomic.Int64
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
