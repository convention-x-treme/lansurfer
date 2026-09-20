package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	DefaultPollInterval   = 2 * time.Second
	DefaultClaimLease     = 60 * time.Second
	DefaultSendTimeout    = 20 * time.Second
	DefaultDiscoveryBatch = 200
	DefaultClaimBatch     = 20
)

// Worker discovers new activity, materializes work/delivery rows, claims due
// deliveries, and attempts them against registered transports on a fixed
// poll interval. It runs entirely against WorkerRepository and never touches
// gamerequest's tables directly.
type Worker struct {
	repo             WorkerRepository
	transports       []Transport
	transportsByName map[string]Transport
	logger           *slog.Logger
	now              func() time.Time
	jitter           func(time.Duration) time.Duration
	pollInterval     time.Duration
	claimLease       time.Duration
	sendTimeout      time.Duration
	discoveryBatch   int
	claimBatch       int
	done             chan struct{}
}

func NewWorker(repo WorkerRepository, transports []Transport, logger *slog.Logger, now func() time.Time) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	byName := make(map[string]Transport, len(transports))
	for _, transport := range transports {
		byName[transport.Name()] = transport
	}
	return &Worker{
		repo: repo, transports: transports, transportsByName: byName, logger: logger, now: now, jitter: applyJitter,
		pollInterval: DefaultPollInterval, claimLease: DefaultClaimLease, sendTimeout: DefaultSendTimeout,
		discoveryBatch: DefaultDiscoveryBatch, claimBatch: DefaultClaimBatch, done: make(chan struct{}),
	}
}

// Run polls until ctx is done, then closes the channel Wait blocks on. It
// ties every database operation to ctx, so application shutdown (which
// cancels ctx) stops new work without requiring a separate stop signal.
func (w *Worker) Run(ctx context.Context) {
	defer close(w.done)
	w.Tick(ctx)
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Wait blocks until Run has returned (so no further database operations are
// in flight) or ctx is done, whichever comes first. The composition root
// uses a bounded ctx during shutdown so it never waits forever, mirroring
// IGDBSyncService.Wait.
func (w *Worker) Wait(ctx context.Context) {
	select {
	case <-w.done:
	case <-ctx.Done():
	}
}

// Tick runs one discovery-and-delivery pass. It is exported so integration
// tests can drive the worker deterministically instead of waiting on its
// internal poll ticker.
func (w *Worker) Tick(ctx context.Context) {
	w.materialize(ctx)
	w.processDue(ctx)
}

func (w *Worker) materialize(ctx context.Context) {
	activities, err := w.repo.DiscoverActivity(ctx, w.discoveryBatch)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("discover new notification activity", "error", err)
		}
		return
	}
	for _, activity := range activities {
		var targets []Target
		for _, transport := range w.transports {
			transportTargets, err := transport.Targets(ctx, activity)
			if err != nil {
				w.logger.Error("resolve transport targets", "transport", transport.Name(), "error", err)
				continue
			}
			targets = append(targets, transportTargets...)
		}
		if err := w.repo.MaterializeWork(ctx, activity, targets); err != nil && ctx.Err() == nil {
			w.logger.Error("materialize notification work", "activity_sequence", activity.Sequence, "error", err)
		}
	}
}

func (w *Worker) processDue(ctx context.Context) {
	claimed, err := w.repo.ClaimDueDeliveries(ctx, w.claimBatch, w.now().Add(w.claimLease))
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("claim due notification deliveries", "error", err)
		}
		return
	}
	for _, delivery := range claimed {
		w.processOne(ctx, delivery)
	}
}

func (w *Worker) processOne(ctx context.Context, delivery ClaimedDelivery) {
	transport, ok := w.transportsByName[delivery.Target.Transport]
	if !ok {
		w.recordOutcome(ctx, delivery, SendResult{
			Outcome: OutcomePermanent, Err: fmt.Errorf("transport %q is not configured", delivery.Target.Transport),
		})
		return
	}

	// Eligibility is re-checked before every send, including retries: a
	// target materialized as eligible can become ineligible later (a
	// subscription removed, a user leaving the request), and old work must
	// never be sent to a target it no longer applies to. Suppression is
	// worker policy computed from a fresh Targets() call, not something the
	// transport reports from Send.
	targets, err := transport.Targets(ctx, delivery.Activity)
	if err != nil {
		w.logger.Error("recheck transport eligibility", "transport", transport.Name(), "error", err)
	} else if !containsTarget(targets, delivery.Target) {
		if recordErr := w.repo.RecordSuppressed(ctx, delivery.DeliveryID, ErrNoLongerEligible.Error()); recordErr != nil && ctx.Err() == nil {
			w.logger.Error("record notification delivery suppressed", "delivery_id", delivery.DeliveryID, "error", recordErr)
		}
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
	result := transport.Send(sendCtx, delivery.Activity, delivery.Target)
	cancel()
	w.recordOutcome(ctx, delivery, result)
}

func (w *Worker) recordOutcome(ctx context.Context, delivery ClaimedDelivery, result SendResult) {
	var recordErr error
	switch result.Outcome {
	case OutcomeSucceeded:
		recordErr = w.repo.RecordSuccess(ctx, delivery.DeliveryID)
	case OutcomePermanent:
		recordErr = w.repo.RecordPermanent(ctx, delivery.DeliveryID, errorMessage(result.Err))
	case OutcomeUncertain:
		recordErr = w.repo.RecordUncertain(ctx, delivery.DeliveryID, errorMessage(result.Err))
	default: // OutcomeRetryable
		attemptsMade := int(delivery.AttemptCount) + 1
		if attemptsMade >= MaxAutomaticAttempts {
			recordErr = w.repo.RecordExhausted(ctx, delivery.DeliveryID, errorMessage(result.Err))
		} else {
			delay := computeBackoff(attemptsMade, result.RetryAfter, w.jitter)
			recordErr = w.repo.RecordRetry(ctx, delivery.DeliveryID, w.now().Add(delay), errorMessage(result.Err))
		}
	}
	if recordErr != nil && ctx.Err() == nil {
		w.logger.Error("record notification delivery outcome", "delivery_id", delivery.DeliveryID, "error", recordErr)
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
