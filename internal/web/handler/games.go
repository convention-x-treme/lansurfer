package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// GameDiscovery combines the authoritative local Game catalog with the
// active synchronized IGDB provider index. Provider search results are
// read-only display in this handler; following/requesting an unmaterialized
// provider result is not implemented here.
type GameDiscovery interface {
	Browse(context.Context, string, string) ([]game.DiscoveryItem, error)
	MyGames(context.Context, string) ([]game.SubscriptionGame, error)
	FollowLocal(context.Context, string, string) error
	Unfollow(context.Context, string, string) error
}

type Games struct {
	discovery GameDiscovery
	brand     branding.Brand
}

func NewGames(discovery GameDiscovery, brand branding.Brand) *Games {
	return &Games{discovery: discovery, brand: brand}
}

func (h *Games) Browse(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := r.URL.Query().Get("q")
	items, err := h.discovery.Browse(r.Context(), account.ID, search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			h.renderBrowse(w, r, http.StatusUnprocessableEntity, nil, search, "Search must be 120 characters or fewer.")
			return
		}
		webmiddleware.LogError(r, "browse game catalog", err)
		h.renderError(w, r)
		return
	}
	h.renderBrowse(w, r, http.StatusOK, items, search, "")
}

func (h *Games) MyGames(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	items, err := h.discovery.MyGames(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "load followed games", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.MyGames(h.brand, gameNavigation(r), items))
}

func (h *Games) Follow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.FollowLocal(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, true)
}

func (h *Games) Unfollow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.Unfollow(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, false)
}

func (h *Games) renderBrowse(w http.ResponseWriter, r *http.Request, status int, items []game.DiscoveryItem, search, message string) {
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, status, pages.GameCatalogResults(items, search, message))
		return
	}
	render(w, r, status, pages.GameCatalog(h.brand, gameNavigation(r), items, search, message))
}

func (h *Games) mutationSuccess(w http.ResponseWriter, r *http.Request, subscribed bool) {
	if r.Header.Get("HX-Request") == "true" {
		if !subscribed && r.FormValue("view") == "mine" {
			w.WriteHeader(http.StatusOK)
			return
		}
		render(w, r, http.StatusOK, pages.GameFollowControl(chi.URLParam(r, "gameID"), subscribed, false))
		return
	}
	target := "/games"
	if r.FormValue("view") == "mine" {
		target = "/games/mine"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Games) renderMutationError(w http.ResponseWriter, r *http.Request, cause error) {
	if errors.Is(cause, game.ErrNotFound) {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	webmiddleware.LogError(r, "update game subscription", cause)
	http.Error(w, "update game subscription", http.StatusInternalServerError)
}

func (h *Games) renderError(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusInternalServerError, pages.GameCatalogError(h.brand, gameNavigation(r)))
}

func gameNavigation(r *http.Request) components.NavigationState {
	account, _ := webmiddleware.CurrentUser(r.Context())
	return components.NavigationState{
		Viewer: &components.Viewer{DisplayName: account.DisplayName, Role: string(account.Role)},
	}
}

func RequireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := webmiddleware.CurrentUser(r.Context()); !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
