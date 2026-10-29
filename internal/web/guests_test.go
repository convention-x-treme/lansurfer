package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

type guestBackend struct {
	enabled     bool
	showOnLogin bool
	setErr      error
	stateErr    error
	invite      string
	registered  []string
	registerErr error
	guests      []user.User
	deleted     [][]string
	deleteAll   int
	deleteErr   error
	mutations   int
}

func (b *guestBackend) State(context.Context) (auth.GuestAccessState, error) {
	return auth.GuestAccessState{Enabled: b.enabled, ShowOnLogin: b.showOnLogin, Invite: b.invite}, b.stateErr
}
func (b *guestBackend) SetEnabled(_ context.Context, enabled bool) error {
	b.mutations++
	b.enabled = enabled
	return nil
}
func (b *guestBackend) SetShowOnLogin(_ context.Context, show bool) error {
	b.mutations++
	if b.setErr != nil {
		return b.setErr
	}
	b.showOnLogin = show
	return nil
}
func (b *guestBackend) Rotate(context.Context) error { b.mutations++; return nil }
func (b *guestBackend) Valid(_ context.Context, invite string) (bool, error) {
	return b.enabled && invite == b.invite, nil
}
func (b *guestBackend) Register(_ context.Context, invite, nick string) (user.User, auth.BrowserSession, error) {
	b.registered = append(b.registered, nick)
	if b.registerErr != nil {
		return user.User{}, auth.BrowserSession{}, b.registerErr
	}
	return user.User{ID: "guest-1", GameNick: nick}, auth.BrowserSession{Token: "guest-session", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (b *guestBackend) ListGuests(context.Context) ([]user.User, error) { return b.guests, nil }
func (b *guestBackend) DeleteGuests(_ context.Context, ids []string) ([]string, error) {
	b.mutations++
	b.deleted = append(b.deleted, ids)
	return ids, b.deleteErr
}
func (b *guestBackend) DeleteAllGuests(context.Context) ([]string, error) {
	b.mutations++
	b.deleteAll++
	return nil, b.deleteErr
}

const testInvite = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopq"

func guestRouter(backend *guestBackend, account *user.User, demo bool) http.Handler {
	dependencies := RouterDependencies{
		GuestInvites: backend, GuestRegistration: backend, GuestQueries: backend, GuestDeletion: backend,
		BaseURL: "https://lan.example",
	}
	if account != nil {
		viewer := &routerAuthentication{account: *account}
		dependencies.Authentication, dependencies.Sessions = viewer, viewer
	} else {
		viewer := &routerAuthentication{}
		dependencies.Authentication, dependencies.Sessions = viewer, viewer
	}
	if demo {
		dependencies.Demo = &webmiddleware.DemoSchedule{Hour: 4, Location: time.UTC}
	}
	return NewRouter(dependencies)
}

func guestRequest(router http.Handler, method, target string, body url.Values, signedIn, csrf bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if signedIn {
		request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	}
	if csrf {
		addCSRF(request)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestGuestSignupHTTP(t *testing.T) {
	backend := &guestBackend{enabled: true, invite: testInvite}
	router := guestRouter(backend, nil, false)
	join := url.Values{"invite": {testInvite}, "game_nick": {"Night Otter"}}

	if page := guestRequest(router, http.MethodGet, "/auth/guest?invite="+testInvite, nil, false, false); page.Code != http.StatusOK ||
		!strings.Contains(page.Body.String(), `name="invite"`) || len(backend.registered) != 0 {
		t.Fatalf("GET valid = %d, registered %v", page.Code, backend.registered)
	}
	for _, target := range []string{"/auth/guest", "/auth/guest?invite=wrong"} {
		if page := guestRequest(router, http.MethodGet, target, nil, false, false); page.Code != http.StatusNotFound ||
			!strings.Contains(page.Body.String(), "This guest invite is invalid or no longer available.") {
			t.Fatalf("GET %s = %d", target, page.Code)
		}
	}
	if page := guestRequest(router, http.MethodPost, "/auth/guest", join, false, false); page.Code != http.StatusForbidden || len(backend.registered) != 0 {
		t.Fatalf("POST without CSRF = %d", page.Code)
	}

	response := guestRequest(router, http.MethodPost, "/auth/guest", join, false, true)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/welcome" {
		t.Fatalf("POST valid = %d %q", response.Code, response.Header().Get("Location"))
	}
	var session *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == webmiddleware.SessionCookieName {
			session = cookie
		}
	}
	if session == nil || session.Value != "guest-session" || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.MaxAge <= 0 && session.Expires.IsZero() {
		t.Fatalf("session cookie = %#v", session)
	}

	backend.registerErr = user.ErrGameNickTaken
	taken := guestRequest(router, http.MethodPost, "/auth/guest", join, false, true)
	if taken.Code != http.StatusUnprocessableEntity || !strings.Contains(taken.Body.String(), "Night Otter") || !strings.Contains(taken.Body.String(), testInvite) {
		t.Fatalf("taken nick = %d", taken.Code)
	}
	backend.registerErr = auth.ErrGuestInviteInvalid
	if stale := guestRequest(router, http.MethodPost, "/auth/guest", join, false, true); stale.Code != http.StatusNotFound {
		t.Fatalf("stale invite = %d", stale.Code)
	}
	backend.registerErr = errors.New("database down")
	failed := guestRequest(router, http.MethodPost, "/auth/guest", join, false, true)
	sessionIssued := false
	for _, cookie := range failed.Result().Cookies() {
		sessionIssued = sessionIssued || cookie.Name == webmiddleware.SessionCookieName
	}
	if failed.Code != http.StatusServiceUnavailable || sessionIssued {
		t.Fatalf("failed creation = %d, cookies %v", failed.Code, failed.Header().Values("Set-Cookie"))
	}
}

func TestGuestSignupDoesNotSwitchSignedInAccounts(t *testing.T) {
	backend := &guestBackend{enabled: true, invite: testInvite}
	player := user.User{ID: "player", GameNick: "Player", Role: user.RoleUser}
	router := guestRouter(backend, &player, false)
	for _, response := range []*httptest.ResponseRecorder{
		guestRequest(router, http.MethodGet, "/auth/guest?invite="+testInvite, nil, true, false),
		guestRequest(router, http.MethodPost, "/auth/guest", url.Values{"invite": {testInvite}, "game_nick": {"Other"}}, true, true),
	} {
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
			t.Fatalf("signed-in guest route = %d %q", response.Code, response.Header().Get("Location"))
		}
	}
	if len(backend.registered) != 0 {
		t.Fatal("a signed-in browser created a guest")
	}
}

func TestGuestManagementIsAdminOnly(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/admin/guests"},
		{http.MethodPost, "/admin/guests/access"},
		{http.MethodPost, "/admin/guests/login-option"},
		{http.MethodPost, "/admin/guests/invite/rotate"},
		{http.MethodPost, "/admin/guests/11111111-1111-1111-1111-111111111111/delete"},
		{http.MethodPost, "/admin/guests/delete-selected"},
		{http.MethodPost, "/admin/guests/delete-all"},
	}
	player := user.User{ID: "player", GameNick: "Player", Role: user.RoleUser}
	for name, account := range map[string]*user.User{"anonymous": nil, "player": &player} {
		backend := &guestBackend{enabled: true, invite: testInvite}
		router := guestRouter(backend, account, false)
		for _, route := range routes {
			body := url.Values{"enabled": {"false"}, "confirm_delete": {"true"}, "user_id": {"x"}}
			if response := guestRequest(router, route.method, route.path, body, account != nil, true); response.Code < 300 || response.Code == http.StatusOK && route.method == http.MethodGet {
				t.Errorf("%s %s %s = %d", name, route.method, route.path, response.Code)
			}
		}
		if backend.mutations != 0 {
			t.Errorf("%s reached a mutation", name)
		}
	}
}

