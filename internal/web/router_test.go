package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(func(context.Context) error { return nil }).ServeHTTP(response, request)

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

			NewRouter(test.ready).ServeHTTP(response, request)

			assertResponse(t, response.Result(), test.wantStatus, test.wantBody)
		})
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			response := httptest.NewRecorder()

			NewRouter(func(context.Context) error { return nil }).ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestHomeRendersFrontendFoundation(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	NewRouter(func(context.Context) error { return nil }).ServeHTTP(response, request)

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

			NewRouter(func(context.Context) error { return nil }).ServeHTTP(response, request)

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

			NewRouter(func(context.Context) error { return nil }).ServeHTTP(response, request)

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
