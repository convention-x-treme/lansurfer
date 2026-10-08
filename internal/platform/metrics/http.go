package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const unmatchedRoute = "unmatched"

// Service owns an instance-local registry; nothing is registered globally.
type Service struct {
	registry *prometheus.Registry
	handler  http.Handler
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

// New builds the registry. A nil pool omits database and pool metrics.
func New(pool *pgxpool.Pool) *Service {
	reg := prometheus.NewRegistry()
	s := &Service{
		registry: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lansurfer_http_requests_total",
			Help: "HTTP requests by method, chi route template and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lansurfer_http_request_duration_seconds",
			Help:    "HTTP request duration by method and chi route template.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lansurfer_http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		s.requests, s.duration, s.inFlight,
	)
	if pool != nil {
		reg.MustRegister(&dbCollector{pool: pool})
	}
	s.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError, MaxRequestsInFlight: 1})
	return s
}

// ServeHTTP writes the exposition. Authentication is the caller's job.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// Middleware records request metrics. Install outside the recoverer so
// recovered panics are seen as the 500 they produce.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure a route context we can read after routing; chi reuses it.
		rctx := chi.RouteContext(r.Context())
		if rctx == nil {
			rctx = chi.NewRouteContext()
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		}
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		s.inFlight.Inc()
		completed := false
		defer func() {
			s.inFlight.Dec()
			status := ww.Status()
			if !completed {
				status = http.StatusInternalServerError // panic escaped (e.g. http.ErrAbortHandler)
			} else if status == 0 {
				status = http.StatusOK
			}
			method, route := methodLabel(r.Method), routeLabel(rctx, status)
			s.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
			s.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		}()
		next.ServeHTTP(ww, r)
		completed = true
	})
}

func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return m
	}
	return "UNKNOWN"
}

// routeLabel returns the matched chi template. chi leaves a partial or empty
// pattern on 404/405, which is collapsed so unmatched paths cannot grow labels.
func routeLabel(rctx *chi.Context, status int) string {
	p := rctx.RoutePattern()
	if p == "" || (strings.HasSuffix(p, "/*") && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed)) {
		return unmatchedRoute
	}
	return p
}
