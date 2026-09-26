package notification

import (
	"context"
	"errors"
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

type materializationRepo struct {
	activity Activity
	work     [][]Target
}

func (r *materializationRepo) DiscoverActivity(context.Context, int) ([]Activity, error) {
	if len(r.work) != 0 {
		return nil, nil
	}
	return []Activity{r.activity}, nil
}
func (r *materializationRepo) MaterializeWork(_ context.Context, _ Activity, targets []Target) error {
	r.work = append(r.work, append([]Target(nil), targets...))
	return nil
}
func (r *materializationRepo) ClaimDueDeliveries(context.Context, int, time.Time) ([]ClaimedDelivery, error) {
	return nil, nil
}
func (r *materializationRepo) RecordSuccess(context.Context, int64) error { return nil }
func (r *materializationRepo) RecordRetry(context.Context, int64, time.Time, string) error {
	return nil
}
func (r *materializationRepo) RecordExhausted(context.Context, int64, string) error  { return nil }
func (r *materializationRepo) RecordPermanent(context.Context, int64, string) error  { return nil }
func (r *materializationRepo) RecordSuppressed(context.Context, int64, string) error { return nil }
func (r *materializationRepo) RecordUncertain(context.Context, int64, string) error  { return nil }

type materializationTransport struct {
	name    string
	targets []Target
	fail    bool
	calls   int
}

func (t *materializationTransport) Name() string { return t.name }
func (t *materializationTransport) Targets(context.Context, Activity) ([]Target, error) {
	t.calls++
	if t.fail {
		return nil, errors.New("temporary target read failure")
	}
	return t.targets, nil
}
func (t *materializationTransport) Send(context.Context, Activity, Target) SendResult {
	panic("materialization test must not send")
}

func TestWorkerMaterializeRetriesAllTransports(t *testing.T) {
	repo := &materializationRepo{activity: Activity{Sequence: 1}}
	a := &materializationTransport{name: "push", targets: []Target{{Transport: "push", Key: "a"}}, fail: true}
	b := &materializationTransport{name: "discord", targets: []Target{{Transport: "discord", Key: "b"}}}
	worker := NewWorker(repo, []Transport{a, b}, nil, time.Now)
	worker.Tick(t.Context())
	if len(repo.work) != 0 || a.calls != 1 || b.calls != 1 {
		t.Fatalf("failed resolution materialized partial work: work=%v calls=%d/%d", repo.work, a.calls, b.calls)
	}
	a.fail = false
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	if len(repo.work) != 1 || len(repo.work[0]) != 2 ||
		repo.work[0][0] != a.targets[0] || repo.work[0][1] != b.targets[0] {
		t.Fatalf("retry work = %v, want one complete work row", repo.work)
	}
}

func TestWorkerMaterializeRetriesSingleTransport(t *testing.T) {
	repo := &materializationRepo{activity: Activity{Sequence: 1}}
	transport := &materializationTransport{name: "push", targets: []Target{{Transport: "push", Key: "a"}}, fail: true}
	worker := NewWorker(repo, []Transport{transport}, nil, time.Now)
	worker.Tick(t.Context())
	if len(repo.work) != 0 {
		t.Fatalf("failed single transport created work: %v", repo.work)
	}
	transport.fail = false
	worker.Tick(t.Context())
	if len(repo.work) != 1 || len(repo.work[0]) != 1 || repo.work[0][0] != transport.targets[0] {
		t.Fatalf("single transport retry work = %v", repo.work)
	}
}

func TestWorkerMaterializeZeroTargets(t *testing.T) {
	repo := &materializationRepo{activity: Activity{Sequence: 1}}
	worker := NewWorker(repo, []Transport{&materializationTransport{name: "push"}}, nil, time.Now)
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	if len(repo.work) != 1 || len(repo.work[0]) != 0 {
		t.Fatalf("zero-target work = %v, want one row and no deliveries", repo.work)
	}
}

type eligibilityRepo struct {
	materializationRepo
	delivery              ClaimedDelivery
	retryAt               time.Time
	retryID               int64
	exhausted             int64
	recorded              string
	suppressed, succeeded int
}

func (r *eligibilityRepo) ClaimDueDeliveries(context.Context, int, time.Time) ([]ClaimedDelivery, error) {
	return []ClaimedDelivery{r.delivery}, nil
}
func (r *eligibilityRepo) RecordRetry(_ context.Context, id int64, when time.Time, reason string) error {
	r.retryID, r.retryAt, r.recorded = id, when, reason
	return nil
}
func (r *eligibilityRepo) RecordExhausted(_ context.Context, id int64, reason string) error {
	r.exhausted, r.recorded = id, reason
	return nil
}
func (r *eligibilityRepo) RecordSuppressed(context.Context, int64, string) error {
	r.suppressed++
	return nil
}
func (r *eligibilityRepo) RecordSuccess(context.Context, int64) error {
	r.succeeded++
	return nil
}

type eligibilityTransport struct {
	materializationTransport
	sendCalls int
}

func (t *eligibilityTransport) Send(context.Context, Activity, Target) SendResult {
	t.sendCalls++
	return SendResult{Outcome: OutcomeSucceeded}
}

type eligibilityClaimedTransport struct{ *eligibilityTransport }

func (t *eligibilityClaimedTransport) SendClaimed(context.Context, ClaimedDelivery) SendResult {
	t.sendCalls++
	return SendResult{Outcome: OutcomeSucceeded}
}

func TestWorkerEligibilityErrorRetriesWithoutSend(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "Send", true: "SendClaimed"}[claimed], func(t *testing.T) {
			now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			repo := &eligibilityRepo{delivery: ClaimedDelivery{DeliveryID: 42, Target: Target{Transport: "push", Key: "a"}}}
			transport := &eligibilityTransport{materializationTransport: materializationTransport{name: "push", fail: true}}
			var sender Transport = transport
			if claimed {
				sender = &eligibilityClaimedTransport{transport}
			}
			worker := NewWorker(repo, []Transport{sender}, nil, func() time.Time { return now })
			worker.jitter = func(d time.Duration) time.Duration { return d }
			worker.Tick(t.Context())
			if transport.sendCalls != 0 || repo.retryID != 42 || !repo.retryAt.Equal(now.Add(5*time.Second)) ||
				repo.suppressed != 0 || repo.succeeded != 0 {
				t.Fatalf("unknown eligibility: sends=%d retry=%d at=%v suppressed=%d succeeded=%d", transport.sendCalls, repo.retryID, repo.retryAt, repo.suppressed, repo.succeeded)
			}
		})
	}
}

func TestWorkerEligibilityErrorExhaustsWithoutSend(t *testing.T) {
	repo := &eligibilityRepo{delivery: ClaimedDelivery{
		DeliveryID: 42, AttemptCount: MaxAutomaticAttempts - 1, Target: Target{Transport: "push", Key: "a"},
	}}
	transport := &eligibilityTransport{materializationTransport: materializationTransport{name: "push", fail: true}}
	NewWorker(repo, []Transport{transport}, nil, time.Now).Tick(t.Context())
	if transport.sendCalls != 0 || repo.exhausted != 42 || repo.retryID != 0 || repo.suppressed != 0 || repo.succeeded != 0 {
		t.Fatalf("final unknown eligibility: sends=%d exhausted=%d retry=%d suppressed=%d succeeded=%d", transport.sendCalls, repo.exhausted, repo.retryID, repo.suppressed, repo.succeeded)
	}
}
