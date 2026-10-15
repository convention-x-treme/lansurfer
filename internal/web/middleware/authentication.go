// Package middleware provides HTTP middleware for LANsurfer's web layer.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
				LogError(r, "authenticate browser session", err)
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

// ContextWithUser attaches the authenticated user to ctx. Production code
// only ever does this inside the Authentication middleware above; it is
// exported so other packages' handler tests can simulate a signed-in
// request without driving the whole middleware chain.
func ContextWithUser(ctx context.Context, account user.User) context.Context {
	return context.WithValue(ctx, currentUserKey{}, account)
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

const DeviceCookieName = "lansurfer_device"

// Twitch Developer Services Agreement, Schedule 1, A: persistent cookies
// expire within 13 months.
const deviceCookieLifetime = 365 * 24 * time.Hour

// DeviceCookie identifies one browser across authentication sessions so
// server-side logout can remove exactly that browser's Push subscription.
// deviceID is a random opaque token generated once when Push is first
// enabled; only its hash (via internal/platform/crypto.Fingerprint) is ever
// persisted with a subscription.
func DeviceCookie(deviceID string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: DeviceCookieName, Value: deviceID, Path: "/", Expires: time.Now().Add(deviceCookieLifetime),
		MaxAge: int(deviceCookieLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

func ExpiredDeviceCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name: DeviceCookieName, Path: "/", Expires: time.Unix(1, 0),
		MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

// CurrentOrNewDeviceID returns the existing device cookie's value, or
// generates and returns a fresh 32-byte random one if none is present yet.
// It never sets the cookie itself; the caller does that only once it has
// successfully persisted a subscription keyed on the returned ID.
func CurrentOrNewDeviceID(r *http.Request) (string, error) {
	if cookie, err := r.Cookie(DeviceCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate device ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
