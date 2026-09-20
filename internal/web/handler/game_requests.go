package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

const gameRequestTimeLayout = "2006-01-02T15:04"

type GameRequestCommands interface {
	Create(context.Context, string, string, *int32, time.Time, string) (gamerequest.GameRequest, error)
	Join(context.Context, string, string) error
	Leave(context.Context, string, string) error
	Close(context.Context, string, string) error
	Cancel(context.Context, string, string) error
}

type GameRequestQueries interface {
	ListActive(context.Context, string, requestquery.Filter) ([]requestquery.Request, error)
	ByID(context.Context, string, string) (requestquery.Request, error)
	Trending(context.Context) ([]requestquery.TrendingGame, error)
	CountOpen(context.Context) (int64, error)
}

// GameResolver turns an opaque IGDB provider reference into the one
// authoritative local Game for it, materializing that Game on first use
// (spec: "Follow and Game Request integration"). Only provider and external
// ID ever cross this boundary; the resolved Game's ID/name always come from
// the resolver, never from browser-supplied form fields.
type GameResolver interface {
	Resolve(context.Context, game.ProviderReference) (game.Game, error)
}

// GameRequestDiscovery is the composer's own narrow read-only view of the
// shared Game discovery service (Task 3/5): Browse renders the "Find your
// game"/"Change game" search, and Select looks up an already-chosen local or
// provider Game for display only. Neither call ever materializes a provider
// Game -- that only ever happens through GameResolver.Resolve, and only at
// the final validated Create POST (spec: "provider materialization must
// happen only during the final validated Game Request POST, never during
// GET/search/selection").
type GameRequestDiscovery interface {
	Browse(context.Context, string, string) (game.DiscoveryPage, error)
	Select(context.Context, string, game.GameSelection) (game.DiscoveryItem, error)
}

// gameCatalogUnavailableMessage explains a selected provider Game that has
// disappeared from the catalog by the time it is actually used -- either a
// stale/tampered composer URL (New, Selection) or the final Create POST
// racing a provider record disappearing (spec: "When a provider record
// disappears..."). It never suggests retrying the same choice, since catalog
// visibility, not a transient failure, is why it is gone.
const gameCatalogUnavailableMessage = "This game is no longer available from the game catalog. Choose another game."

type GameRequests struct {
	commands  GameRequestCommands
	queries   GameRequestQueries
	resolver  GameResolver
	discovery GameRequestDiscovery
	brand     branding.Brand
	now       func() time.Time
}

func NewGameRequests(commands GameRequestCommands, queries GameRequestQueries, resolver GameResolver, discovery GameRequestDiscovery, brand branding.Brand) *GameRequests {
	return &GameRequests{commands: commands, queries: queries, resolver: resolver, discovery: discovery, brand: brand, now: time.Now}
}

func (h *GameRequests) List(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	filter := discoveryFilter(r)
	requests, err := h.queries.ListActive(r.Context(), account.ID, filter)
	if errors.Is(err, requestquery.ErrInvalidSearch) {
		h.renderList(w, r, http.StatusUnprocessableEntity, nil, filter, "Search must be 120 characters or fewer.")
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "list active Game Requests", err)
		h.renderError(w, r)
		return
	}
	h.renderList(w, r, http.StatusOK, requests, filter, "")
}

// renderList answers a filter submission with the list alone and a full page
// load with the whole screen, so filtering stays server-rendered either way.
func (h *GameRequests) renderList(w http.ResponseWriter, r *http.Request, status int, requests []requestquery.Request, filter requestquery.Filter, message string) {
	navigation := requestNavigation(r)
	if r.Header.Get("HX-Request") == "true" {
		// The fragment carries the message too, or a rejected search would
		// swap in an empty list with nothing explaining why.
		render(w, r, status, pages.GameRequestListContent(h.brand, requests, filter, navigation.Viewer, "", true, nil, message))
		return
	}
	trending, err := h.queries.Trending(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "rank trending Games", err)
		h.renderError(w, r)
		return
	}
	openRequests, err := h.queries.CountOpen(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "count open Game Requests", err)
		h.renderError(w, r)
		return
	}
	render(w, r, status, pages.GameRequestList(h.brand, navigation, filter, requests, trending, openRequests, message, notice(r)))
}

