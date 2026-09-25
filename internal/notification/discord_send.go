package notification

import (
	"context"
	"errors"
	"net/http"
)

// Recovery and broken-destination handling are separate E3-19/E3-20 work.
// Keep their signals distinct without changing destination or sync state here.
var (
	ErrDiscordMessageRecoveryPending  = errors.New("discord message recovery is not configured")
	ErrDiscordDestinationBreakPending = errors.New("discord destination break handling is not configured")
)

var errDiscordSendAborted = errors.New("discord send transaction intentionally rolled back")

// errDiscordCreateUnrecorded means an earlier automatic create may have
// reached Discord without a durable message ID. Creating again could duplicate.
var errDiscordCreateUnrecorded = errors.New("discord create already attempted without recorded outcome")

// NewDiscordSendTransport is the isolated E3-18 transport constructor. Runtime
// registration remains E3-21. The repository must use the same PostgreSQL
// transaction for every callback read and write, including the current snapshot.
func NewDiscordSendTransport(store DiscordMessageSyncRepository, key []byte, baseURL, instanceName string, client *http.Client) *DiscordTransport {
	return &DiscordTransport{
		destinations: store, syncStore: store, encryptionKey: key,
		baseURL: baseURL, instanceName: instanceName,
		webhooks: newDiscordWebhookClient(client, discordWebhookAPIURL),
	}
}

// Send retains the E1 Transport contract. A zero delivery ID is resolved
// against the unique durable (activity sequence, target) claim under the lock;
// Worker passes the exact claimed delivery to SendClaimed instead.
func (t *DiscordTransport) Send(ctx context.Context, activity Activity, target Target) SendResult {
	return t.SendClaimed(ctx, ClaimedDelivery{Activity: activity, Target: target})
}

func (t *DiscordTransport) SendClaimed(ctx context.Context, delivery ClaimedDelivery) SendResult {
	id, ok := decodeDiscordTarget(delivery.Target)
	if !ok {
		return SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
	}
	// A cheap preliminary check avoids waiting on a lock for an already stale
	// generation. This is NOT the credential used for sending: resolve again
	// on the transaction handle after acquiring the lock.
	if _, _, err := t.ResolveTarget(ctx, delivery.Target); err != nil {
		if errors.Is(err, ErrNoLongerEligible) {
			return SendResult{Outcome: OutcomeNoLongerEligible, Err: err}
		}
		return SendResult{Outcome: OutcomeRetryable, Err: errors.New("read discord destination failed")}
	}
	if t.syncStore == nil || t.webhooks == nil {
		return SendResult{Outcome: OutcomeRetryable, Err: ErrTransportNotConfigured}
	}

	result := SendResult{Outcome: OutcomeRetryable, Err: errors.New("discord synchronization failed")}
	created := false
	advance := func(repo DiscordMessageSyncRepository, messageID string) error {
		changed, err := repo.AdvanceDiscordMessageSync(ctx, DiscordMessageSync{
			DestinationID: id, GameRequestID: delivery.Activity.GameRequestID,
			MessageID: messageID, AppliedVersion: delivery.Activity.Sequence,
		})
		if err != nil {
			return err
		}
		if !changed {
			return errors.New("discord sync advance invariant failed")
		}
		return nil
	}
	err := t.syncStore.WithDiscordMessageSyncLock(ctx, id, delivery.Activity.GameRequestID, func(repo DiscordMessageSyncRepository) error {
		destination, ciphertext, err := repo.GetDiscordDestination(ctx)
		if errors.Is(err, ErrDiscordDestinationNotFound) {
			result = SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
			return errDiscordSendAborted
		}
		if err != nil {
			return err
		}
		if destination.ID != id || destination.State != DiscordDestinationConnected || len(ciphertext) == 0 {
			result = SendResult{Outcome: OutcomeNoLongerEligible, Err: ErrNoLongerEligible}
			return errDiscordSendAborted
		}
		claimID, err := repo.ClaimedDiscordDelivery(ctx, delivery.DeliveryID, delivery.Activity.Sequence, delivery.Activity.GameRequestID, delivery.Target)
		if err != nil {
			return err
		}
		if claimID == 0 {
			result = SendResult{Outcome: OutcomeAlreadyRecorded}
			return errDiscordSendAborted
		}
		sync, err := repo.GetDiscordMessageSync(ctx, id, delivery.Activity.GameRequestID)
		if errors.Is(err, ErrDiscordMessageSyncNotFound) {
			sync = DiscordMessageSync{}
		} else if err != nil {
			return err
		}
		newer, err := repo.NewerDiscordDelivery(ctx, delivery.Activity.Sequence, delivery.Activity.GameRequestID, delivery.Target)
		if err != nil {
			return err
		}
		version := newer
		if sync.AppliedVersion > version {
			version = sync.AppliedVersion
		}
		if version >= delivery.Activity.Sequence {
			changed, err := repo.RecordSuperseded(ctx, claimID, version)
			if err != nil {
				return err
			}
			if !changed {
				return errors.New("discord supersession invariant failed")
			}
			result = SendResult{Outcome: OutcomeAlreadyRecorded}
			return nil
		}
		summary, err := repo.GetGameRequestSummary(ctx, delivery.Activity.GameRequestID)
		if err != nil {
			return err
		}
		credential, err := DecryptDiscordCredential(t.encryptionKey, ciphertext)
		if err != nil {
			return errors.New("decrypt discord delivery credential failed")
		}
		message := buildDiscordMessage(summary, t.baseURL, t.instanceName)
		if sync.MessageID != "" {
			classified := classifyDiscordEdit(t.webhooks.EditMessage(ctx, credential, sync.MessageID, message))
			result = discordSendResult(classified)
			if classified.Kind != discordDeliverySucceeded {
				return errDiscordSendAborted
			}
			return advance(repo, sync.MessageID)
		}
		// The reservation commits on its own connection before Discord sees
		// the create. If the message ID or outcome is then lost, no automatic
		// replay for this destination generation and request creates again.
		// ponytail: holds a second pool connection per create; an exhausted
		// pool times out as retryable before any provider call.
		reserved, err := t.syncStore.ReserveDiscordCreate(ctx, id, delivery.Activity.GameRequestID)
		if err != nil {
			return err
		}
		if !reserved {
			result = SendResult{Outcome: OutcomeUncertain, Err: errDiscordCreateUnrecorded}
			return errDiscordSendAborted
		}
		created = true
		provider := t.webhooks.CreateMessage(ctx, credential, message)
		classified := classifyDiscordCreate(provider)
		if classified.Kind == discordDeliverySucceeded {
			result = SendResult{Outcome: OutcomeSucceeded}
			if err := advance(repo, provider.MessageID); err != nil {
				return err
			}
			return releaseDiscordCreate(ctx, repo, id, delivery.Activity.GameRequestID)
		}
		if !discordCreateRejected(provider) {
			// Keep the reservation: Discord may have created the message.
			result = SendResult{Outcome: OutcomeUncertain, Err: errors.New("discord create outcome uncertain")}
			return errDiscordSendAborted
		}
		result = discordSendResult(classified)
		return releaseDiscordCreate(ctx, repo, id, delivery.Activity.GameRequestID)
	})
	if errors.Is(err, errDiscordSendAborted) {
		return result
	}
	if err != nil {
		// After a create request is sent, a lost sync write or commit leaves
		// its reservation in place. A known-message edit is safe to replay.
		if created {
			return SendResult{Outcome: OutcomeUncertain, Err: errors.New("discord create synchronization uncertain")}
		}
		return SendResult{Outcome: OutcomeRetryable, Err: errors.New("discord synchronization failed")}
	}
	return result
}

