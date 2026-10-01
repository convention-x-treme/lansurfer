// Package web provides LANsurfer's HTTP delivery layer.
package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/a-h/templ"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/web/assets"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	"github.com/convention-x-treme/lansurfer/internal/web/handler"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	readinessTimeout = time.Second
	// requestTimeout is a safety boundary, not a latency target: it keeps a
	// blocked query or an exhausted connection pool from leaving the browser
	// waiting forever on a request it can never complete.
	requestTimeout = 15 * time.Second
)

// ReadyCheck reports whether dependencies required to serve traffic are ready.
type ReadyCheck func(context.Context) error

// Sessions resolves the current browser session for request authentication
// and revokes it on logout. Starting/completing OAuth flows is a separate
// narrow interface (handler.Authentication), since a different concrete type
// (*auth.OAuthService vs *auth.SessionService) backs each.
type Sessions interface {
	webmiddleware.Authenticator
	handler.Sessions
}

// Discovery combines Games' and the Game Request composer's separate narrow
// consuming interfaces (handler.GameDiscovery, handler.GameRequestDiscovery):
// one concrete *game.DiscoveryService backs both, and this composition root
// is where it is fine to say so explicitly, rather than widening either
// handler package's own narrower interface for a need only one of them has.
type Discovery interface {
	handler.GameDiscovery
	handler.GameRequestDiscovery
}

type RouterDependencies struct {
	Ready                ReadyCheck
	Authentication       handler.Authentication
	Sessions             Sessions
	PendingIdentities    handler.PendingIdentities
	AccountRegistration  handler.AccountRegistration
	GameNickClaims       handler.GameNickClaims
	AccountDeleter       handler.AccountDeleter
	Identities           handler.IdentityLookup
	IdentityUnlinker     handler.IdentityUnlinker
	Games                handler.GameAdministration
	ProviderSync         handler.ProviderSyncAdministration
	Discovery            Discovery
	GameResolver         handler.GameResolver
	GameRequests         handler.GameRequestCommands
	DiscordSetup         handler.DiscordDestinationSetup
	DiscordAuthorization handler.DiscordDestinationAuthorization
	DiscordConnector     handler.DiscordDestinationConnector
	DiscordLifecycle     handler.DiscordDestinationLifecycle
	DiscordStatus        handler.DiscordDestinationStatusReader
	RequestQueries       handler.GameRequestQueries
	// Landing feeds the public home page; nil renders it without Game tiles.
	Landing handler.LandingQueries
	// NavigationQueries and NavigationCatalog back the shared sidebar every
	// authenticated screen renders. The same concrete read model and
	// discovery service behind RequestQueries/Discovery satisfy them; saying
	// so is this composition root's job, not a reason to widen either
	// handler's own narrower interface.
	NavigationQueries handler.NavigationQueries
	NavigationCatalog handler.NavigationCatalog
	Chat              handler.GameRequestChat
	Realtime          handler.GameRequestEvents
	// RealtimeChat gates chat signals on the realtime stream; nil drops them.
	RealtimeChat  handler.ChatAccess
	Subscriptions handler.SubscriptionManager
	// PushDevices is the same concrete *notification.SubscriptionService as
	// Subscriptions, exposed through the handler.PushDeviceRemover interface
	// Logout needs. Two fields (rather than widening either handler
	// package's narrower interface) because AuthenticationHandler and
	// NotificationSettingsHandler each declare their own narrow consuming
	// interface, and this composition root is where it is fine to say one
	// concrete type backs both.
	PushDevices handler.PushDeviceRemover
	// VAPIDPublicKey is config.Config.WebPush.PublicKey, rendered into the
	// notification settings page for the browser's Push subscription call.
	VAPIDPublicKey string
	// WebPushConfigured is config.Config.WebPush != nil: whether Web Push is
	// configured at all on this instance, independent of Subscriptions being
	// wired (which also requires DataEncryptionKey).
	WebPushConfigured bool
	// Covers serves public same-origin IGDB cover images.
	Covers handler.MediaImages
	// Avatars serves public same-origin selected provider avatars.
	Avatars handler.MediaImages
	// ViewerAvatars resolves the signed-in viewer's selected avatar key.
	ViewerAvatars handler.ViewerAvatars
	// AvatarSelector changes the signed-in user's avatar source.
	AvatarSelector handler.AvatarSelector
	SecureCookies  bool
	Logger         *slog.Logger
	TrustedProxies []netip.Prefix
	// InstanceName is the raw, already-trimmed config.Config.InstanceName
	// value. Empty means unset; NewRouter resolves the display fallback.
	InstanceName string
	// Legal is the operator's legal identity from config.Config.Legal,
	// rendered by the footer and the Terms/Privacy pages.
	Legal webmiddleware.LegalIdentity
	// BaseURL is config.Config.BaseURL, used only by /admin/pwa diagnostics
	// (manifest/worker URLs and the expected secure-context status).
	BaseURL string
}

