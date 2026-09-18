package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestLoginRendersProviderChoice(t *testing.T) {
	handler := NewAuthentication(&stubAuthentication{available: true}, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Login(response, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	body := response.Body.String()
	for _, want := range []string{`href="/auth/oidc"`, "Continue with your CXT account", "CXT IdP", "Continue with Discord", "disabled"} {
		if response.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("response = %d %q", response.Code, body)
		}
	}
}

func TestBeginLoginSetsProtectedFlowCookiesAndRedirects(t *testing.T) {
	authentication := &stubAuthentication{available: true, flow: auth.LoginFlow{
		AuthorizationURL: "https://identity.example/authorize",
		State:            "state",
		Nonce:            "nonce",
		PKCEVerifier:     "verifier",
	}}
	handler := NewAuthentication(authentication, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.BeginLogin(response, httptest.NewRequest(http.MethodGet, "/auth/oidc", nil))

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusFound || result.Header.Get("Location") != authentication.flow.AuthorizationURL {
		t.Fatalf("status/location = %d, %q", result.StatusCode, result.Header.Get("Location"))
	}
	for _, name := range []string{stateCookieName, nonceCookieName, pkceCookieName} {
		cookie := responseCookie(t, result, name)
		if cookie.Path != callbackPath || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie %s = %#v", name, cookie)
		}
	}
}

func TestCallbackValidatesStateAndCreatesSession(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	authentication := &stubAuthentication{
		available: true,
		session:   auth.BrowserSession{Token: "opaque-session", ExpiresAt: expiresAt},
	}
	handler := NewAuthentication(authentication, branding.Default(), true)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state"})
	request.AddCookie(&http.Cookie{Name: nonceCookieName, Value: "nonce"})
	request.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
	response := httptest.NewRecorder()

	handler.Callback(response, request)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusSeeOther || result.Header.Get("Location") != "/game-requests" {
		t.Fatalf("status/location = %d, %q", result.StatusCode, result.Header.Get("Location"))
	}
	if authentication.completedCode != "code" || authentication.completedVerifier != "verifier" || authentication.completedNonce != "nonce" {
		t.Fatalf("complete arguments = %q, %q, %q", authentication.completedCode, authentication.completedVerifier, authentication.completedNonce)
	}
	sessionCookie := responseCookie(t, result, webmiddleware.SessionCookieName)
	if sessionCookie.Value != "opaque-session" || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}
}

func TestCallbackRejectsMismatchedStateBeforeExchange(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, branding.Default(), false)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=attacker", nil)
	request.AddCookie(&http.Cookie{Name: stateCookieName, Value: "expected"})
	request.AddCookie(&http.Cookie{Name: nonceCookieName, Value: "nonce"})
	request.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
	response := httptest.NewRecorder()

	handler.Callback(response, request)

	if response.Code != http.StatusBadRequest || authentication.completeCalls != 0 {
		t.Fatalf("status/complete calls = %d, %d", response.Code, authentication.completeCalls)
	}
	if !strings.Contains(response.Body.String(), "could not be verified") {
		t.Fatalf("response body = %q", response.Body.String())
	}
}

func TestCallbackMapsProviderAndPersistenceFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid provider callback", err: auth.ErrInvalidCallback, wantStatus: http.StatusUnauthorized},
		{name: "persistence failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication := &stubAuthentication{available: true, completeErr: test.err}
			handler := NewAuthentication(authentication, branding.Default(), false)
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil)
			request.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state"})
			request.AddCookie(&http.Cookie{Name: nonceCookieName, Value: "nonce"})
			request.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
			response := httptest.NewRecorder()

			handler.Callback(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestLogoutRevokesAndExpiresSession(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, branding.Default(), false)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	handler.Logout(response, request)

	result := response.Result()
	defer result.Body.Close()
	if authentication.loggedOutToken != "opaque-session" || result.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout token/status = %q, %d", authentication.loggedOutToken, result.StatusCode)
	}
	if cookie := responseCookie(t, result, webmiddleware.SessionCookieName); cookie.MaxAge != -1 {
		t.Fatalf("expired cookie = %#v", cookie)
	}
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response has no %s cookie", name)
	return nil
}

type stubAuthentication struct {
	available         bool
	flow              auth.LoginFlow
	session           auth.BrowserSession
	completedCode     string
	completedVerifier string
	completedNonce    string
	completeCalls     int
	completeErr       error
	loggedOutToken    string
}

func (s *stubAuthentication) Available() bool { return s.available }

func (s *stubAuthentication) BeginLogin() (auth.LoginFlow, error) { return s.flow, nil }

func (s *stubAuthentication) CompleteLogin(_ context.Context, code, verifier, nonce string) (user.User, auth.BrowserSession, error) {
	s.completeCalls++
	s.completedCode, s.completedVerifier, s.completedNonce = code, verifier, nonce
	return user.User{}, s.session, s.completeErr
}

func (s *stubAuthentication) Logout(_ context.Context, token string) error {
	s.loggedOutToken = token
	return nil
}
