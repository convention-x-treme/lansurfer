package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
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
	ListActive(context.Context, string) ([]requestquery.Request, error)
	ByID(context.Context, string, string) (requestquery.Request, error)
	CreationOptions(context.Context) (requestquery.CreationOptions, error)
	CurrentEvent(context.Context) (requestquery.EventContext, error)
}

// GameResolver turns an opaque IGDB provider reference into the one
// authoritative local Game for it, materializing that Game on first use
// (spec: "Follow and Game Request integration"). Only provider and external
// ID ever cross this boundary; the resolved Game's ID/name always come from
// the resolver, never from browser-supplied form fields.
type GameResolver interface {
	Resolve(context.Context, game.ProviderReference) (game.Game, error)
}

type GameRequests struct {
	commands GameRequestCommands
	queries  GameRequestQueries
	resolver GameResolver
	brand    branding.Brand
	now      func() time.Time
}

func NewGameRequests(commands GameRequestCommands, queries GameRequestQueries, resolver GameResolver, brand branding.Brand) *GameRequests {
	return &GameRequests{commands: commands, queries: queries, resolver: resolver, brand: brand, now: time.Now}
}

func (h *GameRequests) List(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	eventContext, err := h.queries.CurrentEvent(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "load current Event for Game Requests", err)
		h.renderError(w, r)
		return
	}
	requests, err := h.queries.ListActive(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "list active Game Requests", err)
		h.renderError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestList(h.brand, requestNavigation(r), eventContext, requests, notice(r)))
}

func (h *GameRequests) RefreshList(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	requests, err := h.queries.ListActive(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "refresh active Game Requests", err)
		http.Error(w, "refresh game requests", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestListContent(requests, r.URL.Query().Get("highlight")))
}

func (h *GameRequests) New(w http.ResponseWriter, r *http.Request) {
	options, err := h.queries.CreationOptions(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "load Game Request creation options", err)
		h.renderError(w, r)
		return
	}
	form := pages.GameRequestForm{
		GameID:     strings.TrimSpace(r.URL.Query().Get("game_id")),
		Provider:   strings.TrimSpace(r.URL.Query().Get("provider")),
		ExternalID: strings.TrimSpace(r.URL.Query().Get("external_id")),
	}
	render(w, r, http.StatusOK, pages.NewGameRequest(h.brand, requestNavigation(r), options, form, ""))
}

func (h *GameRequests) Create(w http.ResponseWriter, r *http.Request) {
	form, maxPlayers, startsAt, err := h.parseCreateForm(r)
	if err == nil {
		form.Description, err = gamerequest.ValidateCreateInput(maxPlayers, startsAt, form.Description)
	}
	if err == nil && form.GameID == "" && (form.Provider != "" || form.ExternalID != "") {
		var ref game.ProviderReference
		ref, err = game.NewProviderReference(form.Provider, form.ExternalID)
		if err == nil {
			var resolved game.Game
			resolved, err = h.resolver.Resolve(r.Context(), ref)
			if err == nil {
				form.GameID = resolved.ID
			}
		}
	}
	if err == nil {
		account, _ := webmiddleware.CurrentUser(r.Context())
		var created gamerequest.GameRequest
		created, err = h.commands.Create(r.Context(), account.ID, form.GameID, maxPlayers, startsAt, form.Description)
		if err == nil {
			h.redirect(w, r, "/game-requests/"+created.ID+"?notice=request-created")
			return
		}
	}
	options, optionsErr := h.queries.CreationOptions(r.Context())
	if optionsErr != nil {
		webmiddleware.LogError(r, "reload Game Request creation options", optionsErr)
		h.renderError(w, r)
		return
	}
	render(w, r, gameRequestErrorStatus(err), pages.NewGameRequest(h.brand, requestNavigation(r), options, form, gameRequestMessage(err)))
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
	render(w, r, http.StatusOK, pages.GameRequestDetailContent(request, account.ID))
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
		StartsAt: strings.TrimSpace(r.FormValue("starts_at")), TimezoneOffset: strings.TrimSpace(r.FormValue("timezone_offset")),
		Description: r.FormValue("description"),
		Provider:    strings.TrimSpace(r.FormValue("provider")), ExternalID: strings.TrimSpace(r.FormValue("external_id")),
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
	if form.StartsAt != "" {
		offsetMinutes := int64(0)
		if form.TimezoneOffset != "" {
			value, err := strconv.ParseInt(form.TimezoneOffset, 10, 16)
			if err != nil || value < -14*60 || value > 14*60 {
				return form, maxPlayers, time.Time{}, gamerequest.ErrInvalidStartsAt
			}
			offsetMinutes = value
		}
		location := time.FixedZone("browser-local", int(-offsetMinutes*60))
		parsed, err := time.ParseInLocation(gameRequestTimeLayout, form.StartsAt, location)
		if err != nil {
			return form, maxPlayers, time.Time{}, gamerequest.ErrInvalidStartsAt
		}
		startsAt = parsed.UTC()
	}
	return form, maxPlayers, startsAt, nil
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
	return components.NavigationState{Viewer: &components.Viewer{DisplayName: account.DisplayName, Role: string(account.Role)}}
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
	case errors.Is(err, gamerequest.ErrNoCurrentEvent), errors.Is(err, gamerequest.ErrGameUnavailable),
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
	case errors.Is(err, gamerequest.ErrNoCurrentEvent):
		return "No current Event is configured. Ask an administrator to select one."
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
