//go:build integration

package postgres

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/metrics"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/convention-x-treme/lansurfer/internal/web"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestMetricsHTTPAccessAndSourceFailureIsolation(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	admin, err := users.Register(t.Context(), "PrivateMetricsAdmin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.PromoteToAdmin(t.Context(), admin.ID); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessionService(authpostgres.NewSessionStore(pool), users)
	session, err := sessions.NewSession(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	service, keys := metrics.New(pool), metrics.NewStore(pool)
	router := web.NewRouter(web.RouterDependencies{Sessions: sessions, MetricsKeys: keys, Metrics: service, MetricsMiddleware: service.Middleware})
	request := func(method, path, authorization string, signedIn, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		if signedIn {
			r.AddCookie(webmiddleware.SessionCookie(session.Token, session.ExpiresAt, false))
		}
		if csrf {
			token := strings.Repeat("A", 43)
			r.AddCookie(&http.Cookie{Name: "lansurfer_csrf", Value: token})
			r.Header.Set("X-CSRF-Token", token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if r := request("GET", "/metrics", "", true, false); r.Code != 401 {
		t.Fatalf("browser session granted scrape access: %d", r.Code)
	}
	if r := request("POST", "/admin/metrics/key", "", false, true); r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/auth/login" {
		t.Fatalf("anonymous generation: %d", r.Code)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE users SET role='user' WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if r := request("POST", "/admin/metrics/key", "", true, true); r.Code != http.StatusForbidden {
		t.Fatalf("non-admin generated key: %d", r.Code)
	}
	if _, err := users.PromoteToAdmin(t.Context(), admin.ID); err != nil {
		t.Fatal(err)
	}
	if r := request("POST", "/admin/metrics/key", "", true, false); r.Code != 403 {
		t.Fatalf("generation without CSRF: %d", r.Code)
	}
	generated := request("POST", "/admin/metrics/key", "", true, true)
	if generated.Code != 200 || generated.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("generation status/cache: %d %v", generated.Code, generated.Header())
	}
	// Extract the only new-key input, not any navigation or CSRF values.
	_, rest, ok := strings.Cut(generated.Body.String(), `id="metrics-api-key"`)
	if !ok {
		t.Fatal("generation response omitted new key")
	}
	_, rest, ok = strings.Cut(rest, `value="`)
	if !ok {
		t.Fatal("new key input omitted value")
	}
	key, _, _ := strings.Cut(rest, `"`)
	if r := request("GET", "/admin/metrics", "", true, false); strings.Contains(r.Body.String(), key) {
		t.Fatal("GET disclosed plaintext key")
	}
	for _, header := range []string{"", key, "Basic " + key, "Bearer wrong", "Bearer " + key + " extra"} {
		if r := request("GET", "/metrics", header, false, false); r.Code != 401 {
			t.Fatalf("bad credential accepted: %q status %d", header, r.Code)
		}
	}
	scrape := request("GET", "/metrics", "Bearer "+key, false, false)
	if scrape.Code != 200 {
		t.Fatalf("scrape status = %d", scrape.Code)
	}
	for _, value := range []string{"lansurfer_users 1\n", "lansurfer_browser_sessions 1\n", "lansurfer_games_started_total 0\n", `lansurfer_metrics_collection_success{source="aggregates"} 1`, "go_goroutines ", "process_cpu_seconds_total ", "lansurfer_db_pool_max_connections "} {
		if !strings.Contains(scrape.Body.String(), value) {
			t.Errorf("scrape missing %q", value)
		}
	}
	for _, secret := range []string{admin.ID, admin.GameNick, session.Token, key} {
		if strings.Contains(scrape.Body.String(), secret) {
			t.Error("scrape disclosed identity or secret")
		}
	}
	rotated, err := keys.Rotate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if r := request("GET", "/metrics", "Bearer "+key, false, false); r.Code != 401 {
		t.Fatal("rotated key accepted")
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE metrics_product_totals RENAME TO unavailable_totals`); err != nil {
		t.Fatal(err)
	}
	degraded := request("GET", "/metrics", "Bearer "+rotated, false, false)
	if degraded.Code != 200 || !strings.Contains(degraded.Body.String(), `lansurfer_metrics_collection_success{source="product_totals"} 0`) || !strings.Contains(degraded.Body.String(), "lansurfer_users 1\n") || strings.Contains(degraded.Body.String(), "lansurfer_games_started_total") {
		t.Fatal("optional source failure did not degrade safely")
	}
	if _, err := users.Register(t.Context(), "StillOperational"); err != nil {
		t.Fatalf("normal account registration affected: %v", err)
	}
	if r := request("GET", "/healthz", "", false, false); r.Code != http.StatusOK {
		t.Fatalf("normal HTTP traffic affected by metrics source failure: %d", r.Code)
	}
}
