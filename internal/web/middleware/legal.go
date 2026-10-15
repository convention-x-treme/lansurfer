package middleware

import (
	"context"
	"net/http"
)

type imprintKey struct{}

// Imprint carries LANSURFER_IMPRINT_ENABLED into every request's context,
// the same way Instance carries the instance name, so the shared footer can
// link /imprint without a parameter at every call site.
func Imprint(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), imprintKey{}, enabled)))
		})
	}
}

// ImprintEnabled reports the flag carried by Imprint; false without it.
func ImprintEnabled(ctx context.Context) bool {
	enabled, _ := ctx.Value(imprintKey{}).(bool)
	return enabled
}
