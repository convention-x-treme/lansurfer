package notification

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// webPushRoundTripper rejects unsafe URLs before the transport can open a connection.
// Keep the request URL unchanged so TLS verifies the original Push service hostname.
type webPushRoundTripper struct{ next http.RoundTripper }

func (t webPushRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil ||
		strings.Contains(req.URL.Hostname(), "%") {
		return nil, errors.New("Web Push destination is not a public HTTPS URL")
	}
	if port := req.URL.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("Web Push destination has an invalid port")
		}
	}
	return t.next.RoundTrip(req)
}

var webPushProhibitedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

func publicWebPushAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range webPushProhibitedRanges {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// newWebPushHTTPClient validates every resolved address, then dials only vetted
// literals. A dialer cannot re-resolve an attacker-controlled hostname. Neither
// proxies nor redirects may redirect a Push POST outside this boundary.
func newWebPushHTTPClient(
	lookup func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) *http.Client {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	if dial == nil {
		dial = new(net.Dialer).DialContext
	}
	transport := &http.Transport{
		Proxy:             nil,
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || host == "" {
				return nil, errors.New("invalid Web Push destination")
			}
			var addresses []netip.Addr
			if literal, err := netip.ParseAddr(host); err == nil {
				addresses = append(addresses, literal)
			} else {
				resolved, err := lookup(ctx, host)
				if err != nil {
					return nil, errors.New("Web Push DNS lookup failed")
				}
				for _, answer := range resolved {
					ip, ok := netip.AddrFromSlice(answer.IP)
					if !ok || answer.Zone != "" {
						return nil, errors.New("Web Push DNS returned a prohibited address")
					}
					addresses = append(addresses, ip)
				}
			}
			if len(addresses) == 0 {
				return nil, errors.New("Web Push DNS returned no addresses")
			}
			for _, ip := range addresses {
				if !publicWebPushAddress(ip) {
					return nil, errors.New("Web Push destination is not public")
				}
			}
			var conn net.Conn
			for _, ip := range addresses {
				conn, err = dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, errors.New("Web Push connection failed")
		},
	}
	return &http.Client{
		Transport:     webPushRoundTripper{next: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
