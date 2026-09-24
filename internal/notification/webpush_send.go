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
	sub, err := t.delivery.GetSubscriptionByID(ctx, id)
	if errors.Is(err, ErrDeliveryNotFound) {
		return SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get push subscription: %w", err)}
	}
	if sub.ID != id || sub.UserID != owner {
		return SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	// Reuse Targets' audience policy rather than duplicating its preference and
	// membership rules. The Worker also checks this before calling Send.
	eligible, err := t.Targets(ctx, activity)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("recheck push eligibility: %w", err)}
	}
	if !containsTarget(eligible, target) {
		return SendResult{Outcome: OutcomePermanent, Err: ErrNoLongerEligible}
	}
	summary, err := t.summaries.GetGameRequestSummary(ctx, activity.GameRequestID)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get game request summary: %w", err)}
	}
	nick, err := t.actorGameNick(ctx, activity)
	if err != nil {
		return SendResult{Outcome: OutcomeRetryable, Err: fmt.Errorf("get actor Game Nick: %w", err)}
	}
	payload, err := buildWebPushPayload(activity, summary, nick, t.baseURL, t.instanceName)
	if err != nil {
		return SendResult{Outcome: OutcomePermanent, Err: err}
	}

	// Plaintext exists only in this last step, immediately before provider use.
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
	outcome := t.sender.Send(ctx, endpoint, p256dh, auth, payload, 300)
	if outcome.Expired {
		// Scope deletion to the snapshotted owner: a reassigned row must survive.
		_, _ = t.subscriptions.DeleteSubscription(ctx, id, owner)
	}
	return SendResult{Outcome: outcome.Outcome, RetryAfter: outcome.RetryAfter, Err: outcome.Err}
}