func (h *GameRequests) RefreshList(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	filter := discoveryFilter(r)
	requests, err := h.queries.ListActive(r.Context(), account.ID, filter)
	if err != nil {
		webmiddleware.LogError(r, "refresh active Game Requests", err)
		http.Error(w, "refresh game requests", http.StatusInternalServerError)
		return
	}
	trending, err := h.queries.Trending(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "refresh trending Games", err)
		http.Error(w, "refresh game requests", http.StatusInternalServerError)
		return
	}
	navigation := requestNavigation(r)
	render(w, r, http.StatusOK, pages.GameRequestListContent(h.brand, requests, filter, navigation.Viewer, r.URL.Query().Get("highlight"), true, trending, ""))
}

// discoveryFilter reads the stable 1.0 Game Request discovery filters. An
// unknown or absent value simply leaves the list unfiltered.
func discoveryFilter(r *http.Request) requestquery.Filter {
	query := r.URL.Query()
	return requestquery.Filter{
		Search:         strings.TrimSpace(query.Get("q")),
		Scope:          requestquery.ParseScope(query.Get("scope")),
		OpenPlacesOnly: query.Get("open_places") == "1",
	}
}

// New renders the Game Request composer. It never materializes a provider
// Game: with a game_id or provider/external_id selection in the query, it
// calls the read-only discovery.Select; otherwise it calls discovery.Browse
// for the shared "Find your game" search, exactly like /games (Task 4).
func (h *GameRequests) New(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	form := composerFormFromValues(r.URL.Query())

	selection, hasSelection := gameSelectionFromValues(
		r.URL.Query().Get("game_id"), r.URL.Query().Get("provider"), r.URL.Query().Get("external_id"),
	)
	if !hasSelection {
		page, err := h.discovery.Browse(r.Context(), account.ID, search)
		if err != nil {
			webmiddleware.LogError(r, "browse Game Request discovery", err)
			h.renderError(w, r)
			return
		}
		render(w, r, http.StatusOK, pages.NewGameRequest(h.brand, requestNavigation(r), page, nil, false, search, form, ""))
		return
	}

	item, err := h.discovery.Select(r.Context(), account.ID, selection)
	if err != nil {
		if !isCatalogSelectionError(err) {
			webmiddleware.LogError(r, "select Game Request game", err)
			h.renderError(w, r)
			return
		}
		page, browseErr := h.discovery.Browse(r.Context(), account.ID, "")
		if browseErr != nil {
			webmiddleware.LogError(r, "browse Game Request discovery", browseErr)
			h.renderError(w, r)
			return
		}
		render(w, r, http.StatusUnprocessableEntity, pages.NewGameRequest(h.brand, requestNavigation(r), page, nil, false, "", form, gameCatalogUnavailableMessage))
		return
	}
	populateSelectionForm(&form, item)
	render(w, r, http.StatusOK, pages.NewGameRequest(h.brand, requestNavigation(r), game.DiscoveryPage{}, &item, true, "", form, ""))
}

