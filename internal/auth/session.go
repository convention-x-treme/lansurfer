package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

// SessionService owns browser session issuance, resolution, and revocation.
// Link/unlink never revoke a session; only Logout revokes the current one.
type SessionService struct {
	store           SessionStore
	users           userLookup
	now             func() time.Time
	sessionLifetime time.Duration
	generateToken   func() (string, error)
}

func NewSessionService(store SessionStore, users userLookup) *SessionService {
	return &SessionService{store: store, users: users, now: time.Now, sessionLifetime: defaultSessionLifetime, generateToken: randomToken}
}

func (s *SessionService) NewSession(ctx context.Context, userID string) (BrowserSession, error) {
	token, err := s.generateToken()
	if err != nil {
		return BrowserSession{}, fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := s.now().Add(s.sessionLifetime)
	if err := s.store.Create(ctx, hashToken(token), userID, expiresAt); err != nil {
		return BrowserSession{}, fmt.Errorf("create session: %w", err)
	}
	return BrowserSession{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *SessionService) Authenticate(ctx context.Context, token string) (user.User, error) {
	if strings.TrimSpace(token) == "" {
		return user.User{}, ErrSessionNotFound
	}
	session, err := s.store.Resolve(ctx, hashToken(token))
	if err != nil {
		return user.User{}, err
	}
	return s.users.ByID(ctx, session.UserID)
}

func (s *SessionService) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if err := s.store.Delete(ctx, hashToken(token)); errors.Is(err, ErrSessionNotFound) {
		return nil
	} else {
		return err
	}
}
