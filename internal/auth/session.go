package auth

import (
	"context"
	"fmt"
	"time"
)

type SessionService struct {
	store           SessionStore
	now             func() time.Time
	sessionLifetime time.Duration
	generateToken   func() (string, error)
}

func NewSessionService(store SessionStore) *SessionService {
	return &SessionService{store: store, now: time.Now, sessionLifetime: defaultSessionLifetime, generateToken: randomToken}
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
