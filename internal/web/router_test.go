package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/platform/mediaproxy"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/handler"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

type reconcileSubscriptions struct {
	handler.SubscriptionManager
	removed string
}

func (s *reconcileSubscriptions) RemoveDeviceByHash(_ context.Context, _, deviceID string) error {
	s.removed = deviceID
	return nil
}

func TestNotificationReconcileRouteRequiresSessionAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, session, csrf string
		want                int
	}{
		{"signed in", "opaque-session", "valid", http.StatusSeeOther},
		{"anonymous", "", "valid", http.StatusSeeOther},
		{"missing csrf", "opaque-session", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subscriptions := &reconcileSubscriptions{}
			account := &routerAuthentication{account: user.User{ID: "user-1", GameNick: "NightOtter"}}
			router := NewRouter(RouterDependencies{Authentication: account, Sessions: account, Subscriptions: subscriptions, WebPushConfigured: true})
			request := httptest.NewRequest(http.MethodPost, "/account/notifications/reconcile", nil)
			if tc.session != "" {
				request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: tc.session})
			}
			request.AddCookie(&http.Cookie{Name: webmiddleware.DeviceCookieName, Value: "device-1"})
			if tc.csrf != "" {
				addCSRF(request)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
			if tc.name == "signed in" && (subscriptions.removed != "device-1" || response.Header().Get("Location") != "/account/notifications") {
				t.Fatalf("removed/location = %q/%q", subscriptions.removed, response.Header().Get("Location"))
			}
			if tc.name != "signed in" && subscriptions.removed != "" {
				t.Fatalf("unexpected removal: %q", subscriptions.removed)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	assertResponse(t, response.Result(), http.StatusOK, "ok\n")
}

func TestReady(t *testing.T) {
	tests := []struct {
		name       string
		ready      ReadyCheck
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database ready",
			ready:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "ready\n",
		},
		{
			name:       "database unavailable",
			ready:      func(context.Context) error { return errors.New("unavailable") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready\n",
		},
		{
			name: "database check times out",
			ready: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: test.ready}).ServeHTTP(response, request)

			assertResponse(t, response.Result(), test.wantStatus, test.wantBody)
		})
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestRootRendersLoginLandingPage(t *testing.T) {
	authentication := &routerAuthentication{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if got := result.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, want := range []string{
		"LANsurfer",
		"Find your crew. Catch the next game.",
		"Continue with CXT account",
		"Continue with Discord",
		`href="https://www.convention-x-treme.de"`,
		`href="/assets/app.css"`,
		`src="/assets/app.js"`,
		`data-theme-toggle`,
		`href="/terms"`,
		`href="/privacy"`,
		"By logging in you agree to the",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	if remoteAssetPattern.Match(body) {
		t.Error("login page unexpectedly loads a remote runtime asset")
	}
}

// remoteAssetPattern matches elements that fetch remote content at runtime.
// Outbound anchors to the operating community are not runtime dependencies.
var remoteAssetPattern = regexp.MustCompile(`(?i)<(?:script|link|img|iframe|source|video|audio|embed)\b[^>]*\b(?:src|href)\s*=\s*["']https?://`)

func TestHomeRendersAuthenticatedViewer(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{GameNick: "Ada Player", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests" {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestHomeRedirectsUpgradedUserWithoutGameNickToOnboarding(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "user-1", Role: user.RoleUser}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestGamesRouteGatesUserWithoutGameNickToOnboarding(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "user-1", Role: user.RoleUser}}
	request := httptest.NewRequest(http.MethodGet, "/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
		Discovery:      &routerDiscovery{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestOnboardingShowsFormForUpgradedUserWithoutGameNick(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "user-1", Role: user.RoleUser}}
	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
		Identities:     &routerIdentities{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Choose your Game Nick") {
		t.Fatalf("status/body = %d, %q", response.Code, response.Body.String())
	}
}

func TestAuthLoginStartRedirectsSignedInUserToAccount(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "user-1", Role: user.RoleUser}}
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" || authentication.began.ProviderKind != "" {
		t.Fatalf("status/location/begin = %d, %q, %#v", response.Code, response.Header().Get("Location"), authentication.began)
	}
}

// /auth/logout lives in the authenticated middleware group (so Logout can
// read CurrentUser to best-effort remove the signed-out device's Push
// subscription), but RequireAuthentication is never applied to it: an
// anonymous request (no session cookie) must still succeed as a no-op
// logout, not be rejected as unauthenticated.
func TestAuthLogoutSucceedsForAnonymousRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	addCSRF(request)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil },
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d (anonymous logout must succeed as a no-op, not be rejected)", response.Code, http.StatusSeeOther)
	}
}

func TestAccountLinkStartBindsSignedInUserAndRedirectsToProvider(t *testing.T) {
	authentication := &routerAuthentication{
		account: user.User{ID: "user-1", GameNick: "NightOtter", Role: user.RoleUser},
		start:   auth.OAuthStart{AuthorizationURL: "https://discord.example/authorize"},
	}
	request := httptest.NewRequest(http.MethodPost, "/account/link/discord", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	// Not a 3xx redirect: form-action CSP is enforced against every redirect
	// hop chained from a form submission, so this POST completes as a
	// same-origin 200 page that itself navigates on to the provider.
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), authentication.start.AuthorizationURL) {
		t.Fatalf("status/body = %d, %q", response.Code, response.Body.String())
	}
	if authentication.began.Operation != auth.FlowLink || authentication.began.InitiatingUserID != "user-1" {
		t.Fatalf("begin input = %#v", authentication.began)
	}
}

func TestTwitchAuthRoutesUseTwitchProvider(t *testing.T) {
	authentication := &routerAuthentication{start: auth.OAuthStart{AuthorizationURL: "https://id.twitch.tv/oauth2/authorize"}}
	router := NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	})

	beginResponse := httptest.NewRecorder()
	router.ServeHTTP(beginResponse, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
	if beginResponse.Code != http.StatusFound || authentication.began.ProviderKind != "twitch" {
		t.Fatalf("begin status/input = %d, %#v", beginResponse.Code, authentication.began)
	}

	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?code=code&state=state", nil)
	callbackRequest.AddCookie(&http.Cookie{Name: "lansurfer_auth_binding", Value: "binding"})
	callbackResponse := httptest.NewRecorder()
	router.ServeHTTP(callbackResponse, callbackRequest)
	if authentication.completed.ProviderKind != "twitch" {
		t.Fatalf("completion input = %#v", authentication.completed)
	}
}

func TestAuthCallbackRoutesSignedInIdentityConflictToAccount(t *testing.T) {
	authentication := &routerAuthentication{
		account:     user.User{ID: "user-1", Role: user.RoleUser},
		completeErr: auth.ErrIdentityConflict,
	}
	request := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	request.AddCookie(&http.Cookie{Name: "lansurfer_auth_binding", Value: "binding"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
		Sessions:       authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account?link_error=1" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestPatternFragments(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/ui/patterns/empty", want: "Quiet out there. Start something?"},
		{path: "/ui/patterns/error", want: "Something went wrong"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			result := response.Result()
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
			}
			body, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), test.want) {
				t.Errorf("body does not contain %q", test.want)
			}
		})
	}
}

