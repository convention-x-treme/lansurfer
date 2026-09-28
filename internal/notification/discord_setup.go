package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
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

// DiscordConnectService installs or replaces the instance's single Discord
// destination after a successful, already-validated OAuth callback (see
// DiscordSetupService.Consume). It owns exactly the approved "destination
// replacement" transaction semantics: the previous destination stays
// active until the new one is durably stored, and only a successful local
// commit may trigger a best-effort remote cleanup of the old webhook.
//
// DiscordConnectService performs no setup-state check itself; callers must
// only reach Connect after Consume has already validated the state/admin/
// browser binding.
type DiscordConnectService struct {
	destinations  DiscordDestinationRepository
	provider      DiscordWebhookOAuthProvider
	encryptionKey []byte
	logger        *slog.Logger
}

func NewDiscordConnectService(destinations DiscordDestinationRepository, provider DiscordWebhookOAuthProvider, encryptionKey []byte, logger *slog.Logger) *DiscordConnectService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DiscordConnectService{destinations: destinations, provider: provider, encryptionKey: encryptionKey, logger: logger}
}

// Connect exchanges code for a newly authorized webhook, encrypts its
// credential, and atomically installs it as the singleton destination,
// replacing any existing one. ConnectDiscordDestination is one atomic
// database statement (INSERT ... ON CONFLICT DO UPDATE against the
// destination's boolean singleton primary key), so a failure exchanging,
// encrypting, or storing leaves any previous destination completely
// unchanged: there is no intermediate phase in which no valid destination
// exists, and no old-then-new delete/insert pair outside that one
// statement.
//
// Connect never scans or replays historical notification_work: it only
// ever touches the destination row itself.
func (s *DiscordConnectService) Connect(ctx context.Context, code string) error {
	result, err := s.provider.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange discord webhook oauth code: %w", err)
	}

	previous, previousCiphertext, err := s.destinations.GetDiscordDestination(ctx)
	hadPreviousConnectedWebhook := err == nil && previous.State == DiscordDestinationConnected && len(previousCiphertext) > 0
	if err != nil && !errors.Is(err, ErrDiscordDestinationNotFound) {
		return fmt.Errorf("get existing discord destination: %w", err)
	}

	ciphertext, err := EncryptDiscordCredential(s.encryptionKey, result.Credential)
	if err != nil {
		return fmt.Errorf("encrypt discord webhook credential: %w", err)
	}

	destination := DiscordDestination{
		State: DiscordDestinationConnected, GuildID: result.GuildID, ChannelID: result.ChannelID, WebhookName: result.WebhookName,
	}
	if err := s.destinations.ConnectDiscordDestination(ctx, destination, ciphertext); err != nil {
		return fmt.Errorf("install discord destination: %w", err)
	}

	// The new destination is now durably committed. Only now, and only
	// best-effort, may the old remote webhook be cleaned up: a missing
	// previous destination, a broken one (already credential-less), or a
	// cleanup failure below must never roll back or block the replacement
	// that has already succeeded.
	if hadPreviousConnectedWebhook {
		s.cleanupOldWebhook(ctx, previousCiphertext)
	}
	return nil
}

// cleanupOldWebhook decrypts ciphertext only in memory, in this immediate
// cleanup path, and deletes the resulting credential's remote webhook.
// Neither the decrypted credential nor the delete failure below is ever
// logged with secret material: only the safe diagnostic error message
// (never containing webhook ID/token) is recorded.
func (s *DiscordConnectService) cleanupOldWebhook(ctx context.Context, ciphertext DiscordCredentialCiphertext) {
	credential, err := DecryptDiscordCredential(s.encryptionKey, ciphertext)
	if err != nil {
		s.logger.Error("decrypt previous discord webhook credential for cleanup", "error", err)
		return
	}
	if err := s.provider.DeleteWebhook(ctx, credential); err != nil {
		s.logger.Error("delete previous discord webhook", "error", err)
	}
}
