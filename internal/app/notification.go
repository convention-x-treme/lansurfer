package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
)

// newNotificationWorker ties the worker to the caller's root context by
// convention (the caller starts it with `go worker.Run(ctx)` using the
// application's own ctx), matching appBoundIGDBSync's rationale: a
// background worker must keep running, and be cancellable only by
// application shutdown, independent of any single request.
func newNotificationWorker(store *notificationpostgres.Store, logger *slog.Logger) *notification.Worker {
	return notification.NewWorker(store, notificationTransports(), logger, time.Now)
}

// notificationTransports is the single place every notification.Transport
// this application registers is listed. newNotificationWorker and the CLI
// diagnostics/retry helpers below all call this rather than passing nil
// directly, so a transport added here reaches manual retry too instead of
// leaving it permanently unable to find the transport it needs.
//
// E1 registers none. E2/E3 append their transports here as they land.
func notificationTransports() []notification.Transport {
	return nil
}

// ListNotificationProblems opens its own standalone database connection and
// reports every exhausted, permanent, or uncertain delivery for operator
// diagnosis, following the same one-shot pattern as ListUsers.
func ListNotificationProblems(ctx context.Context, cfg config.Config, logger *slog.Logger) ([]notification.ProblemDelivery, error) {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}

	service := notification.NewService(notificationpostgres.NewStore(pool), notificationTransports())
	problems, err := service.ListProblems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notification problems: %w", err)
	}
	return problems, nil
}

// RetryNotificationDelivery reopens one exhausted or permanent delivery for
// a fresh automatic retry budget, after re-checking present-day eligibility.
func RetryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, false)
}

// RetryUncertainNotificationDelivery reopens one uncertain delivery despite
// the possibility of a duplicate externally visible effect. It is a
// distinct action from RetryNotificationDelivery.
func RetryUncertainNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, true)
}

func retryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger, uncertain bool) error {
	id, err := strconv.ParseInt(deliveryID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse delivery ID: %w", err)
	}

	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	service := notification.NewService(notificationpostgres.NewStore(pool), notificationTransports())
	if uncertain {
		return service.ManualRetryUncertain(ctx, id)
	}
	return service.ManualRetry(ctx, id)
}
