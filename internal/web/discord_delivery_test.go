package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

type discordSetupStub struct {
	starts, consumes                           int
	startState                                 string
	startAdmin, startBinding                   string
	consumeAdmin, consumeBinding, consumeState string
	consumeErr                                 error
}

func (s *discordSetupStub) Start(_ context.Context, admin, binding string) (string, error) {
	s.starts++
	s.startAdmin, s.startBinding = admin, binding
	return s.startState, nil
}
func (s *discordSetupStub) Consume(_ context.Context, admin, binding, state string) (notification.DiscordSetupState, error) {
	s.consumes++
	s.consumeAdmin, s.consumeBinding, s.consumeState = admin, binding, state
	if s.consumes > 1 && s.consumeErr == nil {
		return notification.DiscordSetupState{}, notification.ErrDiscordSetupStateInvalid
	}
	return notification.DiscordSetupState{}, s.consumeErr
}

type discordAuthorizationStub struct{ url string }

func (s discordAuthorizationStub) AuthorizationURL(state string) string {
	return s.url + "?state=" + url.QueryEscape(state)
}

type discordConnectStub struct {
	calls int
	err   error
}

func (s *discordConnectStub) Connect(context.Context, string) error {
	s.calls++
	return s.err
}

type discordLifecycleStub struct {
	calls       int
	destination notification.DiscordDestination
}

func (s *discordLifecycleStub) Disconnect(context.Context) error {
	s.calls++
	s.destination = notification.DiscordDestination{}
	return nil
}

type discordDestinationStatusStub struct {
	destination notification.DiscordDestination
	err         error
}

func (s *discordDestinationStatusStub) GetDiscordDestinationStatus(context.Context) (notification.DiscordDestination, error) {
	return s.destination, s.err
}

func discordRouter(setup *discordSetupStub, connect *discordConnectStub, lifecycle *discordLifecycleStub, auth *routerAuthentication) http.Handler {
	return NewRouter(RouterDependencies{
		Authentication: auth, Sessions: auth,
		DiscordSetup: setup, DiscordAuthorization: discordAuthorizationStub{url: "https://discord.example/authorize"},
		DiscordConnector: connect, DiscordLifecycle: lifecycle,
		DiscordStatus: &discordDestinationStatusStub{err: notification.ErrDiscordDestinationNotFound},
	})
}

func discordAdminRequest(method, target string, admin *routerAuthentication, csrf bool) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	request.AddCookie(&http.Cookie{Name: "lansurfer_auth_binding", Value: "browser-binding"})
	if csrf {
		addCSRF(request)
	}
	return request
}

func TestDiscordConnectStartRequiresAdminAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name              string
		user              user.User
		withSession, csrf bool
		want              int
	}{
		{name: "admin", user: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}, withSession: true, csrf: true, want: http.StatusOK},
		{name: "non-admin", user: user.User{ID: "user-1", GameNick: "Player", Role: user.RoleUser}, withSession: true, csrf: true, want: http.StatusForbidden},
		{name: "unauthenticated", want: http.StatusSeeOther},
		{name: "csrf rejected", user: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}, withSession: true, want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := &discordSetupStub{startState: "setup-state"}
			account := &routerAuthentication{account: tc.user}
			request := httptest.NewRequest(http.MethodPost, "/admin/discord/connect", nil)
			if tc.withSession {
				request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
			}
			if tc.name != "csrf rejected" {
				addCSRF(request)
			}
			response := httptest.NewRecorder()
			discordRouter(setup, &discordConnectStub{}, &discordLifecycleStub{}, account).ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
			if tc.name == "admin" {
				if setup.starts != 1 || response.Header().Get("Location") != "" {
					t.Fatalf("start calls/location = %d/%q", setup.starts, response.Header().Get("Location"))
				}
				body := response.Body.String()
				if !strings.Contains(body, `content="0;url=https://discord.example/authorize?state=setup-state"`) ||
					!strings.Contains(body, `href="https://discord.example/authorize?state=setup-state"`) ||
					!strings.Contains(response.Header().Get("Content-Security-Policy"), "form-action 'self'") {
					t.Fatalf("missing OAuth bridge or CSP: body=%q CSP=%q", body, response.Header().Get("Content-Security-Policy"))
				}
				if setup.startAdmin != "admin-1" || setup.startBinding == "" {
					t.Fatalf("setup binding = admin %q, browser %q", setup.startAdmin, setup.startBinding)
				}
				if len(response.Result().Cookies()) == 0 {
					t.Fatal("missing CSRF or browser-binding cookie")
				}
			} else if setup.starts != 0 {
				t.Fatalf("setup started %d times for rejected request", setup.starts)
			}
		})
	}
}

