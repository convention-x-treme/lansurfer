package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/game/igdb"
)

// igdbSyncInterval is the daily background refresh cadence described in the
// IGDB discovery design.
const igdbSyncInterval = 24 * time.Hour

// igdbSync is satisfied by *game.IGDBSyncService and by disabledIGDBSync. It
// is also the narrower interface consumed by both the daily scheduler and the
// admin manual-sync surface.
type igdbSync interface {
	Status(context.Context) (game.ProviderStatus, error)
	TryStart(context.Context) bool
	// Wait blocks until any in-flight sync has finished, or ctx is done.
	Wait(context.Context)
}

// disabledIGDBSync is wired in whenever IGDB credentials are not configured.
// It never calls IGDB and never affects application readiness.
type disabledIGDBSync struct{}

func (disabledIGDBSync) Status(context.Context) (game.ProviderStatus, error) {
	return game.ProviderStatus{State: game.ProviderDisabled}, nil
}

func (disabledIGDBSync) TryStart(context.Context) bool { return false }

func (disabledIGDBSync) Wait(context.Context) {}

// appBoundIGDBSync ties every sync attempt to the application's own root
// context rather than to whatever context a caller happens to pass in. This
// matters for the admin "Sync now" HTTP handler: an HTTP request's context is
// cancelled as soon as the handler returns, but a sync must keep running
// (and be cancellable by application shutdown instead) after the response
// has been sent.
type appBoundIGDBSync struct {
	ctx    context.Context
	syncer igdbSync
}

func (a appBoundIGDBSync) Status(ctx context.Context) (game.ProviderStatus, error) {
	return a.syncer.Status(ctx)
}

func (a appBoundIGDBSync) TryStart(context.Context) bool {
	return a.syncer.TryStart(a.ctx)
}

func (a appBoundIGDBSync) Wait(ctx context.Context) {
	a.syncer.Wait(ctx)
}

// igdbCredentials is the narrow slice of shared Twitch application
// credentials that IGDB synchronization needs. It exists so this game-module
// constructor does not depend on the auth-flavored config.Twitch type.
type igdbCredentials struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
}

// newIGDBSync builds the sync service wired to the application's root
// context when IGDB credentials are configured, or a disabled adapter
// otherwise. It never makes a network call.
func newIGDBSync(ctx context.Context, cfg igdbCredentials, repo game.ProviderIndexRepository, logger *slog.Logger) igdbSync {
	if !cfg.Enabled {
		return disabledIGDBSync{}
	}
	client := igdb.NewClient(cfg.ClientID, cfg.ClientSecret)
	return appBoundIGDBSync{ctx: ctx, syncer: game.NewIGDBSyncService(client, repo, time.Now, logger)}
}

// syncStarter is the minimal capability the daily scheduler needs.
type syncStarter interface {
	TryStart(context.Context) bool
}

// runDailyIGDBSync starts an immediate sync attempt and then one more every
// interval until ctx is cancelled. It relies on syncer's own single-flight
// guard, so an attempt that is still running when the ticker fires is simply
// skipped rather than queued or run concurrently.
func runDailyIGDBSync(ctx context.Context, syncer syncStarter, interval time.Duration) {
	syncer.TryStart(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncer.TryStart(ctx)
		}
	}
}
