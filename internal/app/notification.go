package app

import (
	"log/slog"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

// newNotificationWorker ties the worker to the caller's root context by
// convention (the caller starts it with `go worker.Run(ctx)` using the
// application's own ctx), matching appBoundIGDBSync's rationale: a
// background worker must keep running, and be cancellable only by
// application shutdown, independent of any single request.
//
// No transports are registered yet: E1 has none. E2/E3 append their
// transports to this slice as they land.
func newNotificationWorker(store *notificationpostgres.Store, logger *slog.Logger) *notification.Worker {
	return notification.NewWorker(store, nil, logger, time.Now)
}
