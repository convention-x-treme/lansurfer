package auth

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

func TestNewSessionStoresHashedOpaqueToken(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	store := &stubSessionStore{}
	service := NewSessionService(store, &stubUsers{})
	service.now = func() time.Time { return fixedNow }
	service.generateToken = func() (string, error) { return "opaque-session", nil }

	session, err := service.NewSession(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if session.Token != "opaque-session" || !session.ExpiresAt.Equal(fixedNow.Add(defaultSessionLifetime)) {
		t.Fatalf("session = %#v", session)
	}
	if store.createdHash != sha256.Sum256([]byte("opaque-session")) || store.createdUserID != "user-1" {
		t.Fatalf("store did not receive the token hash: %#v", store)
	}
}

func TestAuthenticateAndLogoutUseHashedOpaqueToken(t *testing.T) {
	wantHash := sha256.Sum256([]byte("opaque-token"))
	store := &stubSessionStore{resolved: Session{UserID: "user-1"}}
	service := NewSessionService(store, &stubUsers{byID: user.User{ID: "user-1", GameNick: "NightOtter"}})

	account, err := service.Authenticate(t.Context(), "opaque-token")
	if err != nil || account.ID != "user-1" {
		t.Fatalf("Authenticate() = %#v, %v", account, err)
	}
	if store.resolvedHash != wantHash {
		t.Error("Resolve received a raw or unexpected token")
	}
	if err := service.Logout(t.Context(), "opaque-token"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if store.deletedHash != wantHash {
		t.Error("Delete received a raw or unexpected token")
	}
}

func TestAuthenticateRejectsEmptyToken(t *testing.T) {
	service := NewSessionService(&stubSessionStore{}, &stubUsers{})
	if _, err := service.Authenticate(t.Context(), ""); err != ErrSessionNotFound {
		t.Fatalf("Authenticate() error = %v, want ErrSessionNotFound", err)
	}
}

func TestLogoutIgnoresAlreadyRevokedSession(t *testing.T) {
	store := &stubSessionStore{deleteErr: ErrSessionNotFound}
	service := NewSessionService(store, &stubUsers{})
	if err := service.Logout(t.Context(), "opaque-token"); err != nil {
		t.Fatalf("Logout() error = %v, want nil", err)
	}
}

type stubSessionStore struct {
	createdHash   [sha256.Size]byte
	createdUserID string
	resolvedHash  [sha256.Size]byte
	deletedHash   [sha256.Size]byte
	resolved      Session
	deleteErr     error
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
	return s.deleteErr
}
