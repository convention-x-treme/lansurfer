// Package notification owns durable notification processing: it
// asynchronously consumes the gamerequest game_request_activity handoff and
// persists per-activity work and per-target delivery state, independent of
// any concrete transport. It does not own or write Game Requests.
package notification

import (
	"context"
	"errors"
	"time"
)

// ActivityKind mirrors gamerequest.ActivityKind's five durable kinds. It is
// redeclared here rather than imported so this module never depends on
// gamerequest's package, only on the read-only activity rows it discovers.
type ActivityKind string

const (
	ActivityCreated   ActivityKind = "created"
	ActivityJoined    ActivityKind = "joined"
	ActivityLeft      ActivityKind = "left"
	ActivityClosed    ActivityKind = "closed"
	ActivityCancelled ActivityKind = "cancelled"
)

// Activity is a read-only snapshot of one live game_request_activity row.
type Activity struct {
	Sequence      int64
	GameRequestID string
	ActorID       string
	Kind          ActivityKind
	OccurredAt    time.Time
}

// Target names one external delivery target within one transport, for
// example a Web Push subscription or a Discord destination generation.
// It is intentionally opaque outside the owning transport.
type Target struct {
	Transport string
	Key       string
}

// Outcome is the minimal transport-facing result vocabulary. Exhausted is
// deliberately not a member: it is worker policy computed from AttemptCount,
// never something a transport adapter decides.
type Outcome int

const (
	OutcomeSucceeded Outcome = iota
	OutcomeRetryable
	OutcomePermanent
	OutcomeUncertain
	// Discord only: no longer eligible is suppressed; already recorded means
	// the exact claimed delivery was closed inside its sync transaction.
	OutcomeNoLongerEligible
	OutcomeAlreadyRecorded
)

// SendResult is what a Transport reports for one delivery attempt.
// RetryAfter carries a provider-specified hint (for example HTTP 429's
// Retry-After) and is only meaningful when Outcome is OutcomeRetryable.
type SendResult struct {
	Outcome    Outcome
	RetryAfter *time.Duration
	Err        error
}

// Transport is implemented by concrete delivery mechanisms (Web Push,
// Discord; a fake is used in this package's tests). Targets resolves
// which external targets are currently eligible for an activity; Send
// attempts delivery to one of them. Neither method may block past ctx.
type Transport interface {
	Name() string
	Targets(ctx context.Context, activity Activity) ([]Target, error)
	Send(ctx context.Context, activity Activity, target Target) SendResult
}

// DeliveryStatus is the durable state of one notification_delivery row.
// "Retryable failure" has no state of its own: it is StatusPending with a
// future NextAttemptAt.
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusClaimed    DeliveryStatus = "claimed"
	StatusSucceeded  DeliveryStatus = "succeeded"
	StatusPermanent  DeliveryStatus = "permanent"
	StatusExhausted  DeliveryStatus = "exhausted"
	StatusUncertain  DeliveryStatus = "uncertain"
	StatusSuppressed DeliveryStatus = "suppressed"
	StatusSuperseded DeliveryStatus = "superseded"
)

// ClaimedDelivery is one delivery row the Worker has just claimed, together
// with enough of its Activity snapshot to call a Transport.
type ClaimedDelivery struct {
	DeliveryID   int64
	Target       Target
	AttemptCount int32
	Activity     Activity
}

// ProblemDelivery is one row in a terminal problem state, for authorized
// diagnostics.
type ProblemDelivery struct {
	DeliveryID   int64
	Target       Target
	Status       DeliveryStatus
	AttemptCount int32
	LastError    string
	UpdatedAt    time.Time
	Activity     Activity
}

// DeliveryDetail is the minimal view Service needs to validate and reopen
// one delivery for manual retry.
type DeliveryDetail struct {
	DeliveryID int64
	Target     Target
	Status     DeliveryStatus
	Activity   Activity
}

var (
	ErrDeliveryNotFound           = errors.New("notification delivery not found")
	ErrDeliveryNotRetryable       = errors.New("notification delivery is not exhausted or permanent")
	ErrDeliveryNotUncertain       = errors.New("notification delivery is not uncertain")
	ErrNoLongerEligible           = errors.New("target is no longer eligible for delivery")
	ErrDiscordReservationNotOwned = errors.New("discord create reservation belongs to another delivery")
	ErrTransportNotConfigured     = errors.New("transport is not configured")
)

// BaselineInitializer is consumed directly by the composition root: exactly
// one synchronous call before the HTTP server and Worker start.
type BaselineInitializer interface {
	InitializeBaseline(ctx context.Context) error
}