func TestGuestLoginOptionAvailability(t *testing.T) {
	for _, tc := range []struct {
		enabled, show bool
		invite        string
		fail          bool
		want          bool
	}{
		{false, true, testInvite, false, false}, {true, false, testInvite, false, false},
		{true, true, "", false, false}, {true, true, testInvite, true, false},
		{true, true, testInvite, false, true},
	} {
		backend := &guestBackend{enabled: tc.enabled, showOnLogin: tc.show, invite: tc.invite}
		if tc.fail {
			backend.stateErr = errors.New("unavailable")
		}
		response := guestRequest(guestRouter(backend, nil, false), http.MethodGet, "/auth/login", nil, false, false)
		body := response.Body.String()
		if response.Code != http.StatusOK || strings.Contains(body, "/auth/guest/start") != tc.want || strings.Contains(body, testInvite) {
			t.Fatalf("state %+v: status %d", tc, response.Code)
		}
		if tc.want && (strings.Contains(body, "Login is not configured") || !strings.Contains(body, "Terms of Service")) {
			t.Fatal("guest-only login lacks legal notice or shows warning")
		}
	}
	player := user.User{ID: "player", GameNick: "Player", Role: user.RoleUser}
	backend := &guestBackend{enabled: true, showOnLogin: true, invite: testInvite}
	page := guestRequest(guestRouter(backend, &player, false), http.MethodGet, "/auth/login", nil, true, false)
	if strings.Contains(page.Body.String(), "/auth/guest/start") {
		t.Fatal("signed-in viewer sees guest entry")
	}
	page = guestRequest(NewRouter(RouterDependencies{GuestInvites: backend}), http.MethodGet, "/auth/login", nil, false, false)
	if strings.Contains(page.Body.String(), "/auth/guest/start") {
		t.Fatal("guest entry visible without all dependencies")
	}
}

