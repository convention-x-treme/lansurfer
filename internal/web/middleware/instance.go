package middleware

import (
	"context"
	"net/http"
)

type instanceNameKey struct{}

// Instance carries the already-resolved minimum instance identity (the
// trimmed LANSURFER_INSTANCE_NAME, or the fixed brand fallback when unset)
// into every request's context, the same way Theme carries the visitor's
// theme choice. The value is fixed for the process, so this only needs to
// wrap the router once in the composition root; templ layouts read it with
// InstanceName instead of taking a new parameter at every page/handler call
// site.
func Instance(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), instanceNameKey{}, name)))
		})
	}
}

// InstanceName returns the resolved instance name carried by Instance's
// middleware, or an empty string if it was never applied.
func InstanceName(ctx context.Context) string {
	name, _ := ctx.Value(instanceNameKey{}).(string)
	return name
}
