package handler

import (
	"context"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

// DemoAdminPromoter grants the administrator role in demo mode.
type DemoAdminPromoter interface {
	PromoteToAdmin(context.Context, string) (user.User, error)
}

// DemoAdmin promotes the session's own user, never a submitted ID. NewRouter
// registers it only in demo mode, behind authentication and RequireGameNick.
func DemoAdmin(promoter DemoAdminPromoter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := webmiddleware.CurrentUser(r.Context())
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		if _, err := promoter.PromoteToAdmin(r.Context(), account.ID); err != nil {
			webmiddleware.LogError(r, "promote demo administrator", err)
			http.Error(w, "administrator rights could not be granted; try again", http.StatusInternalServerError)
			return
		}
		demoRedirect(w, r, "/admin/games?notice=demo-admin-granted")
	}
}

// DemoForbidden replaces a destructive or externally visible admin action in
// demo mode: the wrapped handler never runs. This is deployment policy at the
// transport edge; domain packages stay unaware of demo mode.
func DemoForbidden(target string) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			demoRedirect(w, r, target+"?notice=demo-forbidden")
		})
	}
}

func demoRedirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
