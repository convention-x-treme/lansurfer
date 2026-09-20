package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

var errInvalidAdminForm = errors.New("invalid administration form")

type GameAdministration interface {
	List(context.Context, string) ([]game.Game, error)
	Create(context.Context, string, string) (game.Game, error)
	Update(context.Context, string, string, string) (game.Game, error)
	SetActive(context.Context, string, bool) error
	AddAlias(context.Context, string, string) (game.Alias, error)
	RemoveAlias(context.Context, string, string) error
}

// ProviderSyncAdministration is the narrow interface the admin IGDB status
// page and manual "Sync now" action need. It is satisfied by
// *game.IGDBSyncService and by the disabled adapter wired in when IGDB
// credentials are not configured.
type ProviderSyncAdministration interface {
	Status(context.Context) (game.ProviderStatus, error)
	TryStart(context.Context) bool
}

type Administration struct {
	games         GameAdministration
	providerSync  ProviderSyncAdministration
	brand         branding.Brand
	instanceName  string
	baseURL       string
	secureContext bool
}

func NewAdministration(
	games GameAdministration, providerSync ProviderSyncAdministration, brand branding.Brand,
	instanceName, baseURL string, secureContext bool,
) *Administration {
	return &Administration{
		games: games, providerSync: providerSync, brand: brand,
		instanceName: instanceName, baseURL: baseURL, secureContext: secureContext,
	}
}

// PWA renders the read-only PWA/manifest/service-worker diagnostics facts an
// administrator can check without inspecting deployment configuration
// directly. It has no persistence and no player-facing counterpart.
func (h *Administration) PWA(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.AdminPWA(h.brand, adminNavigation(r), pages.PWADiagnostics{
		InstanceName:     h.instanceName,
		BaseURL:          h.baseURL,
		ManifestURL:      h.baseURL + "/manifest.webmanifest",
		ServiceWorkerURL: h.baseURL + "/service-worker.js",
		SecureContext:    h.secureContext,
	}))
}

func (h *Administration) Games(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	items, err := h.games.List(r.Context(), search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			render(w, r, http.StatusUnprocessableEntity, pages.AdminGames(h.brand, adminNavigation(r), nil, search, "Search must be 120 characters or fewer.", ""))
			return
		}
		h.renderAdminError(w, r, err)
		return
	}
	render(w, r, http.StatusOK, pages.AdminGames(h.brand, adminNavigation(r), items, search, "", notice(r)))
}

func (h *Administration) CreateGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGamesWithError(w, r, errInvalidAdminForm)
		return
	}
	_, err := h.games.Create(r.Context(), r.FormValue("name"), r.FormValue("slug"))
	if err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=game-created")
}

func (h *Administration) UpdateGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGamesWithError(w, r, errInvalidAdminForm)
		return
	}
	_, err := h.games.Update(r.Context(), chi.URLParam(r, "gameID"), r.FormValue("name"), r.FormValue("slug"))
	if err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=game-updated")
}

func (h *Administration) SetGameActive(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGamesWithError(w, r, errInvalidAdminForm)
		return
	}
	active, err := strconv.ParseBool(r.FormValue("active"))
	if err == nil {
		err = h.games.SetActive(r.Context(), chi.URLParam(r, "gameID"), active)
	} else {
		err = errInvalidAdminForm
	}
	if err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=game-status-updated")
}

func (h *Administration) AddGameAlias(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGamesWithError(w, r, errInvalidAdminForm)
		return
	}
	_, err := h.games.AddAlias(r.Context(), chi.URLParam(r, "gameID"), r.FormValue("alias"))
	if err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=alias-added")
}

func (h *Administration) RemoveGameAlias(w http.ResponseWriter, r *http.Request) {
	err := h.games.RemoveAlias(r.Context(), chi.URLParam(r, "gameID"), chi.URLParam(r, "aliasID"))
	if err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=alias-removed")
}

func (h *Administration) IGDB(w http.ResponseWriter, r *http.Request) {
	status, err := h.providerSync.Status(r.Context())
	if err != nil {
		h.renderAdminError(w, r, err)
		return
	}
	render(w, r, http.StatusOK, pages.AdminIGDB(h.brand, adminNavigation(r), status, notice(r)))
}

func (h *Administration) SyncIGDB(w http.ResponseWriter, r *http.Request) {
	if h.providerSync.TryStart(r.Context()) {
		h.redirect(w, r, "/admin/igdb?notice=igdb-sync-started")
		return
	}
	h.redirect(w, r, "/admin/igdb?notice=igdb-sync-already-running")
}

func (h *Administration) renderGamesWithError(w http.ResponseWriter, r *http.Request, cause error) {
	status := adminErrorStatus(cause)
	if status == http.StatusInternalServerError {
		h.renderAdminError(w, r, cause)
		return
	}
	items, err := h.games.List(r.Context(), "")
	if err != nil {
		h.renderAdminError(w, r, err)
		return
	}
	render(w, r, status, pages.AdminGames(h.brand, adminNavigation(r), items, "", adminMessage(cause), ""))
}

func (h *Administration) renderAdminError(w http.ResponseWriter, r *http.Request, cause error) {
	webmiddleware.LogError(r, "serve administration request", cause)
	render(w, r, http.StatusInternalServerError, pages.AdminError(h.brand, adminNavigation(r)))
}

func (h *Administration) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func adminNavigation(r *http.Request) components.NavigationState {
	account, _ := webmiddleware.CurrentUser(r.Context())
	return components.NavigationState{
		Viewer:  &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)},
		Section: "administration",
	}
}

func adminMessage(err error) string {
	switch {
	case errors.Is(err, errInvalidAdminForm):
		return "The form could not be read."
	case errors.Is(err, game.ErrInvalidName):
		return "Game name is required and must be 120 characters or fewer."
	case errors.Is(err, game.ErrInvalidSlug):
		return "Slug must use lowercase letters, numbers, and single hyphens."
	case errors.Is(err, game.ErrInvalidAlias):
		return "Alias is required and must be 120 characters or fewer."
	case errors.Is(err, game.ErrAlreadyExists):
		return "That game name, slug, or alias is already in use."
	case errors.Is(err, game.ErrNotFound), errors.Is(err, game.ErrAliasNotFound):
		return "That game or alias no longer exists."
	default:
		return "The change could not be saved. Please try again."
	}
}

func adminErrorStatus(err error) int {
	switch {
	case errors.Is(err, errInvalidAdminForm):
		return http.StatusBadRequest
	case errors.Is(err, game.ErrNotFound), errors.Is(err, game.ErrAliasNotFound):
		return http.StatusNotFound
	case errors.Is(err, game.ErrInvalidName), errors.Is(err, game.ErrInvalidSlug),
		errors.Is(err, game.ErrInvalidAlias), errors.Is(err, game.ErrAlreadyExists):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func notice(r *http.Request) string {
	switch strings.TrimSpace(r.URL.Query().Get("notice")) {
	case "game-created":
		return "Game created."
	case "game-updated":
		return "Game updated."
	case "game-status-updated":
		return "Game availability updated."
	case "alias-added":
		return "Alias added."
	case "alias-removed":
		return "Alias removed."
	case "request-created":
		return "Game Request created."
	case "igdb-sync-started":
		return "IGDB synchronization requested."
	case "igdb-sync-already-running":
		return "IGDB synchronization is already in progress."
	default:
		return ""
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := webmiddleware.CurrentUser(r.Context())
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if account.Role != user.RoleAdmin {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
