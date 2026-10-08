package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones for LoadLocation in minimal container images
	"unicode/utf8"

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

// maxPlayersLimit bounds what the composer accepts in the max players
// field. A LAN round of a thousand players is a typo, not a plan, and the
// field says so before the request is created.
const maxPlayersLimit = 999

type GameRequestCommands interface {
	Create(context.Context, string, string, *int32, time.Time, string) (gamerequest.GameRequest, error)
	Join(context.Context, string, string) error
	Leave(context.Context, string, string) error
	Start(context.Context, string, string) error
	Close(context.Context, string, string) error
	Cancel(context.Context, string, string) error
	UpdateDetails(context.Context, string, string, gamerequest.UpdateDetailsInput) error
	RecordView(context.Context, string, string) error
}

type GameRequestQueries interface {
	ListActive(context.Context, string, requestquery.Filter) ([]requestquery.Request, error)
	ByID(context.Context, string, string) (requestquery.Request, error)
	Trending(context.Context) ([]requestquery.TrendingGame, error)
	CountActive(context.Context) (int64, error)
	HasCreated(context.Context, string) (bool, error)
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
	BrowseFollowed(context.Context, string, string) (game.DiscoveryPage, error)
	Select(context.Context, string, game.GameSelection) (game.DiscoveryItem, error)
}

// browsePicker is the composer's "Find your game" list. Before a real
// search it is the viewer's followed Games alone -- the short list of what
// they actually play, which is what a Game Request usually starts from --
// while any search of two characters or more stays the same full catalog
// search /games runs. A one-character search never reaches the repository
// (DiscoveryService.Browse's SearchTooShort rule) and so still shows that
// same followed list, rather than flashing the whole local catalog between
// the first and second keystroke.
func (h *GameRequests) browsePicker(ctx context.Context, userID, search string) (game.DiscoveryPage, error) {
	if utf8.RuneCountInString(search) < 2 {
		return h.discovery.BrowseFollowed(ctx, userID, search)
	}
	return h.discovery.Browse(ctx, userID, search)
}

// gameCatalogUnavailableMessage explains a selected provider Game that has
// disappeared from the catalog by the time it is actually used -- either a
// stale/tampered composer URL (New, Selection) or the final Create POST
// racing a provider record disappearing (spec: "When a provider record
// disappears..."). It never suggests retrying the same choice, since catalog
// visibility, not a transient failure, is why it is gone.
const gameCatalogUnavailableMessage = "This game is no longer available from the game catalog. Choose another game."

type GameRequests struct {
	commands           GameRequestCommands
	queries            GameRequestQueries
	resolver           GameResolver
	discovery          GameRequestDiscovery
	sessionInformation GameRequestSessionInformation
	navigation         *Navigation
	brand              branding.Brand
	now                func() time.Time
}

func NewGameRequests(commands GameRequestCommands, queries GameRequestQueries, resolver GameResolver, discovery GameRequestDiscovery, navigation *Navigation, brand branding.Brand, session ...GameRequestSessionInformation) *GameRequests {
	h := &GameRequests{commands: commands, queries: queries, resolver: resolver, discovery: discovery, navigation: navigation, brand: brand, now: time.Now}
	if len(session) > 0 {
		h.sessionInformation = session[0]
	}
	return h
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
	navigation := h.navigationFor(r)
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
	activeRequests, err := h.queries.CountActive(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "count active Game Requests", err)
		h.renderError(w, r)
		return
	}
	render(w, r, status, pages.GameRequestList(h.brand, navigation, filter, requests, trending, activeRequests, message, notice(r)))
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
	navigation := h.navigationFor(r)
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
// calls the read-only discovery.Select; otherwise it calls browsePicker for
// the "Find your game" list: followed Games before a search, the same shared
// catalog search /games runs once the player types.
func (h *GameRequests) New(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	form := composerFormFromValues(r.URL.Query())

	selection, hasSelection := gameSelectionFromValues(
		r.URL.Query().Get("game_id"), r.URL.Query().Get("provider"), r.URL.Query().Get("external_id"),
	)
	if !hasSelection {
		page, err := h.browsePicker(r.Context(), account.ID, search)
		if err != nil {
			webmiddleware.LogError(r, "browse Game Request discovery", err)
			h.renderError(w, r)
			return
		}
		render(w, r, http.StatusOK, pages.NewGameRequest(h.brand, h.navigationFor(r), page, nil, false, search, form, pages.ComposerMessage{}, h.firstRequest(r, account.ID)))
		return
	}

	item, err := h.discovery.Select(r.Context(), account.ID, selection)
	if err != nil {
		if !isCatalogSelectionError(err) {
			webmiddleware.LogError(r, "select Game Request game", err)
			h.renderError(w, r)
			return
		}
		page, browseErr := h.browsePicker(r.Context(), account.ID, "")
		if browseErr != nil {
			webmiddleware.LogError(r, "browse Game Request discovery", browseErr)
			h.renderError(w, r)
			return
		}
		render(w, r, http.StatusUnprocessableEntity, pages.NewGameRequest(h.brand, h.navigationFor(r), page, nil, false, "", form, pages.ComposerMessage{Text: gameCatalogUnavailableMessage}, h.firstRequest(r, account.ID)))
		return
	}
	populateSelectionForm(&form, item)
	render(w, r, http.StatusOK, pages.NewGameRequest(h.brand, h.navigationFor(r), game.DiscoveryPage{}, &item, true, "", form, pages.ComposerMessage{}, h.firstRequest(r, account.ID)))
}

