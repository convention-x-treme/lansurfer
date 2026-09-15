package middleware

import (
	"context"
	"net/http"
	"time"
)

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
			ctx, cancel := context.WithTimeout(r.Context(), limit)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
