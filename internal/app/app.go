package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	"github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/sse"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/convention-x-treme/lansurfer/internal/web"
)

const (
	databaseStartupTimeout = 60 * time.Second
	migrationTimeout       = 5 * time.Minute
	oidcDiscoveryTimeout   = 30 * time.Second
	shutdownTimeout        = 10 * time.Second
)

// Run constructs and runs the LANsurfer application until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := postgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = postgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	userService := user.NewService(userpostgres.NewStore(pool))
	var oidcProvider auth.Provider
	if cfg.OIDC != nil {
		discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, oidcDiscoveryTimeout)
		oidcProvider, err = auth.DiscoverOIDCProvider(discoveryCtx, auth.OIDCConfig{
			IssuerURL:    cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.BaseURL + "/auth/callback",
		})
		cancelDiscovery()
		if err != nil {
			return fmt.Errorf("initialize OIDC: %w", err)
		}
	}
	authService := auth.NewService(
		oidcProvider,
		authpostgres.NewIdentityStore(pool),
		authpostgres.NewSessionStore(pool),
		userService,
	)
	gameStore := gamepostgres.NewStore(pool)
	gameService := game.NewService(gameStore)
	realtime := sse.NewBroker()
	gameRequests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), gameService, realtime)
	gameRequestQueries := gamerequestquery.NewStore(pool)
	igdbSyncService := newIGDBSync(ctx, cfg.IGDB, gameStore, logger)
	gameDiscovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, igdbSyncService)

	server := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: web.NewRouter(web.RouterDependencies{
			Ready:          postgres.ReadyCheck(pool),
			Authentication: authService,
			Games:          gameService,
			Discovery:      gameDiscovery,
			GameResolver:   gameDiscovery,
			GameRequests:   gameRequests,
			RequestQueries: gameRequestQueries,
			Realtime:       realtime,
			ProviderSync:   igdbSyncService,
			SecureCookies:  strings.HasPrefix(cfg.BaseURL, "https://"),
			Logger:         logger,
			TrustedProxies: cfg.TrustedProxies,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "address", cfg.ListenAddr)
		serverErrors <- server.ListenAndServe()
	}()
	go runDailyIGDBSync(ctx, igdbSyncService, igdbSyncInterval)

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down HTTP server")
	}
	realtime.Close()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	return nil
}
