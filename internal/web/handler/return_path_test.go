package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

func TestValidReturnPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/games":                     true,
		"/game-requests?q=Counter":   true,
		"/game-requests/new":         true,
		"/authors":                   true,
		"":                           false,
		"games":                      false,
		"https://evil.example/":      false,
		"//evil.example/":            false,
		`/\evil.example`:             false,
		`/games\..\x`:                false,
		"/games\r\nSet-Cookie: a=b":  false,
		"/auth/login":                false,
		"/auth":                      false,
		"/auth/callback?code=x":      false,
		"/onboarding":                false,
		"/onboarding/pending-1":      false,
		"/%0d%0a":                    true,
		"/\x7f":                      false,
		"javascript:alert(1)":        false,
		"/http://evil.example":       true, // a path, not a host
		"/games#fragment":            true,
		"/games/../auth/login":       false,
		"/auth/../auth/login":        false,
		"/onboarding%2Fx":            false,
		"/ok?return_to=//evil.test/": true,
	} {
		if got := validReturnPath(path); got != want {
			t.Errorf("validReturnPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestLoginCapturesReturnPath(t *testing.T) {
	handler := NewAuthentication(&stubAuthentication{available: true}, &stubSessions{}, branding.Default(), true, nil, nil)
	response := httptest.NewRecorder()

	handler.Login(response, httptest.NewRequest(http.MethodGet, "/auth/login?return_to="+url.QueryEscape("/game-requests?q=Counter Strike"), nil))

	cookie := responseCookie(t, response.Result(), returnCookieName)
	value, _ := url.QueryUnescape(cookie.Value)
	if value != "/game-requests?q=Counter Strike" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 900 {
		t.Fatalf("return cookie = %#v (%q)", cookie, value)
	}
}

func TestLoginIgnoresInvalidReturnPath(t *testing.T) {
	handler := NewAuthentication(&stubAuthentication{available: true}, &stubSessions{}, branding.Default(), true, nil, nil)
	response := httptest.NewRecorder()

	handler.Login(response, httptest.NewRequest(http.MethodGet, "/auth/login?return_to=//evil.example", nil))

	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == returnCookieName {
			t.Fatalf("invalid return path was stored: %#v", cookie)
		}
	}
}

func returnCookieRequest(r *http.Request, value string) *http.Request {
	r.AddCookie(&http.Cookie{Name: returnCookieName, Value: url.QueryEscape(value)})
	return r
}

func TestCallbackRedirectsToReturnPathAndClearsIt(t *testing.T) {
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{
		Operation: auth.FlowLogin, Resolution: auth.Resolution{Session: auth.BrowserSession{Token: "opaque-session"}},
	}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), true, nil, nil)
	request := returnCookieRequest(httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil), "/games")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	if got := response.Header().Get("Location"); got != "/games" {
		t.Fatalf("Location = %q, want /games", got)
	}
	if cleared := responseCookie(t, response.Result(), returnCookieName); cleared.MaxAge >= 0 {
		t.Fatalf("return cookie not cleared: %#v", cleared)
	}
}

func TestCallbackFallsBackWhenReturnCookieIsTampered(t *testing.T) {
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{
		Operation: auth.FlowLogin, Resolution: auth.Resolution{Session: auth.BrowserSession{Token: "opaque-session"}},
	}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), true, nil, nil)
	request := returnCookieRequest(httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil), "https://evil.example/")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	if got := response.Header().Get("Location"); got != "/game-requests" {
		t.Fatalf("Location = %q, want fallback", got)
	}
}

func TestCallbackLinkFlowIgnoresReturnPath(t *testing.T) {
	authentication := &stubAuthentication{available: true, result: auth.OAuthResult{Operation: auth.FlowLink}}
	handler := NewAuthentication(authentication, &stubSessions{}, branding.Default(), true, nil, nil)
	request := returnCookieRequest(httptest.NewRequest(http.MethodGet, "/auth/callback?code=code&state=state", nil), "/games")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.CallbackFor("oidc")(response, request)

	if got := response.Header().Get("Location"); got != "/account" {
		t.Fatalf("Location = %q, want /account", got)
	}
}

func TestOnboardingCompletionRedirectsToReturnPath(t *testing.T) {
	t.Run("pending identity", func(t *testing.T) {
		registration := &stubRegistration{session: auth.BrowserSession{Token: "opaque-session"}}
		handler := NewOnboarding(&stubPending{}, registration, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), true)
		request := withURLParam(httptest.NewRequest(http.MethodPost, "/onboarding/pending-1", strings.NewReader("game_nick=NightOtter")), "pendingID", "pending-1")
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
		request = returnCookieRequest(request, "/games")
		response := httptest.NewRecorder()

		handler.SubmitPending(response, request)

		if got := response.Header().Get("Location"); got != "/games" {
			t.Fatalf("Location = %q, want /games", got)
		}
	})
	t.Run("existing account", func(t *testing.T) {
		handler := NewOnboarding(&stubPending{}, &stubRegistration{}, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), true)
		request := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader("game_nick=NightOtter"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request = returnCookieRequest(withCurrentUser(request, user.User{ID: "user-1"}), "/game-requests?q=Trackmania")
		response := httptest.NewRecorder()

		handler.SubmitExisting(response, request)

		if got := response.Header().Get("Location"); got != "/game-requests?q=Trackmania" {
			t.Fatalf("Location = %q", got)
		}
	})
}

func TestRequireAuthenticationCapturesOnlyPageGETs(t *testing.T) {
	protected := RequireAuthentication(true)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for name, tc := range map[string]struct {
		method  string
		target  string
		headers map[string]string
		want    string
	}{
		"page GET":     {method: http.MethodGet, target: "/games?q=cs", want: "/games?q=cs"},
		"POST":         {method: http.MethodPost, target: "/games"},
		"HTMX":         {method: http.MethodGet, target: "/game-requests", headers: map[string]string{"HX-Request": "true"}},
		"event stream": {method: http.MethodGet, target: "/events/game-requests", headers: map[string]string{"Accept": "text/event-stream"}},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.target, nil)
			for key, value := range tc.headers {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()

			protected.ServeHTTP(response, request)

			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
				t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
			}
			var got string
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == returnCookieName {
					got, _ = url.QueryUnescape(cookie.Value)
				}
			}
			if got != tc.want {
				t.Fatalf("captured return path = %q, want %q", got, tc.want)
			}
		})
	}
}