// Discovery is the composer's live-search fragment (used by the "Change
// game" search, Task 7 Step 6): it always calls the same read-only
// discovery.Browse as New/Games and never materializes anything. changeGame
// is always true here, since this fragment is only ever reachable from the
// "Change game" panel that itself only exists once a selection is already
// active -- unlike the composer's own first-pick discovery list, which
// stays a plain full-page navigation (New, changeGame=false).
func (h *GameRequests) Discovery(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := r.URL.Query().Get("q")
	page, err := h.discovery.Browse(r.Context(), account.ID, search)
	if err != nil {
		webmiddleware.LogError(r, "browse Game Request discovery", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDiscovery(page, search, true, requestNavigation(r).Viewer, pages.GameRequestForm{}))
}

// Selection is the composer's read-only reselection endpoint (Task 7 Step
// 7): it parses exactly the same identity fields as New, calls the
// read-only discovery.Select, and never touches GameResolver.Resolve, so
// picking a different game in the composer never materializes a provider
// Game by itself. For htmx it swaps only the selected-game display and the
// hidden identity fields -- never #game-request-details -- so the request
// details the player already typed stay exactly as they are in the browser.
func (h *GameRequests) Selection(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	selection, ok := gameSelectionFromValues(
		r.URL.Query().Get("game_id"), r.URL.Query().Get("provider"), r.URL.Query().Get("external_id"),
	)
	if !ok {
		http.Error(w, "select a game", http.StatusUnprocessableEntity)
		return
	}
	item, err := h.discovery.Select(r.Context(), account.ID, selection)
	if err != nil {
		h.renderSelectionError(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, http.StatusOK, pages.GameRequestSelectionSwap(item))
		return
	}
	h.redirect(w, r, "/game-requests/new?"+pages.GameRequestSelectionQuery(item))
}

func (h *GameRequests) renderSelectionError(w http.ResponseWriter, r *http.Request, cause error) {
	if isCatalogSelectionError(cause) {
		http.Error(w, "game not found", http.StatusNotFound)
		return
	}
	webmiddleware.LogError(r, "select Game Request game", cause)
	http.Error(w, "select game", http.StatusInternalServerError)
}

func (h *GameRequests) Create(w http.ResponseWriter, r *http.Request) {
	form, maxPlayers, startsAt, err := h.parseCreateForm(r)
	if err == nil {
		form.Description, err = gamerequest.ValidateCreateInput(maxPlayers, startsAt, form.Description)
	}
	catalogUnavailable := false
	if err == nil && form.GameID == "" && (form.Provider != "" || form.ExternalID != "") {
		var ref game.ProviderReference
		ref, err = game.NewProviderReference(form.Provider, form.ExternalID)
		if err == nil && h.resolver == nil {
			catalogUnavailable = true
			form.Provider = ""
			form.ExternalID = ""
		} else if err == nil {
			var resolved game.Game
			resolved, err = h.resolver.Resolve(r.Context(), ref)
			if err == nil {
				form.GameID = resolved.ID
			} else if errors.Is(err, game.ErrProviderRecordNotFound) {
				// The catalog no longer has this provider record: clear it from the
				// form so the composer does not keep offering to resubmit the same
				// dead selection, and ask for a different game instead. No
				// compensating delete/unmaterialize is needed -- Resolve never got
				// far enough to materialize anything for this reference.
				catalogUnavailable = true
				form.Provider = ""
				form.ExternalID = ""
			}
		}
	}
	if err == nil {
		account, _ := webmiddleware.CurrentUser(r.Context())
		var created gamerequest.GameRequest
		// Resolve above may already have materialized an authoritative local
		// Game (form.GameID). If commands.Create itself now fails, that
		// materialization is intentionally not undone -- form.GameID keeps it
		// selected on the re-render below, with no rollback/delete compensation.
		created, err = h.commands.Create(r.Context(), account.ID, form.GameID, maxPlayers, startsAt, form.Description)
		if err == nil {
			h.redirect(w, r, "/game-requests/"+created.ID+"?notice=request-created")
			return
		}
	}
	status := gameRequestErrorStatus(err)
	message := gameRequestMessage(err)
	if catalogUnavailable {
		status = http.StatusUnprocessableEntity
		message = gameCatalogUnavailableMessage
	}
	h.renderComposerError(w, r, form, status, message)
}

// renderComposerError re-renders the composer after a Create failure,
// keeping every posted request-detail value (max players, start, description)
// and, when the previously selected Game is still valid, keeping it selected
// too (Task 7 Step 9). It never re-resolves a provider reference -- only the
// read-only discovery.Select -- so re-rendering a failed submission can never
// itself materialize anything.
func (h *GameRequests) renderComposerError(w http.ResponseWriter, r *http.Request, form pages.GameRequestForm, status int, message string) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	var selected *game.DiscoveryItem
	if selection, ok := gameSelectionFromValues(form.GameID, form.Provider, form.ExternalID); ok {
		if item, err := h.discovery.Select(r.Context(), account.ID, selection); err == nil {
			selected = &item
		}
	}
	var page game.DiscoveryPage
	if selected == nil {
		var err error
		page, err = h.discovery.Browse(r.Context(), account.ID, "")
		if err != nil {
			webmiddleware.LogError(r, "reload Game Request discovery", err)
			h.renderError(w, r)
			return
		}
	}
	render(w, r, status, pages.NewGameRequest(h.brand, requestNavigation(r), page, selected, true, "", form, message))
}

