package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ErrRequestTimeout is the cancellation cause RequestTimeout attaches to a
// request context. RequestLogging reads it back with context.Cause so a
// request the server gave up on is distinguishable from one the client
// abandoned.
var ErrRequestTimeout = errors.New("request timeout exceeded")

// RequestTimeout bounds how long a request may occupy a handler, so that a
// blocked query or an exhausted connection pool surfaces as a failed request
// instead of a browser that waits forever. The realtime stream is exempt
// because it is long-lived by design.
func RequestTimeout(limit time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == realtimePath {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeoutCause(r.Context(), limit, ErrRequestTimeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
