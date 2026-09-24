package notification

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	webpush "github.com/marknefedov/go-webpush/v2"
)

type fakePushHTTPClient func(*http.Request) (*http.Response, error)

func (f fakePushHTTPClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestWebPushSenderMapsProviderOutcomes(t *testing.T) {
	vapid, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := base64.RawURLEncoding.EncodeToString(vapid.PrivateKey().D.FillBytes(make([]byte, 32)))
	clientKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	p256dh := base64.RawURLEncoding.EncodeToString(clientKey.PublicKey().Bytes())
	for _, tc := range []struct {
		name       string
		status     int
		retryAfter string
		network    bool
		outcome    Outcome
		expired    bool
	}{
		{"accepted", 201, "", false, OutcomeSucceeded, false},
		{"expired", 410, "", false, OutcomePermanent, true},
		{"invalid", 404, "", false, OutcomePermanent, true},
		{"rate limited", 429, "900", false, OutcomeRetryable, false},
		{"temporary", 503, "", false, OutcomeRetryable, false},
		{"network", 0, "", true, OutcomeRetryable, false},
		{"other 4xx", 403, "", false, OutcomePermanent, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			sender, err := newWebPushSender(vapid.PublicKeyString(), privateKey, "mailto:admin@example.com", fakePushHTTPClient(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("TTL") != "300" || req.Method != http.MethodPost || req.Header.Get("Authorization") == "" || req.Header.Get("Content-Encoding") != "aes128gcm" {
					t.Errorf("invalid Web Push request method or headers")
				}
				if tc.network {
					return nil, errors.New("secret endpoint failure")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.retryAfter}}, Body: io.NopCloser(strings.NewReader("secret provider body"))}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			result := sender.send(context.Background(), "https://push.example/subscription-secret", p256dh, auth, []byte(`{"title":"Test"}`), 3600)
			if calls != 1 || result.Outcome != tc.outcome || result.Expired != tc.expired {
				t.Fatalf("calls=%d outcome=%+v, want outcome=%v expired=%v", calls, result, tc.outcome, tc.expired)
			}
			if tc.outcome == OutcomeRetryable && tc.retryAfter != "" && (result.RetryAfter == nil || *result.RetryAfter != 900*time.Second) {
				t.Fatalf("Retry-After = %v, want 900 seconds", result.RetryAfter)
			}
			if result.Err != nil && (strings.Contains(result.Err.Error(), "secret") || strings.Contains(fmt.Sprint(result.Err), "push.example")) {
				t.Fatalf("sensitive provider material in error: %v", result.Err)
			}
		})
	}
}
