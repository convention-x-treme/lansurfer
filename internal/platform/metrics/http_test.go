package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	dto "github.com/prometheus/client_model/go"
)

func requestCount(t *testing.T, s *Service, method, route, status string) float64 {
	t.Helper()
	families, err := s.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "lansurfer_http_requests_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			if labels(m) == method+"|"+route+"|"+status {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labels(m *dto.Metric) string {
	out := ""
	for i, l := range m.GetLabel() {
		if i > 0 {
			out += "|"
		}
		out += l.GetValue()
	}
	return out
}

func seriesCount(t *testing.T, s *Service, name string) int {
	t.Helper()
	families, err := s.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == name {
			return len(f.GetMetric())
		}
	}
	return 0
}

func newRouter(s *Service, inFlight func()) http.Handler {
	r := chi.NewRouter()
	r.Use(s.Middleware)
	r.Use(middleware.Recoverer)
	r.Get("/games/{id}", func(w http.ResponseWriter, r *http.Request) {
		if inFlight != nil {
			inFlight()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r.Get("/implicit", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Route("/api", func(r chi.Router) {
		r.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {})
	})
	return r
}

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRouteTemplateNotRawPath(t *testing.T) {
	s := New(nil)
	h := newRouter(s, nil)
	do(h, "GET", "/games/11111111-1111-1111-1111-111111111111")
	do(h, "GET", "/games/22222222-2222-2222-2222-222222222222")
	do(h, "GET", "/api/items/42")

	if got := requestCount(t, s, "GET", "/games/{id}", "204"); got != 2 {
		t.Fatalf("games template count = %v, want 2", got)
	}
	if got := requestCount(t, s, "GET", "/api/items/{id}", "200"); got != 1 {
		t.Fatalf("nested template count = %v, want 1", got)
	}
	if n := seriesCount(t, s, "lansurfer_http_requests_total"); n != 2 {
		t.Fatalf("series = %d, want 2 (IDs must not create series)", n)
	}
}

func TestUnmatchedAndUnknownMethodBounded(t *testing.T) {
	s := New(nil)
	h := newRouter(s, nil)
	for _, p := range []string{"/nope/1", "/nope/2", "/api/secret/123", "/api/other"} {
		do(h, "GET", p)
	}
	do(h, "BREW", "/nope/3")
	do(h, "PROPFIND", "/games/1") // chi answers unknown methods with 405 before routing

	if got := requestCount(t, s, "GET", unmatchedRoute, "404"); got != 4 {
		t.Fatalf("unmatched GET 404 = %v, want 4", got)
	}
	if got := requestCount(t, s, "UNKNOWN", unmatchedRoute, "405"); got != 2 {
		t.Fatalf("UNKNOWN 405 = %v, want 2", got)
	}
	if n := seriesCount(t, s, "lansurfer_http_requests_total"); n != 2 {
		t.Fatalf("series = %d, want 2", n)
	}
}

func TestStatusCapture(t *testing.T) {
	s := New(nil)
	h := newRouter(s, nil)
	if rec := do(h, "GET", "/implicit"); rec.Code != 200 {
		t.Fatalf("implicit status = %d", rec.Code)
	}
	if rec := do(h, "POST", "/panic"); rec.Code != 500 {
		t.Fatalf("panic status = %d", rec.Code)
	}
	if got := requestCount(t, s, "GET", "/implicit", "200"); got != 1 {
		t.Fatalf("implicit 200 = %v", got)
	}
	if got := requestCount(t, s, "POST", "/panic", "500"); got != 1 {
		t.Fatalf("recovered panic 500 = %v, want 1 (middleware sits outside recoverer)", got)
	}
}

func inFlightValue(t *testing.T, s *Service) float64 {
	t.Helper()
	families, err := s.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "lansurfer_http_requests_in_flight" {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatal("in-flight gauge missing")
	return 0
}

func TestInFlightTransitions(t *testing.T) {
	s := New(nil)
	var during float64
	h := newRouter(s, func() { during = inFlightValue(t, s) })

	if got := inFlightValue(t, s); got != 0 {
		t.Fatalf("before = %v", got)
	}
	do(h, "GET", "/games/1")
	if during != 1 {
		t.Fatalf("during = %v, want 1", during)
	}
	if got := inFlightValue(t, s); got != 0 {
		t.Fatalf("after = %v, want 0", got)
	}
	do(h, "POST", "/panic")
	if got := inFlightValue(t, s); got != 0 {
		t.Fatalf("after panic = %v, want 0", got)
	}
}

func TestEscapedPanicRecordsAbort(t *testing.T) {
	s := New(nil)
	h := s.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	func() {
		defer func() { _ = recover() }()
		do(h, "GET", "/x")
	}()
	if got := requestCount(t, s, "GET", unmatchedRoute, "500"); got != 1 {
		t.Fatalf("aborted request = %v, want 1", got)
	}
	if got := inFlightValue(t, s); got != 0 {
		t.Fatalf("in-flight after abort = %v", got)
	}
}

func TestStreamingFlushPreserved(t *testing.T) {
	s := New(nil)
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("wrapped writer lost http.Flusher")
			return
		}
		_, _ = w.Write([]byte("x"))
		f.Flush()
	}))
	do(h, "GET", "/stream")
}
