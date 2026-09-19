package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

func TestBeginLoginCreatesIndependentFlowSecrets(t *testing.T) {
	provider := &stubProvider{}
	service := NewService(provider, &stubIdentityStore{}, &stubSessionStore{}, &stubUsers{})
	tokens := []string{"state", "nonce", "verifier"}
	service.generateToken = func() (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	}

	flow, err := service.BeginLogin()
	if err != nil {
		t.Fatalf("BeginLogin() error = %v", err)
	}
	if flow.State != "state" || flow.Nonce != "nonce" || flow.PKCEVerifier != "verifier" {
		t.Fatalf("flow = %#v", flow)
	}
	if provider.authorizationState != "state" || provider.authorizationNonce != "nonce" || provider.authorizationVerifier != "verifier" {
		t.Fatalf("provider arguments = %q, %q, %q", provider.authorizationState, provider.authorizationNonce, provider.authorizationVerifier)
	}
}

func TestCompleteLoginRegistersLocalUserAndStoresHashedSession(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	provider := &stubProvider{profile: Profile{
		Provider:          " https://identity.example/realms/lan ",
		Subject:           " subject-1 ",
		PreferredUsername: " player-one ",
		Email:             " player@example.test ",
	}}
	identities := &stubIdentityStore{findErr: ErrIdentityNotFound}
	sessions := &stubSessionStore{}
	users := &stubUsers{registered: user.User{ID: "user-1", DisplayName: "player-one", Role: user.RoleUser}}
	service := NewService(provider, identities, sessions, users)
	service.now = func() time.Time { return fixedNow }
	service.generateToken = func() (string, error) { return "opaque-browser-token", nil }

	account, session, err := service.CompleteLogin(t.Context(), "code", "verifier", "nonce")
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if account.ID != "user-1" || users.registeredName != "player-one" {
		t.Fatalf("account = %#v, registered name = %q", account, users.registeredName)
	}
	if identities.created.ProviderKind != "oidc" || identities.created.ProviderKey != "https://identity.example/realms/lan" || identities.created.Subject != "subject-1" {
		t.Fatalf("created identity = %#v", identities.created)
	}
	if sessions.createdHash != sha256.Sum256([]byte(session.Token)) {
		t.Error("session store did not receive the token hash")
	}
	if sessions.createdUserID != account.ID || !session.ExpiresAt.Equal(fixedNow.Add(defaultSessionLifetime)) {
		t.Fatalf("stored session user/expiry = %q, %v", sessions.createdUserID, session.ExpiresAt)
	}
	if provider.exchangeCode != "code" || provider.exchangeVerifier != "verifier" || provider.exchangeNonce != "nonce" {
		t.Fatalf("exchange arguments = %q, %q, %q", provider.exchangeCode, provider.exchangeVerifier, provider.exchangeNonce)
	}
}

