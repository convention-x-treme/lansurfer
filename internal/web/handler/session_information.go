package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

type GameRequestSessionInformation interface {
	SessionInformation(context.Context, string, string) (gamerequest.SessionInformation, error)
	CreateSessionInformationEntry(context.Context, string, string, gamerequest.SessionInformationInput) error
	UpdateSessionInformationEntry(context.Context, string, string, string, gamerequest.SessionInformationInput) error
	DeleteSessionInformationEntry(context.Context, string, string, string) error
	CanSessionInformation(context.Context, string, string) (bool, error)
}

func (h *GameRequests) sessionView(r *http.Request, mode string) (pages.SessionInformationView, error) {
	view := pages.SessionInformationView{RequestID: chi.URLParam(r, "requestID")}
	if h.sessionInformation == nil {
		return view, nil
	}
	account, _ := webmiddleware.CurrentUser(r.Context())
	info, err := h.sessionInformation.SessionInformation(r.Context(), view.RequestID, account.ID)
	if errors.Is(err, gamerequest.ErrNotMember) {
		view.Visible, view.JoinRequired = true, true
		return view, nil
	}
	if errors.Is(err, gamerequest.ErrNotActive) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Visible, view.CanEdit, view.Entries = true, info.CanEdit, info.Entries
	if info.CanEdit && mode == "add" {
		view.Form = &pages.SessionInformationForm{Mode: "add"}
	}
	if info.CanEdit && mode != "" && mode != "add" {
		for _, entry := range info.Entries {
			if entry.ID == mode {
				view.Form = &pages.SessionInformationForm{Mode: "edit", EntryID: entry.ID, Key: entry.Key, Value: entry.Value}
				return view, nil
			}
		}
	}
	return view, nil
}

func (h *GameRequests) SessionInformation(w http.ResponseWriter, r *http.Request) {
	mode := sessionModeFromQuery(r)
	view, err := h.sessionView(r, mode)
	if err != nil {
		h.sessionFailure(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		target := "/game-requests/" + view.RequestID
		if mode == "add" {
			target += "?session_add=1"
		} else if mode != "" {
			target += "?session_edit=" + mode
		}
		w.Header().Set("HX-Replace-Url", target)
	}
	render(w, r, http.StatusOK, pages.GameRequestSessionInformation(view))
}
func (h *GameRequests) CreateSessionInformation(w http.ResponseWriter, r *http.Request) {
	h.mutateSessionInformation(w, r, "")
}
func (h *GameRequests) UpdateSessionInformation(w http.ResponseWriter, r *http.Request) {
	h.mutateSessionInformation(w, r, chi.URLParam(r, "entryID"))
}
func (h *GameRequests) DeleteSessionInformation(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	err := h.sessionInformation.DeleteSessionInformationEntry(r.Context(), chi.URLParam(r, "requestID"), chi.URLParam(r, "entryID"), account.ID)
	if err != nil {
		h.sessionFailure(w, r, err)
		return
	}
	h.sessionSuccess(w, r)
}
func (h *GameRequests) mutateSessionInformation(w http.ResponseWriter, r *http.Request, entryID string) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	input := gamerequest.SessionInformationInput{Key: r.FormValue("key"), Value: r.FormValue("value")}
	var err error
	if entryID == "" {
		err = h.sessionInformation.CreateSessionInformationEntry(r.Context(), chi.URLParam(r, "requestID"), account.ID, input)
	} else {
		err = h.sessionInformation.UpdateSessionInformationEntry(r.Context(), chi.URLParam(r, "requestID"), entryID, account.ID, input)
	}
	if err != nil {
		h.sessionMutationFailure(w, r, entryID, input, err)
		return
	}
	h.sessionSuccess(w, r)
}
func (h *GameRequests) sessionSuccess(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/game-requests/"+chi.URLParam(r, "requestID"), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Replace-Url", "/game-requests/"+chi.URLParam(r, "requestID"))
	h.SessionInformation(w, r)
}
func (h *GameRequests) sessionFailure(w http.ResponseWriter, r *http.Request, err error) {
	status := gameRequestErrorStatus(err)
	if status == http.StatusInternalServerError {
		webmiddleware.LogError(r, "session information", err)
		http.Error(w, "session information unavailable", status)
		return
	}
	http.Error(w, gameRequestMessage(err), status)
}

func (h *GameRequests) sessionMutationFailure(w http.ResponseWriter, r *http.Request, entryID string, input gamerequest.SessionInformationInput, err error) {
	status := gameRequestErrorStatus(err)
	if status == http.StatusInternalServerError {
		h.sessionFailure(w, r, err)
		return
	}
	mode := "add"
	if entryID != "" {
		mode = entryID
	}
	view, viewErr := h.sessionView(r, mode)
	if viewErr != nil {
		h.sessionFailure(w, r, viewErr)
		return
	}
	if view.Form == nil {
		h.sessionFailure(w, r, err)
		return
	}
	view.Form.Key, view.Form.Value = input.Key, input.Value
	view.Form.Error = gameRequestMessage(err)
	switch {
	case errors.Is(err, gamerequest.ErrInvalidSessionInformationKey):
		view.Form.ErrorField = "key"
	case errors.Is(err, gamerequest.ErrInvalidSessionInformationValue):
		view.Form.ErrorField = "value"
	}
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, http.StatusOK, pages.GameRequestSessionInformation(view))
		return
	}
	account, _ := webmiddleware.CurrentUser(r.Context())
	request, queryErr := h.queries.ByID(r.Context(), view.RequestID, account.ID)
	if queryErr != nil {
		h.sessionFailure(w, r, queryErr)
		return
	}
	render(w, r, status, pages.GameRequestDetail(h.brand, h.navigationFor(r), request, account.ID, pages.GameRequestDetailMessage{}, components.Notice{}, view))
}

func sessionModeFromQuery(r *http.Request) string {
	if r.URL.Query().Get("session_add") == "1" {
		return "add"
	}
	return r.URL.Query().Get("session_edit")
}