// NewRouter constructs the HTTP router.
func NewRouter(dependencies RouterDependencies) http.Handler {
	brand := branding.Default()
	// resolvedInstanceName is what a surface renders when it always needs a
	// name (the manifest, the administration diagnostics). The layouts need
	// the opposite: whether one was configured at all, which is why the
	// middleware below carries the raw value rather than this fallback.
	resolvedInstanceName := dependencies.InstanceName
	if resolvedInstanceName == "" {
		resolvedInstanceName = brand.Name
	}
	authenticationHandler := handler.NewAuthentication(
		dependencies.Authentication, dependencies.Sessions, brand, dependencies.SecureCookies,
		dependencies.PushDevices,
	)
	onboardingHandler := handler.NewOnboarding(
		dependencies.PendingIdentities, dependencies.AccountRegistration, dependencies.GameNickClaims,
		dependencies.Identities, dependencies.Authentication, brand, dependencies.SecureCookies,
	)
	discordDeliveryConfigured := dependencies.DiscordSetup != nil && dependencies.DiscordAuthorization != nil &&
		dependencies.DiscordConnector != nil && dependencies.DiscordLifecycle != nil && dependencies.DiscordStatus != nil
	navigation := handler.NewNavigation(dependencies.NavigationQueries, dependencies.NavigationCatalog, dependencies.ViewerAvatars, discordDeliveryConfigured)
	legalHandler := handler.NewLegal(navigation, brand)
	accountHandler := handler.NewAccount(dependencies.Identities, dependencies.IdentityUnlinker, dependencies.AvatarSelector, dependencies.GameNickClaims, dependencies.AccountDeleter, dependencies.Authentication, navigation, brand, dependencies.SecureCookies)
	administrationHandler := handler.NewAdministration(
		dependencies.Games, dependencies.ProviderSync, navigation, brand,
		resolvedInstanceName, dependencies.BaseURL, SecureContextStatus(dependencies.BaseURL),
	)
	gamesHandler := handler.NewGames(dependencies.Discovery, navigation, brand)
	gameRequestsHandler := handler.NewGameRequests(dependencies.GameRequests, dependencies.RequestQueries, dependencies.GameResolver, dependencies.Discovery, navigation, brand)
	var discordDeliveryHandler *handler.DiscordDelivery
	if discordDeliveryConfigured {
		discordDeliveryHandler = handler.NewDiscordDelivery(
			dependencies.DiscordSetup, dependencies.DiscordAuthorization, dependencies.DiscordConnector,
			dependencies.DiscordLifecycle, dependencies.DiscordStatus, navigation, brand, dependencies.SecureCookies,
		)
	}
	realtimeHandler := handler.NewRealtime(dependencies.Realtime, dependencies.RealtimeChat)
	notificationSettingsHandler := handler.NewNotificationSettings(
		dependencies.Subscriptions, dependencies.VAPIDPublicKey,
		dependencies.WebPushConfigured, brand, dependencies.SecureCookies, navigation,
	)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	// RequestTimeout wraps RequestLogging so the logger sees the context the
	// timeout derived, and can report a timed-out request as such instead of
	// as a completed one.
	router.Use(webmiddleware.RequestTimeout(requestTimeout))
	router.Use(webmiddleware.RequestLogging(dependencies.Logger, dependencies.TrustedProxies))
	router.Use(middleware.Recoverer)
	router.Use(webmiddleware.SecurityHeaders(dependencies.SecureCookies))
	router.Use(webmiddleware.CSRF(dependencies.SecureCookies))
	router.Use(webmiddleware.Theme)
	// The raw configured value, not the brand fallback: an unset instance must
	// reach the layouts as empty, or the secondary instance label renders the
	// brand name a second time beside the wordmark on every screen.
	router.Use(webmiddleware.Instance(dependencies.InstanceName))
	router.Use(webmiddleware.Legal(dependencies.Legal))

	router.Handle("/assets/*", http.StripPrefix("/assets/", assets.Handler()))
	// Covers are public like /assets/: an anonymous page may show them, and
	// the handler never touches the session.
	media := handler.NewMedia(dependencies.Covers, dependencies.Avatars)
	router.Get("/media/igdb/{size}/{imageID}", media.IGDBCover)
	router.Head("/media/igdb/{size}/{imageID}", media.IGDBCover)
	router.Get("/media/avatar/{avatarKey}", media.Avatar)
	router.Head("/media/avatar/{avatarKey}", media.Avatar)
	manifest := manifestHandler(brand, resolvedInstanceName)
	router.Get("/manifest.webmanifest", manifest)
	router.Head("/manifest.webmanifest", manifest)
	serviceWorker, err := serviceWorkerHandler()
	if err != nil {
		panic("read compiled service worker: " + err.Error())
	}
	router.Get("/service-worker.js", serviceWorker)
	router.Head("/service-worker.js", serviceWorker)
	router.Group(func(authenticated chi.Router) {
		authenticated.Use(webmiddleware.Authentication(dependencies.Sessions, dependencies.SecureCookies))
		authenticated.Get("/", rootHandler(brand, dependencies.Landing))
		// Public: no RequireAuthentication, but the session is resolved so a
		// signed-in player keeps the application shell.
		authenticated.Get("/terms", legalHandler.Terms)
		authenticated.Get("/privacy", legalHandler.Privacy)
		authenticated.Get("/auth/login", authenticationHandler.Login)
		authenticated.Get("/auth/oidc", authenticationHandler.BeginFor("oidc"))
		authenticated.Get("/auth/discord", authenticationHandler.BeginFor("discord"))
		authenticated.Get("/auth/twitch", authenticationHandler.BeginFor("twitch"))
		authenticated.Get("/auth/callback", authenticationHandler.CallbackFor("oidc"))
		authenticated.Get("/auth/discord/callback", authenticationHandler.CallbackFor("discord"))
		authenticated.Get("/auth/twitch/callback", authenticationHandler.CallbackFor("twitch"))
		// Logout is registered here rather than on the top-level router, like
		// every other /auth/* route, so webmiddleware.Authentication resolves
		// CurrentUser before the handler runs. It needs the current account's
		// ID to remove exactly that account's device subscription on sign-out
		// (see AuthenticationHandler.removePushDevice); it does not require
		// authentication (RequireAuthentication is never applied here), so an
		// already-signed-out request still succeeds as a no-op.
		authenticated.Post("/auth/logout", authenticationHandler.Logout)
		authenticated.Route("/onboarding", func(onboarding chi.Router) {
			onboarding.Get("/", onboardingHandler.ShowExisting)
			onboarding.Post("/", onboardingHandler.SubmitExisting)
			onboarding.Get("/{pendingID}", onboardingHandler.ShowPending)
			onboarding.Post("/{pendingID}", onboardingHandler.SubmitPending)
		})
		authenticated.Route("/account", func(account chi.Router) {
			account.Use(handler.RequireAuthentication(dependencies.SecureCookies), handler.RequireGameNick)
			account.Get("/", accountHandler.Show)
			account.Post("/game-nick", accountHandler.RenameGameNick)
			account.Post("/link/oidc", authenticationHandler.LinkFor("oidc"))
			account.Post("/link/discord", authenticationHandler.LinkFor("discord"))
			account.Post("/link/twitch", authenticationHandler.LinkFor("twitch"))
			account.Post("/identities/{identityID}/unlink", accountHandler.Unlink)
			account.Post("/avatar", accountHandler.SelectAvatar)
			account.Post("/delete", accountHandler.Delete)
			account.Route("/notifications", func(notifications chi.Router) {
				notifications.Get("/", notificationSettingsHandler.Show)
				if dependencies.Subscriptions != nil {
					notifications.Post("/subscribe", notificationSettingsHandler.Subscribe)
					notifications.Post("/disable", notificationSettingsHandler.Disable)
					notifications.Post("/resume", notificationSettingsHandler.Resume)
					notifications.Post("/audience", notificationSettingsHandler.SetAudience)
					notifications.Post("/reconcile", notificationSettingsHandler.ReconcileCurrentDevice)
					notifications.Post("/devices/{subscriptionID}/remove", notificationSettingsHandler.RemoveDevice)
				}
			})
		})
		if dependencies.Discovery != nil {
			authenticated.Route("/games", func(games chi.Router) {
				games.Use(handler.RequireAuthentication(dependencies.SecureCookies), handler.RequireGameNick)
				games.Get("/", gamesHandler.Browse)
				games.Get("/mine", gamesHandler.MyGames)
				games.Post("/providers/{provider}/{externalID}/follow", gamesHandler.FollowProvider)
				games.Post("/{gameID}/follow", gamesHandler.Follow)
				games.Post("/{gameID}/unfollow", gamesHandler.Unfollow)
			})
		}
		if dependencies.GameRequests != nil && dependencies.RequestQueries != nil {
			authenticated.Route("/game-requests", func(requests chi.Router) {
				requests.Use(handler.RequireAuthentication(dependencies.SecureCookies), handler.RequireGameNick)
				requests.Get("/", gameRequestsHandler.List)
				requests.Get("/new", gameRequestsHandler.New)
				requests.Get("/new/discovery", gameRequestsHandler.Discovery)
				requests.Get("/refresh", gameRequestsHandler.RefreshList)
				requests.Post("/", gameRequestsHandler.Create)
				requests.Get("/{requestID}", gameRequestsHandler.View)
				requests.Get("/{requestID}/refresh", gameRequestsHandler.RefreshView)
				requests.Post("/{requestID}/join", gameRequestsHandler.Join)
				requests.Post("/{requestID}/leave", gameRequestsHandler.Leave)
				requests.Post("/{requestID}/start", gameRequestsHandler.Start)
				requests.Post("/{requestID}/close", gameRequestsHandler.Close)
				requests.Post("/{requestID}/cancel", gameRequestsHandler.Cancel)
				if dependencies.Chat != nil {
					chatHandler := handler.NewChat(dependencies.Chat)
					requests.Get("/{requestID}/chat", chatHandler.Log)
					requests.Post("/{requestID}/chat", chatHandler.Post)
				}
			})
		}
		if dependencies.Realtime != nil {
			authenticated.With(handler.RequireAuthentication(dependencies.SecureCookies), handler.RequireGameNick).Get("/events/game-requests", realtimeHandler.GameRequests)
		}
		if dependencies.Games != nil || discordDeliveryHandler != nil {
			authenticated.Route("/admin", func(admin chi.Router) {
				admin.Use(handler.RequireAdmin, handler.RequireGameNick)
				if dependencies.Games != nil {
					admin.Get("/games", administrationHandler.Games)
					admin.Post("/games", administrationHandler.CreateGame)
					admin.Post("/games/{gameID}", administrationHandler.UpdateGame)
					admin.Post("/games/{gameID}/active", administrationHandler.SetGameActive)
					admin.Post("/games/{gameID}/aliases", administrationHandler.AddGameAlias)
					admin.Post("/games/{gameID}/aliases/{aliasID}/remove", administrationHandler.RemoveGameAlias)
					admin.Get("/games/{gameID}/igdb", administrationHandler.GameIGDB)
					admin.Post("/games/{gameID}/igdb", administrationHandler.LinkGameIGDB)
					admin.Post("/games/{gameID}/igdb/unlink", administrationHandler.UnlinkGameIGDB)
					admin.Get("/games/{gameID}/merge", administrationHandler.MergeGamePage)
					admin.Post("/games/{gameID}/merge", administrationHandler.MergeGame)
					admin.Post("/games/{gameID}/delete", administrationHandler.DeleteGame)
					if dependencies.ProviderSync != nil {
						admin.Get("/igdb", administrationHandler.IGDB)
						admin.Get("/igdb/status", administrationHandler.IGDBStatus)
						admin.Post("/igdb/sync", administrationHandler.SyncIGDB)
					}
					admin.Get("/pwa", administrationHandler.PWA)
				}
				if discordDeliveryHandler != nil {
					admin.Get("/discord", discordDeliveryHandler.Show)
					admin.Post("/discord/connect", discordDeliveryHandler.Start)
					admin.Post("/discord/reconnect", discordDeliveryHandler.Start)
					admin.Get("/discord/callback", discordDeliveryHandler.Callback)
					admin.Post("/discord/disconnect", discordDeliveryHandler.Disconnect)
				}
			})
		}
	})
	router.Get("/ui/patterns/empty", componentHandler(components.EmptyState(brand)))
	router.Get("/ui/patterns/error", componentHandler(components.ErrorState()))
	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readinessHandler(dependencies.Ready))

	return router
}

func rootHandler(brand branding.Brand, landing handler.LandingQueries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if account, ok := webmiddleware.CurrentUser(r.Context()); ok {
			if account.GameNick == "" {
				http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
			return
		}
		var games []requestquery.LandingGame
		if landing != nil {
			var err error
			if games, err = landing.Landing(r.Context()); err != nil {
				// The landing page must never fail a visitor over a ranking.
				webmiddleware.LogError(r, "load landing Games", err)
				games = nil
			}
		}
		componentHandler(pages.Home(brand, games)).ServeHTTP(w, r)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func readinessHandler(ready ReadyCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := ready(ctx); err != nil {
			webmiddleware.LogError(r, "readiness check failed", err)
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}

		_, _ = w.Write([]byte("ready\n"))
	}
}

func componentHandler(component templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rendered bytes.Buffer
		if err := component.Render(r.Context(), &rendered); err != nil {
			webmiddleware.LogError(r, "render HTML component", err)
			http.Error(w, "render page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rendered.WriteTo(w)
	}
}
