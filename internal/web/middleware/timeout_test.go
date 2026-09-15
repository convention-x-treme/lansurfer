package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestRequestTimeoutCancelsSlowHandlers(t *testing.T) {
	var handlerContextErr error
	handler := middleware.RequestTimeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		handlerContextErr = r.Context().Err()
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/game-requests", nil))

	if handlerContextErr == nil {
		t.Fatal("handler context was never cancelled")
	}
}

func TestRequestTimeoutLeavesTheRealtimeStreamRunning(t *testing.T) {
	var deadlineSet bool
	handler := middleware.RequestTimeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, deadlineSet = r.Context().Deadline()
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/events/game-requests", nil))

	if deadlineSet {
		t.Error("realtime stream must not run under a request deadline")
	}
}
