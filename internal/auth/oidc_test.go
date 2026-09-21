package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestOIDCAuthorizationURLIncludesStateNonceAndPKCE(t *testing.T) {
	provider := &OIDCProvider{oauth: oauth2.Config{
		ClientID:    "lansurfer",
		RedirectURL: "https://lan.example/auth/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL: "https://identity.example/authorize",
		},
		Scopes: []string{"openid", "profile", "email"},
	}}

	authorizationURL := provider.AuthorizationURL("state", "nonce", "verifier")
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	wantChallenge := sha256.Sum256([]byte("verifier"))
	if query.Get("state") != "state" || query.Get("nonce") != "nonce" ||
		query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(wantChallenge[:]) ||
		query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization query = %v", query)
	}
	if query.Get("client_id") != "lansurfer" || query.Get("redirect_uri") != "https://lan.example/auth/callback" ||
		!strings.Contains(query.Get("scope"), "openid") {
		t.Fatalf("authorization query = %v", query)
	}
}

func TestTwitchAuthorizationURLRequestsOnlyOpenIDAndUsername(t *testing.T) {
	provider := &OIDCProvider{
		providerKind: "twitch",
		providerKey:  "twitch",
		oauth: oauth2.Config{
			ClientID: "lansurfer", RedirectURL: "https://lan.example/auth/twitch/callback",
			Endpoint: oauth2.Endpoint{AuthURL: "https://id.twitch.tv/oauth2/authorize"},
			Scopes:   []string{"openid"},
		},
		authorizationOptions: []oauth2.AuthCodeOption{twitchUsernameClaims()},
	}
	parsed, err := url.Parse(provider.AuthorizationURL("state", "nonce", "verifier"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("scope") != "openid" || query.Get("claims") != `{"id_token":{"preferred_username":null}}` {
		t.Fatalf("authorization query = %v", query)
	}
}

func TestOIDCProfileMapsTwitchUsernameWithoutEmail(t *testing.T) {
	provider := &OIDCProvider{providerKind: "twitch", providerKey: "twitch", usePreferredUsernameAsName: true}
	profile := provider.profileFromClaims(" twitch-subject ", map[string]any{
		"preferred_username": " NightOtter ", "email": "private@example.test",
	})
	if profile.ProviderKind != "twitch" || profile.ProviderKey != "twitch" || profile.Subject != "twitch-subject" ||
		profile.Name != "NightOtter" || profile.PreferredUsername != "NightOtter" || profile.GameNickSuggestion != "NightOtter" || profile.Email != "" {
		t.Fatalf("Profile = %#v", profile)
	}
}

func TestOIDCGameNickSuggestionUsesConfiguredClaimThenFallbacks(t *testing.T) {
	tests := []struct {
		name        string
		customClaim string
		claims      map[string]any
		want        string
	}{
		{name: "custom claim", customClaim: "nickname", claims: map[string]any{"nickname": " NightOtter ", "preferred_username": "fallback"}, want: "NightOtter"},
		{name: "non-string custom", customClaim: "nickname", claims: map[string]any{"nickname": 42, "preferred_username": "NightOtter"}, want: "NightOtter"},
		{name: "invalid custom", customClaim: "nickname", claims: map[string]any{"nickname": "bad\u200bnick", "preferred_username": "NightOtter"}, want: "NightOtter"},
		{name: "name fallback", customClaim: "nickname", claims: map[string]any{"name": "Alice Player"}, want: "Alice Player"},
		{name: "invalid candidates", customClaim: "nickname", claims: map[string]any{"nickname": "", "preferred_username": "bad\x00nick", "name": strings.Repeat("x", 33)}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := oidcGameNickSuggestion(test.claims, test.customClaim); got != test.want {
				t.Fatalf("oidcGameNickSuggestion() = %q, want %q", got, test.want)
			}
		})
	}
}
