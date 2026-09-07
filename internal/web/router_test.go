package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/event"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

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

func TestHomeRendersFrontendFoundation(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

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
		`href="/assets/app.css"`,
		`src="/assets/app.js"`,
		`hx-get="/ui/patterns/empty"`,
		`data-theme-toggle`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	if strings.Contains(string(body), "https://") {
		t.Error("home page unexpectedly contains a runtime external dependency")
	}
}

func TestHomeRendersAuthenticatedViewer(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{DisplayName: "Ada Player", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	for _, want := range []string{"Ada Player", "admin", `action="/auth/logout"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestPatternFragments(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/ui/patterns/empty", want: "No Game Requests yet"},
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
		"/assets/fonts/montserrat-latin-wght-normal.woff2",
		"/assets/fonts/jetbrains-mono-latin-wght-normal.woff2",
		"/assets/images/otter.png",
		"/assets/licenses/htmx-BSD-2-Clause.txt",
		"/assets/licenses/montserrat-OFL-1.1.txt",
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
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{DisplayName: "Player", Role: user.RoleUser}},
		Events:         &routerEvents{}, Games: &routerGames{},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAdminCanCreateEvent(t *testing.T) {
	events := &routerEvents{}
	router := adminTestRouter(events, &routerGames{})
	form := url.Values{
		"name": {"CXT 2027"}, "starts_at": {"2027-05-01T10:00"}, "ends_at": {"2027-05-03T10:00"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/events", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/events?notice=event-created" {
		t.Fatalf("response = %d %q", response.Code, response.Header().Get("Location"))
	}
	if events.created.Name != "CXT 2027" || events.created.StartsAt.Location() != time.UTC {
		t.Fatalf("created event = %#v", events.created)
	}
}

func TestAdminGameSearchRendersAliasMatch(t *testing.T) {
	games := &routerGames{items: []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true, Aliases: []game.Alias{{ID: "alias-1", Value: "WC3"}}}}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games?q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(&routerEvents{}, games).ServeHTTP(response, request)

	if response.Code != http.StatusOK || games.search != "wc3" {
		t.Fatalf("response status = %d, search = %q", response.Code, games.search)
	}
	for _, want := range []string{"Warcraft III", "WC3", "Deactivate game"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
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

type routerAuthentication struct {
	account user.User
}

func adminTestRouter(events *routerEvents, games *routerGames) http.Handler {
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{DisplayName: "Ada Admin", Role: user.RoleAdmin}},
		Events:         events,
		Games:          games,
	})
}

type routerEvents struct{ created event.Event }

func (*routerEvents) List(context.Context) ([]event.Event, error) { return nil, nil }
func (r *routerEvents) Create(_ context.Context, name string, startsAt, endsAt time.Time) (event.Event, error) {
	r.created = event.Event{Name: name, StartsAt: startsAt, EndsAt: endsAt}
	return r.created, nil
}
func (*routerEvents) Update(context.Context, string, string, time.Time, time.Time) (event.Event, error) {
	return event.Event{}, nil
}
func (*routerEvents) SetCurrent(context.Context, string) error { return nil }

type routerGames struct {
	items  []game.Game
	search string
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
func (*routerGames) AddAlias(context.Context, string, string) (game.Alias, error) {
	return game.Alias{}, nil
}
func (*routerGames) RemoveAlias(context.Context, string, string) error { return nil }

func (*routerAuthentication) Available() bool { return true }

func (*routerAuthentication) BeginLogin() (auth.LoginFlow, error) { return auth.LoginFlow{}, nil }

func (*routerAuthentication) CompleteLogin(context.Context, string, string, string) (user.User, auth.BrowserSession, error) {
	return user.User{}, auth.BrowserSession{}, nil
}

func (*routerAuthentication) Logout(context.Context, string) error { return nil }

func (a *routerAuthentication) Authenticate(context.Context, string) (user.User, error) {
	return a.account, nil
}
