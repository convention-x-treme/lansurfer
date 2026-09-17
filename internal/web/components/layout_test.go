package components

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

// The document must already carry the visitor's theme. Applying it from script
// repaints the page after the deferred bundle runs, which players see as a dark
// flash on every navigation.
func TestLayoutRendersTheVisitorsThemeChoice(t *testing.T) {
	tests := map[string]struct {
		cookie string
		want   string
	}{
		"light choice": {cookie: "lansurfer-light", want: `<html lang="en" data-theme="lansurfer-light">`},
		"dark choice":  {cookie: "lansurfer-dark", want: `<html lang="en" data-theme="lansurfer-dark">`},
		// Without a choice the attribute stays absent so the stylesheet's
		// prefers-color-scheme rules apply; an empty attribute would defeat them.
		"no choice": {want: `<html lang="en">`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var rendered bytes.Buffer
			if err := Layout(branding.Default(), "Game Requests", NavigationState{}).Render(themeContext(test.cookie), &rendered); err != nil {
				t.Fatalf("render Layout: %v", err)
			}
			if body := rendered.String(); !strings.Contains(body, test.want) {
				t.Fatalf("document element = %q, want %q", body[:min(len(body), 200)], test.want)
			}
		})
	}
}

// themeContext returns the request context a visitor with the given theme
// cookie reaches the templates with, or no cookie at all when it is empty.
func themeContext(cookie string) context.Context {
	request := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: webmiddleware.ThemeCookieName, Value: cookie})
	}
	var carried context.Context
	webmiddleware.Theme(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		carried = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), request)
	return carried
}
