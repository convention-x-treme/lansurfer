package notification

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDiagnosticsRepository struct {
	problems       []ProblemDelivery
	detail         DeliveryDetail
	detailErr      error
	reopened       bool
	reopenedErr    error
	uncertainErr   error
	reopenedUncCal bool
	reopenedCal    bool
}

func (f *fakeDiagnosticsRepository) ListProblemDeliveries(context.Context) ([]ProblemDelivery, error) {
	return f.problems, nil
}

func (f *fakeDiagnosticsRepository) GetDeliveryDetail(context.Context, int64) (DeliveryDetail, error) {
	return f.detail, f.detailErr
}

func (f *fakeDiagnosticsRepository) ReopenDelivery(context.Context, int64) (bool, error) {
	f.reopenedCal = true
	return f.reopened, f.reopenedErr
}

func (f *fakeDiagnosticsRepository) ReopenUncertainDelivery(context.Context, int64) (bool, error) {
	f.reopenedUncCal = true
	return f.reopened, f.uncertainErr
}

type fakeManualRetryTransport struct {
	name    string
	targets []Target
}

func (f fakeManualRetryTransport) Name() string { return f.name }
func (f fakeManualRetryTransport) Targets(context.Context, Activity) ([]Target, error) {
	return f.targets, nil
}
func (f fakeManualRetryTransport) Send(context.Context, Activity, Target) SendResult {
	return SendResult{Outcome: OutcomeSucceeded}
}

type fakeReminderRetryTransport struct {
	fakeManualRetryTransport
	reminderTargets []Target
	reminderCalls   int
}

func (f *fakeReminderRetryTransport) ReminderTargets(context.Context, StartReminder) ([]Target, error) {
	f.reminderCalls++
	return f.reminderTargets, nil
}
func (*fakeReminderRetryTransport) SendReminder(context.Context, StartReminder, Target, func(context.Context) (bool, error)) SendResult {
	return SendResult{Outcome: OutcomeSucceeded}
}

func TestServiceManualRetryUsesReminderEligibilityForOneTarget(t *testing.T) {
	target := Target{Transport: "push", Key: "one"}
	repo := &fakeDiagnosticsRepository{
		detail: DeliveryDetail{
			DeliveryID: 1, Status: StatusUncertain, Target: target,
			StartReminder: &StartReminder{ID: 8, GameRequestID: "request"},
		},
		reopened: true,
	}
	transport := &fakeReminderRetryTransport{
		fakeManualRetryTransport: fakeManualRetryTransport{name: "push", targets: []Target{{Transport: "push", Key: "wrong"}}},
		reminderTargets:          []Target{target, {Transport: "push", Key: "second"}},
	}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if transport.reminderCalls != 1 || !repo.reopenedUncCal || repo.reopenedCal {
		t.Fatalf("reminder calls=%d uncertain=%v ordinary=%v", transport.reminderCalls, repo.reopenedUncCal, repo.reopenedCal)
	}
}

func TestServiceManualRetryRejectsDeliveryNotInExhaustedOrPermanentState(t *testing.T) {
	repo := &fakeDiagnosticsRepository{detail: DeliveryDetail{DeliveryID: 1, Status: StatusUncertain}}
	service := NewService(repo, nil)
	if err := service.ManualRetry(t.Context(), 1); !errors.Is(err, ErrDeliveryNotRetryable) {
		t.Fatalf("ManualRetry() on an uncertain delivery = %v, want %v", err, ErrDeliveryNotRetryable)
	}
	if repo.reopenedCal {
		t.Fatal("ManualRetry() must not reopen a delivery in the wrong state")
	}
}

func TestServiceManualRetryUncertainRejectsDeliveryNotUncertain(t *testing.T) {
	repo := &fakeDiagnosticsRepository{detail: DeliveryDetail{DeliveryID: 1, Status: StatusExhausted}}
	service := NewService(repo, nil)
	if err := service.ManualRetryUncertain(t.Context(), 1); !errors.Is(err, ErrDeliveryNotUncertain) {
		t.Fatalf("ManualRetryUncertain() on an exhausted delivery = %v, want %v", err, ErrDeliveryNotUncertain)
	}
	if repo.reopenedUncCal {
		t.Fatal("ManualRetryUncertain() must not reopen a delivery that is not uncertain")
	}
}

