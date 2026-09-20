package web

import (
	"net/netip"
	"net/url"
	"strings"
)

// SecureContextStatus reports whether baseURL is expected to satisfy the
// browser secure-context requirement that PWA installation and Web Push
// depend on: HTTPS always qualifies, and HTTP qualifies only for a
// loopback/localhost origin, which browsers already treat as a secure
// context for local development. Any other HTTP origin does not qualify.
func SecureContextStatus(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHost(parsed.Hostname())
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}