func TestCompleteLoginReusesIdentityWithoutMergingByEmail(t *testing.T) {
	provider := &stubProvider{profile: Profile{Provider: "issuer", Subject: "subject", Name: "Current name", Email: "same@example.test"}}
	identities := &stubIdentityStore{found: Identity{UserID: "existing-user"}}
	users := &stubUsers{byID: user.User{ID: "existing-user", DisplayName: "Local name", Role: user.RoleAdmin}}
	service := NewService(provider, identities, &stubSessionStore{}, users)
	service.generateToken = func() (string, error) { return "token", nil }

	account, _, err := service.CompleteLogin(t.Context(), "code", "verifier", "nonce")
	if err != nil {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if account.ID != "existing-user" || users.registerCalls != 0 {
		t.Fatalf("account = %#v, register calls = %d", account, users.registerCalls)
	}
	if identities.updatedName != "Current name" || identities.updatedEmail != "same@example.test" {
		t.Fatalf("updated identity metadata = %q, %q", identities.updatedName, identities.updatedEmail)
	}
}

func TestAuthenticateAndLogoutUseHashedOpaqueToken(t *testing.T) {
	wantHash := sha256.Sum256([]byte("opaque-token"))
	sessions := &stubSessionStore{resolved: Session{UserID: "user-1"}}
	users := &stubUsers{byID: user.User{ID: "user-1", DisplayName: "Player"}}
	service := NewService(&stubProvider{}, &stubIdentityStore{}, sessions, users)

	account, err := service.Authenticate(t.Context(), "opaque-token")
	if err != nil || account.ID != "user-1" {
		t.Fatalf("Authenticate() = %#v, %v", account, err)
	}
	if sessions.resolvedHash != wantHash {
		t.Error("Resolve received a raw or unexpected token")
	}
	if err := service.Logout(t.Context(), "opaque-token"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if sessions.deletedHash != wantHash {
		t.Error("Delete received a raw or unexpected token")
	}
}

func TestUnavailableServiceRejectsLogin(t *testing.T) {
	service := NewService(nil, &stubIdentityStore{}, &stubSessionStore{}, &stubUsers{})
	if _, err := service.BeginLogin(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("BeginLogin() error = %v, want ErrUnavailable", err)
	}
}

type stubProvider struct {
	profile               Profile
	exchangeErr           error
	authorizationState    string
	authorizationNonce    string
	authorizationVerifier string
	exchangeCode          string
	exchangeVerifier      string
	exchangeNonce         string
}

func (p *stubProvider) AuthorizationURL(state, nonce, verifier string) string {
	p.authorizationState, p.authorizationNonce, p.authorizationVerifier = state, nonce, verifier
	return "https://identity.example/authorize"
}

func (p *stubProvider) Exchange(_ context.Context, code, verifier, nonce string) (Profile, error) {
	p.exchangeCode, p.exchangeVerifier, p.exchangeNonce = code, verifier, nonce
	return p.profile, p.exchangeErr
}

type stubIdentityStore struct {
	found        Identity
	findErr      error
	created      Identity
	createErr    error
	updatedName  string
	updatedEmail string
	listed       []Identity
}

func (s *stubIdentityStore) ByProviderSubject(context.Context, string, string, string) (Identity, error) {
	return s.found, s.findErr
}

func (s *stubIdentityStore) ListByUser(context.Context, string) ([]Identity, error) {
	return s.listed, nil
}

func (s *stubIdentityStore) Create(_ context.Context, identity Identity) (Identity, error) {
	s.created = identity
	return identity, s.createErr
}

func (s *stubIdentityStore) UpdateMetadata(_ context.Context, _, _, _, name, email string) error {
	s.updatedName, s.updatedEmail = name, email
	return nil
}

func (*stubIdentityStore) Unlink(context.Context, string, string) error { return nil }

type stubSessionStore struct {
	createdHash   [sha256.Size]byte
	createdUserID string
	resolvedHash  [sha256.Size]byte
	deletedHash   [sha256.Size]byte
	resolved      Session
}

func (s *stubSessionStore) Create(_ context.Context, hash [sha256.Size]byte, userID string, _ time.Time) error {
	s.createdHash, s.createdUserID = hash, userID
	return nil
}

func (s *stubSessionStore) Resolve(_ context.Context, hash [sha256.Size]byte) (Session, error) {
	s.resolvedHash = hash
	return s.resolved, nil
}

func (s *stubSessionStore) Delete(_ context.Context, hash [sha256.Size]byte) error {
	s.deletedHash = hash
	return nil
}

type stubUsers struct {
	registered     user.User
	registeredName string
	registerCalls  int
	byID           user.User
}

func (s *stubUsers) Register(_ context.Context, name string) (user.User, error) {
	s.registerCalls++
	s.registeredName = name
	return s.registered, nil
}

func (s *stubUsers) ByID(context.Context, string) (user.User, error) {
	return s.byID, nil
}

func (s *stubUsers) RollbackRegistration(context.Context, string) error { return nil }
