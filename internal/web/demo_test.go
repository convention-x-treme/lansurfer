package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

type demoMetricsKeys struct{ rotated bool }

func (*demoMetricsKeys) Configured(context.Context) (bool, error) { return true, nil }
func (k *demoMetricsKeys) Rotate(context.Context) (string, error) {
	k.rotated = true
	return "key", nil
}
func (*demoMetricsKeys) Valid(context.Context, string) (bool, error) { return false, nil }

type demoPromoter struct{ promoted []string }

func (p *demoPromoter) PromoteToAdmin(_ context.Context, id string) (user.User, error) {
	p.promoted = append(p.promoted, id)
	return user.User{ID: id, Role: user.RoleAdmin}, nil
}

type demoFixture struct {
	games     *routerGames
	sync      *routerProviderSync
	setup     *discordSetupStub
	lifecycle *discordLifecycleStub
	metrics   *demoMetricsKeys
	promoter  *demoPromoter
	router    http.Handler
}

func newDemoFixture(account user.User, demo bool) *demoFixture {
	f := &demoFixture{
		games: &routerGames{}, sync: &routerProviderSync{}, setup: &discordSetupStub{},
		lifecycle: &discordLifecycleStub{}, metrics: &demoMetricsKeys{}, promoter: &demoPromoter{},
	}
	viewer := &routerAuthentication{account: account}
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: viewer, Sessions: viewer,
		Games: f.games, ProviderSync: f.sync,
		MetricsKeys: f.metrics, Metrics: http.NotFoundHandler(),
		DiscordSetup: f.setup, DiscordAuthorization: discordAuthorizationStub{url: "https://discord.example/authorize"},
		DiscordConnector: &discordConnectStub{}, DiscordLifecycle: f.lifecycle,
		DiscordStatus: &discordDestinationStatusStub{},
		DemoAdmins:    f.promoter,
	}
	if demo {
		dependencies.Demo = &webmiddleware.DemoSchedule{Hour: 4, Location: time.UTC}
	}
	f.router = NewRouter(dependencies)
	return f
}

func (f *demoFixture) serve(method, target string, body url.Values, csrf bool, header ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	request.AddCookie(&http.Cookie{Name: "lansurfer_auth_binding", Value: "browser-binding"})
	if csrf {
		addCSRF(request)
	}
	for i := 0; i+1 < len(header); i += 2 {
		request.Header.Set(header[i], header[i+1])
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

var demoBlockedRoutes = []struct{ path, target string }{
	{"/admin/games/g1/delete", "/admin/games"},
	{"/admin/games/g1/merge", "/admin/games"},
	{"/admin/games/g1/aliases/a1/remove", "/admin/games"},
	{"/admin/games/g1/igdb/unlink", "/admin/games"},
	{"/admin/igdb/sync", "/admin/igdb"},
	{"/admin/discord/connect", "/admin/discord"},
	{"/admin/discord/reconnect", "/admin/discord"},
	{"/admin/discord/disconnect", "/admin/discord"},
	{"/admin/metrics/key", "/admin/metrics"},
}

func TestDemoModeBlocksDestructiveAdminActions(t *testing.T) {
	admin := user.User{ID: "admin-1", GameNick: "Ada Admin", Role: user.RoleAdmin}
	f := newDemoFixture(admin, true)
	form := url.Values{"source_id": {"g2"}}
	for _, route := range demoBlockedRoutes {
		response := f.serve(http.MethodPost, route.path, form, true)
		if want := route.target + "?notice=demo-forbidden"; response.Code != http.StatusSeeOther || response.Header().Get("Location") != want {
			t.Errorf("POST %s = %d %q, want 303 %q", route.path, response.Code, response.Header().Get("Location"), want)
		}
	}
	if f.games.deletedGame != "" || f.games.mergedSource != "" || f.games.unlinkedGame != "" ||
		f.sync.started || f.setup.starts != 0 || f.lifecycle.calls != 0 || f.metrics.rotated {
		t.Errorf("a blocked action reached its service: games=%+v sync=%v discord=%d/%d metrics=%v",
			f.games, f.sync.started, f.setup.starts, f.lifecycle.calls, f.metrics.rotated)
	}

	htmx := f.serve(http.MethodPost, "/admin/games/g1/delete", nil, true, "HX-Request", "true")
	if htmx.Code != http.StatusNoContent || htmx.Header().Get("HX-Redirect") != "/admin/games?notice=demo-forbidden" {
		t.Errorf("HTMX delete = %d %q", htmx.Code, htmx.Header().Get("HX-Redirect"))
	}

	page := f.serve(http.MethodGet, "/admin/metrics?notice=demo-forbidden", nil, false)
	body := page.Body.String()
	if !strings.Contains(body, "data-demo-banner") || !strings.Contains(body, "This action is disabled in the demo.") {
		t.Errorf("redirect target lacks demo banner notice: %s", body)
	}
	if strings.Contains(body, `action="/demo/admin"`) {
		t.Error("administrator is offered self-promotion")
	}
}

func TestDemoModeOffLeavesRoutesUnchanged(t *testing.T) {
	f := newDemoFixture(user.User{ID: "admin-1", GameNick: "Ada Admin", Role: user.RoleAdmin}, false)
	response := f.serve(http.MethodPost, "/admin/games/g1/delete", nil, true)
	if strings.Contains(response.Header().Get("Location"), "demo-forbidden") || f.games.deletedGame != "g1" {
		t.Errorf("delete outside demo mode = %d %q, deleted %q", response.Code, response.Header().Get("Location"), f.games.deletedGame)
	}
	if response := f.serve(http.MethodPost, "/demo/admin", nil, true); response.Code != http.StatusNotFound || len(f.promoter.promoted) != 0 {
		t.Errorf("POST /demo/admin outside demo mode = %d, promoted %v", response.Code, f.promoter.promoted)
	}
	if page := f.serve(http.MethodGet, "/admin/games", nil, false); strings.Contains(page.Body.String(), "data-demo-banner") {
		t.Error("banner rendered outside demo mode")
	}
}

func TestDemoAdminPromotesOnlyTheSessionUser(t *testing.T) {
	f := newDemoFixture(user.User{ID: "player-1", GameNick: "Ada Player", Role: user.RoleUser}, true)

	page := f.serve(http.MethodGet, "/help", nil, false)
	if !strings.Contains(page.Body.String(), `action="/demo/admin"`) {
		t.Fatalf("player is not offered self-promotion: %s", page.Body.String())
	}

	if response := f.serve(http.MethodPost, "/demo/admin", nil, false); response.Code != http.StatusForbidden || len(f.promoter.promoted) != 0 {
		t.Fatalf("POST without CSRF = %d, promoted %v", response.Code, f.promoter.promoted)
	}

	response := f.serve(http.MethodPost, "/demo/admin", url.Values{"user_id": {"someone-else"}}, true)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/games?notice=demo-admin-granted" {
		t.Fatalf("POST /demo/admin = %d %q", response.Code, response.Header().Get("Location"))
	}
	if len(f.promoter.promoted) != 1 || f.promoter.promoted[0] != "player-1" {
		t.Errorf("promoted = %v, want only the session user", f.promoter.promoted)
	}
}

func TestDemoAdminRequiresGameNick(t *testing.T) {
	f := newDemoFixture(user.User{ID: "pending-1", Role: user.RoleUser}, true)
	response := f.serve(http.MethodPost, "/demo/admin", nil, true)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" || len(f.promoter.promoted) != 0 {
		t.Errorf("POST without Game Nick = %d %q, promoted %v", response.Code, response.Header().Get("Location"), f.promoter.promoted)
	}
}
