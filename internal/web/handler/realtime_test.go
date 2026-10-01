package handler

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestRealtimeStreamsGameRequestEvents(t *testing.T) {
	events := make(chan gamerequest.Event, 1)
	events <- gamerequest.Event{Type: gamerequest.EventStarted, RequestID: "request-1", GameID: "game-1"}
	close(events)
	source := &stubGameRequestEvents{events: events}
	request := httptest.NewRequest("GET", "/events/game-requests", nil)
	response := httptest.NewRecorder()

	NewRealtime(source).GameRequests(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	for _, want := range []string{": connected", "event: game-request", `"type":"GameRequestStarted"`, `"requestId":"request-1"`} {
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

// The number of open streams is what issue #15's investigation had to
// reconstruct by hand, and the close reason is what distinguishes a browser
// that navigated away from a server that shut the stream down.
func TestRealtimeLogsStreamLifecycle(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		events chan gamerequest.Event
		ctx    func() context.Context
		reason string
	}{
		{
			name:   "client closed",
			events: make(chan gamerequest.Event),
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			reason: "client-closed",
		},
		{
			name:   "server shutdown",
			events: func() chan gamerequest.Event { closed := make(chan gamerequest.Event); close(closed); return closed }(),
			ctx:    context.Background,
			reason: "server-shutdown",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			source := &stubGameRequestEvents{events: testCase.events}
			realtime := NewRealtime(source)
			handler := webmiddleware.RequestLogging(logger, nil)(http.HandlerFunc(realtime.GameRequests))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/events/game-requests", nil).WithContext(testCase.ctx()))

			logLines := output.String()
			opened, closed, _ := strings.Cut(logLines, `"msg":"realtime stream closed"`)
			if !strings.Contains(opened, `"msg":"realtime stream opened"`) || !strings.Contains(opened, `"streams":1`) {
				t.Errorf("stream open was not logged with its count: %s", logLines)
			}
			if !strings.Contains(closed, `"reason":"`+testCase.reason+`"`) || !strings.Contains(closed, `"streams":0`) {
				t.Errorf("stream close was not logged with reason %q and its count: %s", testCase.reason, logLines)
			}
		})
	}
}

type stubGameRequestEvents struct {
	events       <-chan gamerequest.Event
	unsubscribed bool
}

func (s *stubGameRequestEvents) Subscribe() (<-chan gamerequest.Event, func()) {
	return s.events, func() { s.unsubscribed = true }
}

func (s *stubGameRequestEvents) Subscribers() int {
	if s.unsubscribed {
		return 0
	}
	return 1
}
