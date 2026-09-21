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
	handler := NewAuthentication(&stubAuthentication{available: true, providers: []auth.ProviderAvailability{
		{Kind: "oidc", DisplayName: "CXT account"}, {Kind: "discord", DisplayName: "Discord"},
	}}, &stubSessions{}, branding.Default(), true, nil, nil)
	response := httptest.NewRecorder()

	handler.Login(response, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	body := response.Body.String()
	for _, want := range []string{`href="/auth/oidc"`, "Continue with CXT account", `href="/auth/discord"`, "Continue with Discord"} {
		if response.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("response = %d %q", response.Code, body)
		}
	}
}

func TestBeginForIssuesBrowserBindingAndRedirects(t *testing.T) {
	authentication := &stubAuthentication{available: true, start: auth.OAuthStart{AuthorizationURL: "https://identity.example/authorize"}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), true, nil, nil)
	response := httptest.NewRecorder()

	handler.BeginFor("oidc")(response, httptest.NewRequest(http.MethodGet, "/auth/oidc", nil))

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusFound || result.Header.Get("Location") != authentication.start.AuthorizationURL {
		t.Fatalf("status/location = %d, %q", result.StatusCode, result.Header.Get("Location"))
	}
	if authentication.began.ProviderKind != "oidc" || authentication.began.Operation != auth.FlowLogin || authentication.began.BrowserBinding == "" {
		t.Fatalf("begin input = %#v", authentication.began)
	}
	cookie := responseCookie(t, result, browserBindingCookieName)
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Value != authentication.began.BrowserBinding {
		t.Errorf("browser-binding cookie = %#v", cookie)
	}
}

func TestBeginForReusesExistingBrowserBinding(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/discord", nil)
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "existing-binding"})
	response := httptest.NewRecorder()

	handler.BeginFor("discord")(response, request)

	if authentication.began.ProviderKind != "discord" || authentication.began.BrowserBinding != "existing-binding" {
		t.Fatalf("begin input = %#v", authentication.began)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == browserBindingCookieName {
			t.Fatalf("handler re-issued the browser-binding cookie: %#v", cookie)
		}
	}
}

func TestBeginForStartsResolutionWhenResolveParameterPresent(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc?resolve=pending-1", nil)
	response := httptest.NewRecorder()

	handler.BeginFor("oidc")(response, request)

	if authentication.began.Operation != auth.FlowResolution || authentication.began.ParentFlowID != "pending-1" {
		t.Fatalf("begin input = %#v", authentication.began)
	}
}

// Already-signed-in redirects for BeginFor/LinkFor/CallbackFor depend on
// webmiddleware.CurrentUser, which only ever gets populated by the
// webmiddleware.Authentication middleware from a real session cookie. That
// middleware sits in front of these handlers only at the router level, so
// those scenarios are covered by the router-level tests in
// internal/web/router_test.go rather than as handler unit tests here.

func TestLinkForRejectsAnonymousRequest(t *testing.T) {
	handler := NewAuthentication(&stubAuthentication{available: true}, &stubSessions{}, branding.Default(), false, nil, nil)
	response := httptest.NewRecorder()

	handler.LinkFor("discord")(response, httptest.NewRequest(http.MethodPost, "/account/link/discord", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

// LinkFor must not answer its triggering POST with a 3xx redirect straight to
// the external authorization URL: browsers enforce the form-action CSP
// directive against every redirect hop chained from a form submission, so
// that would be silently blocked. It instead completes the POST with a
// same-origin page that navigates onward itself.
func TestLinkForRendersRedirectPageInsteadOfHTTPRedirect(t *testing.T) {
	authentication := &stubAuthentication{available: true, start: auth.OAuthStart{AuthorizationURL: "https://discord.example/authorize"}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/link/discord", nil), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.LinkFor("discord")(response, request)

	if response.Code != http.StatusOK || response.Result().Header.Get("Location") != "" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Result().Header.Get("Location"))
	}
	if !strings.Contains(response.Body.String(), authentication.start.AuthorizationURL) {
		t.Fatalf("body does not carry the authorization URL: %q", response.Body.String())
	}
}

func TestCallbackForCompletesLoginAndCreatesSession(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{
		Operation: auth.FlowLogin, Resolution: auth.Resolution{Session: auth.BrowserSession{Token: "opaque-session", ExpiresAt: expiresAt}},
	}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), true, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusSeeOther || result.Header.Get("Location") != "/game-requests" {
		t.Fatalf("status/location = %d, %q", result.StatusCode, result.Header.Get("Location"))
	}
	if authentication.completed.ProviderKind != "oidc" || authentication.completed.Code != "code" ||
		authentication.completed.State != "state" || authentication.completed.BrowserBinding != "binding" {
		t.Fatalf("complete arguments = %#v", authentication.completed)
	}
	sessionCookie := responseCookie(t, result, webmiddleware.SessionCookieName)
	if sessionCookie.Value != "opaque-session" || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}
}

