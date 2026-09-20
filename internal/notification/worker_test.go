package notification

import (
	"context"
	"testing"
	"time"
)

// blockingWorkerRepository blocks its first DiscoverActivity call until
// unblock is closed or ctx is done, so tests can control exactly when the
// Worker's first tick finishes.
type blockingWorkerRepository struct {
	unblock chan struct{}
}

func (r *blockingWorkerRepository) DiscoverActivity(ctx context.Context, limit int) ([]Activity, error) {
	select {
	case <-r.unblock:
	case <-ctx.Done():
	}
	return nil, ctx.Err()
}

func (r *blockingWorkerRepository) MaterializeWork(context.Context, Activity, []Target) error {
	return nil
}
func (r *blockingWorkerRepository) ClaimDueDeliveries(context.Context, int, time.Time) ([]ClaimedDelivery, error) {
	return nil, nil
}
func (r *blockingWorkerRepository) RecordSuccess(context.Context, int64) error { return nil }
func (r *blockingWorkerRepository) RecordRetry(context.Context, int64, time.Time, string) error {
	return nil
}
func (r *blockingWorkerRepository) RecordExhausted(context.Context, int64, string) error  { return nil }
func (r *blockingWorkerRepository) RecordPermanent(context.Context, int64, string) error  { return nil }
func (r *blockingWorkerRepository) RecordSuppressed(context.Context, int64, string) error { return nil }
func (r *blockingWorkerRepository) RecordUncertain(context.Context, int64, string) error  { return nil }

func TestWorkerWaitBlocksUntilRunReturnsAfterShutdown(t *testing.T) {
	repo := &blockingWorkerRepository{unblock: make(chan struct{})}
	worker := NewWorker(repo, nil, nil, time.Now)
	runCtx, cancelRun := context.WithCancel(context.Background())

	runReturned := make(chan struct{})
	go func() {
		worker.Run(runCtx)
		close(runReturned)
	}()

	// Run's first tick is blocked inside DiscoverActivity. A Wait bounded by
	// a short timeout must time out rather than observe Run finishing.
	earlyWaitCtx, cancelEarly := context.WithTimeout(t.Context(), 20*time.Millisecond)
	worker.Wait(earlyWaitCtx)
	cancelEarly()
	select {
	case <-runReturned:
		t.Fatal("Run returned before shutdown was requested and the first tick was released")
	default:
	}

	cancelRun()
	close(repo.unblock)

	waitCtx, cancelWait := context.WithTimeout(t.Context(), time.Second)
	defer cancelWait()
	worker.Wait(waitCtx)
	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Wait returned before Run had actually finished")
	}
}
