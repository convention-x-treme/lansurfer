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
	// ID identifies this connection generation, not the singleton row or webhook.
	ID           string
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
	// ErrDiscordMessageSyncNotFound means no message has been synchronized
	// for this destination generation and Game Request.
	ErrDiscordMessageSyncNotFound = errors.New("discord message sync not found")
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

// DiscordMessageSync is one destination generation's synchronized message for
// one Game Request. AppliedVersion is the highest activity sequence applied
// for this request, never a global activity cursor.
type DiscordMessageSync struct {
	DestinationID  string
	GameRequestID  string
	MessageID      string
	AppliedVersion int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DiscordMessageSyncRepository keeps message references isolated by
// destination generation. Advance is a PostgreSQL compare/update: it
// advances only to a newer version, or fills a cleared reference at the
// same version. Clear requires the exact current message and version, and
// never lowers the applied version. A false result means the state changed.
type DiscordMessageSyncRepository interface {
	GetDiscordMessageSync(ctx context.Context, destinationID, gameRequestID string) (DiscordMessageSync, error)
	AdvanceDiscordMessageSync(ctx context.Context, sync DiscordMessageSync) (bool, error)
	ClearDiscordMessageSync(ctx context.Context, destinationID, gameRequestID, messageID string, appliedVersion int64) (bool, error)
	// WithDiscordMessageSyncLock serializes one destination/request's
	// external mutation and state write in a PostgreSQL transaction.
	WithDiscordMessageSyncLock(ctx context.Context, destinationID, gameRequestID string, apply func(DiscordMessageSyncRepository) error) error
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

// DiscordTransport materializes generation-scoped targets. E3-18 adds Send
// only after synchronized provider mutation is available; no placeholder
// provider call is exposed here.
type DiscordTransport struct {
	destinations interface {
		GetDiscordDestination(context.Context) (DiscordDestination, DiscordCredentialCiphertext, error)
	}
}

func NewDiscordTransport(destinations interface {
	GetDiscordDestination(context.Context) (DiscordDestination, DiscordCredentialCiphertext, error)
}) *DiscordTransport {
	return &DiscordTransport{destinations: destinations}
}

func (*DiscordTransport) Name() string { return DiscordTransportName }

func (t *DiscordTransport) Targets(ctx context.Context, _ Activity) ([]Target, error) {
	destination, _, err := t.destinations.GetDiscordDestination(ctx)
	if errors.Is(err, ErrDiscordDestinationNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if destination.State != DiscordDestinationConnected {
		return nil, nil
	}
	if !validDiscordDestinationID(destination.ID) {
		return nil, errors.New("invalid discord destination identity")
	}
	return []Target{{Transport: t.Name(), Key: "d1:" + destination.ID}}, nil
}

// ResolveTarget never resolves an old generation to the singleton's current
// credential. The worker also calls Targets before each attempt or retry.
func (t *DiscordTransport) ResolveTarget(ctx context.Context, target Target) (DiscordDestination, DiscordCredentialCiphertext, error) {
	id, ok := decodeDiscordTarget(target)
	if !ok {
		return DiscordDestination{}, nil, ErrNoLongerEligible
	}
	destination, ciphertext, err := t.destinations.GetDiscordDestination(ctx)
	if errors.Is(err, ErrDiscordDestinationNotFound) {
		return DiscordDestination{}, nil, ErrNoLongerEligible
	}
	if err != nil {
		return DiscordDestination{}, nil, err
	}
	if destination.State != DiscordDestinationConnected || destination.ID != id || len(ciphertext) == 0 {
		return DiscordDestination{}, nil, ErrNoLongerEligible
	}
	return destination, ciphertext, nil
}

func decodeDiscordTarget(target Target) (string, bool) {
	if target.Transport != DiscordTransportName || len(target.Key) != 39 || target.Key[:3] != "d1:" {
		return "", false
	}
	id := target.Key[3:]
	return id, validDiscordDestinationID(id)
}

func validDiscordDestinationID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for index := range len(id) {
		switch index {
		case 8, 13, 18, 23:
			if id[index] != '-' {
				return false
			}
		default:
			if !((id[index] >= '0' && id[index] <= '9') || (id[index] >= 'a' && id[index] <= 'f')) {
				return false
			}
		}
	}
	return true
}
