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
