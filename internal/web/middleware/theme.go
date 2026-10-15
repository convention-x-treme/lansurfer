package middleware

import (
	"context"
	"net/http"
	"time"
)

// ThemeCookieName holds the visitor's explicit light/dark choice. The Content-
// Security-Policy forbids inline scripts, so the choice cannot be read from
// storage before the first paint; a cookie lets the server render it instead.
// The server sets it so it can be HttpOnly: the Twitch Developer Services
// Agreement (Schedule 1, A) requires same-site, HTTP-only, secure cookies that
// expire within 13 months (https://legal.twitch.com/legal/developer-agreement/).
const ThemeCookieName = "lansurfer_theme"

const themeCookieLifetime = 365 * 24 * time.Hour

const (
	darkTheme  = "lansurfer-dark"
	lightTheme = "lansurfer-light"
)

// ThemeCookie stores an explicit theme choice, or returns nil for anything
// other than the two known themes.
func ThemeCookie(theme string, secure bool) *http.Cookie {
	if theme != darkTheme && theme != lightTheme {
		return nil
	}
	return &http.Cookie{
		Name: ThemeCookieName, Value: theme, Path: "/", Expires: time.Now().Add(themeCookieLifetime),
		MaxAge: int(themeCookieLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

type themeKey struct{}

// Theme carries an explicit theme choice into the rendered document. Without a
// choice the document keeps no theme attribute and the stylesheet follows the
// operating system, so neither path repaints after the bundle runs.
func Theme(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(ThemeCookieName)
		if err != nil || (cookie.Value != darkTheme && cookie.Value != lightTheme) {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), themeKey{}, cookie.Value)))
	})
}

// ThemeChoice returns the visitor's explicit theme, or an empty string when
// they have not chosen one.
func ThemeChoice(ctx context.Context) string {
	theme, _ := ctx.Value(themeKey{}).(string)
	return theme
}
