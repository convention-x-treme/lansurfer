package handler

import (
	"net/http"

	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

// SetTheme stores the visitor's light/dark choice. The toggle script has
// already repainted the page, so the response carries only the cookie.
func SetTheme(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := webmiddleware.ThemeCookie(r.PostFormValue("theme"), secure)
		if cookie == nil {
			http.Error(w, "unknown theme", http.StatusBadRequest)
			return
		}
		http.SetCookie(w, cookie)
		w.WriteHeader(http.StatusNoContent)
	}
}