func TestCallbackForRedirectsUnknownIdentityToOnboarding(t *testing.T) {
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{
		Operation: auth.FlowLogin, Resolution: auth.Resolution{Pending: auth.PendingIdentity{ID: "pending-1"}},
	}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("discord")(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding/pending-1" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestCallbackForRedirectsSuccessfulLinkToAccount(t *testing.T) {
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{Operation: auth.FlowLink}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("discord")(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestCallbackForRejectsMissingBrowserBindingBeforeExchange(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil)
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	if response.Code != http.StatusBadRequest || authentication.completeCalls != 0 {
		t.Fatalf("status/complete calls = %d, %d", response.Code, authentication.completeCalls)
	}
	if !strings.Contains(response.Body.String(), "could not be verified") {
		t.Fatalf("response body = %q", response.Body.String())
	}
}

func TestCallbackForMapsFlowAndPersistenceFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid provider callback", err: auth.ErrInvalidCallback, wantStatus: http.StatusUnauthorized},
		{name: "expired flow", err: auth.ErrFlowExpired, wantStatus: http.StatusBadRequest},
		{name: "wrong browser", err: auth.ErrFlowBrowser, wantStatus: http.StatusBadRequest},
		{name: "unknown resolution identity", err: auth.ErrResolutionIdentityUnknown, wantStatus: http.StatusUnauthorized},
		{name: "anonymous identity conflict gets an actionable message", err: auth.ErrIdentityConflict, wantStatus: http.StatusConflict},
		{name: "anonymous provider already linked gets an actionable message", err: auth.ErrProviderAlreadyLinked, wantStatus: http.StatusConflict},
		{name: "persistence failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication := &stubAuthentication{available: true, completeErr: test.err}
			handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil)
			request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
			response := httptest.NewRecorder()

			handler.CallbackFor("oidc")(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

// Signed-in identity-conflict routing to /account is covered at the router
// level (see the note above TestLinkForRejectsAnonymousRequest).

func TestCallbackForRejectsProviderDeclinedBeforeCompletion(t *testing.T) {
	authentication := &stubAuthentication{available: true}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?error=access_denied", nil)
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	if response.Code != http.StatusUnauthorized || authentication.completeCalls != 0 {
		t.Fatalf("status/complete calls = %d, %d", response.Code, authentication.completeCalls)
	}
}

func TestLogoutRevokesAndExpiresSession(t *testing.T) {
	sessions := &stubSessions{}
	handler := NewAuthentication(&stubAuthentication{available: true}, sessions, branding.Default(), false, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	handler.Logout(response, request)

	result := response.Result()
	defer result.Body.Close()
	if sessions.loggedOutToken != "opaque-session" || result.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout token/status = %q, %d", sessions.loggedOutToken, result.StatusCode)
	}
	if cookie := responseCookie(t, result, webmiddleware.SessionCookieName); cookie.MaxAge != -1 {
		t.Fatalf("expired cookie = %#v", cookie)
	}
}

func TestLogoutRemovesPushDeviceForCurrentUser(t *testing.T) {
	remover := &stubPushDeviceRemover{}
	handler := NewAuthentication(&stubAuthentication{available: true}, &stubSessions{}, branding.Default(), false, remover, []byte("0123456789abcdef0123456789abcdef"))
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/auth/logout", nil), user.User{ID: "user-1"})
	request.AddCookie(&http.Cookie{Name: webmiddleware.DeviceCookieName, Value: "device-1"})
	response := httptest.NewRecorder()

	handler.Logout(response, request)

	if remover.userID != "user-1" || remover.deviceIDHash == "" {
		t.Fatalf("remove call = %#v", remover)
	}
	result := response.Result()
	defer result.Body.Close()
	if cookie := responseCookie(t, result, webmiddleware.DeviceCookieName); cookie.MaxAge != -1 {
		t.Fatalf("expired device cookie = %#v", cookie)
	}
}

func TestLogoutToleratesMissingDeviceCookieAndNilSubscriptions(t *testing.T) {
	handler := NewAuthentication(&stubAuthentication{available: true}, &stubSessions{}, branding.Default(), false, nil, nil)
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/auth/logout", nil), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.Logout(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", response.Code)
	}
}

type stubPushDeviceRemover struct {
	userID, deviceIDHash string
}

func (s *stubPushDeviceRemover) RemoveDeviceByHash(_ context.Context, userID, deviceIDHash string) error {
	s.userID, s.deviceIDHash = userID, deviceIDHash
	return nil
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
	available     bool
	providers     []auth.ProviderAvailability
	start         auth.OAuthStart
	began         auth.OAuthStartInput
	result        auth.OAuthResult
	completed     auth.OAuthCompletion
	completeCalls int
	completeErr   error
}

func (s *stubAuthentication) Available() bool                        { return s.available }
func (s *stubAuthentication) Providers() []auth.ProviderAvailability { return s.providers }
func (s *stubAuthentication) Begin(_ context.Context, input auth.OAuthStartInput) (auth.OAuthStart, error) {
	s.began = input
	return s.start, nil
}
func (s *stubAuthentication) Complete(_ context.Context, completion auth.OAuthCompletion) (auth.OAuthResult, error) {
	s.completeCalls++
	s.completed = completion
	return s.result, s.completeErr
}

type stubSessions struct {
	loggedOutToken string
}

func (s *stubSessions) Logout(_ context.Context, token string) error {
	s.loggedOutToken = token
	return nil
}
