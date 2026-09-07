package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/auth"
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

func (*routerAuthentication) Available() bool { return true }

func (*routerAuthentication) BeginLogin() (auth.LoginFlow, error) { return auth.LoginFlow{}, nil }

func (*routerAuthentication) CompleteLogin(context.Context, string, string, string) (user.User, auth.BrowserSession, error) {
	return user.User{}, auth.BrowserSession{}, nil
}

func (*routerAuthentication) Logout(context.Context, string) error { return nil }

func (a *routerAuthentication) Authenticate(context.Context, string) (user.User, error) {
	return a.account, nil
}
