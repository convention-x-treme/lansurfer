package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	GameNickClaim string
}

type TwitchConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type oidcProviderConfig struct {
	OIDCConfig
	ProviderKind               string
	ProviderKey                string
	Scopes                     []string
	AuthorizationOptions       []oauth2.AuthCodeOption
	UsePreferredUsernameAsName bool
	IncludeEmail               bool
}

type OIDCProvider struct {
	providerKind               string
	providerKey                string
	oauth                      oauth2.Config
	verifier                   *oidc.IDTokenVerifier
	gameNickClaim              string
	authorizationOptions       []oauth2.AuthCodeOption
	usePreferredUsernameAsName bool
	includeEmail               bool
}

func (p *OIDCProvider) ProviderKind() string { return p.providerKind }
func (p *OIDCProvider) ProviderKey() string  { return p.providerKey }

// DiscoverOIDCProvider performs provider discovery once during application
// startup and constructs the long-lived token verifier/key cache.
func DiscoverOIDCProvider(ctx context.Context, cfg OIDCConfig) (*OIDCProvider, error) {
	return discoverOIDCProvider(ctx, oidcProviderConfig{
		OIDCConfig: cfg, ProviderKind: "oidc", Scopes: []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}, IncludeEmail: true,
	})
}

func DiscoverTwitchProvider(ctx context.Context, cfg TwitchConfig) (*OIDCProvider, error) {
	return discoverOIDCProvider(ctx, oidcProviderConfig{
		OIDCConfig:   OIDCConfig{IssuerURL: "https://id.twitch.tv/oauth2", ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL},
		ProviderKind: "twitch", ProviderKey: "twitch", Scopes: []string{oidc.ScopeOpenID},
		AuthorizationOptions: []oauth2.AuthCodeOption{twitchUsernameClaims()}, UsePreferredUsernameAsName: true,
	})
}

func discoverOIDCProvider(ctx context.Context, cfg oidcProviderConfig) (*OIDCProvider, error) {
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
	providerKey := cfg.ProviderKey
	if providerKey == "" {
		providerKey = metadata.Issuer
	}
	return &OIDCProvider{
		providerKind: cfg.ProviderKind,
		providerKey:  providerKey,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
		verifier:                   provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		gameNickClaim:              cfg.GameNickClaim,
		authorizationOptions:       cfg.AuthorizationOptions,
		usePreferredUsernameAsName: cfg.UsePreferredUsernameAsName,
		includeEmail:               cfg.IncludeEmail,
	}, nil
}

func (p *OIDCProvider) AuthorizationURL(state, nonce, verifier string) string {
	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce)}
	options = append(options, p.authorizationOptions...)
	return p.oauth.AuthCodeURL(state, options...)
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
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return Profile{}, fmt.Errorf("decode ID token claims: %w", err)
	}
	return p.profileFromClaims(idToken.Subject, claims), nil
}

func (p *OIDCProvider) profileFromClaims(subject string, claims map[string]any) Profile {
	preferredUsername := strings.TrimSpace(stringClaim(claims, "preferred_username"))
	name := strings.TrimSpace(stringClaim(claims, "name"))
	if p.usePreferredUsernameAsName {
		name = preferredUsername
	}
	email := ""
	if p.includeEmail {
		email = stringClaim(claims, "email")
	}
	return Profile{
		ProviderKind:       p.providerKind,
		ProviderKey:        p.providerKey,
		Subject:            strings.TrimSpace(subject),
		Name:               name,
		PreferredUsername:  preferredUsername,
		Email:              email,
		GameNickSuggestion: oidcGameNickSuggestion(claims, p.gameNickClaim),
	}
}

func twitchUsernameClaims() oauth2.AuthCodeOption {
	return oauth2.SetAuthURLParam("claims", `{"id_token":{"preferred_username":null}}`)
}

func oidcGameNickSuggestion(claims map[string]any, customClaim string) string {
	for _, claim := range []string{strings.TrimSpace(customClaim), "preferred_username", "name"} {
		if claim == "" {
			continue
		}
		candidate, ok := claims[claim].(string)
		if !ok {
			continue
		}
		validated, err := user.ValidateGameNick(candidate)
		if err == nil {
			return validated
		}
	}
	return ""
}

func stringClaim(claims map[string]any, name string) string {
	value, _ := claims[name].(string)
	return value
}