func TestEmbeddedAssetsAreServed(t *testing.T) {
	for _, path := range []string{
		"/assets/app.css",
		"/assets/app.js",
		"/assets/fonts/schibsted-grotesk-latin-wght-normal.woff2",
		"/assets/fonts/schibsted-grotesk-latin-wght-italic.woff2",
		"/assets/fonts/jetbrains-mono-latin-wght-normal.woff2",
		"/assets/images/otter.webp",
		"/assets/licenses/htmx-BSD-2-Clause.txt",
		"/assets/licenses/schibsted-grotesk-OFL-1.1.txt",
		"/assets/licenses/jetbrains-mono-OFL-1.1.txt",
		"/assets/licenses/tailwindcss-MIT.txt",
		"/assets/licenses/daisyui-MIT.txt",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if response.Body.Len() == 0 {
				t.Error("asset response is empty")
			}
		})
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	viewer := &routerAuthentication{account: user.User{GameNick: "Player", Role: user.RoleUser}}
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer,
		Sessions:       viewer,
		Games:          &routerGames{},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAdminRoutesRedirectAnonymousUsersToLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil },
		Games: &routerGames{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminGameSearchRendersAliasMatch(t *testing.T) {
	games := &routerGames{items: []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true, Aliases: []game.Alias{{ID: "alias-1", Value: "WC3"}}}}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games?q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	if response.Code != http.StatusOK || games.search != "wc3" {
		t.Fatalf("response status = %d, search = %q", response.Code, games.search)
	}
	for _, want := range []string{"Warcraft III", "WC3", "Deactivate game"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestAdminGameCatalogShowsIdentityAndDeletionGuards(t *testing.T) {
	games := &routerGames{items: []game.Game{
		{ID: "linked", Name: "Linked Game", Slug: "linked-game", Active: true, IGDBExternalID: "100", IGDBEnriched: true, GameRequestCount: 2,
			IGDBSummary: "A linked adventure.", IGDBGenreNames: []string{"Strategy"}, IGDBGameModeNames: []string{"Multiplayer"},
			IGDBMultiplayerModes: []game.MultiplayerMode{{PlatformIGDBID: 6, PlatformName: "PC", OnlineCoop: true, OnlineMax: new(4)}},
		},
		{ID: "followed", Name: "Followed Game", Slug: "followed-game", Active: true, SubscriptionCount: 1},
		{ID: "unused", Name: "Unused Game", Slug: "unused-game", Active: false},
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	for _, want := range []string{
		"IGDB #100", "enrichment snapshot available", "Manage IGDB link",
		"This game has Game Requests and cannot be deleted", "This game is followed by users",
		"Permanently delete Unused Game?",
		"A linked adventure.", "Strategy", "Multiplayer", "PC", "Online co-op", "Up to 4 online",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

// The catalog filters apply live, like every other list in the product: an
// htmx request must swap the result list alone, and the "not linked to IGDB"
// filter must narrow it to the Games that still need a provider identity.
func TestAdminGameCatalogFiltersLiveToUnlinkedGames(t *testing.T) {
	games := &routerGames{items: []game.Game{
		{ID: "linked", Name: "Linked Game", Slug: "linked-game", Active: true, IGDBExternalID: "100"},
		{ID: "local", Name: "Local Game", Slug: "local-game", Active: true},
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games?local=1", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "app-sidebar") {
		t.Error("htmx response re-rendered the whole page instead of the result list")
	}
	if !strings.Contains(body, "Local Game") {
		t.Error("filtered list does not contain the unlinked game")
	}
	if strings.Contains(body, "Linked Game") {
		t.Error("filtered list still contains a game linked to IGDB")
	}
	if !strings.Contains(body, "1 game · 1 active · 0 linked to IGDB") {
		t.Errorf("count line does not describe the filtered list: %q", body)
	}
}

// The top bar's shortcut and the composer it opens are one contract: the
// link is useless if the fragment does not name the composer.
func TestAdminCatalogLinksTheTopBarShortcutToTheComposer(t *testing.T) {
	games := &routerGames{items: []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true}}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	for _, want := range []string{`href="#add-game"`, `id="add-game"`, "Add a game manually"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

// Adding an alias from an opened Game must not reload the screen: the
// answer is that Game's own entry, re-read and still open, so the panel the
// administrator is working in stays open and in place.
func TestAdminAliasAdditionSwapsTheOpenedEntry(t *testing.T) {
	games := &routerGames{items: []game.Game{
		{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true, Aliases: []game.Alias{{ID: "alias-1", Value: "wc3"}}},
		{ID: "game-2", Name: "Quake III Arena", Slug: "quake-3", Active: true},
	}}
	form := url.Values{"alias": {"wc3"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/game-1/aliases", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "app-sidebar") {
		t.Error("alias addition reloaded the whole screen instead of swapping the entry")
	}
	if !strings.Contains(body, `id="game-game-1"`) || !strings.Contains(body, "open") {
		t.Errorf("response is not the opened entry: %q", body)
	}
	if strings.Contains(body, "Quake III Arena") {
		t.Error("response carries entries other than the Game that changed")
	}
}

// A rejected change has to come back as the same swap: htmx leaves a
// non-2xx response unswapped, so answering with an error status would drop
// the message that explains the rejection.
func TestAdminRejectedAliasKeepsTheEntryAndStatesWhy(t *testing.T) {
	games := &routerGames{
		items:    []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true}},
		aliasErr: game.ErrAlreadyExists,
	}
	form := url.Values{"alias": {"wc3"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/game-1/aliases", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d so htmx swaps the message in", response.Code, http.StatusOK)
	}
	if !strings.Contains(body, "already in use") {
		t.Errorf("response does not state why the alias was rejected: %q", body)
	}
	if !strings.Contains(body, `id="game-game-1"`) {
		t.Error("response is not the entry the change was made in")
	}
}

// Without scripting the same route still posts and redirects, and the
// redirect names the entry so the reload lands on the Game being edited.
func TestAdminAliasAdditionRedirectsToTheEntryWithoutScripting(t *testing.T) {
	games := &routerGames{items: []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true}}}
	form := url.Values{"alias": {"wc3"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/game-1/aliases", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	want := "/admin/games?notice=alias-added#game-game-1"
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != want {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminIGDBLinkSearchGuidesExistingMappingToMerge(t *testing.T) {
	release := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	games := &routerGames{
		items: []game.Game{
			{ID: "target", Name: "Local Game", Slug: "local-game"},
			{ID: "source", Name: "IGDB Game", Slug: "igdb-game", IGDBExternalID: "100"},
		},
		igdbItems: []game.AdminProviderGame{{
			ExternalID: "100", Name: "Provider Name", GameType: "Main Game",
			FirstReleaseDate: &release, CoverImageID: "co-admin",
			Summary: "Provider summary.", GenreNames: []string{"RPG"}, GameModeNames: []string{"Co-op"},
			MultiplayerModes: []game.MultiplayerMode{{PlatformIGDBID: 6, PlatformName: "PC", OfflineCoop: true}},
			LinkedGameID:     "source", LinkedGameName: "IGDB Game",
		}},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/games/target/igdb?q=provider", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK || games.igdbSearch != "provider" {
		t.Fatalf("status/search = %d, %q", response.Code, games.igdbSearch)
	}
	for _, want := range []string{
		"Provider Name", "2024", "/media/igdb/big/co-admin", "Linked to IGDB Game", "/admin/games/target/merge?source=source",
		// The candidate renders in the product's own row and cover
		// vocabulary rather than in a layout of its own, so a long provider
		// name wraps in the row instead of widening the page.
		"catalog-entry", "catalog-cover", "catalog-entry-title",
		"Provider summary.", "RPG", "Co-op", "PC", "Offline co-op",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestAdminIGDBLinkConflictRedirectsToMerge(t *testing.T) {
	games := &routerGames{
		items:   []game.Game{{ID: "target", Name: "Local Game", Slug: "local-game"}},
		linkErr: &game.IGDBLinkConflict{GameID: "source", GameName: "IGDB Game"},
	}
	form := url.Values{"external_id": {"100"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/target/igdb", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	want := "/admin/games/target/merge?source=source&notice=igdb-link-conflict"
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != want {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminMergeKeepsCurrentGameAsTarget(t *testing.T) {
	games := &routerGames{items: []game.Game{
		{ID: "target", Name: "Target", Slug: "target"},
		{ID: "source", Name: "Source", Slug: "source"},
	}}
	form := url.Values{"source_id": {"source"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/target/merge", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	if games.mergedSource != "source" || games.mergedTarget != "target" {
		t.Fatalf("merge source/target = %q/%q", games.mergedSource, games.mergedTarget)
	}
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/games?notice=game-merged" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAdminDeleteRechecksUsageGuard(t *testing.T) {
	games := &routerGames{
		items:     []game.Game{{ID: "used", Name: "Used", Slug: "used", GameRequestCount: 1}},
		deleteErr: game.ErrGameHasRequests,
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/games/used/delete", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminTestRouter(games).ServeHTTP(response, request)

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "cannot be deleted") {
		t.Fatalf("status/body = %d, %q", response.Code, response.Body.String())
	}
}

func TestAdminIGDBRoutesRequireAdmin(t *testing.T) {
	viewer := &routerAuthentication{account: user.User{GameNick: "Player", Role: user.RoleUser}}
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer,
		Sessions:       viewer,
		Games:          &routerGames{},
		ProviderSync:   &routerProviderSync{},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}

	// The polled status fragment carries the same provider state as the page,
	// so it lives behind the same administrator guard.
	fragment := httptest.NewRequest(http.MethodGet, "/admin/igdb/status", nil)
	fragment.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	fragmentResponse := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(fragmentResponse, fragment)

	if fragmentResponse.Code != http.StatusForbidden {
		t.Fatalf("fragment status = %d, want %d", fragmentResponse.Code, http.StatusForbidden)
	}
}

func TestAdminIGDBPageRendersStatusAndHidesSyncWhenDisabled(t *testing.T) {
	providerSync := &routerProviderSync{status: game.ProviderStatus{State: game.ProviderDisabled}}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Disabled") {
		t.Errorf("body does not contain disabled state: %q", body)
	}
	if strings.Contains(body, "Sync now") {
		t.Errorf("body unexpectedly contains a sync action while disabled")
	}
}

func TestAdminIGDBPageShowsSyncActionAndLastError(t *testing.T) {
	lastAttempt := time.Date(2027, 1, 2, 3, 4, 0, 0, time.UTC)
	providerSync := &routerProviderSync{status: game.ProviderStatus{
		State:       game.ProviderFailed,
		LastAttempt: &lastAttempt,
		RecordCount: 42,
		LastError:   "upstream returned 503",
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	body := response.Body.String()
	for _, want := range []string{"Failed", "42", "upstream returned 503", "Sync now"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %q", want, body)
		}
	}
	for _, unwanted := range []string{"client_secret", "access_token", "Bearer "} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body unexpectedly contains %q", unwanted)
		}
	}
}

// While a full sync is in progress the page has to show the staging count
// separately from the last successful baseline, and it has to keep refreshing
// itself, or an operator cannot tell progress from a hung sync.
func TestAdminIGDBPageReportsStagedProgressWhileSyncing(t *testing.T) {
	providerSync := &routerProviderSync{status: game.ProviderStatus{
		State:         game.ProviderSyncing,
		RecordCount:   120000,
		StagedRecords: 200345,
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	body := response.Body.String()
	for _, want := range []string{
		"Syncing", "Staged in this sync", "200345", "120000",
		`aria-live="polite"`,
		`hx-get="/admin/igdb/status"`,
		`hx-trigger="every 2s"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %q", want, body)
		}
	}
	if strings.Contains(body, "Sync now") {
		t.Error("body offers a manual sync while one is already running")
	}
}

// The polling element is the element the refresh replaces, so a settled state
// rendering without the polling attributes is what ends the loop: it must not
// still poll, and it must not claim a staging count that does not exist.
func TestAdminIGDBSettledPageStopsPolling(t *testing.T) {
	providerSync := &routerProviderSync{status: game.ProviderStatus{State: game.ProviderCurrent, RecordCount: 42}}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	body := response.Body.String()
	for _, unwanted := range []string{`hx-trigger="every 2s"`, "/admin/igdb/status", "Staged in this sync"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("settled page unexpectedly contains %q: %q", unwanted, body)
		}
	}
}

// The refresh polls a fragment, not the page: it must render the status card
// without the surrounding chrome, so repeated swaps cannot nest layouts.
func TestAdminIGDBStatusFragmentRendersCardOnly(t *testing.T) {
	providerSync := &routerProviderSync{status: game.ProviderStatus{
		State:         game.ProviderSyncing,
		RecordCount:   120000,
		StagedRecords: 200345,
	}}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb/status", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, want := range []string{`id="igdb-status"`, "Staged in this sync", "200345", `hx-trigger="every 2s"`} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment does not contain %q: %q", want, body)
		}
	}
	for _, unwanted := range []string{"<html", "app-topbar", "IGDB synchronization"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("fragment unexpectedly contains page chrome %q", unwanted)
		}
	}
}

// A failed refresh must not blank the card: the fragment answers 500 so htmx
// leaves the last good status on screen.
func TestAdminIGDBStatusFragmentReportsReadFailure(t *testing.T) {
	providerSync := &routerProviderSync{statusErr: errors.New("database unavailable")}
	request := httptest.NewRequest(http.MethodGet, "/admin/igdb/status", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestAdminIGDBSyncTriggersManualSync(t *testing.T) {
	providerSync := &routerProviderSync{}
	request := httptest.NewRequest(http.MethodPost, "/admin/igdb/sync", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/igdb?notice=igdb-sync-started" {
		t.Fatalf("response = %d %q", response.Code, response.Header().Get("Location"))
	}
	if !providerSync.started {
		t.Error("manual sync was not started")
	}
}

func TestAdminIGDBSyncReportsAlreadyRunningWhenDeclined(t *testing.T) {
	providerSync := &routerProviderSync{declineStart: true}
	request := httptest.NewRequest(http.MethodPost, "/admin/igdb/sync", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	adminIGDBTestRouter(providerSync).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/igdb?notice=igdb-sync-already-running" {
		t.Fatalf("response = %d %q", response.Code, response.Header().Get("Location"))
	}
	if providerSync.started {
		t.Error("manual sync should not have been marked started when declined")
	}
}

func TestAdminPWARequiresAdmin(t *testing.T) {
	viewer := &routerAuthentication{account: user.User{GameNick: "Player", Role: user.RoleUser}}
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer,
		Sessions:       viewer,
		Games:          &routerGames{},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/pwa", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAdminPWARendersServerFacts(t *testing.T) {
	admin := &routerAuthentication{account: user.User{GameNick: "Ada Admin", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/admin/pwa", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: admin,
		Sessions:       admin,
		Games:          &routerGames{},
		InstanceName:   "Camp Nova",
		BaseURL:        "http://lan.example.test",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, want := range []string{
		"Camp Nova",
		"http://lan.example.test",
		"http://lan.example.test/manifest.webmanifest",
		"http://lan.example.test/service-worker.js",
		"No (HTTPS, or a loopback/localhost origin, is required",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

// The browser-only facts must render something sensible before admin-pwa.ts
// ever runs: a player with JavaScript disabled (or the script failing to
// load) still sees a coherent page, not blank cells.
func TestAdminPWARendersWithoutJavaScript(t *testing.T) {
	admin := &routerAuthentication{account: user.User{GameNick: "Ada Admin", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/admin/pwa", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil }, Authentication: admin, Sessions: admin, Games: &routerGames{},
	}).ServeHTTP(response, request)

	body := response.Body.String()
	if count := strings.Count(body, "Not available without JavaScript"); count != 5 {
		t.Errorf("found %d browser-fact placeholders, want 5", count)
	}
	if !strings.Contains(body, `src="/assets/admin-pwa.js"`) {
		t.Error("page does not load the progressive diagnostics script")
	}
}

func TestGameCatalogRedirectsAnonymousUsersToLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/games", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:     func(context.Context) error { return nil },
		Discovery: &routerDiscovery{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAuthenticatedUserCanBrowseAndFollowGames(t *testing.T) {
	subscriptions := &routerDiscovery{
		items: []game.DiscoveryItem{{
			LocalGameID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true,
		}},
		selectItem: game.DiscoveryItem{
			LocalGameID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true, Subscribed: true,
		},
	}
	router := gameTestRouter(subscriptions)

	request := httptest.NewRequest(http.MethodGet, "/games?q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || subscriptions.search != "wc3" {
		t.Fatalf("browse response = %d, search = %q", response.Code, subscriptions.search)
	}
	for _, want := range []string{"Warcraft III", ">Follow<", `hx-post="/games/game-1/follow"`, "Followed"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
	}

	follow := httptest.NewRequest(http.MethodPost, "/games/game-1/follow", nil)
	follow.Header.Set("HX-Request", "true")
	follow.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(follow)
	followResponse := httptest.NewRecorder()
	router.ServeHTTP(followResponse, follow)
	if followResponse.Code != http.StatusOK || subscriptions.followed != "user-1/game-1" {
		t.Fatalf("follow response = %d, write = %q", followResponse.Code, subscriptions.followed)
	}
	// The swap replaces the whole card, not just the button: following also
	// changes the cover tag and the card border, and the re-read behind it is
	// what keeps the cover and metadata on the card that lands.
	fragment := followResponse.Body.String()
	if !strings.Contains(fragment, "Unfollow") || !strings.Contains(fragment, "<article class=\"game-card") {
		t.Errorf("follow fragment = %q", fragment)
	}
	if subscriptions.selectSelection.LocalGameID != "game-1" {
		t.Errorf("follow did not re-read the card it changed: %#v", subscriptions.selectSelection)
	}
}

// Games and My Games are one screen: the followed list is a filter on
// /games, so the retired /games/mine address resolves to that filter rather
// than 404ing bookmarks and links that predate the merge.
func TestMyGamesRedirectsToTheFollowedFilter(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/games/mine", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	gameTestRouter(&routerDiscovery{}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/games?followed=1" {
		t.Fatalf("Location = %q, want %q", location, "/games?followed=1")
	}
}

// "Followed only" narrows the one catalog rather than opening a second one:
// it reaches the followed read path, keeps the search alongside it, and the
// cards it renders are the same cards.
func TestGameCatalogFollowedFilterNarrowsTheSameScreen(t *testing.T) {
	discovery := &routerDiscovery{mine: []game.DiscoveryItem{{
		LocalGameID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true, Subscribed: true,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/games?followed=1&q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	gameTestRouter(discovery).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !discovery.followedOnly || discovery.search != "wc3" {
		t.Fatalf("status = %d, followedOnly = %t, search = %q", response.Code, discovery.followedOnly, discovery.search)
	}
	body := response.Body.String()
	for _, want := range []string{"Warcraft III", "1 game followed", "Unfollow"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
}

func TestGameCatalogRendersUnmaterializedProviderResultWithFollowAction(t *testing.T) {
	releaseDate := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	discovery := &routerDiscovery{items: []game.DiscoveryItem{{
		Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "42"},
		Name:             "Warcraft III: Reign of Chaos",
		GameType:         "Main Game",
		PlatformNames:    []string{"PC (Microsoft Windows)"},
		FirstReleaseDate: &releaseDate,
		CoverImageID:     "co1abc",
		Summary:          "Azeroth braces for war.",
		GenreNames:       []string{"Real Time Strategy (RTS)"},
		Materialized:     false,
	}}}
	router := gameTestRouter(discovery)

	request := httptest.NewRequest(http.MethodGet, "/games?q=warcraft", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		"Warcraft III: Reign of Chaos",
		"2002 · Azeroth braces for war.",
		">RTS<",
		"/media/igdb/big/co1abc",
		">Follow<",
		`hx-post="/games/providers/igdb/42/follow"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	// The unified card never exposes materialization origin (spec: "4.1 One
	// player-facing Game card").
	if strings.Contains(body, "Not yet in LANsurfer catalog") {
		t.Errorf("body unexpectedly exposes materialization origin: %q", body)
	}
	// The card never posts the provider name/slug/metadata; only the opaque
	// provider reference travels, in the URL path.
	if strings.Contains(body, "hx-post=\"/games//follow\"") {
		t.Errorf("follow form posts to malformed local-follow route: %q", body)
	}
}

func TestAuthenticatedUserCanFollowProviderResult(t *testing.T) {
	discovery := &routerDiscovery{
		followResult: game.Game{ID: "local-1", Name: "Warcraft III", Slug: "warcraft-3"},
		selectItem: game.DiscoveryItem{
			LocalGameID: "local-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true, Subscribed: true,
		},
	}
	router := gameTestRouter(discovery)

	request := httptest.NewRequest(http.MethodPost, "/games/providers/igdb/42/follow", nil)
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || discovery.followProvided != "user-1/igdb/42" {
		t.Fatalf("status = %d, followProvided = %q", response.Code, discovery.followProvided)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Following") || !strings.Contains(body, `action="/games/local-1/unfollow"`) {
		t.Errorf("body = %q", body)
	}
}

func TestFollowProviderResultReportsNotFoundWhenProviderRecordMissing(t *testing.T) {
	discovery := &routerDiscovery{followErr: game.ErrProviderRecordNotFound}
	router := gameTestRouter(discovery)

	request := httptest.NewRequest(http.MethodPost, "/games/providers/igdb/42/follow", nil)
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestFollowProviderResultReportsInvalidReference(t *testing.T) {
	discovery := &routerDiscovery{followErr: game.ErrInvalidProviderReference}
	router := gameTestRouter(discovery)

	request := httptest.NewRequest(http.MethodPost, "/games/providers/steam/42/follow", nil)
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
}

func TestGameRequestsRedirectAnonymousUsersToLogin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:        func(context.Context) error { return nil },
		GameRequests: &routerGameRequests{}, RequestQueries: &routerGameRequestQueries{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestRealtimeRedirectsAnonymousUsersToLogin(t *testing.T) {
	events := make(chan gamerequest.Event)
	close(events)
	request := httptest.NewRequest(http.MethodGet, "/events/game-requests", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:    func(context.Context) error { return nil },
		Realtime: &routerRealtime{events: events},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestRealtimeRefreshHighlightsFollowedGame(t *testing.T) {
	queries := &routerGameRequestQueries{items: []requestquery.Request{{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"},
		CreatorID: "creator-1", CreatorGameNick: "Grace", StartsAt: time.Now(),
		Status: gamerequest.StatusOpen, MemberCount: 1, ViewerSubscribed: true,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/game-requests/refresh?highlight=request-1", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	gameRequestTestRouter(&routerGameRequests{}, queries, &routerDiscovery{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	// The refresh also carries the top bar counters out of band, so the stats
	// above the list cannot drift away from the rows below it.
	for _, want := range []string{`id="game-request-list"`, "request-row-incoming", "New Game Request for Warcraft III",
		`hx-swap-oob="beforeend:#live-notices"`, `hx-swap-oob="true"`, `id="request-stats"`, "player in active requests"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q: %q", want, response.Body.String())
		}
	}
}

// Discovery filters are read from the query string and answered server-side:
// an htmx submission gets the list alone, a plain GET gets the whole screen.
func TestGameRequestDiscoveryFiltersAndTrending(t *testing.T) {
	queries := &routerGameRequestQueries{
		items: []requestquery.Request{{
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "creator-1",
			CreatorGameNick: "Grace", StartsAt: time.Now(), Status: gamerequest.StatusOpen, MemberCount: 1,
		}},
		trending: []requestquery.TrendingGame{{GameID: "game-1", GameName: "Warcraft III", ActivityCount: 4, RequestCount: 2}},
	}
	router := gameRequestTestRouter(&routerGameRequests{}, queries, &routerDiscovery{})

	page := httptest.NewRequest(http.MethodGet, "/game-requests?q=wc3&scope=mine&open_places=1", nil)
	page.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	pageResponse := httptest.NewRecorder()
	router.ServeHTTP(pageResponse, page)

	if pageResponse.Code != http.StatusOK {
		t.Fatalf("status = %d", pageResponse.Code)
	}
	want := requestquery.Filter{Search: "wc3", Scope: requestquery.ScopeMine, OpenPlacesOnly: true}
	if queries.filter != want {
		t.Fatalf("filter = %#v, want %#v", queries.filter, want)
	}
	body := pageResponse.Body.String()
	// The window has to be stated next to the ranking, and the ranking itself
	// comes from recorded request activity.
	for _, fragment := range []string{`id="trending-rail"`, "Trending", "last 6 hours", "All", "My Games", "Joined", "Created by me", "Free places", "1 matching active request"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("page does not contain %q", fragment)
		}
	}

	fragment := httptest.NewRequest(http.MethodGet, "/game-requests?q=wc3", nil)
	fragment.Header.Set("HX-Request", "true")
	fragment.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	fragmentResponse := httptest.NewRecorder()
	router.ServeHTTP(fragmentResponse, fragment)

	if fragmentResponse.Code != http.StatusOK {
		t.Fatalf("fragment status = %d", fragmentResponse.Code)
	}
	fragmentBody := fragmentResponse.Body.String()
	if strings.Contains(fragmentBody, "<!doctype html>") {
		t.Error("filter submission returned a whole page instead of the list")
	}
	if !strings.Contains(fragmentBody, `id="game-request-list"`) {
		t.Errorf("filter submission did not return the list: %s", fragmentBody)
	}
}

// The navigation badge sits in the shell beside every section, so it states how
// many Game Requests are active (`open` or `playing`) on the installation.
// Deriving it from the filtered list made the same instance look busy or quiet
// depending on what the viewer had typed, and made an htmx filter and a reload
// of the same URL disagree.
func TestNavigationBadgeIgnoresDiscoveryFilters(t *testing.T) {
	queries := &routerGameRequestQueries{
		items: []requestquery.Request{{
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "creator-1",
			CreatorGameNick: "Grace", StartsAt: time.Now(), Status: gamerequest.StatusOpen, MemberCount: 1,
		}},
		activeTotal: 10,
	}
	router := gameRequestTestRouter(&routerGameRequests{}, queries, &routerDiscovery{})

	request := httptest.NewRequest(http.MethodGet, "/game-requests?q=warcraft", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `class="data-text count-hot ml-auto text-xs">10<`) {
		t.Errorf("badge does not show the installation's active total: %s", body)
	}
	if !strings.Contains(body, "1 matching active request") {
		t.Errorf("filtered list count is missing: %s", body)
	}
}

// A rejected search has to explain itself on the htmx path too. Swapping in an
// empty list with no message reads as "nothing matches" rather than "that
// search was refused".
func TestRejectedSearchExplainsItselfInTheFragment(t *testing.T) {
	queries := &routerGameRequestQueries{listErr: requestquery.ErrInvalidSearch}
	router := gameRequestTestRouter(&routerGameRequests{}, queries, &routerDiscovery{})

	request := httptest.NewRequest(http.MethodGet, "/game-requests?q="+strings.Repeat("x", 200), nil)
	request.Header.Set("HX-Request", "true")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Search must be 120 characters or fewer.") {
		t.Errorf("fragment does not explain the rejected search: %s", body)
	}
	if !strings.Contains(body, `id="game-request-list"`) {
		t.Errorf("fragment is not the list region: %s", body)
	}
}

func TestGameRequestBrowserWorkflow(t *testing.T) {
	commands := &routerGameRequests{}
	queries := &routerGameRequestQueries{
		items: []requestquery.Request{{
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"},
			CreatorID: "creator-1", CreatorGameNick: "Grace", StartsAt: time.Now(),
			Status: gamerequest.StatusOpen, MemberCount: 1,
		}},
	}
	discovery := &routerDiscovery{selectItem: game.DiscoveryItem{LocalGameID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true}}
	router := gameRequestTestRouter(commands, queries, discovery)

	list := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
	list.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	for _, want := range []string{"Game Requests", "Warcraft III", `class="request-row"`, `hx-post="/game-requests/request-1/join"`} {
		if !strings.Contains(listResponse.Body.String(), want) {
			t.Errorf("list body does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"Live <span>· CXT 2027</span>"} {
		if strings.Contains(listResponse.Body.String(), unwanted) {
			t.Errorf("list body unexpectedly contains %q", unwanted)
		}
	}

	newRequest := httptest.NewRequest(http.MethodGet, "/game-requests/new?game_id=game-1", nil)
	newRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	newResponse := httptest.NewRecorder()
	router.ServeHTTP(newResponse, newRequest)
	if newResponse.Code != http.StatusOK || !strings.Contains(newResponse.Body.String(), `name="game_id" value="game-1"`) {
		t.Errorf("preselected creation form = %d %q", newResponse.Code, newResponse.Body.String())
	}
	if discovery.selectSelection != (game.GameSelection{LocalGameID: "game-1"}) {
		t.Errorf("preselected creation form did not read-only select game-1: %#v", discovery.selectSelection)
	}

	form := url.Values{"game_id": {"game-1"}, "max_players": {"4"}, "description": {"Casual players"}}
	create := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	create.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	create.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(create)
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusSeeOther || createResponse.Header().Get("Location") != "/game-requests/request-created?notice=request-created" {
		t.Fatalf("create response = %d %q", createResponse.Code, createResponse.Header().Get("Location"))
	}
	if commands.created.CreatorID != "user-1" || commands.created.GameID != "game-1" || commands.created.MaxPlayers == nil || *commands.created.MaxPlayers != 4 {
		t.Fatalf("created request = %#v", commands.created)
	}

	join := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", nil)
	join.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(join)
	joinResponse := httptest.NewRecorder()
	router.ServeHTTP(joinResponse, join)
	if joinResponse.Code != http.StatusSeeOther || commands.joined != "request-1/user-1" {
		t.Fatalf("join response = %d, command = %q", joinResponse.Code, commands.joined)
	}

	queries.items[0].ViewerJoined = true
	htmxJoin := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", strings.NewReader(url.Values{"return_to": {"list"}}.Encode()))
	htmxJoin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	htmxJoin.Header.Set("HX-Request", "true")
	htmxJoin.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(htmxJoin)
	htmxResponse := httptest.NewRecorder()
	router.ServeHTTP(htmxResponse, htmxJoin)
	if htmxResponse.Code != http.StatusOK || !strings.Contains(htmxResponse.Body.String(), ">Leave</button>") {
		t.Fatalf("htmx join response = %d %q", htmxResponse.Code, htmxResponse.Body.String())
	}
}

func assertResponse(t *testing.T, response *http.Response, wantStatus int, wantBody string) {
	t.Helper()
	defer response.Body.Close()

	if response.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d", response.StatusCode, wantStatus)
	}
	if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if string(body) != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func addCSRF(request *http.Request) {
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	request.AddCookie(&http.Cookie{Name: "lansurfer_csrf", Value: token})
	request.Header.Set("X-CSRF-Token", token)
}

type routerAuthentication struct {
	account     user.User
	start       auth.OAuthStart
	began       auth.OAuthStartInput
	result      auth.OAuthResult
	completed   auth.OAuthCompletion
	completeErr error
}

func adminTestRouter(games *routerGames) http.Handler {
	admin := &routerAuthentication{account: user.User{GameNick: "Ada Admin", Role: user.RoleAdmin}}
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: admin,
		Sessions:       admin,
		Games:          games,
	})
}

func adminIGDBTestRouter(providerSync *routerProviderSync) http.Handler {
	admin := &routerAuthentication{account: user.User{GameNick: "Ada Admin", Role: user.RoleAdmin}}
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: admin,
		Sessions:       admin,
		Games:          &routerGames{},
		ProviderSync:   providerSync,
	})
}

func gameTestRouter(discovery *routerDiscovery) http.Handler {
	viewer := &routerAuthentication{account: user.User{
		ID: "user-1", GameNick: "Ada Player", Role: user.RoleUser,
	}}
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer,
		Sessions:       viewer,
		Discovery:      discovery,
	})
}

func gameRequestTestRouter(commands *routerGameRequests, queries *routerGameRequestQueries, discovery *routerDiscovery) http.Handler {
	if discovery == nil {
		discovery = &routerDiscovery{}
	}
	viewer := &routerAuthentication{account: user.User{
		ID: "user-1", GameNick: "Ada Player", Role: user.RoleUser,
	}}
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer,
		Sessions:       viewer,
		GameRequests:   commands, RequestQueries: queries, Discovery: discovery,
	})
}

type routerIdentities struct {
	identities []auth.Identity
}

func (r *routerIdentities) ListByUser(context.Context, string) ([]auth.Identity, error) {
	return r.identities, nil
}

type routerGames struct {
	items          []game.Game
	igdbItems      []game.AdminProviderGame
	search         string
	igdbSearch     string
	linkedGame     string
	linkedExternal string
	unlinkedGame   string
	mergedSource   string
	mergedTarget   string
	deletedGame    string
	linkErr        error
	mergeErr       error
	deleteErr      error
	aliasErr       error
}

func (r *routerGames) List(_ context.Context, search string) ([]game.Game, error) {
	r.search = search
	return r.items, nil
}
func (*routerGames) Create(context.Context, string, string) (game.Game, error) {
	return game.Game{}, nil
}
func (*routerGames) Update(context.Context, string, string, string) (game.Game, error) {
	return game.Game{}, nil
}
func (*routerGames) SetActive(context.Context, string, bool) error { return nil }
func (r *routerGames) AddAlias(context.Context, string, string) (game.Alias, error) {
	return game.Alias{}, r.aliasErr
}
func (*routerGames) RemoveAlias(context.Context, string, string) error { return nil }
func (r *routerGames) SearchIGDB(_ context.Context, search string) ([]game.AdminProviderGame, error) {
	r.igdbSearch = search
	return r.igdbItems, nil
}
func (r *routerGames) LinkIGDB(_ context.Context, gameID, externalID string) error {
	r.linkedGame, r.linkedExternal = gameID, externalID
	return r.linkErr
}
func (r *routerGames) UnlinkIGDB(_ context.Context, gameID string) error {
	r.unlinkedGame = gameID
	return nil
}
func (r *routerGames) Merge(_ context.Context, sourceID, targetID string) error {
	r.mergedSource, r.mergedTarget = sourceID, targetID
	return r.mergeErr
}
func (r *routerGames) Delete(_ context.Context, gameID string) error {
	r.deletedGame = gameID
	return r.deleteErr
}

type routerProviderSync struct {
	status       game.ProviderStatus
	statusErr    error
	started      bool
	declineStart bool
}

func (r *routerProviderSync) Status(context.Context) (game.ProviderStatus, error) {
	return r.status, r.statusErr
}

func (r *routerProviderSync) TryStart(context.Context) bool {
	if r.declineStart {
		return false
	}
	r.started = true
	return true
}

type routerDiscovery struct {
	items          []game.DiscoveryItem
	mine           []game.DiscoveryItem
	followedOnly   bool
	search         string
	followed       string
	unfollowed     string
	followProvided string
	followResult   game.Game
	followErr      error

	selectUserID    string
	selectSelection game.GameSelection
	selectItem      game.DiscoveryItem
	selectErr       error
}

type routerGameRequests struct {
	created   gamerequest.GameRequest
	joined    string
	left      string
	closed    string
	cancelled string
	joinError error
}

func (r *routerGameRequests) Create(_ context.Context, creatorID, gameID string, maximum *int32, startsAt time.Time, description string) (gamerequest.GameRequest, error) {
	r.created = gamerequest.GameRequest{
		ID: "request-created", CreatorID: creatorID, GameID: gameID, MaxPlayers: maximum,
		StartsAt: startsAt, Description: description, Status: gamerequest.StatusOpen,
	}
	return r.created, nil
}
func (r *routerGameRequests) Join(_ context.Context, requestID, userID string) error {
	if r.joinError != nil {
		return r.joinError
	}
	r.joined = requestID + "/" + userID
	return nil
}
func (r *routerGameRequests) Leave(_ context.Context, requestID, userID string) error {
	r.left = requestID + "/" + userID
	return nil
}
func (*routerGameRequests) RecordView(context.Context, string, string) error { return nil }

func (r *routerGameRequests) Close(_ context.Context, requestID, userID string) error {
	r.closed = requestID + "/" + userID
	return nil
}
func (r *routerGameRequests) Cancel(_ context.Context, requestID, userID string) error {
	r.cancelled = requestID + "/" + userID
	return nil
}

type routerGameRequestQueries struct {
	items       []requestquery.Request
	trending    []requestquery.TrendingGame
	filter      requestquery.Filter
	activeTotal int64
	listErr     error
}

type routerRealtime struct{ events <-chan gamerequest.Event }

func (r *routerRealtime) Subscribe() (<-chan gamerequest.Event, func()) {
	return r.events, func() {}
}

func (r *routerRealtime) Subscribers() int { return 1 }

func (r *routerGameRequestQueries) ListActive(_ context.Context, _ string, filter requestquery.Filter) ([]requestquery.Request, error) {
	r.filter = filter
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.items, nil
}
func (r *routerGameRequestQueries) ByID(_ context.Context, id, _ string) (requestquery.Request, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return requestquery.Request{}, gamerequest.ErrNotFound
}
func (r *routerGameRequestQueries) Trending(context.Context) ([]requestquery.TrendingGame, error) {
	return r.trending, nil
}
func (r *routerGameRequestQueries) CountActive(context.Context) (int64, error) {
	if r.activeTotal != 0 {
		return r.activeTotal, nil
	}
	return int64(len(r.items)), nil
}

func (r *routerDiscovery) Browse(_ context.Context, _, search string) (game.DiscoveryPage, error) {
	r.search = search
	return game.DiscoveryPage{Items: r.items}, nil
}
func (r *routerDiscovery) BrowseFollowed(_ context.Context, _, search string) (game.DiscoveryPage, error) {
	r.search = search
	r.followedOnly = true
	return game.DiscoveryPage{Items: r.mine}, nil
}
func (r *routerDiscovery) FollowLocal(_ context.Context, userID, gameID string) error {
	r.followed = userID + "/" + gameID
	return nil
}
func (r *routerDiscovery) Follow(_ context.Context, userID string, ref game.ProviderReference) (game.Game, error) {
	r.followProvided = userID + "/" + ref.Provider + "/" + ref.ExternalID
	if r.followErr != nil {
		return game.Game{}, r.followErr
	}
	return r.followResult, nil
}
func (r *routerDiscovery) Unfollow(_ context.Context, userID, gameID string) error {
	r.unfollowed = userID + "/" + gameID
	return nil
}
func (r *routerDiscovery) Select(_ context.Context, userID string, selection game.GameSelection) (game.DiscoveryItem, error) {
	r.selectUserID = userID
	r.selectSelection = selection
	if r.selectErr != nil {
		return game.DiscoveryItem{}, r.selectErr
	}
	return r.selectItem, nil
}

func (*routerAuthentication) Available() bool { return true }

func (*routerAuthentication) Providers() []auth.ProviderAvailability {
	return []auth.ProviderAvailability{{Kind: "oidc", DisplayName: "CXT account"}, {Kind: "discord", DisplayName: "Discord"}}
}

func (a *routerAuthentication) Begin(_ context.Context, input auth.OAuthStartInput) (auth.OAuthStart, error) {
	a.began = input
	return a.start, nil
}

func (a *routerAuthentication) Complete(_ context.Context, completion auth.OAuthCompletion) (auth.OAuthResult, error) {
	a.completed = completion
	return a.result, a.completeErr
}

func (*routerAuthentication) Logout(context.Context, string) error { return nil }

func (a *routerAuthentication) Authenticate(context.Context, string) (user.User, error) {
	return a.account, nil
}

// A second Join that lands after the first already succeeded must resolve into
// a dismissible notice, leaving the row and the rest of the page usable.
func TestDuplicateJoinReportsRecoverableConflict(t *testing.T) {
	commands := &routerGameRequests{joinError: gamerequest.ErrAlreadyMember}
	queries := &routerGameRequestQueries{items: []requestquery.Request{{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "user-2",
		CreatorGameNick: "Other", MemberCount: 2, ViewerJoined: true, Status: gamerequest.StatusOpen,
		StartsAt: time.Now().Add(time.Hour),
	}}}
	router := gameRequestTestRouter(commands, queries, nil)

	duplicate := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", strings.NewReader(url.Values{"return_to": {"list"}}.Encode()))
	duplicate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	duplicate.Header.Set("HX-Request", "true")
	duplicate.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(duplicate)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, duplicate)

	if response.Code != http.StatusOK {
		t.Fatalf("duplicate join status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("HX-Retarget"); got != "#live-notices" {
		t.Errorf("HX-Retarget = %q, want %q", got, "#live-notices")
	}
	if got := response.Header().Get("HX-Reswap"); got != "beforeend" {
		t.Errorf("HX-Reswap = %q, want %q", got, "beforeend")
	}
	body := response.Body.String()
	if !strings.Contains(body, "You have already joined this Game Request.") {
		t.Errorf("duplicate join body does not explain the conflict: %s", body)
	}
	if !strings.Contains(body, "data-dismiss-live-toast") {
		t.Errorf("duplicate join notice cannot be dismissed: %s", body)
	}
}

// The instance label is the minimum instance identity, not a second wordmark.
// The component renders it only for a configured name, but NewRouter used to
// resolve an unset LANSURFER_INSTANCE_NAME to the brand name before handing it
// to the middleware, so every signed-in screen rendered "LANsurfer" twice by
// default. The condition is only observable through the router, which is why
// this test lives here rather than beside the component.
func TestInstanceLabelRendersOnlyForAConfiguredInstanceName(t *testing.T) {
	tests := map[string]struct {
		configured string
		wantLabel  bool
	}{
		"unset":                        {configured: "", wantLabel: false},
		"configured":                   {configured: "Camp Nova", wantLabel: true},
		"configured as the brand name": {configured: "LANsurfer", wantLabel: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			viewer := &routerAuthentication{account: user.User{ID: "user-1", GameNick: "Ada Player", Role: user.RoleUser}}
			router := NewRouter(RouterDependencies{
				Ready:          func(context.Context) error { return nil },
				Authentication: viewer,
				Sessions:       viewer,
				GameRequests:   &routerGameRequests{},
				RequestQueries: &routerGameRequestQueries{},
				Discovery:      &routerDiscovery{},
				InstanceName:   test.configured,
			})

			request := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
			request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			body := response.Body.String()
			if got := strings.Contains(body, "data-instance-name"); got != test.wantLabel {
				t.Errorf("instance label rendered = %v, want %v", got, test.wantLabel)
			}
			if test.wantLabel && !strings.Contains(body, test.configured) {
				t.Errorf("configured instance name %q is missing from the page", test.configured)
			}
			// The title keeps its fallback either way, so an unconfigured
			// instance still reads as "<page> · LANsurfer".
			if !strings.Contains(body, "<title>Game Requests · ") {
				t.Errorf("page title is missing its instance name: %s", body)
			}
		})
	}
}

type routerCovers struct {
	image []byte
	err   error
}

func (c routerCovers) Get(ctx context.Context, _, _ string) ([]byte, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.image, ctx.Err()
}

func TestIGDBCoverRoute(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}
	tests := []struct {
		name         string
		covers       routerCovers
		status       int
		cacheControl string
	}{
		{"hit", routerCovers{image: jpeg}, http.StatusOK, "public, max-age=31536000, immutable"},
		{"not found", routerCovers{err: mediaproxy.ErrNotFound}, http.StatusNotFound, "no-store"},
		{"busy", routerCovers{err: mediaproxy.ErrBusy}, http.StatusServiceUnavailable, "no-store"},
		{"internal", routerCovers{err: errors.New("db down")}, http.StatusInternalServerError, "no-store"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			// No session cookie: the route is public.
			NewRouter(RouterDependencies{Covers: test.covers}).
				ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media/igdb/big/co1abc", nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, test.cacheControl)
			}
			if test.status == http.StatusOK && response.Header().Get("Content-Type") != "image/jpeg" {
				t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if test.status == http.StatusServiceUnavailable && response.Header().Get("Retry-After") != "1" {
				t.Errorf("Retry-After = %q, want 1", response.Header().Get("Retry-After"))
			}
		})
	}
}

type routerAvatars struct {
	bucket, id string
}

func (a *routerAvatars) Get(_ context.Context, bucket, id string) ([]byte, error) {
	a.bucket, a.id = bucket, id
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, nil
}

// Avatars go through the same public same-origin proxy contract as covers,
// always in the avatar bucket regardless of the requested path.
func TestAvatarRouteIsPublicAndUsesAvatarBucket(t *testing.T) {
	avatars := &routerAvatars{}
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Avatars: avatars}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media/avatar/0123456789abcdef0123456789abcdef", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status/headers = %d %v", response.Code, response.Header())
	}
	if avatars.bucket != "avatar" || avatars.id != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("Get(%q, %q)", avatars.bucket, avatars.id)
	}
}

func TestIGDBCoverRouteWritesNothingForCanceledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Covers: routerCovers{image: []byte("x")}}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/media/igdb/big/co1abc", nil).WithContext(ctx))
	if response.Body.Len() != 0 {
		t.Errorf("canceled caller received %d body bytes", response.Body.Len())
	}
}

// The Discord Developer Portal needs stable public Terms and Privacy URLs, so
// both pages must render for an anonymous visitor with the operator's details.
func TestLegalPagesArePublicAndShowOperatorIdentity(t *testing.T) {
	authentication := &routerAuthentication{}
	router := NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication,
		Legal: webmiddleware.LegalIdentity{
			EntityName:   "Computerfreunde Karlsdorf-Neuthard e.V.",
			Address:      "Example Street 1, 76689 Karlsdorf-Neuthard, Germany",
			ContactEmail: "datenschutz@convention-x-treme.de",
			ImprintURL:   "https://www.convention-x-treme.de/impressum",
		},
	})
	for path, wants := range map[string][]string{
		"/privacy": {"Privacy Policy", "Last updated", "Core-Backbone GmbH", "30 days", "Discord"},
		"/terms":   {"Terms of Service", "Last updated", `id="contact"`, `href="https://github.com/convention-x-treme/lansurfer/issues" target="_blank" rel="noopener noreferrer"`},
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			body := response.Body.String()
			for _, want := range append(wants,
				"Computerfreunde Karlsdorf-Neuthard e.V.",
				"Example Street 1, 76689 Karlsdorf-Neuthard, Germany",
				`href="mailto:datenschutz@convention-x-treme.de"`,
				`href="/auth/login"`,
			) {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q", want)
				}
			}
		})
	}
}

