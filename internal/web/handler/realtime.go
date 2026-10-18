package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

const realtimeKeepAlive = 20 * time.Second

type GameRequestEvents interface {
	Subscribe() (<-chan gamerequest.Event, func())
	Subscribers() int
}

// LobbyAccess answers whether a user may currently read a request's Lobby.
type LobbyAccess interface {
	CanReadLobby(ctx context.Context, requestID, userID string) (bool, error)
}

type SessionInformationAccess interface {
	CanSessionInformation(ctx context.Context, requestID, userID string) (bool, error)
}

type Realtime struct {
	events             GameRequestEvents
	lobby              LobbyAccess
	sessionInformation SessionInformationAccess
}

func NewRealtime(events GameRequestEvents, lobby LobbyAccess, session ...SessionInformationAccess) *Realtime {
	h := &Realtime{events: events, lobby: lobby}
	if len(session) > 0 {
		h.sessionInformation = session[0]
	}
	return h
}

func (h *Realtime) GameRequests(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	events, unsubscribe := h.events.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	if _, err := w.Write([]byte(": connected\n\n")); err != nil {
		return
	}
	flusher.Flush()

	// A stream lives as long as its page, so open and close are rare enough to
	// log at Info even on a busy instance. The count is the diagnostic that
	// matters: it is what the browser's per-origin connection pool sees.
	logger := webmiddleware.RequestLogger(r)
	logger.Info("realtime stream opened", "streams", h.events.Subscribers())
	reason := "client-closed"
	defer func() {
		// Releasing here rather than relying on the deferred unsubscribe above
		// means the logged count excludes this stream. Unsubscribe is
		// idempotent, so the earlier defer still covers the paths that return
		// before this one is registered.
		unsubscribe()
		logger.Info("realtime stream closed", "reason", reason, "streams", h.events.Subscribers())
	}()

	keepAlive := time.NewTicker(realtimeKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				reason = "subscription-closed"
				return
			}
			name := "game-request"
			if event.Type == gamerequest.EventChatMessage || event.Type == gamerequest.EventSessionInformationChanged {
				if event.RequestID != r.URL.Query().Get("detailRequestId") {
					continue
				}
				viewer, _ := webmiddleware.CurrentUser(r.Context())
				if viewer.ID == "" {
					continue
				}
				if event.Type == gamerequest.EventChatMessage {
					if h.lobby == nil {
						continue
					}
					allowed, err := h.lobby.CanReadLobby(r.Context(), event.RequestID, viewer.ID)
					if err != nil {
						webmiddleware.LogError(r, "authorize game request Lobby signal", err)
						continue
					}
					if !allowed {
						continue
					}
					name = "game-request-chat"
				} else {
					if h.sessionInformation == nil {
						continue
					}
					if allowed, err := h.sessionInformation.CanSessionInformation(r.Context(), event.RequestID, viewer.ID); err != nil || !allowed {
						continue
					}
					name = "game-request-session-information"
				}
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write(append([]byte("event: "+name+"\ndata: "), append(payload, '\n', '\n')...)); err != nil {
				reason = "write-failed"
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				reason = "write-failed"
				return
			}
			flusher.Flush()
		}
	}
}
