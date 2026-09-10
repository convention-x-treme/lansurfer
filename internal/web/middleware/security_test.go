package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFProtectsStateChangingRequests(t *testing.T) {
	protected := CSRF(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CSRFToken(r.Context()) == "" {
			t.Error("CSRF token missing from request context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	page := httptest.NewRecorder()
	protected.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/games", nil))
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != csrfCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("CSRF cookies = %#v", cookies)
	}

	missing := httptest.NewRecorder()
	protected.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/games/game-1/follow", nil))
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing token status = %d, want %d", missing.Code, http.StatusForbidden)
	}

	form := strings.NewReader("csrf_token=" + cookies[0].Value)
	request := httptest.NewRequest(http.MethodPost, "/games/game-1/follow", form)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookies[0])
	accepted := httptest.NewRecorder()
	protected.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("valid token status = %d, want %d", accepted.Code, http.StatusNoContent)
	}
}

func TestCSRFLimitsFormBodies(t *testing.T) {
	protected := CSRF(false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized request reached handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader("value="+strings.Repeat("a", maxFormBytes)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	protected.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, name := range []string{"Content-Security-Policy", "Permissions-Policy", "Referrer-Policy", "Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options"} {
		if response.Header().Get(name) == "" {
			t.Errorf("%s header is missing", name)
		}
	}
}
