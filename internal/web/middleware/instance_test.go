package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstanceCarriesResolvedName(t *testing.T) {
	var got string
	handler := Instance("Camp Nova")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = InstanceName(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got != "Camp Nova" {
		t.Errorf("InstanceName(ctx) = %q, want %q", got, "Camp Nova")
	}
}

func TestInstanceNameWithoutMiddlewareIsEmpty(t *testing.T) {
	if got := InstanceName(context.Background()); got != "" {
		t.Errorf("InstanceName(ctx) = %q, want empty", got)
	}
}
