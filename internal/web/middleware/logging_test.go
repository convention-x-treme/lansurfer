package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
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
