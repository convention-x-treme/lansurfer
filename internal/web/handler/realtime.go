package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

const realtimeKeepAlive = 20 * time.Second

type GameRequestEvents interface {
	Subscribe() (<-chan gamerequest.Event, func())
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

	keepAlive := time.NewTicker(realtimeKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write(append([]byte("event: game-request\ndata: "), append(payload, '\n', '\n')...)); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