// firstRequest reports whether the composer should explain Game Requests to a
// player who has never created one. The hint is decoration: a failed read
// is logged and simply hides it.
func (h *GameRequests) firstRequest(r *http.Request, viewerID string) bool {
	created, err := h.queries.HasCreated(r.Context(), viewerID)
	if err != nil {
		webmiddleware.LogError(r, "check created Game Requests", err)
		return false
	}
	return !created
}

// Discovery is the composer's live-search fragment (Task 7 Step 6): it
// calls the same read-only browsePicker as New and never materializes
// anything. The search form carries the composer's request details as
// hidden transport fields, so the cards this fragment renders keep them in
// their hrefs and a failed-submission recovery never loses what the player
// typed.
func (h *GameRequests) Discovery(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	search := r.URL.Query().Get("q")
	page, err := h.browsePicker(r.Context(), account.ID, search)
	if err != nil {
		webmiddleware.LogError(r, "browse Game Request discovery", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDiscovery(page, search, h.navigationFor(r).Viewer, composerFormFromValues(r.URL.Query())))
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
	message := pages.ComposerMessage{Text: gameRequestMessage(err), Field: gameRequestMessageField(err)}
	if catalogUnavailable {
		status = http.StatusUnprocessableEntity
		message = pages.ComposerMessage{Text: gameCatalogUnavailableMessage}
	}
	h.renderComposerError(w, r, form, status, message)
}

// renderComposerError re-renders the composer after a Create failure,
// keeping every posted request-detail value (max players, start, description)
// and, when the previously selected Game is still valid, keeping it selected
// too (Task 7 Step 9). It never re-resolves a provider reference -- only the
// read-only discovery.Select -- so re-rendering a failed submission can never
// itself materialize anything.
func (h *GameRequests) renderComposerError(w http.ResponseWriter, r *http.Request, form pages.GameRequestForm, status int, message pages.ComposerMessage) {
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
		page, err = h.browsePicker(r.Context(), account.ID, "")
		if err != nil {
			webmiddleware.LogError(r, "reload Game Request discovery", err)
			h.renderError(w, r)
			return
		}
	}
	render(w, r, status, pages.NewGameRequest(h.brand, h.navigationFor(r), page, selected, true, "", form, message, h.firstRequest(r, account.ID)))
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
		MaxPlayers:  strings.TrimSpace(values.Get("max_players")),
		StartPreset: strings.TrimSpace(values.Get("start_preset")),
		StartDate:   strings.TrimSpace(values.Get("start_date")),
		StartTime:   strings.TrimSpace(values.Get("start_time")),
		StartsAt:    strings.TrimSpace(values.Get("starts_at")),
		Timezone:    strings.TrimSpace(values.Get("timezone")),
		Description: values.Get("description"),
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
	if err := h.commands.RecordView(r.Context(), request.ID, account.ID); err != nil {
		webmiddleware.LogError(r, "record Game Request view", err)
	}
	message := detailEditorMessage(request, account.ID, r.URL.Query().Get("edit") == "1")
	if r.Header.Get("HX-Request") == "true" && r.URL.Query().Get("edit") == "1" {
		message.Dialog = true
		if !message.Editing {
			http.Error(w, "game request not found", http.StatusNotFound)
			return
		}
		render(w, r, http.StatusOK, pages.GameRequestDetailsEditor(request, message))
		return
	}
	session, sessionErr := h.sessionView(r, sessionModeFromQuery(r))
	if sessionErr != nil {
		webmiddleware.LogError(r, "load session information", sessionErr)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDetail(h.brand, h.navigationFor(r), request, account.ID, message, notice(r), session))
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
	message := detailEditorMessage(request, account.ID, r.URL.Query().Get("edit") == "1")
	session, sessionErr := h.sessionView(r, sessionModeFromQuery(r))
	if sessionErr != nil {
		webmiddleware.LogError(r, "refresh session information", sessionErr)
		http.Error(w, "refresh session information", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestDetailContentWithSession(request, account.ID, true, message, session))
}
func detailEditorMessage(request requestquery.Request, viewerID string, editing bool) pages.GameRequestDetailMessage {
	if !editing || request.CreatorID != viewerID ||
		(request.Status != gamerequest.StatusOpen && request.Status != gamerequest.StatusPlaying) {
		return pages.GameRequestDetailMessage{}
	}
	return pages.GameRequestDetailMessage{
		Editing: true, InitializeStart: request.Status == gamerequest.StatusOpen,
		Form: requestDetailsForm(request),
	}
}

func (h *GameRequests) Join(w http.ResponseWriter, r *http.Request) {
	h.changeRequest(w, r, h.commands.Join)
}

func (h *GameRequests) Leave(w http.ResponseWriter, r *http.Request) {
	h.changeRequest(w, r, h.commands.Leave)
}

func (h *GameRequests) Start(w http.ResponseWriter, r *http.Request) {
	h.changeRequest(w, r, h.commands.Start)
}

func (h *GameRequests) Close(w http.ResponseWriter, r *http.Request) {
	h.changeRequest(w, r, h.commands.Close)
}

func (h *GameRequests) Cancel(w http.ResponseWriter, r *http.Request) {
	h.changeRequest(w, r, h.commands.Cancel)
}

func (h *GameRequests) changeRequest(w http.ResponseWriter, r *http.Request, change func(context.Context, string, string) error) {
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
	session, sessionErr := h.sessionView(r, "")
	if sessionErr != nil {
		webmiddleware.LogError(r, "reload session information after request error", sessionErr)
		h.renderError(w, r)
		return
	}
	render(w, r, status, pages.GameRequestDetail(h.brand, h.navigationFor(r), request, viewerID, pages.GameRequestDetailMessage{Text: gameRequestMessage(cause)}, components.Notice{}, session))
}

// UpdateDetails changes the Host-editable request fields in one command. The
// current start remains authoritative for playing requests, even if a client
// submits forged date/time fields.
func (h *GameRequests) UpdateDetails(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	requestID := chi.URLParam(r, "requestID")
	if err := r.ParseForm(); err != nil {
		webmiddleware.LogError(r, "parse Game Request details", err)
		h.renderError(w, r)
		return
	}
	form := requestDetailsFormFromValues(r)
	request, err := h.queries.ByID(r.Context(), requestID, account.ID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "load Game Request before details update", err)
		h.renderError(w, r)
		return
	}

	if request.Status == gamerequest.StatusOpen && form.StartPreset == "" {
		form.StartPreset = "custom"
	}
	maxPlayers, err := parseCapacity(form.MaxPlayers)
	startsAt := request.StartsAt
	if err == nil {
		if request.Status == gamerequest.StatusPlaying &&
			(form.StartPreset != "" || form.StartDate != "" || form.StartTime != "" || form.Timezone != "") {
			err = gamerequest.ErrInvalidStartsAt
		} else if request.Status == gamerequest.StatusOpen {
			startsAt, err = parseGameRequestStart(form, h.now())
			if err == nil && form.StartPreset == "custom" {
				location := time.UTC
				if form.Timezone != "" {
					location, err = time.LoadLocation(form.Timezone)
					if err != nil {
						err = gamerequest.ErrInvalidStartsAt
					}
				}
				submitted := form.StartDate + "T" + form.StartTime
				if err == nil && request.StartsAt.In(location).Format(gameRequestTimeLayout) == submitted {
					startsAt = request.StartsAt
				}
			}
		}
	}
	description := form.Description
	if err == nil {
		description, err = gamerequest.ValidateCreateInput(maxPlayers, startsAt, form.Description)
	}
	if err == nil {
		err = h.commands.UpdateDetails(r.Context(), requestID, account.ID, gamerequest.UpdateDetailsInput{
			MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description,
		})
	}
	if err == nil {
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Trigger", "game-request-details-saved")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.redirect(w, r, "/game-requests/"+requestID)
		return
	}
	status := gameRequestErrorStatus(err)
	if status == http.StatusInternalServerError {
		webmiddleware.LogError(r, "change Game Request details", err)
		h.renderError(w, r)
		return
	}
	request, queryErr := h.queries.ByID(r.Context(), requestID, account.ID)
	if errors.Is(queryErr, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if queryErr != nil {
		webmiddleware.LogError(r, "reload Game Request after details error", queryErr)
		h.renderError(w, r)
		return
	}
	message := updateDetailsMessage(err, form)
	message.Editing = request.CreatorID == account.ID &&
		(request.Status == gamerequest.StatusOpen || request.Status == gamerequest.StatusPlaying)
	message.Form = form
	message.InitializeStart = false
	session, sessionErr := h.sessionView(r, "")
	if sessionErr != nil {
		webmiddleware.LogError(r, "reload session information after details error", sessionErr)
		h.renderError(w, r)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		message.Dialog = true
		render(w, r, http.StatusOK, pages.GameRequestDetailsEditor(request, message))
		return
	}
	render(w, r, status, pages.GameRequestDetail(h.brand, h.navigationFor(r), request, account.ID, message, components.Notice{}, session))
}
func parseCapacity(value string) (*int32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 0 || parsed == 1 || parsed > maxPlayersLimit {
		return nil, gamerequest.ErrInvalidMaxPlayers
	}
	if parsed == 0 {
		return nil, nil
	}
	maximum := int32(parsed)
	return &maximum, nil
}

func requestDetailsForm(request requestquery.Request) pages.GameRequestForm {
	maxPlayers := "0"
	if request.MaxPlayers != nil {
		maxPlayers = strconv.Itoa(int(*request.MaxPlayers))
	}
	startsAt := request.StartsAt.UTC()
	return pages.GameRequestForm{
		MaxPlayers: maxPlayers, StartPreset: "custom", StartDate: startsAt.Format("2006-01-02"),
		StartTime: startsAt.Format("15:04"), Timezone: "UTC", Description: request.Description,
	}
}

func requestDetailsFormFromValues(r *http.Request) pages.GameRequestForm {
	return pages.GameRequestForm{
		MaxPlayers: r.FormValue("max_players"), StartPreset: strings.TrimSpace(r.FormValue("start_preset")),
		StartDate: r.FormValue("start_date"), StartTime: r.FormValue("start_time"), Timezone: r.FormValue("timezone"),
		Description: r.FormValue("description"),
	}
}

func updateDetailsMessage(err error, form pages.GameRequestForm) pages.GameRequestDetailMessage {
	switch {
	case errors.Is(err, gamerequest.ErrInvalidMaxPlayers):
		return pages.GameRequestDetailMessage{Text: gameRequestMessage(err), Field: "max_players", Value: form.MaxPlayers}
	case errors.Is(err, gamerequest.ErrCapacityBelowMembers):
		return pages.GameRequestDetailMessage{Text: "Maximum players cannot be lower than the current player count.", Field: "max_players", Value: form.MaxPlayers}
	case errors.Is(err, gamerequest.ErrInvalidStartsAt):
		return pages.GameRequestDetailMessage{Text: gameRequestMessage(err), Field: "start"}
	case errors.Is(err, gamerequest.ErrInvalidDescription):
		return pages.GameRequestDetailMessage{Text: gameRequestMessage(err), Field: "description"}
	case errors.Is(err, gamerequest.ErrNotActive):
		return pages.GameRequestDetailMessage{Text: "This Game Request is no longer active."}
	case errors.Is(err, gamerequest.ErrNotCreator):
		return pages.GameRequestDetailMessage{Text: "Only the Host can edit this Game Request."}
	default:
		return pages.GameRequestDetailMessage{Text: gameRequestMessage(err)}
	}
}

func (h *GameRequests) parseCreateForm(r *http.Request) (pages.GameRequestForm, *int32, time.Time, error) {
	if err := r.ParseForm(); err != nil {
		return pages.GameRequestForm{}, nil, time.Time{}, gamerequest.ErrInvalidDescription
	}
	form := pages.GameRequestForm{
		GameID: r.FormValue("game_id"), MaxPlayers: strings.TrimSpace(r.FormValue("max_players")),
		StartPreset: strings.TrimSpace(r.FormValue("start_preset")), StartDate: strings.TrimSpace(r.FormValue("start_date")),
		StartTime: strings.TrimSpace(r.FormValue("start_time")), StartsAt: strings.TrimSpace(r.FormValue("starts_at")),
		Timezone:    strings.TrimSpace(r.FormValue("timezone")),
		Description: r.FormValue("description"),
		Provider:    strings.TrimSpace(r.FormValue("provider")), ExternalID: strings.TrimSpace(r.FormValue("external_id")),
	}
	// 0 and an empty field both mean "no limit", which the domain spells as
	// a nil maximum. maxPlayersLimit is the composer's own input bound, not
	// a domain rule: the domain only requires a finite maximum to be at least
	// 2, and rounds created before this bound existed keep theirs. A 1 passes
	// this parse and is rejected by the domain with the same field error.
	var maxPlayers *int32
	if form.MaxPlayers != "" {
		value, err := strconv.ParseInt(form.MaxPlayers, 10, 32)
		if err != nil || value < 0 || value > maxPlayersLimit {
			return form, nil, time.Time{}, gamerequest.ErrInvalidMaxPlayers
		}
		if value > 0 {
			parsed := int32(value)
			maxPlayers = &parsed
		}
	}
	startsAt, err := parseGameRequestStart(form, h.now())
	if err != nil {
		return form, maxPlayers, time.Time{}, err
	}
	return form, maxPlayers, startsAt, nil
}

func parseGameRequestStart(form pages.GameRequestForm, now time.Time) (time.Time, error) {
	var startsAt time.Time
	var err error
	switch form.StartPreset {
	case "", "now":
		startsAt = now.UTC()
		if form.StartPreset == "" && form.StartsAt != "" {
			startsAt, err = parseBrowserTime(form.StartsAt, form.Timezone)
		}
	case "15", "30", "60":
		minutes, _ := strconv.Atoi(form.StartPreset)
		startsAt = now.Add(time.Duration(minutes) * time.Minute).UTC()
	case "custom":
		if form.StartDate == "" || form.StartTime == "" {
			return time.Time{}, gamerequest.ErrInvalidStartsAt
		}
		startsAt, err = parseBrowserTime(form.StartDate+"T"+form.StartTime, form.Timezone)
	default:
		return time.Time{}, gamerequest.ErrInvalidStartsAt
	}
	if err != nil {
		return time.Time{}, gamerequest.ErrInvalidStartsAt
	}
	return startsAt, nil
}

// parseBrowserTime reads a browser-local date/time in the viewer's IANA zone,
// so DST applies at the selected date rather than at submission time. An
// empty zone means UTC.
func parseBrowserTime(value, timezone string) (time.Time, error) {
	location := time.UTC
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, gamerequest.ErrInvalidStartsAt
		}
		location = loaded
	}
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
	render(w, r, http.StatusInternalServerError, pages.GameRequestError(h.brand, h.navigationFor(r)))
}