func TestLegalPagesMarkUnconfiguredIdentity(t *testing.T) {
	authentication := &routerAuthentication{}
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/privacy", nil))

	body := response.Body.String()
	if !strings.Contains(body, "not configured") {
		t.Error("privacy page does not mark missing operator details as not configured")
	}
	if strings.Contains(body, "mailto:") {
		t.Error("privacy page renders a mailto link without a configured contact email")
	}
}

func TestLegalPagesUseAppShellForSignedInPlayer(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "u1", GameNick: "Ada Player"}}
	request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "data-authenticated-shell") || strings.Contains(body, `href="/auth/login"`) {
		t.Error("signed-in player did not get the application shell on /privacy")
	}
}

// Onboarding shows the footer, so a player with a session but no Game Nick
// yet can open the legal pages. They must not get the application shell,
// whose links all bounce back to onboarding.
func TestLegalPagesTreatPlayerWithoutGameNickAsAnonymous(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "u1"}}
	request := httptest.NewRequest(http.MethodGet, "/terms", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "data-authenticated-shell") {
		t.Error("player without a Game Nick got the application shell on /terms")
	}
}

// The policy must describe everything the request log and session store keep.
func TestPrivacyPolicyDescribesLoggedFieldsAndSessionRetention(t *testing.T) {
	authentication := &routerAuthentication{}
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/privacy", nil))

	body := response.Body.String()
	for _, want := range []string{"response status", "response size", "duration", "Session records remain stored"} {
		if !strings.Contains(body, want) {
			t.Errorf("privacy policy does not mention %q", want)
		}
	}
}
