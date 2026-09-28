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
	SearchIGDB(context.Context, string) ([]game.AdminProviderGame, error)
	LinkIGDB(context.Context, string, string) error
	UnlinkIGDB(context.Context, string) error
	Merge(context.Context, string, string) error
	Delete(context.Context, string) error
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
	navigation    *Navigation
	brand         branding.Brand
	instanceName  string
	baseURL       string
	secureContext bool
}

func NewAdministration(
	games GameAdministration, providerSync ProviderSyncAdministration, navigation *Navigation,
	brand branding.Brand, instanceName, baseURL string, secureContext bool,
) *Administration {
	return &Administration{
		games: games, providerSync: providerSync, navigation: navigation, brand: brand,
		instanceName: instanceName, baseURL: baseURL, secureContext: secureContext,
	}
}

// PWA renders the read-only PWA/manifest/service-worker diagnostics facts an
// administrator can check without inspecting deployment configuration
// directly. It has no persistence and no player-facing counterpart.
func (h *Administration) PWA(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.AdminPWA(h.brand, h.navigationFor(r, "administration-pwa"), pages.PWADiagnostics{
		InstanceName:     h.instanceName,
		BaseURL:          h.baseURL,
		ManifestURL:      h.baseURL + "/manifest.webmanifest",
		ServiceWorkerURL: h.baseURL + "/service-worker.js",
		SecureContext:    h.secureContext,
	}))
}

// Games serves the catalog administration screen. Its filters apply live,
// so an htmx request swaps only the result list, exactly like the
// player-facing catalog.
func (h *Administration) Games(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	items, err := h.games.List(r.Context(), search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			h.renderGames(w, r, http.StatusUnprocessableEntity, nil, search, "Search must be 120 characters or fewer.", components.Notice{})
			return
		}
		h.renderAdminError(w, r, err)
		return
	}
	h.renderGames(w, r, http.StatusOK, items, search, "", notice(r))
}

func (h *Administration) renderGames(w http.ResponseWriter, r *http.Request, status int, items []game.Game, search, formError string, message components.Notice) {
	localOnly := r.URL.Query().Get("local") == "1"
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, status, pages.AdminCatalogResults(h.brand, items, search, localOnly))
		return
	}
	render(w, r, status, pages.AdminGames(h.brand, h.navigationFor(r, "administration"), items, search, localOnly, formError, message))
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
		h.renderGameEntry(w, r, errInvalidAdminForm, "game-updated")
		return
	}
	_, err := h.games.Update(r.Context(), chi.URLParam(r, "gameID"), r.FormValue("name"), r.FormValue("slug"))
	h.renderGameEntry(w, r, err, "game-updated")
}

func (h *Administration) SetGameActive(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGameEntry(w, r, errInvalidAdminForm, "game-status-updated")
		return
	}
	active, err := strconv.ParseBool(r.FormValue("active"))
	if err == nil {
		err = h.games.SetActive(r.Context(), chi.URLParam(r, "gameID"), active)
	} else {
		err = errInvalidAdminForm
	}
	h.renderGameEntry(w, r, err, "game-status-updated")
}

func (h *Administration) AddGameAlias(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGameEntry(w, r, errInvalidAdminForm, "alias-added")
		return
	}
	_, err := h.games.AddAlias(r.Context(), chi.URLParam(r, "gameID"), r.FormValue("alias"))
	h.renderGameEntry(w, r, err, "alias-added")
}

func (h *Administration) RemoveGameAlias(w http.ResponseWriter, r *http.Request) {
	err := h.games.RemoveAlias(r.Context(), chi.URLParam(r, "gameID"), chi.URLParam(r, "aliasID"))
	h.renderGameEntry(w, r, err, "alias-removed")
}

func (h *Administration) GameIGDB(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	item, err := h.gameByID(r.Context(), chi.URLParam(r, "gameID"))
	if err != nil {
		h.renderGameReconciliationError(w, r, err)
		return
	}
	results, err := h.games.SearchIGDB(r.Context(), search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			render(w, r, http.StatusUnprocessableEntity, pages.AdminGameIGDB(h.brand, h.navigationFor(r, "administration"), item, nil, search, "Search must be 120 characters or fewer."))
			return
		}
		h.renderAdminError(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, http.StatusOK, pages.AdminIGDBLinkResults(item, results, search))
		return
	}
	render(w, r, http.StatusOK, pages.AdminGameIGDB(h.brand, h.navigationFor(r, "administration"), item, results, search, ""))
}

func (h *Administration) LinkGameIGDB(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderGameIGDBWithError(w, r, errInvalidAdminForm, "")
		return
	}
	gameID := chi.URLParam(r, "gameID")
	externalID := r.FormValue("external_id")
	err := h.games.LinkIGDB(r.Context(), gameID, externalID)
	var conflict *game.IGDBLinkConflict
	if errors.As(err, &conflict) {
		target := "/admin/games/" + gameID + "/merge?source=" + conflict.GameID + "&notice=igdb-link-conflict"
		h.redirect(w, r, target)
		return
	}
	if err != nil {
		h.renderGameIGDBWithError(w, r, err, externalID)
		return
	}
	h.redirect(w, r, "/admin/games?notice=igdb-linked")
}

