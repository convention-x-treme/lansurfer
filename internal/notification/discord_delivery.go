package notification

import "time"

// Discord-specific recovery signals are not ordinary E1 outcomes: send
// orchestration must act on them before reporting a delivery to the worker.
type discordDeliveryKind uint8

const (
	discordDeliverySucceeded discordDeliveryKind = iota
	discordDeliveryRetryable
	discordDeliveryPermanent
	discordDeliveryUncertain
	discordDeliveryRecoverMessage
	discordDeliveryBrokenDestination
)

type discordDeliveryResult struct {
	Kind       discordDeliveryKind
	RetryAfter *time.Duration
}

func classifyDiscordCreate(result discordProviderResult) discordDeliveryResult {
	if result.Status == 429 {
		return discordDeliveryResult{Kind: discordDeliveryRetryable, RetryAfter: result.RetryAfter}
	}
	if result.Status >= 500 {
		return discordDeliveryResult{Kind: discordDeliveryRetryable, RetryAfter: result.RetryAfter}
	}
	if result.DiscordCode == 10015 {
		return discordDeliveryResult{Kind: discordDeliveryBrokenDestination}
	}
	if result.Status >= 200 && result.Status < 300 && result.MessageID != "" && result.Err == nil {
		return discordDeliveryResult{Kind: discordDeliverySucceeded}
	}
	if result.Status == 0 {
		if result.Failure == discordBeforeTransmission {
			return discordDeliveryResult{Kind: discordDeliveryRetryable}
		}
		return discordDeliveryResult{Kind: discordDeliveryUncertain}
	}
	if result.Status >= 200 && result.Status < 300 {
		return discordDeliveryResult{Kind: discordDeliveryUncertain}
	}
	return discordDeliveryResult{Kind: discordDeliveryPermanent}
}

func classifyDiscordEdit(result discordProviderResult) discordDeliveryResult {
	if result.Status == 429 {
		return discordDeliveryResult{Kind: discordDeliveryRetryable, RetryAfter: result.RetryAfter}
	}
	if result.Status >= 500 {
		return discordDeliveryResult{Kind: discordDeliveryRetryable}
	}
	if result.DiscordCode == 10015 {
		return discordDeliveryResult{Kind: discordDeliveryBrokenDestination}
	}
	if result.DiscordCode == 10008 {
		return discordDeliveryResult{Kind: discordDeliveryRecoverMessage}
	}
	if result.Status == 0 {
		return discordDeliveryResult{Kind: discordDeliveryRetryable}
	}
	if result.Status >= 200 && result.Status < 300 {
		if result.Err != nil {
			return discordDeliveryResult{Kind: discordDeliveryRetryable}
		}
		return discordDeliveryResult{Kind: discordDeliverySucceeded}
	}
	return discordDeliveryResult{Kind: discordDeliveryPermanent}
}
