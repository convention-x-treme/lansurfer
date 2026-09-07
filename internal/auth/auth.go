// Package auth owns external identities, browser sessions, and authentication
// flows. It does not use provider identities as LANsurfer user IDs.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

const (
	defaultSessionLifetime = 7 * 24 * time.Hour
	randomTokenBytes       = 32
	maxIdentityNameLength  = 255
	maxIdentityEmailLength = 320
	maxProviderLength      = 2048
	maxSubjectLength       = 1024
	maxUserDisplayName     = 100
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
	Provider          string
	Subject           string
	Name              string
	PreferredUsername string
	Email             string
}

type Identity struct {
	ID          string
	UserID      string
	Provider    string
	Subject     string
	DisplayName string
	Email       string
	CreatedAt   time.Time
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

type LoginFlow struct {
	AuthorizationURL string
	State            string
	Nonce            string
	PKCEVerifier     string
}

// Provider represents one configured OIDC provider.
type Provider interface {
	AuthorizationURL(state, nonce, verifier string) string
	Exchange(context.Context, string, string, string) (Profile, error)
}

type IdentityStore interface {
	ByProviderSubject(context.Context, string, string) (Identity, error)
	Create(context.Context, Identity) (Identity, error)
	UpdateMetadata(context.Context, string, string, string, string) error
}

type SessionStore interface {
	Create(context.Context, [sha256.Size]byte, string, time.Time) error
	Resolve(context.Context, [sha256.Size]byte) (Session, error)
	Delete(context.Context, [sha256.Size]byte) error
}

// UserAccounts is the narrow user-module interface consumed by auth.
type UserAccounts interface {
	Register(context.Context, string) (user.User, error)
	ByID(context.Context, string) (user.User, error)
	RollbackRegistration(context.Context, string) error
}

type Service struct {
	provider        Provider
	identities      IdentityStore
	sessions        SessionStore
	users           UserAccounts
	now             func() time.Time
	sessionLifetime time.Duration
	generateToken   func() (string, error)
}

func NewService(provider Provider, identities IdentityStore, sessions SessionStore, users UserAccounts) *Service {
	return &Service{
		provider:        provider,
		identities:      identities,
		sessions:        sessions,
		users:           users,
		now:             time.Now,
		sessionLifetime: defaultSessionLifetime,
		generateToken:   randomToken,
	}
}

func (s *Service) Available() bool {
	return s != nil && s.provider != nil
}

func (s *Service) BeginLogin() (LoginFlow, error) {
	if !s.Available() {
		return LoginFlow{}, ErrUnavailable
	}
	state, err := s.generateToken()
	if err != nil {
		return LoginFlow{}, fmt.Errorf("generate state: %w", err)
	}
	nonce, err := s.generateToken()
	if err != nil {
		return LoginFlow{}, fmt.Errorf("generate nonce: %w", err)
	}
	verifier, err := s.generateToken()
	if err != nil {
		return LoginFlow{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	return LoginFlow{
		AuthorizationURL: s.provider.AuthorizationURL(state, nonce, verifier),
		State:            state,
		Nonce:            nonce,
		PKCEVerifier:     verifier,
	}, nil
}

func (s *Service) CompleteLogin(ctx context.Context, code, verifier, nonce string) (user.User, BrowserSession, error) {
	if !s.Available() {
		return user.User{}, BrowserSession{}, ErrUnavailable
	}
	if code == "" || verifier == "" || nonce == "" {
		return user.User{}, BrowserSession{}, ErrInvalidCallback
	}
	profile, err := s.provider.Exchange(ctx, code, verifier, nonce)
	if err != nil {
		return user.User{}, BrowserSession{}, fmt.Errorf("%w: exchange OIDC callback: %w", ErrInvalidCallback, err)
	}
	profile = normalizeProfile(profile)
	if err := validateProfile(profile); err != nil {
		return user.User{}, BrowserSession{}, err
	}

	account, err := s.accountForProfile(ctx, profile)
	if err != nil {
		return user.User{}, BrowserSession{}, err
	}

	token, err := s.generateToken()
	if err != nil {
		return user.User{}, BrowserSession{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := s.now().Add(s.sessionLifetime)
	if err := s.sessions.Create(ctx, hashToken(token), account.ID, expiresAt); err != nil {
		return user.User{}, BrowserSession{}, fmt.Errorf("create session: %w", err)
	}
	return account, BrowserSession{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (user.User, error) {
	if strings.TrimSpace(token) == "" {
		return user.User{}, ErrSessionNotFound
	}
	session, err := s.sessions.Resolve(ctx, hashToken(token))
	if err != nil {
		return user.User{}, err
	}
	return s.users.ByID(ctx, session.UserID)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if err := s.sessions.Delete(ctx, hashToken(token)); errors.Is(err, ErrSessionNotFound) {
		return nil
	} else {
		return err
	}
}

func (s *Service) accountForProfile(ctx context.Context, profile Profile) (user.User, error) {
	identity, err := s.identities.ByProviderSubject(ctx, profile.Provider, profile.Subject)
	if err == nil {
		if err := s.identities.UpdateMetadata(ctx, profile.Provider, profile.Subject, profile.Name, profile.Email); err != nil {
			return user.User{}, fmt.Errorf("update identity metadata: %w", err)
		}
		return s.users.ByID(ctx, identity.UserID)
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return user.User{}, fmt.Errorf("find identity: %w", err)
	}

	account, err := s.users.Register(ctx, displayName(profile))
	if err != nil {
		return user.User{}, fmt.Errorf("register user: %w", err)
	}
	_, linkErr := s.identities.Create(ctx, Identity{
		UserID:      account.ID,
		Provider:    profile.Provider,
		Subject:     profile.Subject,
		DisplayName: profile.Name,
		Email:       profile.Email,
	})
	if linkErr == nil {
		return account, nil
	}

	rollbackErr := s.users.RollbackRegistration(ctx, account.ID)
	if errors.Is(linkErr, ErrIdentityExists) && rollbackErr == nil {
		identity, findErr := s.identities.ByProviderSubject(ctx, profile.Provider, profile.Subject)
		if findErr != nil {
			return user.User{}, fmt.Errorf("resolve concurrent identity: %w", findErr)
		}
		return s.users.ByID(ctx, identity.UserID)
	}
	return user.User{}, errors.Join(fmt.Errorf("link identity: %w", linkErr), rollbackErr)
}

func normalizeProfile(profile Profile) Profile {
	profile.Provider = strings.TrimSpace(profile.Provider)
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
	return profile
}

func validateProfile(profile Profile) error {
	if profile.Provider == "" || len(profile.Provider) > maxProviderLength ||
		profile.Subject == "" || len(profile.Subject) > maxSubjectLength {
		return ErrInvalidProfile
	}
	return nil
}

func displayName(profile Profile) string {
	name := profile.Name
	if name == "" {
		name = profile.PreferredUsername
	}
	if name == "" {
		name = "Player"
	}
	runes := []rune(name)
	if len(runes) > maxUserDisplayName {
		name = string(runes[:maxUserDisplayName])
	}
	return name
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
