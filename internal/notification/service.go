package notification

import (
	"context"
	"fmt"
)

// Service exposes authorized diagnostics and manual retry over durable
// notification state. It performs no authorization itself: the caller
// (currently the admin CLI in internal/app, see Task 9) is responsible for
// only reaching Service through an already-authorized path.
type Service struct {
	repo             DiagnosticsRepository
	transportsByName map[string]Transport
}

func NewService(repo DiagnosticsRepository, transports []Transport) *Service {
	byName := make(map[string]Transport, len(transports))
	for _, transport := range transports {
		byName[transport.Name()] = transport
	}
	return &Service{repo: repo, transportsByName: byName}
}

func (s *Service) ListProblems(ctx context.Context) ([]ProblemDelivery, error) {
	return s.repo.ListProblemDeliveries(ctx)
}

// ManualRetry reopens one exhausted or permanent delivery for a fresh
// automatic retry budget, after confirming the target is still eligible.
func (s *Service) ManualRetry(ctx context.Context, deliveryID int64) error {
	return s.reopen(ctx, deliveryID, false)
}

// ManualRetryUncertain reopens one uncertain delivery despite the
// possibility of a duplicate externally visible effect. It is a distinct
// action from ManualRetry and only applies to deliveries already in the
// uncertain state.
func (s *Service) ManualRetryUncertain(ctx context.Context, deliveryID int64) error {
	return s.reopen(ctx, deliveryID, true)
}

func (s *Service) reopen(ctx context.Context, deliveryID int64, uncertainOnly bool) error {
	detail, err := s.repo.GetDeliveryDetail(ctx, deliveryID)
	if err != nil {
		return err
	}
	if uncertainOnly {
		if detail.Status != StatusUncertain {
			return ErrDeliveryNotUncertain
		}
	} else if detail.Status != StatusExhausted && detail.Status != StatusPermanent {
		return ErrDeliveryNotRetryable
	}
	transport, ok := s.transportsByName[detail.Target.Transport]
	if !ok {
		return ErrTransportNotConfigured
	}

	var targets []Target
	if reminder := detail.StartReminder; reminder != nil {
		reminderTransport, ok := transport.(StartReminderTransport)
		if !ok {
			return ErrTransportNotConfigured
		}
		targets, err = reminderTransport.ReminderTargets(ctx, *reminder)
	} else {
		targets, err = transport.Targets(ctx, detail.Activity)
	}
	if err != nil {
		return fmt.Errorf("recheck delivery eligibility: %w", err)
	}
	if !containsTarget(targets, detail.Target) {
		return ErrNoLongerEligible
	}

	// The repository rechecks destination generation, exact claim status,
	// and reservation ownership atomically. No-reservation Discord retries
	// must use it too; generic reopening would race with reconnect.
	if uncertainOnly && detail.Target.Transport == DiscordTransportName {
		reservations, ok := s.repo.(DiscordUncertainRetryRepository)
		if !ok {
			return ErrTransportNotConfigured
		}
		destinationID, ok := decodeDiscordTarget(detail.Target)
		if !ok {
			return ErrNoLongerEligible
		}
		return reservations.ReopenUncertainDiscordDeliveryWithReservation(ctx, deliveryID, destinationID)
	}

	var reopened bool
	if uncertainOnly {
		reopened, err = s.repo.ReopenUncertainDelivery(ctx, deliveryID)
	} else {
		reopened, err = s.repo.ReopenDelivery(ctx, deliveryID)
	}
	if err != nil {
		return err
	}
	if !reopened {
		if uncertainOnly {
			return ErrDeliveryNotUncertain
		}
		return ErrDeliveryNotRetryable
	}
	return nil
}

func containsTarget(targets []Target, want Target) bool {
	for _, target := range targets {
		if target == want {
			return true
		}
	}
	return false
}
