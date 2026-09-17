package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestRequestLoggingRecordsStructuredCompletion(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestLogging(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	request := httptest.NewRequest(http.MethodPost, "/game-requests", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	logLine := output.String()
	for _, want := range []string{`"msg":"HTTP request completed"`, `"method":"POST"`, `"path":"/game-requests"`, `"status":201`} {
		if !strings.Contains(logLine, want) {
			t.Errorf("log does not contain %q: %s", want, logLine)
		}
	}
}

func TestClientIPUsesForwardingOnlyFromTrustedPeers(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	untrustedRequest := httptest.NewRequest("GET", "/", nil)
	untrustedRequest.RemoteAddr = "192.0.2.10:1234"
	untrustedRequest.Header.Set("X-Forwarded-For", "198.51.100.2")
	if got := clientIP(untrustedRequest, trusted); got != "192.0.2.10" {
		t.Errorf("untrusted peer client IP = %q", got)
	}

	trustedRequest := httptest.NewRequest("GET", "/", nil)
	trustedRequest.RemoteAddr = "10.0.0.5:1234"
	trustedRequest.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.6")
	if got := clientIP(trustedRequest, trusted); got != "198.51.100.2" {
		t.Errorf("trusted proxy client IP = %q", got)
	}
}

// A request the server abandoned must not be logged as one that completed.
// Issue #15 could not tell the two apart, and the status code cannot: the
// handler still returns 200 for a response the client never received.
func TestRequestLoggingDistinguishesTimeoutFromCompletion(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		wrap    func(http.Handler) http.Handler
		outcome string
		message string
	}{
		{
			name:    "timed out",
			wrap:    RequestTimeout(20 * time.Millisecond),
			outcome: "timed-out",
			message: "HTTP request timed out",
		},
		{
			name: "client aborted",
			wrap: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					go cancel()
					next.ServeHTTP(w, r.WithContext(ctx))
				})
			},
			outcome: "client-aborted",
			message: "HTTP request aborted",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			handler := testCase.wrap(RequestLogging(logger, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			})))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/game-requests", nil))

			logLine := output.String()
			for _, want := range []string{`"msg":"` + testCase.message + `"`, `"outcome":"` + testCase.outcome + `"`} {
				if !strings.Contains(logLine, want) {
					t.Errorf("log does not contain %q: %s", want, logLine)
				}
			}
			if strings.Contains(logLine, "HTTP request completed") {
				t.Errorf("an unfinished request was logged as completed: %s", logLine)
			}
		})
	}
}
