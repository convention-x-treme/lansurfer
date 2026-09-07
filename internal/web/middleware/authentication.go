// Package middleware provides HTTP middleware for LANsurfer's web layer.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
)

const SessionCookieName = "lansurfer_session"

type Authenticator interface {
	Authenticate(context.Context, string) (user.User, error)
}

type currentUserKey struct{}

func Authentication(authenticator Authenticator, secureCookies bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authenticator == nil {
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(SessionCookieName)
			if errors.Is(err, http.ErrNoCookie) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				http.Error(w, "invalid session cookie", http.StatusBadRequest)
				return
			}
			account, err := authenticator.Authenticate(r.Context(), cookie.Value)
			if errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, user.ErrNotFound) {
				http.SetCookie(w, ExpiredSessionCookie(secureCookies))
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				http.Error(w, "authenticate session", http.StatusInternalServerError)
				return
			}
			ctx := context.WithValue(r.Context(), currentUserKey{}, account)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CurrentUser(ctx context.Context) (user.User, bool) {
	account, ok := ctx.Value(currentUserKey{}).(user.User)
	return account, ok
}

func SessionCookie(token string, expiresAt time.Time, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func ExpiredSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}