func (h *Administration) UnlinkGameIGDB(w http.ResponseWriter, r *http.Request) {
	if err := h.games.UnlinkIGDB(r.Context(), chi.URLParam(r, "gameID")); err != nil {
		h.renderGameReconciliationError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=igdb-unlinked")
}

func (h *Administration) MergeGamePage(w http.ResponseWriter, r *http.Request) {
	h.renderMergeGame(w, r, http.StatusOK, "")
}

func (h *Administration) MergeGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderMergeGame(w, r, http.StatusBadRequest, adminMessage(errInvalidAdminForm))
		return
	}
	sourceID := r.FormValue("source_id")
	targetID := chi.URLParam(r, "gameID")
	if err := h.games.Merge(r.Context(), sourceID, targetID); err != nil {
		query := r.URL.Query()
		query.Set("source", sourceID)
		r.URL.RawQuery = query.Encode()
		h.renderMergeGame(w, r, adminErrorStatus(err), adminMessage(err))
		return
	}
	h.redirect(w, r, "/admin/games?notice=game-merged")
}

func (h *Administration) DeleteGame(w http.ResponseWriter, r *http.Request) {
	if err := h.games.Delete(r.Context(), chi.URLParam(r, "gameID")); err != nil {
		h.renderGamesWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/games?notice=game-deleted")
}

func (h *Administration) IGDB(w http.ResponseWriter, r *http.Request) {
	status, err := h.providerSync.Status(r.Context())
	if err != nil {
		h.renderAdminError(w, r, err)
		return
	}
	render(w, r, http.StatusOK, pages.AdminIGDB(h.brand, h.navigationFor(r, "administration-igdb"), status, notice(r)))
}

// IGDBStatus renders the status card on its own for the refresh /admin/igdb
// runs while a sync is in progress. It renders the same component the page
// embeds, so the refreshed markup cannot drift from the page's own. A failed
// read is answered with 500 rather than a degraded card: htmx leaves a non-2xx
// response unswapped, so the last good status stays on screen and the next
// poll retries.
func (h *Administration) IGDBStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.providerSync.Status(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "serve administration request", err)
		http.Error(w, "load IGDB status", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.AdminIGDBStatus(status))
}

func (h *Administration) SyncIGDB(w http.ResponseWriter, r *http.Request) {
	if h.providerSync.TryStart(r.Context()) {
		h.redirect(w, r, "/admin/igdb?notice=igdb-sync-started")
		return
	}
	h.redirect(w, r, "/admin/igdb?notice=igdb-sync-already-running")
}

// renderGameEntry answers one change made inside an opened catalog entry.
// With scripting the response is that entry alone, re-read and still open,
// so the screen neither reloads nor scrolls and the administrator stays in
// the Game they were editing. A rejected change comes back the same way
// with the complaint inside the panel: htmx leaves a non-2xx response
// unswapped, so an error status here would discard the very message that
// explains it.
//
// Without scripting the request is an ordinary form post, so it still
// redirects -- to the entry's own fragment, so the reload at least lands on
// the Game that was being edited.
func (h *Administration) renderGameEntry(w http.ResponseWriter, r *http.Request, cause error, notice string) {
	gameID := chi.URLParam(r, "gameID")
	if r.Header.Get("HX-Request") != "true" {
		if cause != nil {
			h.renderGamesWithError(w, r, cause)
			return
		}
		h.redirect(w, r, "/admin/games?notice="+notice+"#"+pages.AdminEntryID(gameID))
		return
	}
	if cause != nil && adminErrorStatus(cause) == http.StatusInternalServerError {
		h.renderAdminError(w, r, cause)
		return
	}
	item, err := h.gameByID(r.Context(), gameID)
	if err != nil {
		h.renderAdminError(w, r, err)
		return
	}
	message := ""
	if cause != nil {
		message = adminMessage(cause)
	}
	render(w, r, http.StatusOK, pages.AdminGameEntry(item, true, message))
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
	h.renderGames(w, r, status, items, "", adminMessage(cause), components.Notice{})
}

func (h *Administration) renderGameIGDBWithError(w http.ResponseWriter, r *http.Request, cause error, search string) {
	status := adminErrorStatus(cause)
	if status == http.StatusInternalServerError {
		h.renderAdminError(w, r, cause)
		return
	}
	item, err := h.gameByID(r.Context(), chi.URLParam(r, "gameID"))
	if err != nil {
		h.renderGameReconciliationError(w, r, err)
		return
	}
	results, err := h.games.SearchIGDB(r.Context(), search)
	if err != nil && !errors.Is(err, game.ErrInvalidSearch) {
		h.renderAdminError(w, r, err)
		return
	}
	render(w, r, status, pages.AdminGameIGDB(h.brand, h.navigationFor(r, "administration"), item, results, search, adminMessage(cause)))
}