func TestServiceManualRetryRejectsWhenTargetNoLongerEligible(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail: DeliveryDetail{DeliveryID: 1, Status: StatusExhausted, Target: target, Activity: Activity{Sequence: 1}},
	}
	transport := fakeManualRetryTransport{name: "test", targets: nil} // recheck finds no eligible targets
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetry(t.Context(), 1); !errors.Is(err, ErrNoLongerEligible) {
		t.Fatalf("ManualRetry() with no eligible targets = %v, want %v", err, ErrNoLongerEligible)
	}
	if repo.reopenedCal {
		t.Fatal("ManualRetry() must not reopen a delivery that failed its eligibility recheck")
	}
}

func TestServiceManualRetrySucceedsAndReopensDelivery(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail:   DeliveryDetail{DeliveryID: 1, Status: StatusExhausted, Target: target, Activity: Activity{Sequence: 1}},
		reopened: true,
	}
	transport := fakeManualRetryTransport{name: "test", targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetry(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetry() error = %v, want nil", err)
	}
	if !repo.reopenedCal {
		t.Fatal("ManualRetry() must call ReopenDelivery once eligibility is confirmed")
	}
}

func TestServiceManualRetryUncertainIsDistinctFromOrdinaryManualRetry(t *testing.T) {
	target := Target{Transport: "test", Key: "target-1"}
	repo := &fakeDiagnosticsRepository{
		detail:   DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
		reopened: true,
	}
	transport := fakeManualRetryTransport{name: "test", targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v, want nil", err)
	}
	if !repo.reopenedUncCal || repo.reopenedCal {
		t.Fatal("ManualRetryUncertain() must call ReopenUncertainDelivery, not ReopenDelivery")
	}
}

func TestServiceListProblemsReturnsRepositoryResult(t *testing.T) {
	want := []ProblemDelivery{{DeliveryID: 1, Status: StatusExhausted, UpdatedAt: time.Now()}}
	repo := &fakeDiagnosticsRepository{problems: want}
	service := NewService(repo, nil)
	got, err := service.ListProblems(t.Context())
	if err != nil || len(got) != 1 || got[0].DeliveryID != want[0].DeliveryID {
		t.Fatalf("ListProblems() = %#v, %v, want %#v", got, err, want)
	}
}

// fakeDiscordUncertainRetryRepository composes fakeDiagnosticsRepository
// with the Discord-only reservation override, so Service.reopen's type
// assertion for DiscordUncertainRetryRepository succeeds exactly like the
// real *notificationpostgres.Store, while a plain fakeDiagnosticsRepository
// (used by every non-Discord test above) never satisfies it.
type fakeDiscordUncertainRetryRepository struct {
	*fakeDiagnosticsRepository
	reservationCalled        bool
	reservationDestinationID string
	reservationFound         bool
	reservationReopened      bool
	reservationErr           error
}

func (f *fakeDiscordUncertainRetryRepository) ReopenUncertainDiscordDeliveryWithReservation(_ context.Context, _ int64, destinationID string) error {
	f.reservationCalled = true
	f.reservationDestinationID = destinationID
	if f.reservationErr != nil {
		return f.reservationErr
	}
	if !f.reservationReopened {
		return ErrDeliveryNotUncertain
	}
	return nil
}

func discordTarget(destinationID string) Target {
	return Target{Transport: DiscordTransportName, Key: "d1:" + destinationID}
}

const testDiscordDestinationID = "11111111-2222-3333-4444-555555555555"

func TestServiceManualRetryUncertainDiscordWithMatchingReservationAtomicallyReopens(t *testing.T) {
	target := discordTarget(testDiscordDestinationID)
	repo := &fakeDiscordUncertainRetryRepository{
		fakeDiagnosticsRepository: &fakeDiagnosticsRepository{
			detail: DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
		},
		reservationFound: true, reservationReopened: true,
	}
	transport := fakeManualRetryTransport{name: DiscordTransportName, targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v, want nil", err)
	}
	if !repo.reservationCalled || repo.reservationDestinationID != testDiscordDestinationID {
		t.Fatalf("reservation override called = %v, destination = %q", repo.reservationCalled, repo.reservationDestinationID)
	}
	if repo.reopenedUncCal {
		t.Fatal("a matching reservation must bypass the generic ReopenUncertainDelivery path")
	}
}

