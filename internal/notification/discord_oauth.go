package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	discordWebhookAuthorizationURL = "https://discord.com/oauth2/authorize"
	discordWebhookTokenURL         = "https://discord.com/api/oauth2/token"
	// discordWebhookAPIURL is the webhook-token management endpoint base:
	// DELETE {discordWebhookAPIURL}/{id}/{token} deletes a webhook using
	// only its own credential, with no bot/gateway/OAuth-token involved.
	discordWebhookAPIURL = "https://discord.com/api/webhooks"
	// discordWebhookScope is the only scope this client ever requests.
	// Discord delivery must never request identify, guilds, bot, or
	// applications.commands: those belong to Discord login or a bot/gateway
	// integration, neither of which is part of E3.
	discordWebhookScope    = "webhook.incoming"
	discordWebhookGrant    = "authorization_code"
	discordTokenBodyLimit  = 1 << 20
	discordTokenClientForm = "application/x-www-form-urlencoded"
)

// ErrDiscordWebhookOAuthResponseInvalid means a `webhook.incoming` token
// response did not carry a usable webhook object (missing/malformed
// webhook, or a required field empty). It never carries the raw response
// body, since a malformed response could otherwise still contain a valid
// OAuth access/refresh token that must never reach an error string.
var ErrDiscordWebhookOAuthResponseInvalid = errors.New("discord webhook oauth response did not include a usable webhook")

// DiscordWebhookOAuthConfig configures DiscordWebhookOAuthClient. HTTPClient
// is injected so tests never perform a real network call; it defaults to
// http.DefaultClient when nil.
type DiscordWebhookOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

// DiscordWebhookOAuthClient implements DiscordWebhookOAuthProvider using
// Discord's `webhook.incoming` authorization-code flow. It is deliberately
// separate from internal/auth's Discord login provider: different
// credentials, different scope, and it discards the OAuth access/refresh
// token immediately after extracting the created webhook's credential.
type DiscordWebhookOAuthClient struct {
	clientID     string
	clientSecret string
	redirectURL  string
	httpClient   *http.Client
}

func NewDiscordWebhookOAuthClient(cfg DiscordWebhookOAuthConfig) *DiscordWebhookOAuthClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &DiscordWebhookOAuthClient{
		clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, redirectURL: cfg.RedirectURL,
		httpClient: httpClient,
	}
}

// AuthorizationURL builds the Discord authorization URL for state using
// only the delivery client ID, the dedicated delivery redirect URI, and the
// exact `webhook.incoming` scope.
func (c *DiscordWebhookOAuthClient) AuthorizationURL(state string) string {
	query := url.Values{
		"client_id":     {c.clientID},
		"redirect_uri":  {c.redirectURL},
		"response_type": {"code"},
		"scope":         {discordWebhookScope},
		"state":         {state},
	}
	return discordWebhookAuthorizationURL + "?" + query.Encode()
}

// discordWebhookTokenResponse is the shape of a successful `webhook.incoming`
// token response. AccessToken and RefreshToken are read only long enough to
// be discarded by Exchange: they are never copied into
// DiscordWebhookOAuthResult, an error, or a log.
type discordWebhookTokenResponse struct {
	AccessToken  string                `json:"access_token"`
	RefreshToken string                `json:"refresh_token"`
	Webhook      *discordWebhookObject `json:"webhook"`
}

type discordWebhookObject struct {
	ID        string  `json:"id"`
	Token     string  `json:"token"`
	GuildID   string  `json:"guild_id"`
	ChannelID string  `json:"channel_id"`
	Name      *string `json:"name"`
}

// Exchange trades code at Discord's token endpoint for the created
// webhook's credential and safe metadata. The OAuth access token and
// refresh token are read into a local, unexported struct and never appear
// in the returned DiscordWebhookOAuthResult, in a returned error, or in any
// log: this function is the only place they ever exist in memory.
func (c *DiscordWebhookOAuthClient) Exchange(ctx context.Context, code string) (DiscordWebhookOAuthResult, error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {discordWebhookGrant},
		"code":          {code},
		"redirect_uri":  {c.redirectURL},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, discordWebhookTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return DiscordWebhookOAuthResult{}, fmt.Errorf("create discord webhook oauth token request: %w", err)
	}
	request.Header.Set("Content-Type", discordTokenClientForm)
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return DiscordWebhookOAuthResult{}, fmt.Errorf("exchange discord webhook oauth code: %w", err)
	}
	defer response.Body.Close()

	body := io.LimitReader(response.Body, discordTokenBodyLimit)
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, body)
		return DiscordWebhookOAuthResult{}, fmt.Errorf("exchange discord webhook oauth code: status %d", response.StatusCode)
	}

	var token discordWebhookTokenResponse
	if err := json.NewDecoder(body).Decode(&token); err != nil {
		return DiscordWebhookOAuthResult{}, fmt.Errorf("decode discord webhook oauth response: %w", err)
	}
	// token.AccessToken and token.RefreshToken are intentionally never read
	// again below; they are discarded when this function returns.

	webhook := token.Webhook
	if webhook == nil || webhook.ID == "" || webhook.Token == "" || webhook.GuildID == "" || webhook.ChannelID == "" {
		return DiscordWebhookOAuthResult{}, ErrDiscordWebhookOAuthResponseInvalid
	}
	name := ""
	if webhook.Name != nil {
		name = strings.TrimSpace(*webhook.Name)
	}
	return DiscordWebhookOAuthResult{
		Credential:  DiscordWebhookCredential{WebhookID: webhook.ID, WebhookToken: webhook.Token},
		GuildID:     webhook.GuildID,
		ChannelID:   webhook.ChannelID,
		WebhookName: name,
	}, nil
}

// DeleteWebhook best-effort deletes the webhook identified by credential
// using the webhook-token management endpoint, which needs only the
// webhook's own ID/token: no bot, gateway, or OAuth access token is
// involved. Callers must treat any error as non-fatal cleanup failure;
// this method never distinguishes "already gone" from other failures, so
// callers must not infer destination state from it.
func (c *DiscordWebhookOAuthClient) DeleteWebhook(ctx context.Context, credential DiscordWebhookCredential) error {
	endpoint := discordWebhookAPIURL + "/" + url.PathEscape(credential.WebhookID) + "/" + url.PathEscape(credential.WebhookToken)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create discord webhook delete request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("delete discord webhook: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, discordTokenBodyLimit))
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete discord webhook: status %d", response.StatusCode)
	}
	return nil
}
