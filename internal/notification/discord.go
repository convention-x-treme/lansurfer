package notification

import (
	"context"
	"errors"
	"time"
)

// DiscordDestinationState is the state of the instance's single Discord
// delivery destination. Absence of a stored destination entirely (as
// opposed to either state below) means "not configured" and is not a
// member of this type.
type DiscordDestinationState string

const (
	DiscordDestinationConnected DiscordDestinationState = "connected"
	DiscordDestinationBroken    DiscordDestinationState = "broken"
)

// DiscordTransportName is the Target.Transport value every Discord
// delivery uses. It is defined here rather than beside a future
// DiscordTransport implementation because the disconnect/broken lifecycle
// transitions (internal/notification/discord_destination.go) already need
// it to bulk-suppress this transport's still-pending/claimed deliveries.
const DiscordTransportName = "discord"

// DiscordDestination is the safe, queryable metadata for the instance's
// single Discord delivery destination. It never carries the webhook
// credential itself (see DiscordWebhookCredential/DiscordCredentialCiphertext)
// and never carries or reconstructs a webhook URL. BrokenAt/BrokenReason are
// only meaningful when State is DiscordDestinationBroken.
type DiscordDestination struct {
	State        DiscordDestinationState
	GuildID      string
	ChannelID    string
	WebhookName  string
	ConnectedAt  time.Time
	UpdatedAt    time.Time
	BrokenAt     time.Time
	BrokenReason string
}

// DiscordWebhookCredential is the plaintext webhook ID/token pair. It exists
// only in memory, immediately before encryption or immediately after
// decryption; internal/notification/discord_credentials.go (a later task)
// owns that boundary. This type is never logged, rendered, or persisted
// directly.
type DiscordWebhookCredential struct {
	WebhookID    string
	WebhookToken string
}

// DiscordCredentialCiphertext is one versioned AES-GCM-encrypted
// DiscordWebhookCredential blob, exactly as persisted by
// DiscordDestinationRepository. Only the encryption boundary in a later
// task decrypts it; the rest of this package carries it opaquely.
type DiscordCredentialCiphertext []byte

// DiscordSetupState is the durable one-use OAuth setup state binding one
// "configure Discord destination" attempt to its initiating administrator
// and browser/session. It never creates or links a Discord authentication
// identity.
type DiscordSetupState struct {
	ID                string
	InitiatingAdminID string
	ExpiresAt         time.Time
}

var (
	// ErrDiscordDestinationNotFound means no Discord destination is
	// currently configured for this instance.
	ErrDiscordDestinationNotFound = errors.New("discord destination not configured")
	// ErrDiscordSetupStateInvalid covers every reason a setup-state lookup
	// must fail closed: unknown, expired, already consumed, or bound to a
	// different administrator/browser/session than the caller presents. A
	// single error deliberately does not distinguish these to a caller
	// completing an OAuth callback.
	ErrDiscordSetupStateInvalid = errors.New("discord setup state is invalid, expired, or already consumed")
)

// DiscordSetupStateRepository is the durable state the Discord destination
// setup flow needs to create its one-use OAuth state and to atomically
// consume it exactly once, bound to the initiating administrator and
// browser/session.
type DiscordSetupStateRepository interface {
	CreateDiscordSetupState(ctx context.Context, stateHash, browserBindingHash []byte, initiatingAdminID string, expiresAt time.Time) (DiscordSetupState, error)
	// ConsumeDiscordSetupState atomically marks the matching, unconsumed,
	// unexpired state consumed and returns it. It returns
	// ErrDiscordSetupStateInvalid for any binding mismatch, expiry, or
	// replay.
	ConsumeDiscordSetupState(ctx context.Context, stateHash, browserBindingHash []byte, initiatingAdminID string, now time.Time) (DiscordSetupState, error)
}

// DiscordDestinationRepository is the durable state Discord delivery needs
// for the instance's single destination: its safe metadata plus the
// encrypted webhook credential blob. Callers decrypt the returned
// ciphertext only immediately before a provider call.
type DiscordDestinationRepository interface {
	// GetDiscordDestination returns ErrDiscordDestinationNotFound when no
	// destination is configured. Ciphertext is nil when the destination is
	// broken.
	GetDiscordDestination(ctx context.Context) (DiscordDestination, DiscordCredentialCiphertext, error)
	// ConnectDiscordDestination atomically installs or replaces the
	// singleton destination as connected.
	ConnectDiscordDestination(ctx context.Context, destination DiscordDestination, credential DiscordCredentialCiphertext) error
	// MarkDiscordDestinationBroken transitions a connected destination to
	// broken, deleting its now-unusable credential ciphertext while
	// retaining safe metadata.
	MarkDiscordDestinationBroken(ctx context.Context, reason string) error
	// DeleteDiscordDestination removes the destination entirely
	// (disconnect).
	DeleteDiscordDestination(ctx context.Context) error
}

// DiscordMessageSync is the durable record of which Discord message
// currently represents one Game Request's synchronized state, and the
// highest applied update marker used to reject a stale mutation. Its
// persistence is added by a later task; this interface exists now so the
// vocabulary in this file does not change shape once that persistence
// lands.
type DiscordMessageSync struct {
	GameRequestID  string
	MessageID      string
	AppliedVersion int64
	UpdatedAt      time.Time
}

// DiscordMessageSyncRepository is the durable state a later Discord message
// synchronization service needs to read the currently synchronized message
// for a Game Request and to record a new one after a successful mutation.
type DiscordMessageSyncRepository interface {
	GetDiscordMessageSync(ctx context.Context, gameRequestID string) (DiscordMessageSync, error)
	RecordDiscordMessageSync(ctx context.Context, sync DiscordMessageSync) error
}

// DiscordWebhookOAuthResult is what a successful `webhook.incoming` OAuth
// exchange returns: the created webhook's credential plus its safe
// metadata. The OAuth access/refresh tokens are deliberately not part of
// this type; an implementation must discard them immediately after
// extracting this result.
type DiscordWebhookOAuthResult struct {
	Credential  DiscordWebhookCredential
	GuildID     string
	ChannelID   string
	WebhookName string
}

// DiscordWebhookOAuthProvider is the narrow OAuth client used to authorize
// and exchange the `webhook.incoming` flow. It is kept separate from
// internal/auth's login providers because Discord login and Discord
// delivery are different integrations with different scopes and
// credentials.
type DiscordWebhookOAuthProvider interface {
	AuthorizationURL(state string) string
	Exchange(ctx context.Context, code string) (DiscordWebhookOAuthResult, error)
	// DeleteWebhook best-effort deletes a webhook this provider previously
	// created, using only credential (the webhook-token API needs no
	// separate OAuth token). Callers use this exclusively for immediate,
	// best-effort cleanup of a webhook that is no longer any destination's
	// active credential; failure here must never roll back or block a
	// local destination change that already committed.
	DeleteWebhook(ctx context.Context, credential DiscordWebhookCredential) error
}