func TestGuestLoginStartUsesCurrentInvite(t *testing.T) {
	backend := &guestBackend{enabled: true, showOnLogin: true, invite: "a+b /"}
	router := guestRouter(backend, nil, false)
	start := func() *httptest.ResponseRecorder {
		return guestRequest(router, http.MethodGet, "/auth/guest/start", nil, false, false)
	}
	if got := start(); got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/auth/guest?invite="+url.QueryEscape(backend.invite) {
		t.Fatalf("start: %d %s", got.Code, got.Header().Get("Location"))
	}
	backend.invite = "new"
	if got := start(); got.Header().Get("Location") != "/auth/guest?invite=new" {
		t.Fatalf("rotated: %d", got.Code)
	}
	backend.showOnLogin = false
	if got := start(); got.Code != http.StatusNotFound {
		t.Fatalf("hidden: %d", got.Code)
	}
	backend.showOnLogin = true
	backend.enabled = false
	if got := start(); got.Code != http.StatusNotFound {
		t.Fatalf("disabled: %d", got.Code)
	}
	backend.enabled = true
	backend.invite = ""
	if got := start(); got.Code != http.StatusNotFound {
		t.Fatalf("empty: %d", got.Code)
	}
	backend.stateErr = errors.New("failed")
	if got := start(); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("error: %d", got.Code)
	}
	player := user.User{ID: "player", GameNick: "Player", Role: user.RoleUser}
	signed := guestRequest(guestRouter(backend, &player, false), http.MethodGet, "/auth/guest/start", nil, true, false)
	if signed.Code != http.StatusSeeOther || signed.Header().Get("Location") != "/account" {
		t.Fatalf("signed in: %d %s", signed.Code, signed.Header().Get("Location"))
	}
	if len(backend.registered) != 0 {
		t.Fatal("start registered an account")
	}
}

func TestGuestLoginOptionManagement(t *testing.T) {
	admin := user.User{ID: "admin", GameNick: "Admin", Role: user.RoleAdmin}
	backend := &guestBackend{}
	router := guestRouter(backend, &admin, false)
	path := "/admin/guests/login-option"
	if got := guestRequest(router, http.MethodPost, path, url.Values{"show_on_login": {"true"}}, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("no CSRF: %d", got.Code)
	}
	if got := guestRequest(router, http.MethodPost, path, url.Values{"show_on_login": {"bad"}}, true, true); got.Code != http.StatusBadRequest {
		t.Fatalf("invalid: %d", got.Code)
	}
	if got := guestRequest(router, http.MethodPost, path, url.Values{"show_on_login": {"true"}}, true, true); got.Code != http.StatusSeeOther || !backend.showOnLogin {
		t.Fatalf("enable: %d", got.Code)
	}
	if got := guestRequest(router, http.MethodPost, path, nil, true, true); got.Code != http.StatusSeeOther || backend.showOnLogin {
		t.Fatalf("disable: %d", got.Code)
	}
	backend.setErr = errors.New("failed")
	if got := guestRequest(router, http.MethodPost, path, url.Values{"show_on_login": {"true"}}, true, true); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("failure: %d", got.Code)
	}
}

