package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// discordSetupStateLifetime is the short expiry required by the
	// approved setup-flow security design.
	discordSetupStateLifetime = 10 * time.Minute
	discordSetupStateBytes    = 32
)

var (
	// ErrDiscordSetupAdminRequired means Start was called without an
	// initiating administrator, which the setup flow always has: the
	// caller must run behind an authenticated global-administrator check.
	ErrDiscordSetupAdminRequired = errors.New("discord setup requires an initiating administrator")
	// ErrDiscordSetupBrowserRequired means Start or Consume was called
	// without the caller's browser/session binding.
	ErrDiscordSetupBrowserRequired = errors.New("discord setup requires a browser/session binding")
)

// DiscordSetupService creates and atomically consumes the one-use OAuth
// setup state that binds one "configure Discord destination" attempt to
// its initiating administrator and browser/session, per the approved E3
// setup-flow security design. It mirrors internal/auth's OAuthService
// state/browser-binding pattern but stays inside the notification module:
// this flow never creates or touches a Discord authentication identity.
//
// DiscordSetupService only owns the setup state's lifecycle. Exchanging
// the OAuth code and installing/replacing the destination happen in a
// later task, only after Consume has already validated the binding.
type DiscordSetupService struct {
	states DiscordSetupStateRepository
	now    func() time.Time
}

func NewDiscordSetupService(states DiscordSetupStateRepository) *DiscordSetupService {
	return &DiscordSetupService{states: states, now: time.Now}
}

// Start creates a new one-use setup state bound to adminID and
// browserBinding and returns the raw state value the caller embeds as the
// OAuth `state` parameter. Only a SHA-256 hash of the raw state and of
// browserBinding is ever persisted; the raw values themselves never reach
// the database.
func (s *DiscordSetupService) Start(ctx context.Context, adminID, browserBinding string) (string, error) {
	if strings.TrimSpace(adminID) == "" {
		return "", ErrDiscordSetupAdminRequired
	}
	if strings.TrimSpace(browserBinding) == "" {
		return "", ErrDiscordSetupBrowserRequired
	}
	state, err := randomDiscordSetupState()
	if err != nil {
		return "", fmt.Errorf("generate discord setup state: %w", err)
	}
	stateHash := hashDiscordSetupSecret(state)
	browserHash := hashDiscordSetupSecret(browserBinding)
	if _, err := s.states.CreateDiscordSetupState(ctx, stateHash[:], browserHash[:], adminID, s.now().Add(discordSetupStateLifetime)); err != nil {
		return "", fmt.Errorf("create discord setup state: %w", err)
	}
	return state, nil
}

// Consume atomically validates and consumes exactly one use of state,
// bound to adminID and browserBinding. It returns
// ErrDiscordSetupStateInvalid for an unknown, replayed, expired, or
// wrong-admin/wrong-browser-binding state. A rejected call never mutates
// any destination: this method only ever writes to the setup-state row
// itself.
func (s *DiscordSetupService) Consume(ctx context.Context, adminID, browserBinding, state string) (DiscordSetupState, error) {
	if strings.TrimSpace(adminID) == "" {
		return DiscordSetupState{}, ErrDiscordSetupAdminRequired
	}
	if strings.TrimSpace(browserBinding) == "" {
		return DiscordSetupState{}, ErrDiscordSetupBrowserRequired
	}
	if strings.TrimSpace(state) == "" {
		return DiscordSetupState{}, ErrDiscordSetupStateInvalid
	}
	stateHash := hashDiscordSetupSecret(state)
	browserHash := hashDiscordSetupSecret(browserBinding)
	return s.states.ConsumeDiscordSetupState(ctx, stateHash[:], browserHash[:], adminID, s.now())
}

func randomDiscordSetupState() (string, error) {
	value := make([]byte, discordSetupStateBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashDiscordSetupSecret(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}
