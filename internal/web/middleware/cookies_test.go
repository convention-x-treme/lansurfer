package middleware

import (
	"net/http"
	"testing"
	"time"
)

// Every persistent cookie LANsurfer sets must stay same-site, HTTP-only, and
// secure, and expire within 13 months, per the Twitch Developer Services
// Agreement, Schedule 1, A (https://legal.twitch.com/legal/developer-agreement/).
// A new cookie, or a script-written one, must be added here and to the
// Privacy Policy cookie list.
func TestPersistentCookiesMeetTwitchCookieRules(t *testing.T) {
	const thirteenMonths = 395 * 24 * time.Hour
	cookies := []*http.Cookie{
		SessionCookie("token", time.Now().Add(7*24*time.Hour), true),
		csrfCookie("token", true),
		DeviceCookie("device", true),
		ThemeCookie(lightTheme, true),
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("%s: HttpOnly=%v Secure=%v SameSite=%v", cookie.Name, cookie.HttpOnly, cookie.Secure, cookie.SameSite)
		}
		if lifetime := time.Duration(cookie.MaxAge) * time.Second; lifetime <= 0 || lifetime > thirteenMonths {
			t.Errorf("%s: lifetime %v, want within 13 months", cookie.Name, lifetime)
		}
	}
}

func TestThemeCookieRejectsUnknownThemes(t *testing.T) {
	for _, theme := range []string{"", "lansurfer-blue", "lansurfer-dark; Path=/"} {
		if cookie := ThemeCookie(theme, true); cookie != nil {
			t.Errorf("ThemeCookie(%q) = %v, want nil", theme, cookie)
		}
	}
}
