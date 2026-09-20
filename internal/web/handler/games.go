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
// active synchronized IGDB provider index. Follow materializes an
// unmaterialized provider result (keyed only by the opaque provider
// reference) and follows the resulting authoritative local Game; FollowLocal
// follows an already-local Game by ID.
type GameDiscovery interface {
	Browse(context.Context, string, string) (game.DiscoveryPage, error)
	MyGames(context.Context, string) ([]game.SubscriptionGame, error)
	FollowLocal(context.Context, string, string) error
	Follow(context.Context, string, game.ProviderReference) (game.Game, error)
	Unfollow(context.Context, string, string) error
}

type Games struct {
	discovery  GameDiscovery
	navigation *Navigation
	brand      branding.Brand
}

func NewGames(discovery GameDiscovery, navigation *Navigation, brand branding.Brand) *Games {
	return &Games{discovery: discovery, navigation: navigation, brand: brand}
}

func (h *Games) Browse(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := r.URL.Query().Get("q")
	page, err := h.discovery.Browse(r.Context(), account.ID, search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			h.renderBrowse(w, r, http.StatusUnprocessableEntity, game.DiscoveryPage{}, search, "Search must be 120 characters or fewer.")
			return
		}
		webmiddleware.LogError(r, "browse game catalog", err)
		h.renderError(w, r)
		return
	}
	h.renderBrowse(w, r, http.StatusOK, page, search, "")
}

func (h *Games) MyGames(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	items, err := h.discovery.MyGames(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "load followed games", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.MyGames(h.brand, h.navigationFor(r, "my-games"), items))
}

func (h *Games) Follow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.FollowLocal(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, true)
}

// FollowProvider follows an IGDB discovery result identified only by its
// opaque provider reference (provider + external ID) in the URL path. The
// browser never supplies a name, slug, or other provider metadata; the Game
// capability reloads and materializes the record itself before following it.
func (h *Games) FollowProvider(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	ref := game.ProviderReference{
		Provider:   chi.URLParam(r, "provider"),
		ExternalID: chi.URLParam(r, "externalID"),
	}
	result, err := h.discovery.Follow(r.Context(), account.ID, ref)
	if err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, http.StatusOK, pages.GameFollowControl(result.ID, true, false))
		return
	}
	http.Redirect(w, r, "/games", http.StatusSeeOther)
}

func (h *Games) Unfollow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.Unfollow(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, false)
}

func (h *Games) renderBrowse(w http.ResponseWriter, r *http.Request, status int, page game.DiscoveryPage, search, message string) {
	navigation := h.navigationFor(r, "games")
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, status, pages.GameCatalogResults(page, search, message, navigation.Viewer))
		return
	}
	render(w, r, status, pages.GameCatalog(h.brand, navigation, page, search, message))
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
	if errors.Is(cause, game.ErrNotFound) || errors.Is(cause, game.ErrProviderRecordNotFound) {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	if errors.Is(cause, game.ErrInvalidProviderReference) {
		http.Error(w, "invalid provider reference", http.StatusUnprocessableEntity)
		return
	}
	webmiddleware.LogError(r, "update game subscription", cause)
	http.Error(w, "update game subscription", http.StatusInternalServerError)
}

func (h *Games) renderError(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusInternalServerError, pages.GameCatalogError(h.brand, h.navigationFor(r, "games")))
}

func (h *Games) navigationFor(r *http.Request, section string) components.NavigationState {
	return h.navigation.For(r, section)
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