func TestGuestManagementActions(t *testing.T) {
	admin := user.User{ID: "admin", GameNick: "Ada Admin", Role: user.RoleAdmin}
	guestID := "11111111-1111-1111-1111-111111111111"
	backend := &guestBackend{enabled: false, invite: testInvite, guests: []user.User{{ID: guestID, GameNick: "Night Otter", Role: user.RoleUser, CreatedAt: time.Now()}}}
	router := guestRouter(backend, &admin, false)

	page := guestRequest(router, http.MethodGet, "/admin/guests", nil, true, false)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "https://lan.example/auth/guest?invite="+testInvite) || !strings.Contains(body, "Night Otter") || !strings.Contains(body, "does not work until") {
		t.Fatalf("management page = %d", page.Code)
	}

	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		return guestRequest(router, http.MethodPost, path, form, true, true)
	}
	if response := post("/admin/guests/access", url.Values{"enabled": {"maybe"}}); response.Code != http.StatusBadRequest {
		t.Errorf("invalid enabled = %d", response.Code)
	}
	if response := post("/admin/guests/access", url.Values{"enabled": {"true"}}); response.Code != http.StatusSeeOther || !backend.enabled {
		t.Errorf("enable = %d, enabled %v", response.Code, backend.enabled)
	}
	for _, path := range []string{"/admin/guests/" + guestID + "/delete", "/admin/guests/delete-selected", "/admin/guests/delete-all"} {
		if response := post(path, url.Values{"user_id": {guestID}}); response.Code != http.StatusBadRequest {
			t.Errorf("%s without confirmation = %d", path, response.Code)
		}
	}
	if response := post("/admin/guests/delete-selected", url.Values{"confirm_delete": {"true"}}); response.Code != http.StatusBadRequest {
		t.Errorf("empty selection = %d", response.Code)
	}
	confirmed := url.Values{"confirm_delete": {"true"}, "user_id": {guestID, guestID}}
	if response := post("/admin/guests/delete-selected", confirmed); response.Code != http.StatusSeeOther || len(backend.deleted) != 1 {
		t.Errorf("delete selected = %d, calls %v", response.Code, backend.deleted)
	}
	if response := post("/admin/guests/"+guestID+"/delete", url.Values{"confirm_delete": {"true"}}); response.Code != http.StatusSeeOther || len(backend.deleted) != 2 || backend.deleted[1][0] != guestID {
		t.Errorf("delete one = %d, calls %v", response.Code, backend.deleted)
	}
	if response := post("/admin/guests/delete-all", url.Values{"confirm_delete": {"true"}}); response.Code != http.StatusSeeOther || backend.deleteAll != 1 {
		t.Errorf("delete all = %d", response.Code)
	}
	backend.deleteErr = user.ErrLastAdmin
	if response := post("/admin/guests/delete-all", url.Values{"confirm_delete": {"true"}}); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Another administrator must remain") {
		t.Errorf("last admin = %d", response.Code)
	}
}

func TestGuestDeletingTheActingAdministratorEndsTheirSession(t *testing.T) {
	admin := user.User{ID: "11111111-1111-1111-1111-111111111111", GameNick: "Ada Admin", Role: user.RoleAdmin}
	backend := &guestBackend{}
	router := guestRouter(backend, &admin, false)
	response := guestRequest(router, http.MethodPost, "/admin/guests/"+admin.ID+"/delete", url.Values{"confirm_delete": {"true"}}, true, true)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth/login" {
		t.Fatalf("self deletion = %d %q", response.Code, response.Header().Get("Location"))
	}
	var expired bool
	for _, cookie := range response.Result().Cookies() {
		expired = expired || cookie.Name == webmiddleware.SessionCookieName && cookie.MaxAge < 0
	}
	if !expired {
		t.Fatal("session cookie not expired")
	}
}

func TestGuestManagementIsReadOnlyInDemoMode(t *testing.T) {
	admin := user.User{ID: "admin", GameNick: "Ada Admin", Role: user.RoleAdmin}
	guestID := "11111111-1111-1111-1111-111111111111"
	backend := &guestBackend{enabled: true, invite: testInvite}
	router := guestRouter(backend, &admin, true)

	for _, path := range []string{
		"/admin/guests/access", "/admin/guests/login-option", "/admin/guests/invite/rotate", "/admin/guests/" + guestID + "/delete",
		"/admin/guests/delete-selected", "/admin/guests/delete-all",
	} {
		form := url.Values{"enabled": {"false"}, "confirm_delete": {"true"}, "user_id": {guestID}}
		response := guestRequest(router, http.MethodPost, path, form, true, true)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/guests?notice=demo-forbidden" {
			t.Errorf("POST %s = %d %q", path, response.Code, response.Header().Get("Location"))
		}
	}
	if backend.mutations != 0 || !backend.enabled {
		t.Fatalf("a blocked action reached the service: %d mutations", backend.mutations)
	}
	htmx := httptest.NewRequest(http.MethodPost, "/admin/guests/login-option", strings.NewReader("show_on_login=true"))
	htmx.Header.Set("HX-Request", "true")
	htmx.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	htmx.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(htmx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, htmx)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("HX-Redirect") != "/admin/guests?notice=demo-forbidden" {
		t.Errorf("HTMX delete-all = %d %q", recorder.Code, recorder.Header().Get("HX-Redirect"))
	}
	if backend.mutations != 0 || backend.showOnLogin {
		t.Fatal("demo HTMX changed preference")
	}

	if page := guestRequest(router, http.MethodGet, "/admin/guests", nil, true, false); page.Code != http.StatusOK {
		t.Errorf("GET management in demo = %d", page.Code)
	}
	if page := guestRequest(router, http.MethodGet, "/auth/guest?invite="+testInvite, nil, false, false); page.Code != http.StatusOK {
		t.Errorf("GET signup in demo = %d", page.Code)
	}
}