// gameSelectionFromValues parses the composer's identity fields (shared by
// New, Selection, and the Create error re-render) into a game.GameSelection.
// Exactly one of gameID or provider/externalID must be non-empty; ok is
// false when none of them are set at all -- an ambiguous combination of both
// is left for game.GameSelection/DiscoveryService.Select itself to reject.
func gameSelectionFromValues(gameID, provider, externalID string) (game.GameSelection, bool) {
	gameID = strings.TrimSpace(gameID)
	provider = strings.TrimSpace(provider)
	externalID = strings.TrimSpace(externalID)
	if gameID == "" && provider == "" && externalID == "" {
		return game.GameSelection{}, false
	}
	if gameID != "" {
		return game.GameSelection{LocalGameID: gameID}, true
	}
	return game.GameSelection{Reference: game.ProviderReference{Provider: provider, ExternalID: externalID}}, true
}

// populateSelectionForm carries a resolved selection's own (normalized)
// identity onto the composer form's hidden fields, rather than echoing the
// browser's raw, unnormalized query values back.
func populateSelectionForm(form *pages.GameRequestForm, item game.DiscoveryItem) {
	form.GameID = item.LocalGameID
	if form.GameID == "" {
		form.Provider = item.Reference.Provider
		form.ExternalID = item.Reference.ExternalID
	}
}

// composerFormFromValues restores request-detail transport state from a
// normal composer GET. Selection is parsed separately and remains opaque.
func composerFormFromValues(values url.Values) pages.GameRequestForm {
	form := pages.GameRequestForm{
		MaxPlayers:     strings.TrimSpace(values.Get("max_players")),
		StartPreset:    strings.TrimSpace(values.Get("start_preset")),
		StartDate:      strings.TrimSpace(values.Get("start_date")),
		StartTime:      strings.TrimSpace(values.Get("start_time")),
		StartsAt:       strings.TrimSpace(values.Get("starts_at")),
		TimezoneOffset: strings.TrimSpace(values.Get("timezone_offset")),
		Description:    values.Get("description"),
	}
	if form.StartPreset == "" {
		form.StartPreset = "now"
	}
	return form
}

// isCatalogSelectionError reports whether cause means "this selection does
// not identify a usable catalog Game" -- a stale/tampered local Game ID, a
// provider record the active generation no longer has, or a malformed
// provider reference -- as opposed to an unexpected infrastructure failure.
func isCatalogSelectionError(cause error) bool {
	return errors.Is(cause, game.ErrNotFound) ||
		errors.Is(cause, game.ErrProviderRecordNotFound) ||
		errors.Is(cause, game.ErrInvalidProviderReference) ||
		errors.Is(cause, game.ErrInvalidGameSelection)
}

func (h *GameRequests) View(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	request, err := h.queries.ByID(r.Context(), chi.URLParam(r, "requestID"), account.ID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "load Game Request", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDetail(h.brand, requestNavigation(r), request, account.ID, "", notice(r)))
}

func (h *GameRequests) RefreshView(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	request, err := h.queries.ByID(r.Context(), chi.URLParam(r, "requestID"), account.ID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "refresh Game Request", err)
		http.Error(w, "refresh game request", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDetailContent(request, account.ID, true))
}

func (h *GameRequests) Join(w http.ResponseWriter, r *http.Request) {
	h.changeMembership(w, r, h.commands.Join)
}

func (h *GameRequests) Leave(w http.ResponseWriter, r *http.Request) {
	h.changeMembership(w, r, h.commands.Leave)
}

