package notification

import (
	"context"
	"fmt"
	"time"
)

func (t *WebPushTransport) ReminderTargets(ctx context.Context, reminder StartReminder) ([]Target, error) {
	eligible, err := t.reminderEligible(ctx, reminder)
	if err != nil {
		return nil, fmt.Errorf("get game request summary: %w", err)
	}
	if !eligible {
		return nil, nil
	}
	participants, err := t.participants.ListParticipants(ctx, reminder.GameRequestID)
	if err != nil {
		return nil, fmt.Errorf("list game request participants: %w", err)
	}
	var targets []Target
	for _, userID := range dedupeExcept(participants, "") {
		preference, err := t.preferences.GetPreference(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("get push preference for %s: %w", userID, err)
		}
		if !preference.WebPushEnabled {
			continue
		}
		subs, err := t.subscriptions.ListSubscriptions(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list subscriptions for %s: %w", userID, err)
		}
		for _, sub := range subs {
			targets = append(targets, Target{Transport: t.Name(), Key: encodeWebPushTarget(sub.ID, userID)})
		}
	}
	return targets, nil
}

func (t *WebPushTransport) SendReminder(ctx context.Context, reminder StartReminder, target Target, reserve func(context.Context) (bool, error)) SendResult {
	if reserve == nil {
		return SendResult{Outcome: OutcomePermanent, Err: fmt.Errorf("start reminder reservation required")}
	}
	if target.Transport != t.Name() {
	}
	eligibleNow, err := t.reminderEligible(ctx, reminder)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get game request summary: %w", err)}
	}
	if !eligibleNow {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	id, owner, err := decodeWebPushTarget(target.Key)
	if err != nil {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	sub, result := t.loadEligibleSubscription(ctx, id, owner)
	if result.Outcome != OutcomeSucceeded {
		if result.Outcome == OutcomePermanent {
			return SendResult{Outcome: OutcomeNoLongerEligible, Err: result.Err}
		}
		return result
	}
	eligible, err := t.ReminderTargets(ctx, reminder)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("recheck reminder eligibility: %w", err)}
	}
	if !containsTarget(eligible, target) {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	summary, err := t.summaries.GetGameRequestSummary(ctx, reminder.GameRequestID)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get game request summary: %w", err)}
	}
	now := t.now()
	remaining := reminder.StartsAt.Sub(now)
	if summary.Status != "open" || !summary.StartsAt.Equal(reminder.StartsAt) || remaining <= 0 || remaining > StartReminderLead {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	payload, err := buildWebPushTimePayload("starting_soon", reminder.GameRequestID, reminder.StartsAt, summary, t.baseURL, t.instanceName)
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}
	ttl := min(300, int(remaining/time.Second))
	if ttl < 1 {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	return t.sendWithSubscription(ctx, id, owner, sub, payload, ttl, true, reserve)
}

func (t *WebPushTransport) reminderEligible(ctx context.Context, reminder StartReminder) (bool, error) {
	if reminder.GameRequestID == "" || reminder.StartsAt.IsZero() {
		return false, nil
	}
	summary, err := t.summaries.GetGameRequestSummary(ctx, reminder.GameRequestID)
	if err != nil {
		return false, err
	}
	remaining := reminder.StartsAt.Sub(t.now())
	return summary.Status == "open" && summary.StartsAt.Equal(reminder.StartsAt) && remaining > 0 && remaining <= StartReminderLead, nil
}
