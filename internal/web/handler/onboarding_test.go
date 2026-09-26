package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/go-chi/chi/v5"
)

func TestShowPendingRendersSuggestionAndResolveOptions(t *testing.T) {
	pending := &stubPending{value: auth.PendingIdentity{ID: "pending-1", ProviderKind: "discord", GameNickSuggestion: "NightOtter"}}
	authentication := &stubAuthentication{providers: []auth.ProviderAvailability{
		{Kind: "discord", DisplayName: "Discord"}, {Kind: "oidc", DisplayName: "CXT account"},
	}}
	handler := NewOnboarding(pending, &stubRegistration{}, &stubGameNickClaims{}, &stubIdentityLookup{}, authentication, branding.Default(), false)
	response := httptest.NewRecorder()
	response.Code = http.StatusOK
	request := withURLParam(httptest.NewRequest(http.MethodGet, "/onboarding/pending-1", nil), "pendingID", "pending-1")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})

	handler.ShowPending(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `value="NightOtter"`) {
		t.Fatalf("status/body = %d, %q", response.Code, body)
	}
	if !strings.Contains(body, `href="/auth/oidc?resolve=pending-1"`) || strings.Contains(body, "/auth/discord?resolve=pending-1") {
		t.Fatalf("resolve options should only offer the other provider: %q", body)
	}
	if !strings.Contains(body, `class="ph ph-key"`) {
		t.Fatalf("resolve option should carry the OIDC provider glyph: %q", body)
	}
	if pending.lookedUpID != "pending-1" || pending.lookedUpBinding != "binding" {
		t.Fatalf("pending lookup = %q, %q", pending.lookedUpID, pending.lookedUpBinding)
	}
}

