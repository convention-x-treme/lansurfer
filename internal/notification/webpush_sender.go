package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	webpush "github.com/marknefedov/go-webpush/v2"
)

type libraryWebPushSender struct {
	client  *webpush.Client
	keys    webpush.VAPIDKeys
	subject string
}

func newWebPushSender(publicKey, privateKey, subject string, httpClient webpush.HTTPClient) (*libraryWebPushSender, error) {
	if subject == "" {
		return nil, errors.New("Web Push VAPID subject required")
	}
	var keys webpush.VAPIDKeys
	keyJSON, err := json.Marshal(struct {
		PublicKey  string `json:"publicKey"`
		PrivateKey string `json:"privateKey"`
	}{strings.TrimRight(publicKey, "="), strings.TrimRight(privateKey, "=")})
	if err != nil {
		return nil, errors.New("invalid Web Push VAPID keys")
	}
	if err := json.Unmarshal(keyJSON, &keys); err != nil {
		return nil, errors.New("invalid Web Push VAPID keys")
	}
	return &libraryWebPushSender{client: webpush.NewClient(webpush.Config{HTTPClient: httpClient}), keys: keys, subject: subject}, nil
}

func (s *libraryWebPushSender) send(ctx context.Context, endpoint, p256dh, auth string, payload []byte, ttlSeconds int) sendOutcome {
	keys, err := webpush.DecodeSubscriptionKeys(auth, p256dh)
	if err != nil {
		return sendOutcome{Outcome: OutcomePermanent, Expired: true, Err: errors.New("invalid Web Push subscription keys")}
	}
	result, err := s.client.Send(ctx, payload, &webpush.Subscription{Endpoint: endpoint, Keys: keys}, webpush.SendOptions{
		Subject: s.subject, VAPIDKeys: &s.keys, TTL: min(ttlSeconds, 300),
	})
	if result != nil && result.Response != nil && result.Response.Body != nil {
		defer result.Response.Body.Close()
	}
	if err == nil {
		return sendOutcome{Outcome: OutcomeSucceeded}
	}
	var provider *webpush.PushServiceError
	if errors.As(err, &provider) {
		out := sendOutcome{Outcome: OutcomePermanent, Expired: provider.SubscriptionExpired, Err: fmt.Errorf("Web Push provider HTTP %d", provider.StatusCode)}
		if provider.Temporary || provider.StatusCode == http.StatusTooManyRequests || provider.StatusCode >= 500 {
			out.Outcome = OutcomeRetryable
			if value := provider.Header.Get("Retry-After"); value != "" {
				if seconds, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64); parseErr == nil && seconds >= 0 && seconds <= int64((1<<63-1)/int64(time.Second)) {
					delay := time.Duration(seconds) * time.Second
					out.RetryAfter = &delay
				} else if !provider.RetryAfter.IsZero() {
					delay := time.Until(provider.RetryAfter)
					out.RetryAfter = &delay
				}
			}
		}
		return out
	}
	return sendOutcome{Outcome: OutcomeRetryable, Err: errors.New("Web Push send failed")}
}
