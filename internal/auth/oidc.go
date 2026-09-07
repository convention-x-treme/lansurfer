package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type OIDCProvider struct {
	issuer   string
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// DiscoverOIDCProvider performs provider discovery once during application
// startup and constructs the long-lived token verifier/key cache.
func DiscoverOIDCProvider(ctx context.Context, cfg OIDCConfig) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	var metadata struct {
		Issuer string `json:"issuer"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read OIDC provider issuer: %w", err)
	}
	if metadata.Issuer == "" {
		return nil, errors.New("OIDC discovery document has no issuer")
	}
	return &OIDCProvider{
		issuer: metadata.Issuer,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

func (p *OIDCProvider) AuthorizationURL(state, nonce, verifier string) string {
	return p.oauth.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

func (p *OIDCProvider) Exchange(ctx context.Context, code, verifier, nonce string) (Profile, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Profile{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Profile{}, errors.New("OIDC token response has no ID token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Profile{}, fmt.Errorf("verify ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return Profile{}, errors.New("ID token nonce does not match login flow")
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Profile{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	return Profile{
		Provider:          p.issuer,
		Subject:           strings.TrimSpace(idToken.Subject),
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
		Email:             claims.Email,
	}, nil
}
