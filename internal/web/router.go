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
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const readinessTimeout = time.Second

// ReadyCheck reports whether dependencies required to serve traffic are ready.
type ReadyCheck func(context.Context) error

// NewRouter constructs the HTTP router.
func NewRouter(ready ReadyCheck) http.Handler {
	brand := branding.Default()
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	router.Handle("/assets/*", http.StripPrefix("/assets/", assets.Handler()))
	router.Get("/", componentHandler(pages.Home(brand)))
	router.Get("/ui/patterns/empty", componentHandler(components.EmptyState(brand)))
	router.Get("/ui/patterns/error", componentHandler(components.ErrorState()))
	router.Get("/healthz", healthHandler)
	router.Get("/readyz", readinessHandler(ready))

	return router
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
