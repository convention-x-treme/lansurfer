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
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
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
	if !web.SecureContextStatus(cfg.BaseURL) {
		logger.Warn(
			"LANSURFER_BASE_URL is not a secure context: normal LANsurfer operation is unaffected, "+
				"but PWA installation and Web Push require HTTPS (or a loopback/localhost origin)",
			"base_url", cfg.BaseURL,
		)
	}

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

	notificationStore := notificationpostgres.NewStore(pool)
	if err := notificationStore.InitializeBaseline(ctx); err != nil {
		return fmt.Errorf("initialize notification baseline: %w", err)
	}

	userService := user.NewService(userpostgres.NewStore(pool))
	sessionService := auth.NewSessionService(authpostgres.NewSessionStore(pool), userService)
	flowStore := authpostgres.NewFlowStore(pool)
	flowService := auth.NewFlowService(flowStore)
	identityStore := authpostgres.NewIdentityStore(pool)
	resolutionService := auth.NewResolutionService(
		identityStore, flowService, sessionService, userService, authpostgres.NewPendingIdentityAttacher(pool),
	)
	accountRegistration := NewAccountRegistration(pool, sessionService)

	var providers []auth.ConfiguredProvider
	if cfg.OIDC != nil {
		discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, oidcDiscoveryTimeout)
		oidcProvider, discoverErr := auth.DiscoverOIDCProvider(discoveryCtx, auth.OIDCConfig{
			IssuerURL:     cfg.OIDC.Issuer,
			ClientID:      cfg.OIDC.ClientID,
			ClientSecret:  cfg.OIDC.ClientSecret,
			RedirectURL:   cfg.BaseURL + "/auth/callback",
			GameNickClaim: cfg.OIDC.GameNickClaim,
		})
		cancelDiscovery()
		if discoverErr != nil {
			return fmt.Errorf("initialize OIDC: %w", discoverErr)
		}
		providers = append(providers, auth.ConfiguredProvider{DisplayName: cfg.OIDC.DisplayName, Adapter: oidcProvider})
	}
	if cfg.Discord != nil {
		discordProvider := auth.NewDiscordProvider(auth.DiscordConfig{
			ClientID: cfg.Discord.ClientID, ClientSecret: cfg.Discord.ClientSecret,
			RedirectURL: cfg.BaseURL + "/auth/discord/callback",
		})
		providers = append(providers, auth.ConfiguredProvider{DisplayName: "Discord", Adapter: discordProvider})
	}
	oauthService := auth.NewOAuthService(providers, flowService, resolutionService)

	gameStore := gamepostgres.NewStore(pool)
	gameService := game.NewService(gameStore)
	realtime := sse.NewBroker()
	gameRequests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), gameService, realtime)
	gameRequestQueries := gamerequestquery.NewStore(pool)
	notificationWorker := newNotificationWorker(notificationStore, logger)
	igdbSyncService := newIGDBSync(ctx, cfg.Twitch, gameStore, logger)
	gameDiscovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, igdbSyncService)

	server := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: web.NewRouter(web.RouterDependencies{
			Ready:               postgres.ReadyCheck(pool),
			Authentication:      oauthService,
			Sessions:            sessionService,
			PendingIdentities:   flowService,
			AccountRegistration: accountRegistration,
			GameNickClaims:      userService,
			Identities:          identityStore,
			IdentityUnlinker:    identityStore,
			Games:               gameService,
			Discovery:           gameDiscovery,
			GameResolver:        gameDiscovery,
			GameRequests:        gameRequests,
			RequestQueries:      gameRequestQueries,
			Realtime:            realtime,
			ProviderSync:        igdbSyncService,
			SecureCookies:       strings.HasPrefix(cfg.BaseURL, "https://"),
			Logger:              logger,
			TrustedProxies:      cfg.TrustedProxies,
			InstanceName:        cfg.InstanceName,
			BaseURL:             cfg.BaseURL,
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
	go notificationWorker.Run(ctx)
	go runDailyIGDBSync(ctx, igdbSyncService, igdbSyncInterval)
	go runPeriodicFlowCleanup(ctx, flowStore, authFlowCleanupInterval, logger)
	// Registered after pool.Close() above so it runs first on the way out:
	// an in-flight sync's transaction must finish (or be abandoned within the
	// bound) before the pool it depends on is closed.
	defer func() {
		waitCtx, cancelWait := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelWait()
		igdbSyncService.Wait(waitCtx)
	}()
	// Registered after the igdb wait above so it runs even earlier on the way
	// out (defers are LIFO): the notification worker's in-flight database
	// operations must finish (or be abandoned within the bound) before the
	// pool they depend on is closed.
	defer func() {
		waitCtx, cancelWait := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelWait()
		notificationWorker.Wait(waitCtx)
	}()

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
