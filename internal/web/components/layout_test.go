package components

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/convention-x-treme/lansurfer/internal/web/assets"
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

// instanceContext returns a request context carrying the given resolved
// instance name, the way NewRouter's Instance middleware always does in
// production. An empty name simulates a component test that never ran the
// middleware at all (Layout must fall back to the brand name either way).
func instanceContext(name string) context.Context {
	if name == "" {
		return context.Background()
	}
	var carried context.Context
	webmiddleware.Instance(name)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		carried = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/game-requests", nil))
	return carried
}

func TestLayoutTitleUsesResolvedInstanceName(t *testing.T) {
	var rendered bytes.Buffer
	if err := Layout(branding.Default(), "Game Requests", NavigationState{}).Render(instanceContext("Camp Nova"), &rendered); err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, "<title>Game Requests · Camp Nova</title>") {
		t.Fatalf("title missing resolved instance name, got %q", body[:min(len(body), 400)])
	}
}

func TestLayoutTitleFallsBackToBrandNameWhenUnset(t *testing.T) {
	var rendered bytes.Buffer
	if err := Layout(branding.Default(), "Game Requests", NavigationState{}).Render(instanceContext(""), &rendered); err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, "<title>Game Requests · LANsurfer</title>") {
		t.Fatalf("title = %q, want fallback to brand name", body[:min(len(body), 400)])
	}
}

func TestLayoutRendersManifestAndIconLinks(t *testing.T) {
	brand := branding.Default()
	var rendered bytes.Buffer
	if err := Layout(brand, "Game Requests", NavigationState{}).Render(instanceContext(""), &rendered); err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`<link rel="manifest" href="/manifest.webmanifest">`,
		`<link rel="apple-touch-icon" href="` + brand.Assets.Icons.AppleTouch180 + `">`,
		`<link rel="icon" type="image/png" sizes="32x32" href="` + brand.Assets.Icons.Favicon32 + `">`,
		`<link rel="icon" type="image/png" sizes="16x16" href="` + brand.Assets.Icons.Favicon16 + `">`,
		`<link rel="icon" href="` + brand.Assets.Icons.FaviconICO + `">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("document missing %q", want)
		}
	}
}
func TestLayoutBundleURLsMatchAssetETags(t *testing.T) {
	brand := branding.Default()
	for _, assetURL := range []string{brand.Assets.Stylesheet, brand.Assets.Script} {
		parsed, err := url.Parse(assetURL)
		if err != nil {
			t.Fatalf("parse asset URL %q: %v", assetURL, err)
		}
		version := parsed.Query().Get("v")
		if version == "" {
			t.Errorf("asset URL %q has no content version", assetURL)
			continue
		}
		response := httptest.NewRecorder()
		assets.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(parsed.Path, "/assets/"), nil))
		if response.Code != http.StatusOK || version != strings.Trim(response.Header().Get("ETag"), `"`) {
			t.Errorf("asset URL %q does not match served ETag %q (status %d)", assetURL, response.Header().Get("ETag"), response.Code)
		}
	}
}

// Both shells need the bundled icon webfont, or every Phosphor glyph renders
// as an empty box on the login and onboarding screens.
func TestEveryShellLinksIconStylesheet(t *testing.T) {
	brand := branding.Default()
	want := `<link rel="stylesheet" href="` + brand.Assets.IconStylesheet + `">`
	for name, component := range map[string]templ.Component{
		"Layout":     Layout(brand, "Game Requests", NavigationState{}),
		"AuthLayout": AuthLayout(brand, "Log in"),
	} {
		var rendered bytes.Buffer
		if err := component.Render(instanceContext(""), &rendered); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("%s missing %q", name, want)
		}
	}
}

