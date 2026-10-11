package notification

import (
	"errors"
	"testing"
	"time"
)

func TestDiscordDeliveryClassification(t *testing.T) {
	long := 17 * time.Minute
	cases := []struct {
		name         string
		provider     discordProviderResult
		create, edit discordDeliveryKind
		retryAfter   *time.Duration
	}{
		{"success create with ID", discordProviderResult{Status: 200, MessageID: "m1"}, discordDeliverySucceeded, discordDeliverySucceeded, nil},
		{"success edit no body", discordProviderResult{Status: 204}, discordDeliveryUncertain, discordDeliverySucceeded, nil},
		{"rate limited", discordProviderResult{Status: 429, RetryAfter: &long}, discordDeliveryRetryable, discordDeliveryRetryable, &long},
		{"server 500", discordProviderResult{Status: 500}, discordDeliveryRetryable, discordDeliveryRetryable, nil},
		{"server 502", discordProviderResult{Status: 502}, discordDeliveryRetryable, discordDeliveryRetryable, nil},
		{"server 503", discordProviderResult{Status: 503}, discordDeliveryRetryable, discordDeliveryRetryable, nil},
		{"dial failed", discordProviderResult{Failure: discordBeforeTransmission, Err: errors.New("network")}, discordDeliveryRetryable, discordDeliveryRetryable, nil},
		{"create response lost", discordProviderResult{Failure: discordTransmissionUnknown, Err: errors.New("timeout")}, discordDeliveryUncertain, discordDeliveryRetryable, nil},
		{"create response malformed", discordProviderResult{Status: 200, Failure: discordTransmissionUnknown, Err: errors.New("missing ID")}, discordDeliveryUncertain, discordDeliveryRetryable, nil},
		{"network state unknown", discordProviderResult{Failure: discordTransmissionUnknown}, discordDeliveryUncertain, discordDeliveryRetryable, nil},
		{"bad request", discordProviderResult{Status: 400}, discordDeliveryPermanent, discordDeliveryPermanent, nil},
		{"bad request body lost", discordProviderResult{Status: 400, Err: errors.New("read failed")}, discordDeliveryPermanent, discordDeliveryPermanent, nil},
		{"forbidden", discordProviderResult{Status: 403}, discordDeliveryPermanent, discordDeliveryPermanent, nil},
		{"unknown message", discordProviderResult{Status: 404, DiscordCode: 10008}, discordDeliveryPermanent, discordDeliveryRecoverMessage, nil},
		{"unknown webhook", discordProviderResult{Status: 404, DiscordCode: 10015}, discordDeliveryBrokenDestination, discordDeliveryBrokenDestination, nil},
		{"unknown 404", discordProviderResult{Status: 404}, discordDeliveryPermanent, discordDeliveryPermanent, nil},
		{"redirect not followed", discordProviderResult{Status: 302}, discordDeliveryPermanent, discordDeliveryPermanent, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, operation := range []struct {
				name     string
				classify func(discordProviderResult) discordDeliveryResult
				want     discordDeliveryKind
			}{
				{"create", classifyDiscordCreate, tc.create}, {"edit", classifyDiscordEdit, tc.edit},
			} {
				t.Run(operation.name, func(t *testing.T) {
					result := operation.classify(tc.provider)
					if result.Kind != operation.want {
						t.Errorf("kind = %v, want %v", result.Kind, operation.want)
					}
					if tc.retryAfter != nil && (result.RetryAfter == nil || *result.RetryAfter != *tc.retryAfter) {
						t.Errorf("Retry-After = %v, want %v", result.RetryAfter, *tc.retryAfter)
					}
				})
			}
		})
	}
}

// An explicit 5xx CREATE response is a definitive Discord outcome, not an
// ambiguous one; any Retry-After it carries must reach the worker unchanged. Edit's
// 5xx handling is unaffected by this CREATE-specific correction.
func TestDiscordDeliveryClassificationCreatePreservesRetryAfterOn5xx(t *testing.T) {
	delay := 30 * time.Second
	result := classifyDiscordCreate(discordProviderResult{Status: 503, RetryAfter: &delay})
	if result.Kind != discordDeliveryRetryable || result.RetryAfter == nil || *result.RetryAfter != delay {
		t.Fatalf("create 503 kind = %v, retry-after = %v; want retryable with 30s", result.Kind, result.RetryAfter)
	}
}
