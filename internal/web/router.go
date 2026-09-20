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
	"github.com/convention-x-treme/lansurfer/internal/auth"
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
	Ready               ReadyCheck
	Authentication      handler.Authentication
	Sessions            Sessions
	PendingIdentities   handler.PendingIdentities
	AccountRegistration handler.AccountRegistration
	GameNickClaims      handler.GameNickClaims
	Identities          handler.IdentityLookup
	IdentityUnlinker    handler.IdentityUnlinker
	Games               handler.GameAdministration
	ProviderSync        handler.ProviderSyncAdministration
	Discovery           Discovery
	GameResolver        handler.GameResolver
	GameRequests        handler.GameRequestCommands
	RequestQueries      handler.GameRequestQueries
	Realtime            handler.GameRequestEvents
	SecureCookies       bool
	Logger              *slog.Logger
	TrustedProxies      []netip.Prefix
}

// NewRouter constructs the HTTP router.
func NewRouter(dependencies RouterDependencies) http.Handler {
	brand := branding.Default()
	authenticationHandler := handler.NewAuthentication(dependencies.Authentication, dependencies.Sessions, brand, dependencies.SecureCookies)
	onboardingHandler := handler.NewOnboarding(
		dependencies.PendingIdentities, dependencies.AccountRegistration, dependencies.GameNickClaims,
		dependencies.Identities, dependencies.Authentication, brand, dependencies.SecureCookies,
	)
	accountHandler := handler.NewAccount(dependencies.Identities, dependencies.IdentityUnlinker, dependencies.GameNickClaims, dependencies.Authentication, brand)
	administrationHandler := handler.NewAdministration(dependencies.Games, dependencies.ProviderSync, brand)
	gamesHandler := handler.NewGames(dependencies.Discovery, brand)
	gameRequestsHandler := handler.NewGameRequests(dependencies.GameRequests, dependencies.RequestQueries, dependencies.GameResolver, dependencies.Discovery, brand)
	realtimeHandler := handler.NewRealtime(dependencies.Realtime)
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

	router.Handle("/assets/*", http.StripPrefix("/assets/", assets.Handler()))
	router.Group(func(authenticated chi.Router) {
		authenticated.Use(webmiddleware.Authentication(dependencies.Sessions, dependencies.SecureCookies))
		authenticated.Get("/", rootHandler(brand, dependencies.Authentication))
		authenticated.Get("/auth/login", authenticationHandler.Login)
		authenticated.Get("/auth/oidc", authenticationHandler.BeginFor("oidc"))
		authenticated.Get("/auth/discord", authenticationHandler.BeginFor("discord"))
		authenticated.Get("/auth/callback", authenticationHandler.CallbackFor("oidc"))
		authenticated.Get("/auth/discord/callback", authenticationHandler.CallbackFor("discord"))
		authenticated.Route("/onboarding", func(onboarding chi.Router) {
			onboarding.Get("/", onboardingHandler.ShowExisting)
			onboarding.Post("/", onboardingHandler.SubmitExisting)
			onboarding.Get("/{pendingID}", onboardingHandler.ShowPending)
			onboarding.Post("/{pendingID}", onboardingHandler.SubmitPending)
		})
		authenticated.Route("/account", func(account chi.Router) {
			account.Use(handler.RequireAuthentication, handler.RequireGameNick)
			account.Get("/", accountHandler.Show)
			account.Post("/game-nick", accountHandler.RenameGameNick)
			account.Post("/link/oidc", authenticationHandler.LinkFor("oidc"))
			account.Post("/link/discord", authenticationHandler.LinkFor("discord"))
			account.Post("/identities/{identityID}/unlink", accountHandler.Unlink)
		})
		if dependencies.Discovery != nil {
			authenticated.Route("/games", func(games chi.Router) {
				games.Use(handler.RequireAuthentication, handler.RequireGameNick)
				games.Get("/", gamesHandler.Browse)
				games.Get("/mine", gamesHandler.MyGames)
				games.Post("/providers/{provider}/{externalID}/follow", gamesHandler.FollowProvider)
				games.Post("/{gameID}/follow", gamesHandler.Follow)
				games.Post("/{gameID}/unfollow", gamesHandler.Unfollow)
			})
		}
		if dependencies.GameRequests != nil && dependencies.RequestQueries != nil {
			authenticated.Route("/game-requests", func(requests chi.Router) {
				requests.Use(handler.RequireAuthentication, handler.RequireGameNick)
				requests.Get("/", gameRequestsHandler.List)
				requests.Get("/new", gameRequestsHandler.New)
				requests.Get("/new/discovery", gameRequestsHandler.Discovery)
				requests.Get("/new/selection", gameRequestsHandler.Selection)
				requests.Get("/refresh", gameRequestsHandler.RefreshList)
				requests.Post("/", gameRequestsHandler.Create)
				requests.Get("/{requestID}", gameRequestsHandler.View)
				requests.Get("/{requestID}/refresh", gameRequestsHandler.RefreshView)
				requests.Post("/{requestID}/join", gameRequestsHandler.Join)
				requests.Post("/{requestID}/leave", gameRequestsHandler.Leave)
				requests.Post("/{requestID}/close", gameRequestsHandler.Close)
				requests.Post("/{requestID}/cancel", gameRequestsHandler.Cancel)
			})
		}
		if dependencies.Realtime != nil {
			authenticated.With(handler.RequireAuthentication, handler.RequireGameNick).Get("/events/game-requests", realtimeHandler.GameRequests)
		}
		if dependencies.Games != nil {
			authenticated.Route("/admin", func(admin chi.Router) {
				admin.Use(handler.RequireAdmin, handler.RequireGameNick)
				admin.Get("/games", administrationHandler.Games)
				admin.Post("/games", administrationHandler.CreateGame)
				admin.Post("/games/{gameID}", administrationHandler.UpdateGame)
				admin.Post("/games/{gameID}/active", administrationHandler.SetGameActive)
				admin.Post("/games/{gameID}/aliases", administrationHandler.AddGameAlias)
				admin.Post("/games/{gameID}/aliases/{aliasID}/remove", administrationHandler.RemoveGameAlias)
				if dependencies.ProviderSync != nil {
					admin.Get("/igdb", administrationHandler.IGDB)
					admin.Post("/igdb/sync", administrationHandler.SyncIGDB)
				}
			})
		}
	})
	router.Post("/auth/logout", authenticationHandler.Logout)
	router.Get("/ui/patterns/empty", componentHandler(components.EmptyState(brand)))
	router.Get("/ui/patterns/error", componentHandler(components.ErrorState()))
	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readinessHandler(dependencies.Ready))

	return router
}

func rootHandler(brand branding.Brand, authentication handler.Authentication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if account, ok := webmiddleware.CurrentUser(r.Context()); ok {
			if account.GameNick == "" {
				http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
			return
		}
		var providers []auth.ProviderAvailability
		if authentication != nil {
			providers = authentication.Providers()
		}
		componentHandler(pages.Login(brand, providers)).ServeHTTP(w, r)
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
