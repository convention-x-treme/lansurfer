package notification

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rewriteHostTransport forwards every request to a fixed test-server host
// regardless of the request's original URL, so tests never perform a real
// network call to discord.com while still exercising
// DiscordWebhookOAuthClient's real request construction.
type rewriteHostTransport struct {
	target *url.URL
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func testDiscordWebhookOAuthClient(t *testing.T, handler http.HandlerFunc) *DiscordWebhookOAuthClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return NewDiscordWebhookOAuthClient(DiscordWebhookOAuthConfig{
		ClientID: "delivery-client-id", ClientSecret: "delivery-client-secret",
		RedirectURL: "https://lansurfer.example/admin/discord/callback",
		HTTPClient:  &http.Client{Transport: rewriteHostTransport{target: target}},
	})
}

func TestDiscordWebhookAuthorizationURLExactScope(t *testing.T) {
	client := NewDiscordWebhookOAuthClient(DiscordWebhookOAuthConfig{
		ClientID: "delivery-client-id", RedirectURL: "https://lansurfer.example/admin/discord/callback",
	})

	parsed, err := url.Parse(client.AuthorizationURL("state-value"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != "https://discord.com/oauth2/authorize" {
		t.Fatalf("authorization URL base = %q", parsed.String())
	}
	query := parsed.Query()
	if got := query["scope"]; len(got) != 1 || got[0] != "webhook.incoming" {
		t.Fatalf("scope = %#v, want exactly [\"webhook.incoming\"]", got)
	}
	for _, forbidden := range []string{"identify", "guilds", "bot", "applications.commands"} {
		if strings.Contains(query.Get("scope"), forbidden) {
			t.Fatalf("scope %q must not contain forbidden scope %q", query.Get("scope"), forbidden)
		}
	}
}

func TestDiscordWebhookAuthorizationURLState(t *testing.T) {
	client := NewDiscordWebhookOAuthClient(DiscordWebhookOAuthConfig{
		ClientID: "delivery-client-id", RedirectURL: "https://lansurfer.example/admin/discord/callback",
	})

	parsed, err := url.Parse(client.AuthorizationURL("the-generated-state"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Query().Get("state") != "the-generated-state" {
		t.Fatalf("state = %q, want %q", parsed.Query().Get("state"), "the-generated-state")
	}
}

func TestDiscordWebhookAuthorizationURLRedirectURI(t *testing.T) {
	client := NewDiscordWebhookOAuthClient(DiscordWebhookOAuthConfig{
		ClientID: "delivery-client-id", RedirectURL: "https://lansurfer.example/admin/discord/callback",
	})

	parsed, err := url.Parse(client.AuthorizationURL("state"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != "https://lansurfer.example/admin/discord/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if got := parsed.Query().Get("client_id"); got != "delivery-client-id" {
		t.Fatalf("client_id = %q", got)
	}
}

const discordWebhookTokenFixture = `{
	"access_token": "super-secret-access-token",
	"token_type": "Bearer",
	"expires_in": 604800,
	"refresh_token": "super-secret-refresh-token",
	"scope": "webhook.incoming",
	"webhook": {
		"type": 1,
		"id": "webhook-id-123",
		"name": "Game Requests",
		"avatar": null,
		"channel_id": "channel-456",
		"guild_id": "guild-789",
		"application_id": "app-1",
		"token": "webhook-token-secret"
	}
}`

func TestDiscordWebhookExchangeExtractsCredentialAndSafeMetadata(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "the-code" {
			t.Fatalf("token request form = %#v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(discordWebhookTokenFixture))
	})

	result, err := client.Exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if result.Credential.WebhookID != "webhook-id-123" || result.Credential.WebhookToken != "webhook-token-secret" {
		t.Fatalf("Credential = %#v", result.Credential)
	}
	if result.GuildID != "guild-789" || result.ChannelID != "channel-456" || result.WebhookName != "Game Requests" {
		t.Fatalf("safe metadata = %#v", result)
	}
}

func TestDiscordWebhookExchangeAccessTokenNeverLeavesAdapter(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(discordWebhookTokenFixture))
	})

	result, err := client.Exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	rendered := fmt.Sprintf("%#v", result)
	if strings.Contains(rendered, "super-secret-access-token") {
		t.Fatalf("result leaked the OAuth access token: %s", rendered)
	}
}

func TestDiscordWebhookExchangeRefreshTokenNeverLeavesAdapter(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(discordWebhookTokenFixture))
	})

	result, err := client.Exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	rendered := fmt.Sprintf("%#v", result)
	if strings.Contains(rendered, "super-secret-refresh-token") {
		t.Fatalf("result leaked the OAuth refresh token: %s", rendered)
	}
}

func TestDiscordWebhookExchangeMalformedWebhookFailsClosedWithoutLeakingSecrets(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A response that carries real OAuth secrets but no usable webhook
		// object (e.g. Discord returned webhook.incoming's created webhook
		// with a missing/blank token, which must never surface).
		_, _ = w.Write([]byte(`{
			"access_token": "super-secret-access-token",
			"refresh_token": "super-secret-refresh-token",
			"webhook": {"id": "webhook-id-123", "token": "", "guild_id": "guild-789", "channel_id": "channel-456"}
		}`))
	})

	_, err := client.Exchange(context.Background(), "the-code")
	if err == nil {
		t.Fatal("Exchange() with a malformed webhook succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "did not include a usable webhook") {
		t.Fatalf("Exchange() error = %v, want ErrDiscordWebhookOAuthResponseInvalid", err)
	}
	if strings.Contains(err.Error(), "super-secret-access-token") || strings.Contains(err.Error(), "super-secret-refresh-token") {
		t.Fatalf("error leaked an OAuth secret: %v", err)
	}
}

func TestDiscordWebhookExchangeMissingWebhookFailsClosed(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token": "token", "refresh_token": "refresh"}`))
	})

	_, err := client.Exchange(context.Background(), "the-code")
	if err == nil {
		t.Fatal("Exchange() with no webhook object succeeded, want an error")
	}
}

func TestDiscordWebhookExchangeHTTPFailureIsClassifiedNotRetried(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"access_token": "super-secret-access-token", "error": "server_error"}`))
	})

	_, err := client.Exchange(context.Background(), "the-code")
	if err == nil {
		t.Fatal("Exchange() with a 500 response succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("Exchange() error = %v, want it to name the HTTP status", err)
	}
	if strings.Contains(err.Error(), "super-secret-access-token") {
		t.Fatalf("error leaked an OAuth secret from a failure body: %v", err)
	}
}

func TestDiscordWebhookExchangeMalformedJSONFailsClosed(t *testing.T) {
	client := testDiscordWebhookOAuthClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	})

	_, err := client.Exchange(context.Background(), "the-code")
	if err == nil {
		t.Fatal("Exchange() with malformed JSON succeeded, want an error")
	}
}
