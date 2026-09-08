// Package web provides LANsurfer's HTTP delivery layer.
package web

import (
	"bytes"
	"context"
	"net/http"
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

const readinessTimeout = time.Second

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
	SecureCookies  bool
}

// NewRouter constructs the HTTP router.
func NewRouter(dependencies RouterDependencies) http.Handler {
	brand := branding.Default()
	authenticationHandler := handler.NewAuthentication(dependencies.Authentication, brand, dependencies.SecureCookies)
	administrationHandler := handler.NewAdministration(dependencies.Events, dependencies.Games, brand)
	gamesHandler := handler.NewGames(dependencies.Subscriptions, brand)
	gameRequestsHandler := handler.NewGameRequests(dependencies.GameRequests, dependencies.RequestQueries, brand)
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	router.Handle("/assets/*", http.StripPrefix("/assets/", assets.Handler()))
	router.Group(func(authenticated chi.Router) {
		authenticated.Use(webmiddleware.Authentication(dependencies.Authentication, dependencies.SecureCookies))
		authenticated.Get("/", homeHandler(brand, dependencies.Authentication))
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
				requests.Post("/", gameRequestsHandler.Create)
				requests.Get("/{requestID}", gameRequestsHandler.View)
				requests.Post("/{requestID}/join", gameRequestsHandler.Join)
				requests.Post("/{requestID}/leave", gameRequestsHandler.Leave)
				requests.Post("/{requestID}/close", gameRequestsHandler.Close)
				requests.Post("/{requestID}/cancel", gameRequestsHandler.Cancel)
			})
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
	router.Get("/auth/callback", authenticationHandler.Callback)
	router.Post("/auth/logout", authenticationHandler.Logout)
	router.Get("/ui/patterns/empty", componentHandler(components.EmptyState(brand)))
	router.Get("/ui/patterns/error", componentHandler(components.ErrorState()))
	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readinessHandler(dependencies.Ready))

	return router
}

func homeHandler(brand branding.Brand, authentication Authentication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		navigation := components.NavigationState{LoginAvailable: authentication != nil && authentication.Available()}
		if currentUser, ok := webmiddleware.CurrentUser(r.Context()); ok {
			navigation.Viewer = &components.Viewer{
				DisplayName: currentUser.DisplayName,
				Role:        string(currentUser.Role),
			}
		}
		componentHandler(pages.Home(brand, navigation)).ServeHTTP(w, r)
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
			http.Error(w, "render page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rendered.WriteTo(w)
	}
}