func (h *Administration) renderMergeGame(w http.ResponseWriter, r *http.Request, status int, formError string) {
	items, err := h.games.List(r.Context(), "")
	if err != nil {
		h.renderAdminError(w, r, err)
		return
	}
	target, found := findAdminGame(items, chi.URLParam(r, "gameID"))
	if !found {
		h.renderGameReconciliationError(w, r, game.ErrNotFound)
		return
	}
	sourceID := r.URL.Query().Get("source")
	source, _ := findAdminGame(items, sourceID)
	render(w, r, status, pages.AdminGameMerge(h.brand, h.navigationFor(r, "administration"), target, source, items, sourceID, formError, notice(r)))
}

func (h *Administration) gameByID(ctx context.Context, id string) (game.Game, error) {
	items, err := h.games.List(ctx, "")
	if err != nil {
		return game.Game{}, err
	}
	item, found := findAdminGame(items, id)
	if !found {
		return game.Game{}, game.ErrNotFound
	}
	return item, nil
}

func findAdminGame(items []game.Game, id string) (game.Game, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return game.Game{}, false
}

func (h *Administration) renderGameReconciliationError(w http.ResponseWriter, r *http.Request, cause error) {
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
	h.renderGames(w, r, status, items, "", adminMessage(cause), components.Notice{})
}

func (h *Administration) renderAdminError(w http.ResponseWriter, r *http.Request, cause error) {
	webmiddleware.LogError(r, "serve administration request", cause)
	render(w, r, http.StatusInternalServerError, pages.AdminError(h.brand, h.navigationFor(r, "administration")))
}

func (h *Administration) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Administration) navigationFor(r *http.Request, section string) components.NavigationState {
	return h.navigation.For(r, section)
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
	case errors.Is(err, game.ErrInvalidProviderReference):
		return "Choose a valid IGDB record from the synchronized catalog."
	case errors.Is(err, game.ErrProviderRecordNotFound):
		return "That IGDB record is no longer in the active synchronized catalog. Search again."
	case errors.Is(err, game.ErrGameAlreadyLinked):
		return "This game already has an IGDB link. Unlink it before choosing another record."
	case errors.Is(err, game.ErrIGDBNotLinked):
		return "This game is not linked to IGDB."
	case errors.Is(err, game.ErrConflictingIGDBLink):
		return "Both games have different IGDB links. Unlink the incorrect record, then merge again."
	case errors.Is(err, game.ErrSameGame):
		return "Choose a different source game to merge."
	case errors.Is(err, game.ErrGameHasRequests):
		return "This game has Game Requests and cannot be deleted. Deactivate or merge it instead."
	case errors.Is(err, game.ErrGameHasFollowers):
		return "This game is followed by users. Merge or remove those relationships before deleting."
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
		errors.Is(err, game.ErrInvalidAlias), errors.Is(err, game.ErrAlreadyExists),
		errors.Is(err, game.ErrInvalidProviderReference), errors.Is(err, game.ErrProviderRecordNotFound),
		errors.Is(err, game.ErrGameAlreadyLinked), errors.Is(err, game.ErrIGDBNotLinked),
		errors.Is(err, game.ErrSameGame):
		return http.StatusUnprocessableEntity
	case errors.Is(err, game.ErrIGDBAlreadyLinked), errors.Is(err, game.ErrConflictingIGDBLink),
		errors.Is(err, game.ErrGameHasRequests), errors.Is(err, game.ErrGameHasFollowers):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func notice(r *http.Request) components.Notice {
	info := func(text string) components.Notice { return components.Notice{Text: text, Tone: components.NoticeInfo} }
	success := func(text string) components.Notice {
		return components.Notice{Text: text, Tone: components.NoticeSuccess}
	}
	switch strings.TrimSpace(r.URL.Query().Get("notice")) {
	case "game-created":
		return success("Game created.")
	case "game-updated":
		return info("Game updated.")
	case "game-status-updated":
		return info("Game availability updated.")
	case "alias-added":
		return info("Alias added.")
	case "alias-removed":
		return info("Alias removed.")
	case "igdb-linked":
		return success("IGDB record linked and enrichment refreshed.")
	case "igdb-unlinked":
		return info("IGDB link and enrichment removed.")
	case "igdb-link-conflict":
		return components.Notice{Text: "That IGDB record belongs to the selected source game. Review the merge below.", Tone: components.NoticeWarning}
	case "game-merged":
		return info("Games merged. Game Requests, followers, aliases, and provider identity were preserved.")
	case "game-deleted":
		return info("Unused game permanently deleted.")
	case "request-created":
		return success("Game Request created.")
	case "igdb-sync-started":
		return info("IGDB synchronization requested.")
	case "igdb-sync-already-running":
		return info("IGDB synchronization is already in progress.")
	default:
		return components.Notice{}
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := webmiddleware.CurrentUser(r.Context())
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		if account.Role != user.RoleAdmin {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
