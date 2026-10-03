package handler

import (
	"context"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// GameRequestChat is the request-local chat the service authorizes per call.
type GameRequestChat interface {
	PostMessage(ctx context.Context, requestID, userID, body string) (gamerequest.Message, error)
	Messages(ctx context.Context, requestID, userID string) ([]gamerequest.Message, error)
}

type Chat struct {
	chat GameRequestChat
}

func NewChat(chat GameRequestChat) *Chat {
	return &Chat{chat: chat}
}

// Log renders the chat log. Authorization is the Messages call, never the page.
func (h *Chat) Log(w http.ResponseWriter, r *http.Request) {
	account, _ := webmiddleware.CurrentUser(r.Context())
	requestID := chi.URLParam(r, "requestID")
	messages, err := h.chat.Messages(r.Context(), requestID, account.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, pages.GameRequestChatLog(requestID, messages, account.ID))
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
	h.Log(w, r)
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