// discordCreateRejected reports provider-proven non-creation: the request was
// never transmitted, or Discord answered with a definitive HTTP failure
// response (4xx or 5xx). Only an ambiguous outcome without an HTTP response
// (no confirmed pre-transmission failure) is not proof and keeps the
// reservation.
func discordCreateRejected(provider discordProviderResult) bool {
	if provider.Status == 0 {
		return provider.Failure == discordBeforeTransmission
	}
	return provider.Status >= 400
}

func releaseDiscordCreate(ctx context.Context, repo DiscordMessageSyncRepository, destinationID, gameRequestID string) error {
	released, err := repo.ReleaseDiscordCreate(ctx, destinationID, gameRequestID)
	if err != nil {
		return err
	}
	if !released {
		return errors.New("discord create reservation invariant failed")
	}
	return nil
}

func discordSendResult(classified discordDeliveryResult) SendResult {
	switch classified.Kind {
	case discordDeliverySucceeded:
		return SendResult{Outcome: OutcomeSucceeded}
	case discordDeliveryRetryable:
		return SendResult{Outcome: OutcomeRetryable, RetryAfter: classified.RetryAfter, Err: errors.New("discord provider retryable failure")}
	case discordDeliveryUncertain:
		return SendResult{Outcome: OutcomeUncertain, Err: errors.New("discord create outcome uncertain")}
	case discordDeliveryRecoverMessage:
		return SendResult{Outcome: OutcomePermanent, Err: ErrDiscordMessageRecoveryPending}
	case discordDeliveryBrokenDestination:
		return SendResult{Outcome: OutcomePermanent, Err: ErrDiscordDestinationBreakPending}
	default:
		return SendResult{Outcome: OutcomePermanent, Err: errors.New("discord provider permanent failure")}
	}
}