// A signed-in caller must never be able to complete (or even see the form
// for) someone else's pending identity: on a shared/kiosk browser this would
// otherwise silently create a new account and overwrite the caller's own
// session.
func TestShowPendingRedirectsAlreadySignedInCallerToAccount(t *testing.T) {
	pending := &stubPending{}
	handler := NewOnboarding(pending, &stubRegistration{}, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	request := withCurrentUser(withURLParam(httptest.NewRequest(http.MethodGet, "/onboarding/pending-1", nil), "pendingID", "pending-1"), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.ShowPending(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if pending.lookedUpID != "" {
		t.Fatalf("pending identity was looked up despite the signed-in guard: %#v", pending)
	}
}

func TestSubmitPendingRedirectsAlreadySignedInCallerToAccount(t *testing.T) {
	registration := &stubRegistration{}
	handler := NewOnboarding(&stubPending{}, registration, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	request := withCurrentUser(withURLParam(httptest.NewRequest(http.MethodPost, "/onboarding/pending-1", strings.NewReader("game_nick=NightOtter")), "pendingID", "pending-1"), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.SubmitPending(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if registration.pendingID != "" {
		t.Fatalf("registration was attempted despite the signed-in guard: %#v", registration)
	}
}

func TestShowPendingRedirectsToLoginWithoutBrowserBinding(t *testing.T) {
	handler := NewOnboarding(&stubPending{}, &stubRegistration{}, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	response := httptest.NewRecorder()
	request := withURLParam(httptest.NewRequest(http.MethodGet, "/onboarding/pending-1", nil), "pendingID", "pending-1")

	handler.ShowPending(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestSubmitPendingCreatesAccountAndSession(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	registration := &stubRegistration{session: auth.BrowserSession{Token: "opaque-session", ExpiresAt: expiresAt}}
	handler := NewOnboarding(&stubPending{}, registration, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), true)
	request := withURLParam(httptest.NewRequest(http.MethodPost, "/onboarding/pending-1", strings.NewReader("game_nick=NightOtter")), "pendingID", "pending-1")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.SubmitPending(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if registration.pendingID != "pending-1" || registration.browserBinding != "binding" || registration.gameNick != "NightOtter" {
		t.Fatalf("registration call = %#v", registration)
	}
	sessionCookie := responseCookie(t, response.Result(), "lansurfer_session")
	if sessionCookie.Value != "opaque-session" {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}
}

func TestSubmitPendingPreservesPendingIdentityOnInvalidGameNick(t *testing.T) {
	registration := &stubRegistration{err: user.ErrGameNickTaken}
	pending := &stubPending{value: auth.PendingIdentity{ID: "pending-1", ProviderKind: "discord"}}
	handler := NewOnboarding(pending, registration, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	request := withURLParam(httptest.NewRequest(http.MethodPost, "/onboarding/pending-1", strings.NewReader("game_nick=Taken")), "pendingID", "pending-1")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: browserBindingCookieName, Value: "binding"})
	response := httptest.NewRecorder()

	handler.SubmitPending(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(response.Body.String(), "already taken") || !strings.Contains(response.Body.String(), `value="Taken"`) {
		t.Fatalf("body = %q", response.Body.String())
	}
	// The pending identity must still be resolvable: registration failure must
	// not have consumed it, and the handler must not have consumed it either.
	if pending.lookedUpID != "pending-1" {
		t.Fatalf("pending was not re-fetched to re-render the form: %#v", pending)
	}
}

func TestShowExistingRedirectsAlreadyOnboardedUser(t *testing.T) {
	handler := NewOnboarding(&stubPending{}, &stubRegistration{}, &stubGameNickClaims{}, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request = withCurrentUser(request, user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	handler.ShowExisting(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestSubmitExistingClaimsGameNickForUpgradedUser(t *testing.T) {
	claims := &stubGameNickClaims{}
	handler := NewOnboarding(&stubPending{}, &stubRegistration{}, claims, &stubIdentityLookup{}, &stubAuthentication{}, branding.Default(), false)
	request := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader("game_nick=NightOtter"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = withCurrentUser(request, user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.SubmitExisting(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if claims.userID != "user-1" || claims.gameNick != "NightOtter" {
		t.Fatalf("claim call = %#v", claims)
	}
}

func TestRequireGameNickRedirectsMissingGameNick(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/games", nil)
	request = withCurrentUser(request, user.User{ID: "user-1", GameNick: ""})
	response := httptest.NewRecorder()

	RequireGameNick(next).ServeHTTP(response, request)

	if called || response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" {
		t.Fatalf("called/status/location = %v, %d, %q", called, response.Code, response.Header().Get("Location"))
	}
}

func TestRequireGameNickPassesThroughOnboardedUser(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/games", nil)
	request = withCurrentUser(request, user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	RequireGameNick(next).ServeHTTP(response, request)

	if !called {
		t.Fatal("RequireGameNick did not call through for an onboarded user")
	}
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))
}

func withCurrentUser(r *http.Request, account user.User) *http.Request {
	return r.WithContext(webmiddleware.ContextWithUser(r.Context(), account))
}

type stubPending struct {
	value           auth.PendingIdentity
	err             error
	lookedUpID      string
	lookedUpBinding string
}

func (s *stubPending) Pending(_ context.Context, id, binding string) (auth.PendingIdentity, error) {
	s.lookedUpID, s.lookedUpBinding = id, binding
	return s.value, s.err
}

type stubRegistration struct {
	pendingID, browserBinding, gameNick string
	session                             auth.BrowserSession
	err                                 error
}

func (s *stubRegistration) Register(_ context.Context, pendingID, browserBinding, gameNick string) (user.User, auth.BrowserSession, error) {
	s.pendingID, s.browserBinding, s.gameNick = pendingID, browserBinding, gameNick
	if s.err != nil {
		return user.User{}, auth.BrowserSession{}, s.err
	}
	return user.User{}, s.session, nil
}

type stubGameNickClaims struct {
	userID, gameNick string
	err              error
}

func (s *stubGameNickClaims) ClaimGameNick(_ context.Context, userID, gameNick string) (user.User, error) {
	s.userID, s.gameNick = userID, gameNick
	if s.err != nil {
		return user.User{}, s.err
	}
	return user.User{ID: userID, GameNick: gameNick}, nil
}

type stubIdentityLookup struct {
	identities []auth.Identity
	err        error
}

func (s *stubIdentityLookup) ListByUser(context.Context, string) ([]auth.Identity, error) {
	return s.identities, s.err
}
