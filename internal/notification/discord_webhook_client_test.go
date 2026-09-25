package notification

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type discordRoundTrip func(*http.Request) (*http.Response, error)

func (f discordRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscordWebhookCreateEditAndDelete(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodDelete {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"allowed_mentions":{}`) || !strings.Contains(string(body), `"embeds":[{`) {
				t.Errorf("unsafe payload: %s", body)
			}
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"message-42"}`))
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newDiscordWebhookClient(server.Client(), server.URL+"?unused=1")
	credential := DiscordWebhookCredential{WebhookID: "webhook-id", WebhookToken: "webhook-secret"}
	message := buildDiscordMessage(GameRequestSummary{RequestID: "one"}, "https://play.example", "")
	created := client.CreateMessage(context.Background(), credential, message)
	if created.Status != http.StatusOK || created.MessageID != "message-42" || created.Err != nil {
		t.Fatalf("create result = %+v", created)
	}
	edited := client.EditMessage(context.Background(), credential, "message-42", message)
	if edited.Status != http.StatusNoContent || edited.Err != nil {
		t.Fatalf("edit result = %+v", edited)
	}
	if err := client.DeleteWebhook(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"POST /webhook-id/webhook-secret?unused=1&wait=true",
		"PATCH /webhook-id/webhook-secret/messages/message-42",
		"DELETE /webhook-id/webhook-secret",
	} {
		if !containsPath(paths, want) {
			t.Errorf("missing %q in %v", want, paths)
		}
	}
}
func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func TestDiscordWebhookProviderResponses(t *testing.T) {
	cases := []struct {
		name         string
		status, code int
		header, body string
		delay        time.Duration
	}{
		{"rate header", 429, 0, "1200.5", `{"retry_after":1}`, 1200500 * time.Millisecond},
		{"rate json", 429, 0, "", `{"retry_after":1500.25}`, 1500250 * time.Millisecond},
		{"unknown message", 404, 10008, "", `{"code":10008,"message":"Unknown Message"}`, 0},
		{"unknown webhook", 404, 10015, "", `{"code":10015,"message":"Unknown Webhook"}`, 0},
		{"bad request", 400, 0, "", `{"message":"webhook-secret"}`, 0},
		{"server failure", 503, 0, "", `{"message":"webhook-secret"}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newDiscordWebhookClient(&http.Client{Transport: discordRoundTrip(func(r *http.Request) (*http.Response, error) {
				headers := make(http.Header)
				if tc.header != "" {
					headers.Set("Retry-After", tc.header)
				}
				return &http.Response{StatusCode: tc.status, Header: headers, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}, "https://discord.invalid/api/webhooks")
			result := client.EditMessage(t.Context(), DiscordWebhookCredential{"webhook-id", "webhook-secret"}, "message-id", discordMessage{})
			if result.Status != tc.status || result.DiscordCode != tc.code || result.Err != nil {
				t.Fatalf("result = %+v", result)
			}
			if tc.delay > 0 && (result.RetryAfter == nil || *result.RetryAfter != tc.delay) {
				t.Fatalf("retry after = %v, want %v", result.RetryAfter, tc.delay)
			}
		})
	}
}

func TestDiscordWebhookNetworkTransmissionStateAndSecrets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport discordRoundTrip
		failure   discordTransmission
	}{
		{"dial known before transmission", func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: errors.New("webhook-secret")}
		}, discordBeforeTransmission},
		{"arbitrary network error ambiguous", func(*http.Request) (*http.Response, error) { return nil, errors.New("webhook-secret") }, discordTransmissionUnknown},
		{"lost response after write ambiguous", func(r *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, r.Body)
			return nil, errors.New("webhook-secret")
		}, discordTransmissionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newDiscordWebhookClient(&http.Client{Transport: tc.transport}, "https://discord.invalid/api/webhooks")
			result := client.CreateMessage(t.Context(), DiscordWebhookCredential{"webhook-id", "webhook-secret"}, discordMessage{})
			if result.Failure != tc.failure || result.Status != 0 || result.Err == nil {
				t.Fatalf("result = %+v", result)
			}
			if strings.Contains(result.Err.Error(), "webhook-secret") || strings.Contains(result.Err.Error(), "webhook-id") {
				t.Fatalf("leaked secret: %v", result.Err)
			}
		})
	}
}
