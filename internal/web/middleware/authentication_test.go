package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
)

func TestAuthenticationAddsCurrentUser(t *testing.T) {
	authenticator := authenticatorFunc(func(_ context.Context, token string) (user.User, error) {
		if token != "opaque-token" {
			t.Fatalf("token = %q", token)
		}
		return user.User{ID: "user-1", DisplayName: "Player"}, nil
	})
	handler := Authentication(authenticator, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := CurrentUser(r.Context())
		if !ok || account.ID != "user-1" {
			t.Fatalf("current user = %#v, %v", account, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "opaque-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestAuthenticationClearsUnknownSession(t *testing.T) {
	authenticator := authenticatorFunc(func(context.Context, string) (user.User, error) {
		return user.User{}, auth.ErrSessionNotFound
	})
	handler := Authentication(authenticator, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "unknown"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", result.StatusCode)
	}
	cookies := result.Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].MaxAge != -1 || !cookies[0].Secure {
		t.Fatalf("cookies = %#v", cookies)
	}
}

func TestAuthenticationReportsStoreFailure(t *testing.T) {
	authenticator := authenticatorFunc(func(context.Context, string) (user.User, error) {
		return user.User{}, errors.New("database unavailable")
	})
	handler := Authentication(authenticator, false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler was called")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}

type authenticatorFunc func(context.Context, string) (user.User, error)

func (f authenticatorFunc) Authenticate(ctx context.Context, token string) (user.User, error) {
	return f(ctx, token)
}
