package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/platform/metrics"
	"github.com/go-chi/chi/v5"
)

func TestCollectSamplesContinuesAfterSampleFailure(t *testing.T) {
	var output bytes.Buffer
	calls := 0
	samples, failures, invalid, err := collectSamples(
		context.Background(), 40*time.Millisecond, 5*time.Millisecond, time.Now(),
		bufio.NewWriter(&output),
		func() (bool, error) {
			calls++
			if calls == 1 {
				return false, errors.New("metrics scrape failed")
			}
			return calls == 2, nil
		},
	)
	if err != nil {
		t.Fatalf("collectSamples() error = %v", err)
	}
	if calls < 2 || samples == 0 || failures != 1 || invalid != 1 {
		t.Fatalf("calls=%d samples=%d failures=%d invalid=%d; want continued sampling with one failure and one invalid window", calls, samples, failures, invalid)
	}
	var failure sampleFailure
	if err := json.NewDecoder(&output).Decode(&failure); err != nil {
		t.Fatalf("decode failure record: %v", err)
	}
	if failure.Kind != "sample_error" || failure.Error != "metrics scrape failed" {
		t.Fatalf("failure record = %+v", failure)
	}
}

// The route tree mirrors internal/web/router.go; the labels come from the real
// metrics middleware, so a renamed or regrouped route fails here.
func TestParseMetricsKeepsLoadTestedRouteHistograms(t *testing.T) {
	service := metrics.New(nil)
	router := chi.NewRouter()
	router.Use(service.Middleware)
	ok := func(http.ResponseWriter, *http.Request) {}
	router.Get("/healthz", ok)
	router.Get("/readyz", ok)
	router.Get("/metrics", ok)
	router.Group(func(authenticated chi.Router) {
		authenticated.Get("/", ok)
		authenticated.Group(func(authenticated chi.Router) {
			authenticated.Route("/games", func(games chi.Router) {
				games.Get("/", ok)
				games.Get("/mine", ok)
			})
			authenticated.Route("/game-requests", func(requests chi.Router) {
				requests.Get("/", ok)
				requests.Get("/new", ok)
				requests.Get("/refresh", ok)
				requests.Post("/", ok)
				requests.Get("/{requestID}", ok)
				requests.Get("/{requestID}/refresh", ok)
				requests.Post("/{requestID}/join", ok)
				requests.Post("/{requestID}/leave", ok)
				requests.Group(func(requests chi.Router) {
					requests.Get("/{requestID}/lobby", ok)
					requests.Post("/{requestID}/chat", ok)
				})
			})
			authenticated.Get("/events/game-requests", ok)
		})
	})
	hits := map[string]string{
		"GET /":                                  "/",
		"GET /games":                             "/games/",
		"GET /game-requests":                     "/game-requests/",
		"GET /game-requests/new":                 "/game-requests/new",
		"GET /game-requests/refresh":             "/game-requests/refresh",
		"POST /game-requests":                    "/game-requests",
		"GET /game-requests/{requestID}":         "/game-requests/42",
		"GET /game-requests/{requestID}/refresh": "/game-requests/42/refresh",
		"GET /game-requests/{requestID}/lobby":   "/game-requests/42/lobby",
		"POST /game-requests/{requestID}/join":   "/game-requests/42/join",
		"POST /game-requests/{requestID}/leave":  "/game-requests/42/leave",
		"POST /game-requests/{requestID}/chat":   "/game-requests/42/chat",
		"GET /events/game-requests":              "/events/game-requests",
		"GET /metrics":                           "/metrics",
		"GET /readyz":                            "/readyz",
		"GET /healthz":                           "/healthz",
	}
	for key, path := range hits {
		method, _, _ := strings.Cut(key, " ")
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/games/mine", nil)) // not profiled
	exposition := httptest.NewRecorder()
	service.ServeHTTP(exposition, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	got, err := parseMetrics(exposition.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != len(profiledRoutes) {
		t.Fatalf("test covers %d routes, profiledRoutes has %d", len(hits), len(profiledRoutes))
	}
	for key := range profiledRoutes {
		method, route, _ := strings.Cut(key, " ")
		name := "lansurfer_http_request_duration_seconds{method=" + method + ",route=" + route + "}_count"
		if got[name] != 1 {
			t.Errorf("%s = %v, want 1; exported keys: %v", name, got[name], histogramCounts(got))
		}
	}
	if n := len(histogramCounts(got)); n != len(profiledRoutes) {
		t.Errorf("exported %d route histograms, want exactly %d: %v", n, len(profiledRoutes), histogramCounts(got))
	}
}

func histogramCounts(metrics map[string]float64) (keys []string) {
	for key := range metrics {
		if strings.HasSuffix(key, "}_count") {
			keys = append(keys, key)
		}
	}
	return keys
}

func TestDockerMetricsUsesCurrentComposeProject(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	log := filepath.Join(dir, "docker-args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$DOCKER_ARGS_LOG\"\ncase \"$1\" in\nps) echo container-id;;\nstats) echo '1.0%|1MiB / 10MiB';;\nesac\n"
	if err := os.WriteFile(docker, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_ARGS_LOG", log)
	t.Setenv("COMPOSE_PROJECT_NAME", "lansurfer-loadtest-147u")

	samples := dockerMetrics()
	for _, sample := range samples {
		if !sample.Available {
			t.Errorf("%s sample unavailable: %s", sample.Role, sample.Error)
		}
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(args), "label=com.docker.compose.project=lansurfer-loadtest-147u"); got != 3 {
		t.Fatalf("project filter appears %d times; want app, postgres, and k6 filters", got)
	}
}
