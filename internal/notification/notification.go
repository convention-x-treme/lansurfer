// Package notification owns Track E1's durable notification foundation: it
// asynchronously consumes Track A's game_request_activity handoff and
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
// example a Web Push subscription ID or "instance" for the single Discord
// destination. It is intentionally opaque outside the owning transport.
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
// Discord in later tracks; a fake in this track's tests). Targets resolves
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
	StatusPending   DeliveryStatus = "pending"
	StatusClaimed   DeliveryStatus = "claimed"
	StatusSucceeded DeliveryStatus = "succeeded"
	StatusPermanent DeliveryStatus = "permanent"
	StatusExhausted DeliveryStatus = "exhausted"
	StatusUncertain DeliveryStatus = "uncertain"
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
	ErrDeliveryNotFound       = errors.New("notification delivery not found")
	ErrDeliveryNotRetryable   = errors.New("notification delivery is not exhausted or permanent")
	ErrDeliveryNotUncertain   = errors.New("notification delivery is not uncertain")
	ErrNoLongerEligible       = errors.New("target is no longer eligible for delivery")
	ErrTransportNotConfigured = errors.New("transport is not configured")
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
}

// DiagnosticsRepository is the durable state Service needs for authorized
// diagnostics and manual retry.
type DiagnosticsRepository interface {
	ListProblemDeliveries(ctx context.Context) ([]ProblemDelivery, error)
	GetDeliveryDetail(ctx context.Context, deliveryID int64) (DeliveryDetail, error)
	ReopenDelivery(ctx context.Context, deliveryID int64) (bool, error)
	ReopenUncertainDelivery(ctx context.Context, deliveryID int64) (bool, error)
}
