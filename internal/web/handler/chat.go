package handler

import (
	"context"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// GameRequestChat owns the request-local chat write path.
type GameRequestChat interface {
	PostMessage(ctx context.Context, requestID, userID, body string) (gamerequest.Message, error)
}

type Chat struct {
	chat  GameRequestChat
	lobby *Lobby
}

func NewChat(chat GameRequestChat, lobby *Lobby) *Chat {
	return &Chat{chat: chat, lobby: lobby}
}

func (h *Chat) Post(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	requestID := chi.URLParam(r, "requestID")
	_, err := h.chat.PostMessage(r.Context(), requestID, account.ID, r.FormValue("body"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/game-requests/"+requestID, http.StatusSeeOther)
		return
	}
	h.lobby.View(w, r)
}

func (h *Chat) fail(w http.ResponseWriter, r *http.Request, cause error) {
	status := gameRequestErrorStatus(cause)
	if status == http.StatusInternalServerError {
		webmiddleware.LogError(r, "game request chat", cause)
		http.Error(w, "chat unavailable", status)
		return
	}
	if r.Header.Get("HX-Request") == "true" && r.Method == http.MethodPost {
		w.Header().Set("HX-Retarget", "#live-notices")
		w.Header().Set("HX-Reswap", "beforeend")
		render(w, r, http.StatusOK, pages.GameRequestActionError(gameRequestMessage(cause)))
		return
	}
	http.Error(w, gameRequestMessage(cause), status)
}