func (h *GameRequests) Close(w http.ResponseWriter, r *http.Request) {
	h.changeMembership(w, r, h.commands.Close)
}

func (h *GameRequests) Cancel(w http.ResponseWriter, r *http.Request) {
	h.changeMembership(w, r, h.commands.Cancel)
}

func (h *GameRequests) changeMembership(w http.ResponseWriter, r *http.Request, change func(context.Context, string, string) error) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	requestID := chi.URLParam(r, "requestID")
	if err := change(r.Context(), requestID, account.ID); err != nil {
		h.renderChangeError(w, r, requestID, account.ID, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" && r.FormValue("return_to") == "list" {
		request, err := h.queries.ByID(r.Context(), requestID, account.ID)
		if err != nil {
			webmiddleware.LogError(r, "reload Game Request after command", err)
			h.renderError(w, r)
			return
		}
		render(w, r, http.StatusOK, pages.GameRequestRow(request, account.ID, ""))
		return
	}
	target := "/game-requests/" + requestID
	if r.FormValue("return_to") == "list" {
		target = "/game-requests"
	}
	h.redirect(w, r, target)
}

func (h *GameRequests) renderChangeError(w http.ResponseWriter, r *http.Request, requestID, viewerID string, cause error) {
	status := gameRequestErrorStatus(cause)
	if status == http.StatusInternalServerError {
		webmiddleware.LogError(r, "change Game Request", cause)
		h.renderError(w, r)
		return
	}
	if r.Header.Get("HX-Request") == "true" && r.FormValue("return_to") == "list" {
		w.Header().Set("HX-Retarget", "#live-notices")
		w.Header().Set("HX-Reswap", "beforeend")
		render(w, r, http.StatusOK, pages.GameRequestActionError(gameRequestMessage(cause)))
		return
	}
	request, err := h.queries.ByID(r.Context(), requestID, viewerID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "reload Game Request after command error", err)
		h.renderError(w, r)
		return
	}
	render(w, r, status, pages.GameRequestDetail(h.brand, requestNavigation(r), request, viewerID, gameRequestMessage(cause), ""))
}

func (h *GameRequests) parseCreateForm(r *http.Request) (pages.GameRequestForm, *int32, time.Time, error) {
	if err := r.ParseForm(); err != nil {
		return pages.GameRequestForm{}, nil, time.Time{}, gamerequest.ErrInvalidDescription
	}
	form := pages.GameRequestForm{
		GameID: r.FormValue("game_id"), MaxPlayers: strings.TrimSpace(r.FormValue("max_players")),
		StartPreset: strings.TrimSpace(r.FormValue("start_preset")), StartDate: strings.TrimSpace(r.FormValue("start_date")),
		StartTime: strings.TrimSpace(r.FormValue("start_time")), StartsAt: strings.TrimSpace(r.FormValue("starts_at")),
		TimezoneOffset: strings.TrimSpace(r.FormValue("timezone_offset")),
		Description:    r.FormValue("description"),
		Provider:       strings.TrimSpace(r.FormValue("provider")), ExternalID: strings.TrimSpace(r.FormValue("external_id")),
	}
	var maxPlayers *int32
	if form.MaxPlayers != "" {
		value, err := strconv.ParseInt(form.MaxPlayers, 10, 32)
		if err != nil || value < 1 || value > math.MaxInt32 {
			return form, nil, time.Time{}, gamerequest.ErrInvalidMaxPlayers
		}
		parsed := int32(value)
		maxPlayers = &parsed
	}
	startsAt := h.now().UTC()
	var startErr error
	switch form.StartPreset {
	case "", "now":
		if form.StartPreset == "" && form.StartsAt != "" {
			startsAt, startErr = parseBrowserTime(form.StartsAt, form.TimezoneOffset)
		}
	case "15", "30", "60":
		minutes, _ := strconv.Atoi(form.StartPreset)
		startsAt = h.now().Add(time.Duration(minutes) * time.Minute).UTC()
	case "custom":
		if form.StartDate == "" || form.StartTime == "" {
			return form, maxPlayers, time.Time{}, gamerequest.ErrInvalidStartsAt
		}
		startsAt, startErr = parseBrowserTime(form.StartDate+"T"+form.StartTime, form.TimezoneOffset)
	default:
		return form, maxPlayers, time.Time{}, gamerequest.ErrInvalidStartsAt
	}
	if startErr != nil {
		return form, maxPlayers, time.Time{}, gamerequest.ErrInvalidStartsAt
	}
	return form, maxPlayers, startsAt, nil
}

