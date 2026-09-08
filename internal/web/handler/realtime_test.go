package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestRealtimeStreamsGameRequestEvents(t *testing.T) {
	events := make(chan gamerequest.Event, 1)
	events <- gamerequest.Event{Type: gamerequest.EventCreated, RequestID: "request-1", GameID: "game-1"}
	close(events)
	source := &stubGameRequestEvents{events: events}
	request := httptest.NewRequest("GET", "/events/game-requests", nil)
	response := httptest.NewRecorder()

	NewRealtime(source).GameRequests(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	for _, want := range []string{": connected", "event: game-request", `"type":"GameRequestCreated"`, `"requestId":"request-1"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("stream does not contain %q: %q", want, response.Body.String())
		}
	}
	if !source.unsubscribed {
		t.Error("stream did not unsubscribe")
	}
}

func TestRealtimeUnsubscribesWhenClientDisconnects(t *testing.T) {
	events := make(chan gamerequest.Event)
	source := &stubGameRequestEvents{events: events}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("GET", "/events/game-requests", nil).WithContext(ctx)

	NewRealtime(source).GameRequests(httptest.NewRecorder(), request)

	if !source.unsubscribed {
		t.Error("disconnected stream did not unsubscribe")
	}
}

type stubGameRequestEvents struct {
	events       <-chan gamerequest.Event
	unsubscribed bool
}

func (s *stubGameRequestEvents) Subscribe() (<-chan gamerequest.Event, func()) {
	return s.events, func() { s.unsubscribed = true }
}