func TestLayoutSecondaryInstanceLabel(t *testing.T) {
	tests := map[string]struct {
		instance string
		want     bool
	}{
		"custom name shows a label":                                 {instance: "Camp Nova", want: true},
		"unset shows no label":                                      {instance: "", want: false},
		"name explicitly set to the brand name still shows a label": {instance: "LANsurfer", want: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var rendered bytes.Buffer
			if err := Layout(branding.Default(), "Game Requests", NavigationState{}).Render(instanceContext(test.instance), &rendered); err != nil {
				t.Fatalf("render Layout: %v", err)
			}
			got := strings.Contains(rendered.String(), `data-instance-name`)
			if got != test.want {
				t.Errorf("public header instance label present = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLayoutSecondaryInstanceLabelInAuthenticatedShell(t *testing.T) {
	navigation := NavigationState{Viewer: &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"}}
	var rendered bytes.Buffer
	if err := Layout(branding.Default(), "Game Requests", navigation).Render(instanceContext("Camp Nova"), &rendered); err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, `data-instance-name`) || !strings.Contains(body, "Camp Nova") {
		t.Fatalf("authenticated shell missing instance label, got %q", body[:min(len(body), 800)])
	}
}

// legalContext returns a request context carrying the given legal identity,
// the way NewRouter's Legal middleware does in production.
func legalContext(identity webmiddleware.LegalIdentity) context.Context {
	var carried context.Context
	webmiddleware.Legal(identity)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		carried = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return carried
}

// Discord requires Terms, Privacy, and a reporting path to be reachable from
// every surface, including the login screen where Discord is offered.
func TestEveryShellLinksLegalPages(t *testing.T) {
	brand := branding.Default()
	shells := map[string]templ.Component{
		"public":        Layout(brand, "Privacy Policy", NavigationState{}),
		"authenticated": Layout(brand, "Game Requests", NavigationState{Viewer: &Viewer{ID: "u1", GameNick: "Ada"}}),
		"auth":          AuthLayout(brand, "Log in"),
	}
	for name, shell := range shells {
		t.Run(name, func(t *testing.T) {
			var rendered bytes.Buffer
			ctx := legalContext(webmiddleware.LegalIdentity{ImprintURL: "https://club.example.test/impressum"})
			if err := shell.Render(ctx, &rendered); err != nil {
				t.Fatalf("render: %v", err)
			}
			body := rendered.String()
			for _, want := range []string{
				`<footer`, `href="/terms"`, `href="/privacy"`,
				`href="https://club.example.test/impressum" target="_blank" rel="noopener noreferrer"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("shell does not contain %q", want)
				}
			}
		})
	}
}

func TestFooterOmitsImprintWhenUnset(t *testing.T) {
	var rendered bytes.Buffer
	if err := Footer(branding.Default()).Render(legalContext(webmiddleware.LegalIdentity{}), &rendered); err != nil {
		t.Fatalf("render Footer: %v", err)
	}
	if strings.Contains(rendered.String(), "Imprint") {
		t.Error("footer renders an Imprint link without a configured imprint URL")
	}
}

func TestAuthenticatedLayoutCreditsOperatorOnlyInFooter(t *testing.T) {
	var rendered bytes.Buffer
	navigation := NavigationState{Viewer: &Viewer{ID: "u1", GameNick: "Ada"}}
	if err := Layout(branding.Default(), "Game Requests", navigation).Render(legalContext(webmiddleware.LegalIdentity{}), &rendered); err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	if got := strings.Count(rendered.String(), "Powered by"); got != 1 {
		t.Errorf("operator attribution count = %d, want 1", got)
	}
}

func TestFooterUsesFullViewportWidth(t *testing.T) {
	var rendered bytes.Buffer
	if err := Footer(branding.Default()).Render(legalContext(webmiddleware.LegalIdentity{}), &rendered); err != nil {
		t.Fatalf("render Footer: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `class="w-full `) {
		t.Errorf("footer content is not full-width: %s", body)
	}
	if !strings.Contains(body, `flex-col`) || !strings.Contains(body, `sm:flex-row`) {
		t.Errorf("footer does not split metadata onto a mobile row: %s", body)
	}
	if !strings.Contains(body, `gap-y-2`) || !strings.Contains(body, `text-center`) || !strings.Contains(body, `sm:text-right`) {
		t.Errorf("footer mobile rows are not centered with added spacing: %s", body)
	}
}