func TestServiceManualRetryUncertainDiscordFallsBackToGenericWhenNoReservation(t *testing.T) {
	target := discordTarget(testDiscordDestinationID)
	repo := &fakeDiscordUncertainRetryRepository{
		fakeDiagnosticsRepository: &fakeDiagnosticsRepository{
			detail:   DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
			reopened: true,
		},
		reservationReopened: true,
	}
	transport := fakeManualRetryTransport{name: DiscordTransportName, targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetryUncertain() error = %v, want nil", err)
	}
	if !repo.reservationCalled {
		t.Fatal("the reservation-aware path must still be probed for a Discord delivery")
	}
	if repo.reopenedUncCal {
		t.Fatal("no-reservation Discord retry must use the atomic generation check, not generic reopening")
	}
}

func TestServiceManualRetryUncertainDiscordReservationFoundButDeliveryChangedFailsWithoutGenericFallback(t *testing.T) {
	target := discordTarget(testDiscordDestinationID)
	repo := &fakeDiscordUncertainRetryRepository{
		fakeDiagnosticsRepository: &fakeDiagnosticsRepository{
			detail: DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
		},
		reservationFound: true, reservationReopened: false,
	}
	transport := fakeManualRetryTransport{name: DiscordTransportName, targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); !errors.Is(err, ErrDeliveryNotUncertain) {
		t.Fatalf("ManualRetryUncertain() with a concurrently changed delivery = %v, want %v", err, ErrDeliveryNotUncertain)
	}
	if repo.reopenedUncCal {
		t.Fatal("a found-but-stale reservation must never fall back to the generic reopen path")
	}
}

// A destination generation that no longer matches the delivery's own target
// must never even reach the reservation override: ErrNoLongerEligible comes
// from the ordinary eligibility recheck, before any reservation is touched.
func TestServiceManualRetryUncertainDiscordGenerationMismatchNeverTouchesReservation(t *testing.T) {
	target := discordTarget(testDiscordDestinationID)
	repo := &fakeDiscordUncertainRetryRepository{
		fakeDiagnosticsRepository: &fakeDiagnosticsRepository{
			detail: DeliveryDetail{DeliveryID: 1, Status: StatusUncertain, Target: target, Activity: Activity{Sequence: 1}},
		},
		reservationFound: true, reservationReopened: true,
	}
	// Targets() no longer returns this exact generation's key: a
	// replacement/reconnect already produced a new one.
	transport := fakeManualRetryTransport{name: DiscordTransportName, targets: []Target{discordTarget("99999999-9999-9999-9999-999999999999")}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), 1); !errors.Is(err, ErrNoLongerEligible) {
		t.Fatalf("ManualRetryUncertain() with a stale generation = %v, want %v", err, ErrNoLongerEligible)
	}
	if repo.reservationCalled {
		t.Fatal("a stale generation must never clear another generation's reservation")
	}
}

// Ordinary ManualRetry (exhausted/permanent) must never consult the Discord
// reservation override: only the explicit administrator retry-uncertain
// action may clear a CREATE reservation.
func TestServiceManualRetryOrdinaryNeverTouchesDiscordReservation(t *testing.T) {
	target := discordTarget(testDiscordDestinationID)
	repo := &fakeDiscordUncertainRetryRepository{
		fakeDiagnosticsRepository: &fakeDiagnosticsRepository{
			detail:   DeliveryDetail{DeliveryID: 1, Status: StatusExhausted, Target: target, Activity: Activity{Sequence: 1}},
			reopened: true,
		},
		reservationFound: true, reservationReopened: true,
	}
	transport := fakeManualRetryTransport{name: DiscordTransportName, targets: []Target{target}}
	service := NewService(repo, []Transport{transport})
	if err := service.ManualRetry(t.Context(), 1); err != nil {
		t.Fatalf("ManualRetry() error = %v, want nil", err)
	}
	if repo.reservationCalled {
		t.Fatal("ordinary ManualRetry must never call the uncertain-only reservation override")
	}
	if !repo.reopenedCal {
		t.Fatal("ordinary ManualRetry must still use the generic ReopenDelivery path")
	}
}
