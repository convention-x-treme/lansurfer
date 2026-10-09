package notification

import (
	"context"
	"errors"
	"fmt"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

func (t *WebPushTransport) Send(ctx context.Context, activity Activity, target Target) SendResult {
	id, owner, err := decodeWebPushTarget(target.Key)
	if target.Transport != t.Name() || err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	sub, result := t.loadEligibleSubscription(ctx, id, owner)
	if result.Outcome != OutcomeSucceeded {
		return result
	}
	eligible, err := t.Targets(ctx, activity)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("recheck push eligibility: %w", err)}
	}
	if !containsTarget(eligible, target) {
		outcome := OutcomePermanent
		if activity.Kind == ActivityUpdated {
			outcome = OutcomeNoLongerEligible
		}
		return SendResult{Outcome: outcome, Err: ErrNoLongerEligible}
	}
	summary, err := t.summaries.GetGameRequestSummary(ctx, activity.GameRequestID)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get game request summary: %w", err)}
	}
	var payload []byte
	if activity.Kind == ActivityUpdated {
		if !validPlannedStartChange(activity) || summary.Status != "open" || !summary.StartsAt.Equal(*activity.NewStartsAt) {
			return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
		}
		payload, err = buildWebPushTimePayload("planned_start_changed", activity.GameRequestID, *activity.NewStartsAt, summary, t.baseURL, t.instanceName)
	} else {
		nick, nickErr := t.actorGameNick(ctx, activity)
		if nickErr != nil {
			return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get actor Game Nick: %w", nickErr)}
		}
		payload, err = buildWebPushPayload(activity, summary, nick, t.baseURL, t.instanceName)
	}
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}
	return t.sendWithSubscription(ctx, id, owner, sub, payload, 300, false, nil)
}

func (t *WebPushTransport) loadEligibleSubscription(ctx context.Context, id int64, owner string) (PushSubscriptionRecord, SendResult) {
	sub, err := t.delivery.GetSubscriptionByID(ctx, id)
	if errors.Is(err, ErrDeliveryNotFound) {
		return PushSubscriptionRecord{}, SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	if err != nil {
		return PushSubscriptionRecord{}, SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get push subscription: %w", err)}
	}
	if sub.ID != id || sub.UserID != owner {
		return PushSubscriptionRecord{}, SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	return sub, SendResult{Outcome: OutcomeSucceeded}
}

func (t *WebPushTransport) sendWithSubscription(ctx context.Context, id int64, owner string, sub PushSubscriptionRecord, payload []byte, ttl int, reminder bool, reserve func(context.Context) (bool, error)) SendResult {
	endpoint, err := platformcrypto.Decrypt(t.encryptionKey, sub.EndpointCiphertext)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: errors.New("decrypt push endpoint failed")}
	}
	p256dh, err := platformcrypto.Decrypt(t.encryptionKey, sub.P256dhCiphertext)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: errors.New("decrypt push p256dh failed")}
	}
	auth, err := platformcrypto.Decrypt(t.encryptionKey, sub.AuthCiphertext)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: errors.New("decrypt push auth failed")}
	}
	if reserve != nil {
		reserved, err := reserve(ctx)
		if err != nil {
			return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("reserve Web Push send: %w", err)}
		}
		if !reserved {
			return SendResult{Outcome: OutcomeAlreadyRecorded}
		}
	}
	provider := t.sender.Send(ctx, endpoint, p256dh, auth, payload, ttl)
	if provider.Expired {
		_, _ = t.subscriptions.DeleteSubscription(ctx, id, owner)
	}
	if reminder && provider.AcceptanceUnknown {
		return SendResult{Outcome: OutcomeUncertain, Err: provider.Err}
	}
	return SendResult{Outcome: provider.Outcome, RetryAfter: provider.RetryAfter, Err: provider.Err}
}
