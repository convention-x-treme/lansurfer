package middleware

import (
	"context"
	"net/http"
)

type legalIdentityKey struct{}

// LegalIdentity is the operator's configured legal identity: the web layer's
// copy of config.Legal, so templates do not import configuration.
type LegalIdentity struct {
	EntityName   string
	Address      string
	ContactEmail string
	ImprintURL   string
}

// Legal carries the operator's legal identity into every request's context,
// the same way Instance carries the instance name, so the shared footer and
// the Terms/Privacy pages read it without a parameter at every call site.
func Legal(identity LegalIdentity) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), legalIdentityKey{}, identity)))
		})
	}
}

// LegalInfo returns the identity carried by Legal's middleware, or the zero
// value when the middleware was never applied.
func LegalInfo(ctx context.Context) LegalIdentity {
	identity, _ := ctx.Value(legalIdentityKey{}).(LegalIdentity)
	return identity
}
