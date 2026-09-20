package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceWorkerRoute(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/service-worker.js", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	result := response.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if got := result.Header.Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := result.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if response.Body.Len() == 0 {
		t.Fatal("empty service worker body")
	}
	if !strings.Contains(response.Body.String(), "install") {
		t.Error("service worker body missing install handler")
	}
}

func TestServiceWorkerRejectsOtherMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/service-worker.js", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
