package notification

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	webpush "github.com/marknefedov/go-webpush/v2"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type webPushRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webPushRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebPushNetworkRejectsProhibitedDestinations(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "[::1]", "10.0.0.2", "172.16.0.1", "192.168.1.1", "169.254.0.1",
		"[fc00::1]", "[fe80::1]", "[fec0::1]", "[4000::1]", "[3fff::1]",
		"[::ffff:127.0.0.1]", "100.64.0.1", "[2001:db8::1]", "[64:ff9b::7f00:1]",
		"[fe80::1%25eth0]", "0.1.2.3", "192.0.2.1", "198.18.0.1",
	} {
		t.Run(host, func(t *testing.T) {
			dials := 0
			client := newWebPushHTTPClient(nil, func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("dial should not happen")
			})
			request, err := http.NewRequest(http.MethodPost, "https://"+host+"/secret", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err == nil {
				response.Body.Close()
			}
			if err == nil || dials != 0 {
				t.Fatalf("prohibited host %s: error=%v dials=%d", host, err, dials)
			}
		})
	}
	for _, endpoint := range []string{"http://push.example/path", "https://push.example:0/path", "https://push.example:65536/path", "https://user:password@push.example/path"} {
		t.Run(endpoint, func(t *testing.T) {
			dials := 0
			client := newWebPushHTTPClient(nil, func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("dial should not happen")
			})
			request, err := http.NewRequest(http.MethodPost, endpoint, nil)
			if err != nil {
				return // net/http rejects this malformed endpoint before any dial
			}
			response, err := client.Do(request)
			if err == nil {
				response.Body.Close()
			}
			if err == nil || dials != 0 {
				t.Fatalf("prohibited endpoint %s: error=%v dials=%d", endpoint, err, dials)
			}
		})
	}
}

func TestWebPushNetworkValidatesDialTimeDNS(t *testing.T) {
	for _, tc := range []struct {
		name, host, address string
		answers             []net.IPAddr
		wantDial            bool
		wantLookup          int
	}{
		{name: "public IPv4", host: "push.example", answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, address: "8.8.8.8:443", wantDial: true, wantLookup: 1},
		{name: "public IPv6", host: "push.example", answers: []net.IPAddr{{IP: net.ParseIP("2606:4700::1111")}}, address: "[2606:4700::1111]:443", wantDial: true, wantLookup: 1},
		{name: "public literal", host: "8.8.8.8", address: "8.8.8.8:443", wantDial: true},
		{name: "private DNS", host: "push.example", answers: []net.IPAddr{{IP: net.ParseIP("10.1.1.1")}}, wantLookup: 1},
		{name: "mixed DNS", host: "push.example", answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, wantLookup: 1},
		{name: "empty DNS", host: "push.example", wantLookup: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, dials := 0, 0
			client := newWebPushHTTPClient(func(_ context.Context, host string) ([]net.IPAddr, error) {
				lookups++
				if host != "push.example" {
					t.Fatalf("lookup host = %q", host)
				}
				return tc.answers, nil
			}, func(_ context.Context, network, address string) (net.Conn, error) {
				dials++
				if network != "tcp" || address != tc.address {
					t.Fatalf("dial %s %s, want tcp %s", network, address, tc.address)
				}
				return nil, errors.New("test connection stopped")
			})
			request, err := http.NewRequest(http.MethodPost, "https://"+tc.host+"/private-path", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Do(request)
			if err == nil {
				t.Fatal("expected blocked or test-dial error")
			}
			if request.URL.Hostname() != tc.host || lookups != tc.wantLookup || (dials == 1) != tc.wantDial {
				t.Fatalf("original host=%q lookup=%d dial=%d", request.URL.Hostname(), lookups, dials)
			}
		})
	}
	// A rebinding answer is validated for each new connection, not cached or resolved by the dialer.
	lookups, dials := 0, 0
	client := newWebPushHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups == 1 {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("test connection stopped")
	})
	for range 2 {
		request, _ := http.NewRequest(http.MethodPost, "https://push.example/private-path", nil)
		_, _ = client.Do(request)
	}
	if lookups != 2 || dials != 1 {
		t.Fatalf("DNS rebinding: lookups=%d dials=%d", lookups, dials)
	}
}

func TestWebPushNetworkRejectsRedirects(t *testing.T) {
	for _, destination := range []string{"https://127.0.0.1/private", "http://push.example/private"} {
		t.Run(destination, func(t *testing.T) {
			requests := 0
			client := newWebPushHTTPClient(nil, nil)
			client.Transport = webPushRoundTripper{next: webPushRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{destination}}, Body: io.NopCloser(strings.NewReader("secret response")), Request: r}, nil
			})}
			request, _ := http.NewRequest(http.MethodPost, "https://push.example/secret", nil)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if requests != 1 || response.StatusCode != http.StatusFound {
				t.Fatalf("redirect requests=%d status=%d", requests, response.StatusCode)
			}
		})
	}
}

func TestWebPushNetworkKeepsErrorsSecret(t *testing.T) {
	client := newWebPushHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("DNS provider leaked secret")
	}, nil)
	vapid, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := base64.RawURLEncoding.EncodeToString(vapid.PrivateKey().D.FillBytes(make([]byte, 32)))
	publicKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := newWebPushSender(vapid.PublicKeyString(), privateKey, "mailto:admin@example.com", client)
	if err != nil {
		t.Fatal(err)
	}
	result := sender.Send(t.Context(), "https://push.example/secret-endpoint",
		base64.RawURLEncoding.EncodeToString(publicKey.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(make([]byte, 16)), []byte(`{"title":"Test"}`), 300)
	if result.Outcome != OutcomeRetryable || result.Expired || result.Err == nil ||
		strings.Contains(result.Err.Error(), "secret") || strings.Contains(result.Err.Error(), "push.example") {
		t.Fatalf("Web Push error not generic and retryable: %v", result)
	}
}
