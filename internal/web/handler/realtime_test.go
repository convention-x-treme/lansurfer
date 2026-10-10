package handler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	platformSSE "github.com/convention-x-treme/lansurfer/internal/platform/sse"
	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestRealtimeStreamsGameRequestEvents(t *testing.T) {
	events := make(chan gamerequest.Event, 1)
	events <- gamerequest.Event{Type: gamerequest.EventStarted, RequestID: "request-1", GameID: "game-1"}
	close(events)
	source := &stubGameRequestEvents{events: events}
	request := httptest.NewRequest("GET", "/events/game-requests", nil)
	response := httptest.NewRecorder()

	NewRealtime(source, nil).GameRequests(response, request)

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

// Request detail updates keep using the existing event stream.
func TestRealtimeStreamsRequestUpdates(t *testing.T) {
	events := make(chan gamerequest.Event, 1)
	events <- gamerequest.Event{Type: gamerequest.EventUpdated, RequestID: "request-7"}
	close(events)
	source := &stubGameRequestEvents{events: events}
	response := httptest.NewRecorder()

	NewRealtime(source, nil).GameRequests(response, httptest.NewRequest("GET", "/events/game-requests", nil))

	for _, want := range []string{"event: game-request", `"type":"GameRequestUpdated"`, `"requestId":"request-7"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("stream does not contain %q: %q", want, response.Body.String())
		}
	}
}

func TestRealtimeUnsubscribesWhenClientDisconnects(t *testing.T) {
	events := make(chan gamerequest.Event)
	source := &stubGameRequestEvents{events: events}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("GET", "/events/game-requests", nil).WithContext(ctx)

	NewRealtime(source, nil).GameRequests(httptest.NewRecorder(), request)

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
			realtime := NewRealtime(source, nil)
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

func TestRealtimeHandlerDropsSeventeenthEventBehindBlockedWriter(t *testing.T) {
	broker := platformSSE.NewBroker()
	writer := &blockedSSEWriter{
		connected: make(chan struct{}),
		blocked:   make(chan struct{}),
		release:   make(chan struct{}),
		writes:    make(chan string, 17),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		NewRealtime(broker, nil).GameRequests(writer, httptest.NewRequest("GET", "/events/game-requests", nil).WithContext(ctx))
		close(done)
	}()
	<-writer.connected
	broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "in-flight"})
	<-writer.blocked
	for i := range 17 {
		broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: fmt.Sprintf("request-%02d", i)})
	}
	close(writer.release)
	received := make([]string, 0, 17)
	for range 17 {
		received = append(received, <-writer.writes)
	}
	if !strings.Contains(received[0], `"requestId":"in-flight"`) {
		t.Fatalf("first event = %q, want in-flight event", received[0])
	}
	for i, body := range received[1:] {
		want := fmt.Sprintf(`"requestId":"request-%02d"`, i)
		if !strings.Contains(body, want) {
			t.Fatalf("buffered event %d = %q, want %s", i, body, want)
		}
	}
	select {
	case extra := <-writer.writes:
		t.Fatalf("dropped seventeenth event was delivered: %q", extra)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	<-done
	if broker.Subscribers() != 0 {
		t.Fatalf("subscribers after cancellation = %d, want 0", broker.Subscribers())
	}
}

type blockedSSEWriter struct {
	header    http.Header
	connected chan struct{}
	blocked   chan struct{}
	release   chan struct{}
	writes    chan string
	mu        sync.Mutex
	first     bool
	flushed   bool
}

func (w *blockedSSEWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (*blockedSSEWriter) WriteHeader(int) {}
func (w *blockedSSEWriter) Write(data []byte) (int, error) {
	if strings.HasPrefix(string(data), "event:") {
		if !w.first {
			w.first = true
			close(w.blocked)
			<-w.release
		}
		w.mu.Lock()
		w.writes <- string(data)
		w.mu.Unlock()
	}
	return len(data), nil
}
func (w *blockedSSEWriter) Flush() {
	if !w.flushed {
		w.flushed = true
		close(w.connected)
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

type stubLobbyAccess struct{ allowed map[string]bool }

func (s stubLobbyAccess) CanReadLobby(_ context.Context, _, userID string) (bool, error) {
	return s.allowed[userID], nil
}

// Chat signals are delivered only to current participants; other events still
// reach everyone.
func TestRealtimeChatSignalsOnlyReachParticipants(t *testing.T) {
	stream := func(userID string) string {
		events := make(chan gamerequest.Event, 2)
		events <- gamerequest.Event{Type: gamerequest.EventChatMessage, RequestID: "request-1"}
		events <- gamerequest.Event{Type: gamerequest.EventStarted, RequestID: "request-1"}
		close(events)
		request := httptest.NewRequest("GET", "/events/game-requests?detailRequestId=request-1", nil)
		request = request.WithContext(webmiddleware.ContextWithUser(request.Context(), user.User{ID: userID}))
		response := httptest.NewRecorder()
		source := &stubGameRequestEvents{events: events}
		NewRealtime(
			source,
			stubLobbyAccess{allowed: map[string]bool{"member": true}},
		).GameRequests(response, request)
		return response.Body.String()
	}

	if body := stream("member"); !strings.Contains(body, "event: game-request-chat") {
		t.Errorf("member did not receive chat signal: %q", body)
	}
	body := stream("outsider")
	if strings.Contains(body, "game-request-chat") || !strings.Contains(body, "GameRequestStarted") {
		t.Errorf("outsider stream = %q, want lifecycle event only", body)
	}
}

type countingLobbyAccess struct{ calls int }

func (s *countingLobbyAccess) CanReadLobby(context.Context, string, string) (bool, error) {
	s.calls++
	return true, nil
}

func TestRealtimeSkipsChatAuthorizationForUnwatchedRequest(t *testing.T) {
	events := make(chan gamerequest.Event, 1)
	events <- gamerequest.Event{Type: gamerequest.EventChatMessage, RequestID: "request-2"}
	close(events)
	access := &countingLobbyAccess{}
	request := httptest.NewRequest("GET", "/events/game-requests?detailRequestId=request-1", nil)
	request = request.WithContext(webmiddleware.ContextWithUser(request.Context(), user.User{ID: "member"}))
	response := httptest.NewRecorder()

	NewRealtime(&stubGameRequestEvents{events: events}, access).GameRequests(response, request)

	if access.calls != 0 {
		t.Fatalf("CanReadLobby calls = %d, want 0 for unrelated request", access.calls)
	}
	if strings.Contains(response.Body.String(), "game-request-chat") {
		t.Fatal("unwatched chat signal was delivered")
	}
}
func TestRealtimeSessionInformationSignalsOnlyReachParticipants(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		events := make(chan gamerequest.Event, 1)
		events <- gamerequest.Event{Type: gamerequest.EventSessionInformationChanged, RequestID: "request-1"}
		close(events)
		access := sessionRealtimeAccess{allowed: allowed}
		request := httptest.NewRequest("GET", "/events/game-requests?detailRequestId=request-1", nil)
		request = request.WithContext(webmiddleware.ContextWithUser(request.Context(), user.User{ID: "viewer"}))
		response := httptest.NewRecorder()
		NewRealtime(&stubGameRequestEvents{events: events}, nil, access).GameRequests(response, request)
		got := strings.Contains(response.Body.String(), "event: game-request-session-information")
		if got != allowed {
			t.Fatalf("allowed=%v, delivered=%v", allowed, got)
		}
	}
}

type sessionRealtimeAccess struct {
	allowed bool
	calls   int
}

func (a sessionRealtimeAccess) CanSessionInformation(context.Context, string, string) (bool, error) {
	return a.allowed, nil
}
