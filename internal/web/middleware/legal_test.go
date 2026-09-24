package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegalCarriesIdentity(t *testing.T) {
	want := LegalIdentity{EntityName: "Club e.V.", ContactEmail: "privacy@example.test"}
	var got LegalIdentity
	handler := Legal(want)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LegalInfo(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got != want {
		t.Errorf("LegalInfo(ctx) = %#v, want %#v", got, want)
	}
}

func TestLegalInfoWithoutMiddlewareIsEmpty(t *testing.T) {
	if got := LegalInfo(context.Background()); got != (LegalIdentity{}) {
		t.Errorf("LegalInfo(ctx) = %#v, want zero value", got)
	}
}
