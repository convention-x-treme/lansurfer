package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

// MetricsKeys manages the instance's metrics-only credential.
type MetricsKeys interface {
	Configured(context.Context) (bool, error)
	Rotate(context.Context) (string, error)
	Valid(context.Context, string) (bool, error)
}

type Metrics struct {
	keys       MetricsKeys
	scrape     http.Handler
	navigation *Navigation
	brand      branding.Brand
}

func NewMetrics(keys MetricsKeys, scrape http.Handler, navigation *Navigation, brand branding.Brand) *Metrics {
	return &Metrics{keys: keys, scrape: scrape, navigation: navigation, brand: brand}
}

func (h *Metrics) Scrape(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(r.Header.Values("Authorization")) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		h.unauthorized(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	valid, err := h.keys.Valid(ctx, parts[1])
	cancel()
	if err != nil {
		webmiddleware.LogError(r, "authenticate metrics key", err)
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	if !valid {
		h.unauthorized(w)
		return
	}
	h.scrape.ServeHTTP(w, r)
}

func (h *Metrics) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "metrics access denied", http.StatusUnauthorized)
}

func (h *Metrics) Show(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	configured, err := h.keys.Configured(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "read metrics key status", err)
		http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	h.render(w, r, configured, "")
}

func (h *Metrics) Rotate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key, err := h.keys.Rotate(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "rotate metrics key", err)
		http.Error(w, "metrics key generation failed; try again", http.StatusServiceUnavailable)
		return
	}
	// Return plaintext only in this POST response, never a URL, cookie or flash.
	h.render(w, r, true, key)
}

func (h *Metrics) render(w http.ResponseWriter, r *http.Request, configured bool, key string) {
	render(w, r, http.StatusOK, pages.AdminMetrics(h.brand, h.navigation.For(r, "administration-metrics"), configured, key))
}
