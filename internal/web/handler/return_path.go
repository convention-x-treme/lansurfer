package handler

import (
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	returnCookieName = "lansurfer_return"
	returnLifetime   = 15 * time.Minute
	returnQueryParam = "return_to"
	// defaultReturnPath is where a login lands when no valid return path was
	// captured.
	defaultReturnPath = "/game-requests"
)

// validReturnPath reports whether raw is a same-origin path a login may send
// the browser back to. It is the open-redirect guard: anything that could
// leave the origin, or loop back into the login/onboarding flow itself, is
// dropped rather than repaired.
func validReturnPath(raw string) bool {
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) {
		return false
	}
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return false
	}
	// Clean first: /games/../auth/login is what the browser will request.
	cleaned := path.Clean(parsed.Path)
	return !isFlowPath(cleaned, "/auth") && !isFlowPath(cleaned, "/onboarding")
}

// isFlowPath matches the flow root itself and everything under it, but not a
// sibling that merely shares the prefix (/authors).
func isFlowPath(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

// setReturnPath remembers where to send the browser after login. An invalid
// path is ignored, leaving any earlier valid capture in place.
func setReturnPath(w http.ResponseWriter, raw string, secure bool) {
	if !validReturnPath(raw) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: returnCookieName, Value: url.QueryEscape(raw), Path: "/",
		MaxAge: int(returnLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// consumeReturnPath returns the captured return path (or the default) and
// clears the cookie. The value is validated again: a cookie is client input.
func consumeReturnPath(w http.ResponseWriter, r *http.Request, secure bool) string {
	cookie, err := r.Cookie(returnCookieName)
	if err != nil {
		return defaultReturnPath
	}
	http.SetCookie(w, &http.Cookie{
		Name: returnCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	value, err := url.QueryUnescape(cookie.Value)
	if err != nil || !validReturnPath(value) {
		return defaultReturnPath
	}
	return value
}