// WorkerRepository is the durable state Worker needs to discover activity,
// materialize work/delivery rows, claim due deliveries, and record outcomes.
type WorkerRepository interface {
	DiscoverActivity(ctx context.Context, limit int) ([]Activity, error)
	MaterializeWork(ctx context.Context, activity Activity, targets []Target) error
	ClaimDueDeliveries(ctx context.Context, limit int, leaseUntil time.Time) ([]ClaimedDelivery, error)
	RecordSuccess(ctx context.Context, deliveryID int64) error
	RecordRetry(ctx context.Context, deliveryID int64, nextAttemptAt time.Time, lastError string) error
	RecordExhausted(ctx context.Context, deliveryID int64, lastError string) error
	RecordPermanent(ctx context.Context, deliveryID int64, lastError string) error
	RecordUncertain(ctx context.Context, deliveryID int64, lastError string) error
	RecordSuppressed(ctx context.Context, deliveryID int64, reason string) error
}

// DiagnosticsRepository is the durable state Service needs for authorized
// diagnostics and manual retry.
type DiagnosticsRepository interface {
	ListProblemDeliveries(ctx context.Context) ([]ProblemDelivery, error)
	GetDeliveryDetail(ctx context.Context, deliveryID int64) (DeliveryDetail, error)
	ReopenDelivery(ctx context.Context, deliveryID int64) (bool, error)
	ReopenUncertainDelivery(ctx context.Context, deliveryID int64) (bool, error)
}

// DiscordUncertainRetryRepository atomically rechecks the current connected
// generation, selected uncertain delivery, and CREATE reservation ownership.
// It also handles the no-reservation Discord case inside that same boundary.
// Automatic lease reclamation never invokes this administrator-only method.
type DiscordUncertainRetryRepository interface {
	ReopenUncertainDiscordDeliveryWithReservation(ctx context.Context, deliveryID int64, destinationID string) error
}

// DeliverySupersessionRepository is the Discord-only terminal transition for
// obsolete deliveries. The version is a safe request activity sequence;
// persistence constructs the reason without provider or user material.
type DeliverySupersessionRepository interface {
	RecordSuperseded(ctx context.Context, deliveryID, appliedVersion int64) (bool, error)
}

// PushSubscriptionRecord is one stored browser Push subscription.
// EndpointCiphertext/P256dhCiphertext/AuthCiphertext are opaque AES-GCM
// ciphertext: Store never sees, and this type never carries, plaintext
// subscription material. Only SubscriptionService encrypts/decrypts it.
type PushSubscriptionRecord struct {
	ID                  int64
	UserID              string
	Label               string
	DeviceIDHash        string
	EndpointFingerprint string
	EndpointCiphertext  []byte
	P256dhCiphertext    []byte
	AuthCiphertext      []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// PushPreference is one user's Web Push preferences. The zero value is NOT
// a safe default to use directly: NewRequestAudience zero-values to "",
// neither AudienceFollowed nor AudienceAll. Callers must go through
// PreferenceRepository.GetPreference, which returns the actual safe
// default (disabled, followed-only audience) for a user who has never
// touched Push settings.
type PushPreference struct {
	WebPushEnabled     bool
	NewRequestAudience string // "followed" or "all"
}

const (
	AudienceFollowed = "followed"
	AudienceAll      = "all"
)

// SubscriptionRepository is the durable state SubscriptionService needs for
// managing one user's Push subscriptions.
type SubscriptionRepository interface {
	// UpsertSubscription persists sub, atomically reassigning ownership if
	// EndpointFingerprint already belongs to a different user. Only
	// UserID/Label/DeviceIDHash/EndpointFingerprint/EndpointCiphertext/
	// P256dhCiphertext/AuthCiphertext are read from sub; ID/CreatedAt/
	// UpdatedAt are populated in the returned record.
	UpsertSubscription(ctx context.Context, sub PushSubscriptionRecord) (PushSubscriptionRecord, error)
	ListSubscriptions(ctx context.Context, userID string) ([]PushSubscriptionRecord, error)
	// DeleteSubscription removes one subscription scoped to its owner; it
	// reports whether a row was actually deleted.
	DeleteSubscription(ctx context.Context, subscriptionID int64, userID string) (bool, error)
	DeleteSubscriptionByDeviceHash(ctx context.Context, userID, deviceIDHash string) (bool, error)
}

// DeliveryReadRepository is the durable state a Transport needs to resolve
// full subscription material for one send, and to find users who opted
// into the all-new-requests audience regardless of following.
type DeliveryReadRepository interface {
	GetSubscriptionByID(ctx context.Context, id int64) (PushSubscriptionRecord, error)
	ListUsersWithAudience(ctx context.Context, audience string) ([]string, error)
}

// PreferenceRepository is the durable state SubscriptionService needs for
// one user's Web Push preferences.
type PreferenceRepository interface {
	// GetPreference returns the safe default PushPreference (disabled,
	// followed-only) when the user has no stored row yet. Callers must call
	// this rather than use a zero-value PushPreference directly: the zero
	// value's NewRequestAudience is "", not a valid audience.
	GetPreference(ctx context.Context, userID string) (PushPreference, error)
	SetPreference(ctx context.Context, userID string, preference PushPreference) error
}