func TestDiscordReconnectStartKeepsCurrentDestination(t *testing.T) {
	current := notification.DiscordDestination{State: notification.DiscordDestinationConnected, GuildID: "guild-1"}
	lifecycle := &discordLifecycleStub{destination: current}
	setup := &discordSetupStub{startState: "state"}
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	request := discordAdminRequest(http.MethodPost, "/admin/discord/reconnect", account, true)
	response := httptest.NewRecorder()
	discordRouter(setup, &discordConnectStub{}, lifecycle, account).ServeHTTP(response, request)
	if response.Code != http.StatusOK || setup.starts != 1 || lifecycle.calls != 0 || lifecycle.destination != current {
		t.Fatalf("status/setup/lifecycle/destination = %d/%d/%d/%#v", response.Code, setup.starts, lifecycle.calls, lifecycle.destination)
	}
	if !strings.Contains(response.Body.String(), `href="https://discord.example/authorize?state=state"`) ||
		response.Header().Get("Location") != "" {
		t.Fatalf("reconnect bridge missing or external redirect: location=%q body=%q", response.Header().Get("Location"), response.Body.String())
	}
}

func TestDiscordCallbackConsumesStateBeforeOAuthExchange(t *testing.T) {
	for _, tc := range []struct {
		name        string
		consumeErr  error
		connectErr  error
		wantStatus  int
		wantConnect int
	}{
		{name: "success", wantStatus: http.StatusSeeOther, wantConnect: 1},
		{name: "invalid state", consumeErr: notification.ErrDiscordSetupStateInvalid, wantStatus: http.StatusBadRequest},
		{name: "oauth exchange failure", connectErr: errors.New("exchange failed"), wantStatus: http.StatusSeeOther, wantConnect: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := &discordSetupStub{consumeErr: tc.consumeErr}
			connect := &discordConnectStub{err: tc.connectErr}
			account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
			request := discordAdminRequest(http.MethodGet, "/admin/discord/callback?code=oauth-code&state=setup-state", account, false)
			response := httptest.NewRecorder()
			discordRouter(setup, connect, &discordLifecycleStub{}, account).ServeHTTP(response, request)
			if tc.name == "success" && (setup.consumeAdmin != "admin-1" || setup.consumeBinding != "browser-binding" || setup.consumeState != "setup-state") {
				t.Fatalf("consumed state binding = %#v", setup)
			}
			if response.Code != tc.wantStatus || setup.consumes != 1 || connect.calls != tc.wantConnect {
				t.Fatalf("status/consume/connect = %d/%d/%d", response.Code, setup.consumes, connect.calls)
			}
		})
	}
}

func TestDiscordCallbackReplayDoesNotReconnect(t *testing.T) {
	setup := &discordSetupStub{}
	connect := &discordConnectStub{}
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	router := discordRouter(setup, connect, &discordLifecycleStub{}, account)
	for i := range 2 {
		request := discordAdminRequest(http.MethodGet, "/admin/discord/callback?code=oauth-code&state=setup-state", account, false)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusSeeOther
		if i == 1 {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("callback %d status = %d, want %d", i+1, response.Code, want)
		}
	}
	if setup.consumes != 2 || connect.calls != 1 {
		t.Fatalf("consume/connect calls = %d/%d", setup.consumes, connect.calls)
	}
}