func (h *GameRequests) navigationFor(r *http.Request) components.NavigationState {
	return h.navigation.For(r, "game-requests")
}

func gameRequestErrorStatus(err error) int {
	switch {
	case errors.Is(err, gamerequest.ErrNotFound), errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound):
		return http.StatusNotFound
	case errors.Is(err, gamerequest.ErrNotCreator):
		return http.StatusForbidden
	case errors.Is(err, gamerequest.ErrFull), errors.Is(err, gamerequest.ErrAlreadyMember),
		errors.Is(err, gamerequest.ErrNotMember), errors.Is(err, gamerequest.ErrCreatorCannotLeave),
		errors.Is(err, gamerequest.ErrNotOpen), errors.Is(err, gamerequest.ErrNotPlaying),
		errors.Is(err, gamerequest.ErrChatClosed), errors.Is(err, gamerequest.ErrNotActive),
		errors.Is(err, gamerequest.ErrCapacityBelowMembers), errors.Is(err, gamerequest.ErrSessionInformationKeyExists),
		errors.Is(err, gamerequest.ErrSessionInformationLimit):
		return http.StatusConflict
	case errors.Is(err, gamerequest.ErrInvalidMessage), errors.Is(err, gamerequest.ErrInvalidSessionInformationKey),
		errors.Is(err, gamerequest.ErrInvalidSessionInformationValue):
		return http.StatusUnprocessableEntity
	case errors.Is(err, gamerequest.ErrChatRateLimited):
		return http.StatusTooManyRequests
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

// gameRequestMessageField names the composer control one rejection is
// about, so the message can be rendered with that control instead of as a
// banner the player has to match to a field. Anything not about a single
// field -- a Game that left the catalog, an infrastructure failure --
// reports no field and stays a page-level alert.
func gameRequestMessageField(err error) string {
	switch {
	case errors.Is(err, gamerequest.ErrInvalidMaxPlayers):
		return "max_players"
	case errors.Is(err, gamerequest.ErrInvalidStartsAt):
		return "start"
	case errors.Is(err, gamerequest.ErrInvalidDescription):
		return "description"
	default:
		return ""
	}
}

func gameRequestMessage(err error) string {
	switch {
	case errors.Is(err, gamerequest.ErrGameUnavailable):
		return "Choose an active game from the list."
	case errors.Is(err, gamerequest.ErrInvalidMaxPlayers):
		return "Maximum players must be 0 for unlimited or a whole number from 2 to 999."
	case errors.Is(err, gamerequest.ErrInvalidStartsAt):
		return "Enter a valid planned start date and time."
	case errors.Is(err, gamerequest.ErrInvalidDescription):
		return "Description must be 1000 characters or fewer."
	case errors.Is(err, gamerequest.ErrFull):
		return "This Game Request is already full."
	case errors.Is(err, gamerequest.ErrAlreadyMember):
		return "You have already joined this Game Request."
	case errors.Is(err, gamerequest.ErrNotMember):
		return "You are not currently a member of this Game Request."
	case errors.Is(err, gamerequest.ErrCreatorCannotLeave):
		return "The Host cannot leave their own Game Request."
	case errors.Is(err, gamerequest.ErrNotCreator):
		return "Only the Host can start, close, or cancel this Game Request."
	case errors.Is(err, gamerequest.ErrNotOpen):
		return "This Game Request is no longer open."
	case errors.Is(err, gamerequest.ErrNotPlaying):
		return "This Game Request is not currently playing."
	case errors.Is(err, gamerequest.ErrInvalidMessage):
		return "Messages must be 1 to 1000 characters."
	case errors.Is(err, gamerequest.ErrInvalidSessionInformationKey):
		return "Key must contain 1 to 40 characters and no line breaks."
	case errors.Is(err, gamerequest.ErrInvalidSessionInformationValue):
		return "Value must contain 1 to 300 characters and no line breaks."
	case errors.Is(err, gamerequest.ErrSessionInformationKeyExists):
		return "A session information entry already uses this key."
	case errors.Is(err, gamerequest.ErrSessionInformationLimit):
		return "This Game Request already has the maximum number of session information entries."
	case errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound):
		return "Session information entry not found."
	case errors.Is(err, gamerequest.ErrChatRateLimited):
		return "You are sending messages too fast. Wait a moment and try again."
	case errors.Is(err, gamerequest.ErrChatClosed):
		return "Chat is closed for this Game Request."
	case errors.Is(err, game.ErrInvalidProviderReference):
		return "That provider game reference is invalid."
	case errors.Is(err, game.ErrProviderRecordNotFound):
		return "That provider game could not be found."
	default:
		return "The Game Request could not be changed. Please try again."
	}
}
