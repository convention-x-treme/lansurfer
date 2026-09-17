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

type Authentication interface {
	handler.Authentication
	webmiddleware.Authenticator
}

type RouterDependencies struct {
	Ready          ReadyCheck
	Authentication Authentication
	Events         handler.EventAdministration
	Games          handler.GameAdministration
	Subscriptions  handler.GameSubscriptions
	GameRequests   handler.GameRequestCommands
	RequestQueries handler.GameRequestQueries
	Realtime       handler.GameRequestEvents
	SecureCookies  bool
	Logger         *slog.Logger
	TrustedProxies []netip.Prefix
}

// NewRouter constructs the HTTP router.
func NewRouter(dependencies RouterDependencies) http.Handler {
	brand := branding.Default()
	authenticationHandler := handler.NewAuthentication(dependencies.Authentication, brand, dependencies.SecureCookies)
	administrationHandler := handler.NewAdministration(dependencies.Events, dependencies.Games, brand)
	gamesHandler := handler.NewGames(dependencies.Subscriptions, brand)
	gameRequestsHandler := handler.NewGameRequests(dependencies.GameRequests, dependencies.RequestQueries, brand)
	realtimeHandler := handler.NewRealtime(dependencies.Realtime)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(webmiddleware.RequestLogging(dependencies.Logger, dependencies.TrustedProxies))
	router.Use(middleware.Recoverer)
	router.Use(webmiddleware.RequestTimeout(requestTimeout))
	router.Use(webmiddleware.SecurityHeaders(dependencies.SecureCookies))
	router.Use(webmiddleware.CSRF(dependencies.SecureCookies))
	router.Use(webmiddleware.Theme)

	router.Handle("/assets/*", http.StripPrefix("/assets/", assets.Handler()))
	router.Group(func(authenticated chi.Router) {
		authenticated.Use(webmiddleware.Authentication(dependencies.Authentication, dependencies.SecureCookies))
		authenticated.Get("/", rootHandler(brand, dependencies.Authentication))
		if dependencies.Subscriptions != nil {
			authenticated.Route("/games", func(games chi.Router) {
				games.Use(handler.RequireAuthentication)
				games.Get("/", gamesHandler.Browse)
				games.Get("/mine", gamesHandler.MyGames)
				games.Post("/{gameID}/follow", gamesHandler.Follow)
				games.Post("/{gameID}/unfollow", gamesHandler.Unfollow)
			})
		}
		if dependencies.GameRequests != nil && dependencies.RequestQueries != nil {
			authenticated.Route("/game-requests", func(requests chi.Router) {
				requests.Use(handler.RequireAuthentication)
				requests.Get("/", gameRequestsHandler.List)
				requests.Get("/new", gameRequestsHandler.New)
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
			authenticated.With(handler.RequireAuthentication).Get("/events/game-requests", realtimeHandler.GameRequests)
		}
		if dependencies.Events != nil && dependencies.Games != nil {
			authenticated.Route("/admin", func(admin chi.Router) {
				admin.Use(handler.RequireAdmin)
				admin.Get("/events", administrationHandler.Events)
				admin.Post("/events", administrationHandler.CreateEvent)
				admin.Post("/events/{eventID}", administrationHandler.UpdateEvent)
				admin.Post("/events/{eventID}/current", administrationHandler.SetCurrentEvent)
				admin.Get("/games", administrationHandler.Games)
				admin.Post("/games", administrationHandler.CreateGame)
				admin.Post("/games/{gameID}", administrationHandler.UpdateGame)
				admin.Post("/games/{gameID}/active", administrationHandler.SetGameActive)
				admin.Post("/games/{gameID}/aliases", administrationHandler.AddGameAlias)
				admin.Post("/games/{gameID}/aliases/{aliasID}/remove", administrationHandler.RemoveGameAlias)
			})
		}
	})
	router.Get("/auth/login", authenticationHandler.Login)
	router.Get("/auth/oidc", authenticationHandler.BeginLogin)
	router.Get("/auth/callback", authenticationHandler.Callback)
	router.Post("/auth/logout", authenticationHandler.Logout)
	router.Get("/ui/patterns/empty", componentHandler(components.EmptyState(brand)))
	router.Get("/ui/patterns/error", componentHandler(components.ErrorState()))
	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readinessHandler(dependencies.Ready))

	return router
}

func rootHandler(brand branding.Brand, authentication Authentication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
			http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
			return
		}
		componentHandler(pages.Login(brand, authentication != nil && authentication.Available())).ServeHTTP(w, r)
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