func TestDiscordCallbackCancelRedirectsWithNoticeWithoutConnecting(t *testing.T) {
	for target, want := range map[string]string{
		"/admin/discord/callback?error=access_denied&state=setup-state":                    "/admin/discord?notice=cancelled",
		"/admin/discord/callback?error=server_error&error_description=x&state=setup-state": "/admin/discord?notice=failed",
	} {
		setup := &discordSetupStub{}
		connect := &discordConnectStub{}
		account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
		response := httptest.NewRecorder()
		discordRouter(setup, connect, &discordLifecycleStub{}, account).ServeHTTP(response, discordAdminRequest(http.MethodGet, target, account, false))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != want || setup.consumes != 1 || connect.calls != 0 {
			t.Fatalf("%s: status/location/consume/connect = %d/%q/%d/%d", target, response.Code, response.Header().Get("Location"), setup.consumes, connect.calls)
		}
	}
}
func TestDiscordAdminPageRendersNotConfiguredAndKeepsLoginSeparate(t *testing.T) {
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	router := discordRouter(&discordSetupStub{}, &discordConnectStub{}, &discordLifecycleStub{}, account)

	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if loginResponse.Code != http.StatusOK || !strings.Contains(loginResponse.Body.String(), "Discord") {
		t.Fatalf("Discord login UI status/body = %d/%s", loginResponse.Code, loginResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/discord", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "Not configured") ||
		!strings.Contains(body, "Discord delivery") || !strings.Contains(body, "Discord login stays separate.") ||
		!strings.Contains(body, `href="/admin/discord"`) || !strings.Contains(body, `action="/admin/discord/connect"`) {
		t.Fatalf("Discord delivery page status/body = %d/%s", response.Code, body)
	}
}

func TestDiscordDisconnectUsesLifecycleAndRequiresCSRF(t *testing.T) {
	for _, csrf := range []bool{false, true} {
		lifecycle := &discordLifecycleStub{destination: notification.DiscordDestination{State: notification.DiscordDestinationConnected}}
		account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
		request := discordAdminRequest(http.MethodPost, "/admin/discord/disconnect", account, csrf)
		response := httptest.NewRecorder()
		discordRouter(&discordSetupStub{}, &discordConnectStub{}, lifecycle, account).ServeHTTP(response, request)
		want, wantCalls := http.StatusForbidden, 0
		if csrf {
			want, wantCalls = http.StatusSeeOther, 1
		}
		if response.Code != want || lifecycle.calls != wantCalls {
			t.Fatalf("csrf=%t status/calls = %d/%d", csrf, response.Code, lifecycle.calls)
		}
	}
}
func TestDiscordRoutesDoNotCallDiscordAuthenticationServices(t *testing.T) {
	setup := &discordSetupStub{startState: "state"}
	connect := &discordConnectStub{}
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	router := discordRouter(setup, connect, &discordLifecycleStub{}, account)
	for _, target := range []string{"/admin/discord/connect", "/admin/discord/callback?code=code&state=state"} {
		method := http.MethodPost
		if strings.Contains(target, "callback") {
			method = http.MethodGet
		}
		request := discordAdminRequest(method, target, account, method == http.MethodPost)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
	}
	if account.began.Operation != "" || account.completed.State != "" {
		t.Fatalf("Discord auth flow was used: begin=%#v complete=%#v", account.began, account.completed)
	}
}

func TestDiscordOAuthCallbackWithoutBindingRejected(t *testing.T) {
	setup := &discordSetupStub{}
	connect := &discordConnectStub{}
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/admin/discord/callback?code=code&state=state", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	discordRouter(setup, connect, &discordLifecycleStub{}, account).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || setup.consumes != 0 || connect.calls != 0 {
		t.Fatalf("status/consume/connect = %d/%d/%d", response.Code, setup.consumes, connect.calls)
	}
}

func TestDiscordCallbackDoesNotEchoOAuthCodeOnFailure(t *testing.T) {
	setup := &discordSetupStub{}
	connect := &discordConnectStub{err: errors.New("sensitive provider detail")}
	account := &routerAuthentication{account: user.User{ID: "admin-1", GameNick: "Admin", Role: user.RoleAdmin}}
	request := discordAdminRequest(http.MethodGet, "/admin/discord/callback?code="+url.QueryEscape("secret-code")+"&state=state", account, false)
	response := httptest.NewRecorder()
	discordRouter(setup, connect, &discordLifecycleStub{}, account).ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), "secret-code") || strings.Contains(response.Body.String(), "sensitive provider detail") {
		t.Fatal("callback response leaked OAuth input or provider error")
	}
}
