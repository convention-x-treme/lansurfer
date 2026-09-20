package middleware

import (
	"context"
	"net/http"
)

type instanceNameKey struct{}

// Instance carries the configured minimum instance identity -- the trimmed
// LANSURFER_INSTANCE_NAME, empty when unset -- into every request's context,
// the same way Theme carries the visitor's theme choice. The value is fixed
// for the process, so this only needs to wrap the router once in the
// composition root; templ layouts read it with InstanceName instead of taking
// a new parameter at every page/handler call site.
//
// It deliberately carries the raw value rather than a brand-name fallback.
// The layouts have to distinguish "no instance configured" from "an instance
// named LANsurfer", and only an empty value can say the former; a surface
// that always needs a name applies the fallback itself.
func Instance(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), instanceNameKey{}, name)))
		})
	}
}

// InstanceName returns the configured instance name carried by Instance's
// middleware. It is empty both when no instance name is configured and when
// the middleware was never applied; neither case renders a name.
func InstanceName(ctx context.Context) string {
	name, _ := ctx.Value(instanceNameKey{}).(string)
	return name
}
