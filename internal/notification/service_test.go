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
