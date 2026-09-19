package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"
)

func TestDiscordAuthorizationUsesIdentifyOnly(t *testing.T) {
	provider := NewDiscordProvider(DiscordConfig{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "https://lan.example/auth/discord/callback",
	})
	parsed, err := url.Parse(provider.AuthorizationURL("state", "unused-nonce", "unused-verifier"))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("scope") != "identify" || query.Get("state") != "state" || query.Get("response_type") != "code" {
		t.Fatalf("authorization query = %v", query)
	}
	if query.Get("redirect_uri") != "https://lan.example/auth/discord/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Has("email") || query.Get("scope") == "email" {
		t.Fatalf("authorization query requested email: %v", query)
	}
}

func TestDiscordExchangeFetchesCurrentUserWithoutExposingEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "code" ||
				r.Form.Get("redirect_uri") != "https://lan.example/auth/discord/callback" {
				t.Fatalf("token form = %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "temporary-token", "token_type": "Bearer", "expires_in": 3600, "scope": "identify"})
		case "/api/v10/users/@me":
			if r.Header.Get("Authorization") != "Bearer temporary-token" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "123456789", "global_name": " NightOtter ", "username": "fallback-user", "email": "alice@example.test",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewDiscordProvider(DiscordConfig{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "https://lan.example/auth/discord/callback",
	})
	provider.oauth.Endpoint = oauth2.Endpoint{AuthURL: server.URL + "/oauth2/authorize", TokenURL: server.URL + "/oauth2/token"}
	provider.userURL = server.URL + "/api/v10/users/@me"
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, server.Client())

	profile, err := provider.Exchange(ctx, "code", "unused-verifier", "unused-nonce")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if profile.ProviderKind != "discord" || profile.ProviderKey != "discord" || profile.Subject != "123456789" {
		t.Fatalf("profile identity = %#v", profile)
	}
	if profile.Name != "NightOtter" || profile.PreferredUsername != "fallback-user" || profile.GameNickSuggestion != "NightOtter" {
		t.Fatalf("profile names = %#v", profile)
	}
	if profile.Email != "" {
		t.Fatalf("profile email = %q, want empty", profile.Email)
	}
}

func TestDiscordGameNickSuggestionFallsBackToUsername(t *testing.T) {
	if got := discordGameNickSuggestion("bad\u200bnick", "fallback-user"); got != "fallback-user" {
		t.Fatalf("discordGameNickSuggestion() = %q", got)
	}
	if got := discordGameNickSuggestion("", "bad\x00user"); got != "" {
		t.Fatalf("discordGameNickSuggestion() = %q, want empty", got)
	}
}
