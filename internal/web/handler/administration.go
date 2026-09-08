package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/event"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

const eventTimeLayout = "2006-01-02T15:04"

var errInvalidAdminForm = errors.New("invalid administration form")

type EventAdministration interface {
	List(context.Context) ([]event.Event, error)
	Create(context.Context, string, time.Time, time.Time) (event.Event, error)
	Update(context.Context, string, string, time.Time, time.Time) (event.Event, error)
	SetCurrent(context.Context, string) error
}

type GameAdministration interface {
	List(context.Context, string) ([]game.Game, error)
	Create(context.Context, string, string) (game.Game, error)
	Update(context.Context, string, string, string) (game.Game, error)
	SetActive(context.Context, string, bool) error
	AddAlias(context.Context, string, string) (game.Alias, error)
	RemoveAlias(context.Context, string, string) error
}

type Administration struct {
	events EventAdministration
	games  GameAdministration
	brand  branding.Brand
}

func NewAdministration(events EventAdministration, games GameAdministration, brand branding.Brand) *Administration {
	return &Administration{events: events, games: games, brand: brand}
}

func (h *Administration) Events(w http.ResponseWriter, r *http.Request) {
	items, err := h.events.List(r.Context())
	if err != nil {
		h.renderAdminError(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.AdminEvents(h.brand, adminNavigation(r), items, "", notice(r)))
}

func (h *Administration) CreateEvent(w http.ResponseWriter, r *http.Request) {
	name, startsAt, endsAt, err := eventForm(r)
	if err == nil {
		_, err = h.events.Create(r.Context(), name, startsAt, endsAt)
	}
	if err != nil {
		h.renderEventsWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/events?notice=event-created")
}

func (h *Administration) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	name, startsAt, endsAt, err := eventForm(r)
	if err == nil {
		_, err = h.events.Update(r.Context(), chi.URLParam(r, "eventID"), name, startsAt, endsAt)
	}
	if err != nil {
		h.renderEventsWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/events?notice=event-updated")
}

func (h *Administration) SetCurrentEvent(w http.ResponseWriter, r *http.Request) {
	if err := h.events.SetCurrent(r.Context(), chi.URLParam(r, "eventID")); err != nil {
		h.renderEventsWithError(w, r, err)
		return
	}
	h.redirect(w, r, "/admin/events?notice=current-event-updated")
}

func (h *Administration) Games(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	items, err := h.games.List(r.Context(), search)
	if err != nil {
		if errors.Is(err, game.ErrInvalidSearch) {
			render(w, r, http.StatusUnprocessableEntity, pages.AdminGames(h.brand, adminNavigation(r), nil, search, "Search must be 120 characters or fewer.", ""))
			return
		}
		h.renderAdminError(w, r)
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

func eventForm(r *http.Request) (string, time.Time, time.Time, error) {
	if err := r.ParseForm(); err != nil {
		return "", time.Time{}, time.Time{}, errInvalidAdminForm
	}
	startsAt, err := time.ParseInLocation(eventTimeLayout, r.FormValue("starts_at"), time.UTC)
	if err != nil {
		return "", time.Time{}, time.Time{}, event.ErrInvalidTime
	}
	endsAt, err := time.ParseInLocation(eventTimeLayout, r.FormValue("ends_at"), time.UTC)
	if err != nil {
		return "", time.Time{}, time.Time{}, event.ErrInvalidTime
	}
	return r.FormValue("name"), startsAt, endsAt, nil
}

func (h *Administration) renderEventsWithError(w http.ResponseWriter, r *http.Request, cause error) {
	status := adminErrorStatus(cause)
	if status == http.StatusInternalServerError {
		h.renderAdminError(w, r)
		return
	}
	items, err := h.events.List(r.Context())
	if err != nil {
		h.renderAdminError(w, r)
		return
	}
	render(w, r, status, pages.AdminEvents(h.brand, adminNavigation(r), items, adminMessage(cause), ""))
}

func (h *Administration) renderGamesWithError(w http.ResponseWriter, r *http.Request, cause error) {
	status := adminErrorStatus(cause)
	if status == http.StatusInternalServerError {
		h.renderAdminError(w, r)
		return
	}
	items, err := h.games.List(r.Context(), "")
	if err != nil {
		h.renderAdminError(w, r)
		return
	}
	render(w, r, status, pages.AdminGames(h.brand, adminNavigation(r), items, "", adminMessage(cause), ""))
}

func (h *Administration) renderAdminError(w http.ResponseWriter, r *http.Request) {
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
		Viewer: &components.Viewer{DisplayName: account.DisplayName, Role: string(account.Role)},
	}
}

func adminMessage(err error) string {
	switch {
	case errors.Is(err, errInvalidAdminForm):
		return "The form could not be read."
	case errors.Is(err, event.ErrInvalidName):
		return "Event name is required and must be 120 characters or fewer."
	case errors.Is(err, event.ErrInvalidTime):
		return "Enter valid UTC dates and make sure the event ends after it starts."
	case errors.Is(err, event.ErrNotFound):
		return "That event no longer exists."
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
	case errors.Is(err, event.ErrNotFound), errors.Is(err, game.ErrNotFound), errors.Is(err, game.ErrAliasNotFound):
		return http.StatusNotFound
	case errors.Is(err, event.ErrInvalidName), errors.Is(err, event.ErrInvalidTime),
		errors.Is(err, game.ErrInvalidName), errors.Is(err, game.ErrInvalidSlug),
		errors.Is(err, game.ErrInvalidAlias), errors.Is(err, game.ErrAlreadyExists):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func notice(r *http.Request) string {
	switch strings.TrimSpace(r.URL.Query().Get("notice")) {
	case "event-created":
		return "Event created."
	case "event-updated":
		return "Event updated."
	case "current-event-updated":
		return "Current event updated."
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
