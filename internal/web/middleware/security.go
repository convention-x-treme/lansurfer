package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	csrfCookieName  = "lansurfer_csrf"
	csrfFormField   = "csrf_token"
	csrfHeaderName  = "X-CSRF-Token"
	csrfTokenBytes  = 32
	maxFormBytes    = 64 << 10
	csrfLifetime    = 7 * 24 * time.Hour
	realtimePath    = "/events/game-requests"
	manifestPath    = "/manifest.webmanifest"
	serviceWorkerJS = "/service-worker.js"
)

type csrfTokenKey struct{}

// SecurityHeaders applies the browser security policy used by LANsurfer. The
// policy permits only the embedded same-origin frontend assets and SSE stream.
func SecurityHeaders(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := w.Header()
			headers.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data: https://images.igdb.com; manifest-src 'self'; object-src 'none'; script-src 'self'; style-src 'self'; worker-src 'self'")
			headers.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
			headers.Set("Referrer-Policy", "no-referrer")
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-Frame-Options", "DENY")
			if secure {
				headers.Set("Strict-Transport-Security", "max-age=31536000")
			}
			if !strings.HasPrefix(r.URL.Path, "/assets/") {
				headers.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF implements a double-submit token check for state-changing browser
// requests. Tokens are random, host-only cookies and are rendered into forms.
func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if csrfExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if isSafeMethod(r.Method) {
				token := validCSRFCookie(r)
				if token == "" && issuesCSRFToken(r.URL.Path) {
					var err error
					token, err = newCSRFToken()
					if err != nil {
						http.Error(w, "prepare request security", http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, csrfCookie(token, secure))
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfTokenKey{}, token)))
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
			if err := r.ParseForm(); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			token := validCSRFCookie(r)
			provided := r.Header.Get(csrfHeaderName)
			if provided == "" {
				provided = r.PostFormValue(csrfFormField)
			}
			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfTokenKey{}, token)))
		})
	}
}

func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfTokenKey{}).(string)
	return token
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodTrace
}

func issuesCSRFToken(path string) bool {
	return !csrfExempt(path)
}

func csrfExempt(path string) bool {
	return strings.HasPrefix(path, "/assets/") || path == "/healthz" || path == "/readyz" ||
		path == realtimePath || path == manifestPath || path == serviceWorkerJS
}

func newCSRFToken() (string, error) {
	random := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func validCSRFCookie(r *http.Request) string {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || len(cookie.Value) != base64.RawURLEncoding.EncodedLen(csrfTokenBytes) {
		return ""
	}
	if _, err := base64.RawURLEncoding.DecodeString(cookie.Value); err != nil {
		return ""
	}
	return cookie.Value
}

func csrfCookie(token string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: csrfCookieName, Value: token, Path: "/", Expires: time.Now().Add(csrfLifetime),
		MaxAge: int(csrfLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}
