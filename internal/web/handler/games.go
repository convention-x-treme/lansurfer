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
	BrowseFollowed(context.Context, string, string) (game.DiscoveryPage, error)
	Select(context.Context, string, game.GameSelection) (game.DiscoveryItem, error)
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

// Browse serves the one Games screen. "Followed only" is a filter on it
// rather than a second route: the same search, the same cards, narrowed to
// what this viewer follows.
func (h *Games) Browse(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := r.URL.Query().Get("q")
	followedOnly := followedFilter(r)
	page, err := h.browse(r, account.ID, search, followedOnly)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			h.renderBrowse(w, r, http.StatusUnprocessableEntity, game.DiscoveryPage{}, search, followedOnly, "Search must be 120 characters or fewer.")
			return
		}
		webmiddleware.LogError(r, "browse game catalog", err)
		h.renderError(w, r)
		return
	}
	h.renderBrowse(w, r, http.StatusOK, page, search, followedOnly, "")
}

// MyGames keeps the retired /games/mine route working. The followed list is
// now the Games screen's own filter, so the old address resolves to it
// instead of 404ing bookmarks and links that predate the merge.
func (h *Games) MyGames(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, pages.CatalogURL(true), http.StatusSeeOther)
}

func (h *Games) browse(r *http.Request, userID, search string, followedOnly bool) (game.DiscoveryPage, error) {
	if followedOnly {
		return h.discovery.BrowseFollowed(r.Context(), userID, search)
	}
	return h.discovery.Browse(r.Context(), userID, search)
}

// followedFilter reads the screen's one filter. Any value other than the
// checkbox's own "1" is treated as off rather than rejected: a filter is
// decoration on a list that still has a correct unfiltered answer.
func followedFilter(r *http.Request) bool {
	return r.URL.Query().Get("followed") == "1"
}

func (h *Games) Follow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.FollowLocal(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, account.ID, chi.URLParam(r, "gameID"))
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
	h.mutationSuccess(w, r, account.ID, result.ID)
}

func (h *Games) Unfollow(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	if err := h.discovery.Unfollow(r.Context(), account.ID, chi.URLParam(r, "gameID")); err != nil {
		h.renderMutationError(w, r, err)
		return
	}
	h.mutationSuccess(w, r, account.ID, chi.URLParam(r, "gameID"))
}

func (h *Games) renderBrowse(w http.ResponseWriter, r *http.Request, status int, page game.DiscoveryPage, search string, followedOnly bool, message string) {
	navigation := h.navigationFor(r, navigationSection(followedOnly))
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, status, pages.GameCatalogResults(h.brand, page, search, followedOnly, message, navigation.Viewer))
		return
	}
	render(w, r, status, pages.GameCatalog(h.brand, navigation, page, search, followedOnly, message))
}

// mutationSuccess re-reads the Game the follow just changed and re-renders
// its whole card. Following changes the cover tag and the card border as
// well as the button, so the card is the smallest fragment that can still
// be correct afterwards; the read is Select, which carries the same cover
// and enrichment the browsed card had, so the swapped-in card never
// degrades to a poorer re-render than the one it replaced.
//
// A re-read that fails leaves the mutation itself committed, so this
// reports the follow as succeeded and lets the next page load state the
// result rather than surfacing a write error that did not happen.
func (h *Games) mutationSuccess(w http.ResponseWriter, r *http.Request, userID, gameID string) {
	followedOnly := r.FormValue("followed") == "1"
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, pages.CatalogURL(followedOnly), http.StatusSeeOther)
		return
	}
	item, err := h.discovery.Select(r.Context(), userID, game.GameSelection{LocalGameID: gameID})
	if err != nil {
		webmiddleware.LogError(r, "reload followed game card", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	render(w, r, http.StatusOK, pages.GameCard(item, followedOnly))
}

// navigationSection marks the filtered view as its own navigation entry, so
// the sidebar's followed shortcut reads as current while that filter is on.
func navigationSection(followedOnly bool) string {
	if followedOnly {
		return "games-followed"
	}
	return "games"
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
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
