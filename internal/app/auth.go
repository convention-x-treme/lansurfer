package app

import (
	"context"
	"log/slog"
	"time"
)

// authFlowCleanupInterval keeps auth_flows from growing unbounded without
// adding a write to the hot login/link/onboarding path (flows expire after
// 10 minutes; see auth.authFlowLifetime). A much longer interval still
// bounds how many stale rows can accumulate between sweeps.
const authFlowCleanupInterval = 15 * time.Minute

// flowCleaner deletes expired auth_flows rows. It is satisfied by
// *authpostgres.FlowStore.
type flowCleaner interface {
	CleanupExpired(context.Context, time.Time) error
}

// runPeriodicFlowCleanup sweeps expired auth flows on a fixed interval until
// ctx is cancelled, instead of inline on every flow creation. A failed sweep
// is logged and retried on the next tick rather than treated as fatal: a
// missed cleanup only delays reclaiming already-expired rows and never
// affects flow correctness (expiry is still enforced at read time).
func runPeriodicFlowCleanup(ctx context.Context, cleaner flowCleaner, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cleaner.CleanupExpired(ctx, time.Now()); err != nil {
				logger.Error("clean expired auth flows", "error", err)
			}
		}
	}
}