func parseBrowserTime(value, timezoneOffset string) (time.Time, error) {
	offsetMinutes := int64(0)
	if timezoneOffset != "" {
		parsed, err := strconv.ParseInt(timezoneOffset, 10, 16)
		if err != nil || parsed < -14*60 || parsed > 14*60 {
			return time.Time{}, gamerequest.ErrInvalidStartsAt
		}
		offsetMinutes = parsed
	}
	location := time.FixedZone("browser-local", int(-offsetMinutes*60))
	parsed, err := time.ParseInLocation(gameRequestTimeLayout, value, location)
	if err != nil {
		return time.Time{}, gamerequest.ErrInvalidStartsAt
	}
	return parsed.UTC(), nil
}

func (h *GameRequests) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *GameRequests) renderError(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusInternalServerError, pages.GameRequestError(h.brand, requestNavigation(r)))
}

func requestNavigation(r *http.Request) components.NavigationState {
	account, _ := webmiddleware.CurrentUser(r.Context())
	return components.NavigationState{Viewer: &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)}, Section: "game-requests"}
}

func gameRequestErrorStatus(err error) int {
	switch {
	case errors.Is(err, gamerequest.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, gamerequest.ErrNotCreator):
		return http.StatusForbidden
	case errors.Is(err, gamerequest.ErrFull), errors.Is(err, gamerequest.ErrAlreadyMember),
		errors.Is(err, gamerequest.ErrNotMember), errors.Is(err, gamerequest.ErrCreatorCannotLeave),
		errors.Is(err, gamerequest.ErrNotOpen):
		return http.StatusConflict
	case errors.Is(err, gamerequest.ErrGameUnavailable),
		errors.Is(err, gamerequest.ErrInvalidMaxPlayers), errors.Is(err, gamerequest.ErrInvalidStartsAt),
		errors.Is(err, gamerequest.ErrInvalidDescription), errors.Is(err, game.ErrInvalidProviderReference):
		return http.StatusUnprocessableEntity
	case errors.Is(err, game.ErrProviderRecordNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func gameRequestMessage(err error) string {
	switch {
	case errors.Is(err, gamerequest.ErrGameUnavailable):
		return "Choose an active game from the list."
	case errors.Is(err, gamerequest.ErrInvalidMaxPlayers):
		return "Maximum players must be a whole number of at least 1, or left blank."
	case errors.Is(err, gamerequest.ErrInvalidStartsAt):
		return "Enter a valid scheduled start time, or leave it blank to start now."
	case errors.Is(err, gamerequest.ErrInvalidDescription):
		return "Description must be 1000 characters or fewer."
	case errors.Is(err, gamerequest.ErrFull):
		return "This Game Request is already full."
	case errors.Is(err, gamerequest.ErrAlreadyMember):
		return "You have already joined this Game Request."
	case errors.Is(err, gamerequest.ErrNotMember):
		return "You are not currently a member of this Game Request."
	case errors.Is(err, gamerequest.ErrCreatorCannotLeave):
		return "The creator cannot leave. Close or cancel the Game Request instead."
	case errors.Is(err, gamerequest.ErrNotCreator):
		return "Only the creator can close or cancel this Game Request."
	case errors.Is(err, gamerequest.ErrNotOpen):
		return "This Game Request is no longer open."
	case errors.Is(err, game.ErrInvalidProviderReference):
		return "That provider game reference is invalid."
	case errors.Is(err, game.ErrProviderRecordNotFound):
		return "That provider game could not be found."
	default:
		return "The Game Request could not be changed. Please try again."
	}
}
