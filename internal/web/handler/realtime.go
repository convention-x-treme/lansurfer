package handler

import (
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

type Realtime struct {
	events GameRequestEvents
}

func NewRealtime(events GameRequestEvents) *Realtime {
	return &Realtime{events: events}
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
				reason = "server-shutdown"
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write(append([]byte("event: game-request\ndata: "), append(payload, '\n', '\n')...)); err != nil {
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
