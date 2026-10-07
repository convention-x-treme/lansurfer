package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// GameRequestLobbyQueries loads participant-authorized Lobby history.
type GameRequestLobbyQueries interface {
	Lobby(context.Context, string, string) (requestquery.GameRequestLobby, error)
}

// Lobby serves the authorized Game Request history fragment.
type Lobby struct {
	access  LobbyAccess
	queries GameRequestLobbyQueries
}

// NewLobby wires current-member authorization to the Lobby read model.
func NewLobby(access LobbyAccess, queries GameRequestLobbyQueries) *Lobby {
	return &Lobby{access: access, queries: queries}
}

func (h *Lobby) View(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	requestID := chi.URLParam(r, "requestID")
	allowed, err := h.access.CanReadLobby(r.Context(), requestID, account.ID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "game request lobby", err)
		http.Error(w, "lobby unavailable", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, gameRequestMessage(gamerequest.ErrNotMember), http.StatusForbidden)
		return
	}
	lobby, err := h.queries.Lobby(r.Context(), requestID, account.ID)
	if errors.Is(err, gamerequest.ErrNotFound) {
		http.Error(w, "game request not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, gamerequest.ErrNotMember) {
		http.Error(w, gameRequestMessage(gamerequest.ErrNotMember), http.StatusForbidden)
		return
	}
	if err != nil {
		webmiddleware.LogError(r, "game request lobby", err)
		http.Error(w, "lobby unavailable", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestLobbyLog(lobby, account.ID))
}
