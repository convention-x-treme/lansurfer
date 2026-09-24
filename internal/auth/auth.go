// Package auth owns external identities, browser sessions, and authentication
// flows. It does not use provider identities as LANsurfer user IDs.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultSessionLifetime = 7 * 24 * time.Hour
	randomTokenBytes       = 32
	maxIdentityNameLength  = 255
	maxIdentityEmailLength = 320
	maxProviderLength      = 2048
	maxSubjectLength       = 1024
)

var (
	ErrUnavailable      = errors.New("authentication unavailable")
	ErrInvalidCallback  = errors.New("invalid authentication callback")
	ErrInvalidProfile   = errors.New("invalid identity profile")
	ErrIdentityNotFound = errors.New("identity not found")
	ErrIdentityExists   = errors.New("identity already exists")
	ErrSessionNotFound  = errors.New("session not found")
)

type Profile struct {
	Provider           string
	ProviderKind       string
	ProviderKey        string
	Subject            string
	Name               string
	PreferredUsername  string
	Email              string
	GameNickSuggestion string
	// AvatarRef is optional provider picture metadata: a Discord avatar hash,
	// or an https picture URL for Twitch/OIDC. Invalid values are dropped.
	AvatarRef string
}

type Identity struct {
	ID           string
	UserID       string
	ProviderKind string
	ProviderKey  string
	Subject      string
	DisplayName  string
	Email        string
	AvatarRef    string
	// AvatarKey is the public same-origin cache key derived from AvatarRef;
	// empty when the identity offers no picture.
	AvatarKey      string
	AvatarSelected bool
	CreatedAt      time.Time
}

type Session struct {
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type BrowserSession struct {
	Token     string
	ExpiresAt time.Time
}

// Provider represents one configured OAuth2/OIDC provider adapter.
type Provider interface {
	AuthorizationURL(state, nonce, verifier string) string
	Exchange(context.Context, string, string, string) (Profile, error)
}

type IdentityStore interface {
	ByProviderSubject(context.Context, string, string, string) (Identity, error)
	ListByUser(context.Context, string) ([]Identity, error)
	Create(context.Context, Identity) (Identity, error)
	UpdateMetadata(context.Context, Profile) error
	Unlink(context.Context, string, string) error
}

type SessionStore interface {
	Create(context.Context, [sha256.Size]byte, string, time.Time) error
	Resolve(context.Context, [sha256.Size]byte) (Session, error)
	Delete(context.Context, [sha256.Size]byte) error
}

func normalizeProfile(profile Profile) Profile {
	profile.Provider = strings.TrimSpace(profile.Provider)
	profile.ProviderKind = strings.TrimSpace(profile.ProviderKind)
	profile.ProviderKey = strings.TrimSpace(profile.ProviderKey)
	if profile.ProviderKind == "" && profile.Provider != "" {
		profile.ProviderKind, profile.ProviderKey = "oidc", profile.Provider
	}
	profile.Subject = strings.TrimSpace(profile.Subject)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.PreferredUsername = strings.TrimSpace(profile.PreferredUsername)
	profile.Email = strings.TrimSpace(profile.Email)
	if utf8.RuneCountInString(profile.Name) > maxIdentityNameLength {
		profile.Name = string([]rune(profile.Name)[:maxIdentityNameLength])
	}
	if utf8.RuneCountInString(profile.Email) > maxIdentityEmailLength {
		profile.Email = ""
	}
	profile.AvatarRef = normalizeAvatarRef(profile.ProviderKind, strings.TrimSpace(profile.AvatarRef))
	return profile
}

func validateProfile(profile Profile) error {
	if profile.ProviderKind == "" || (profile.ProviderKind != "oidc" && profile.ProviderKind != "discord" && profile.ProviderKind != "twitch") ||
		profile.ProviderKey == "" || len(profile.ProviderKey) > maxProviderLength ||
		profile.Subject == "" || len(profile.Subject) > maxSubjectLength {
		return ErrInvalidProfile
	}
	return nil
}

func randomToken() (string, error) {
	value := make([]byte, randomTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}
