package notification

import (
	"context"
	"errors"
	"net/http"
)

var errDiscordSendAborted = errors.New("discord send transaction intentionally rolled back")

// errDiscordCreateUnrecorded means an earlier automatic create (including an
// E3-19 replacement create) may have reached Discord without a durable
// message ID. Creating again could duplicate.
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
	// The destination, exact current claim, and other same-generation
	// deliveries are finalized together on the sync transaction. Worker
	// recording the Permanent result afterward is a guarded no-op.
	breakDestination := func(repo DiscordMessageSyncRepository, claimID int64, reason string) error {
		broken, err := repo.MarkDiscordDestinationBroken(ctx, id, reason)
		if err != nil {
			return err
		}
		if !broken {
			return errors.New("discord destination generation changed during break")
		}
		if err := repo.RecordPermanent(ctx, claimID, reason); err != nil {
			return err
		}
		if _, err := repo.SuppressOtherDiscordDeliveries(ctx, delivery.Target.Key, claimID, reason); err != nil {
			return err
		}
		result = SendResult{Outcome: OutcomePermanent, Err: errors.New(reason)}
		return nil
	}
	// createCurrent renders the latest current Game Request snapshot (never
	// historical/intermediate activity, and freshly resolved rather than
	// reused from a failed edit's snapshot) and creates exactly one
	// replacement message under the same E3-18 durable pre-send reservation
	// used by a fresh create: an E3-19 10008-recovery create is externally
	// non-idempotent exactly like the initial create, so it must not bypass
	// that protection or introduce a second reservation mechanism.
	createCurrent := func(repo DiscordMessageSyncRepository, claimID int64, credential DiscordWebhookCredential) error {
		summary, err := repo.GetGameRequestSummary(ctx, delivery.Activity.GameRequestID)
		if err != nil {
			return err
		}
		message := buildDiscordMessage(summary, t.baseURL, t.instanceName)
		// The reservation commits on its own connection before Discord sees
		// the create. If the message ID or outcome is then lost, no automatic
		// replay for this destination generation and request creates again.
		// ponytail: holds a second pool connection per create; an exhausted
		// pool times out as retryable before any provider call.
		reserved, err := t.syncStore.ReserveDiscordCreate(ctx, id, delivery.Activity.GameRequestID, claimID)
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
			return releaseDiscordCreate(ctx, repo, id, delivery.Activity.GameRequestID, claimID)
		}
		if classified.Kind == discordDeliveryBrokenDestination {
			if err := breakDestination(repo, claimID, "discord webhook no longer exists (10015 unknown webhook)"); err != nil {
				return err
			}
			return releaseDiscordCreate(ctx, repo, id, delivery.Activity.GameRequestID, claimID)
		}
		if !discordCreateRejected(provider) {
			// Keep the reservation: Discord may have created the message.
			result = SendResult{Outcome: OutcomeUncertain, Err: errors.New("discord create outcome uncertain")}
			return errDiscordSendAborted
		}
		result = discordSendResult(classified)
		return releaseDiscordCreate(ctx, repo, id, delivery.Activity.GameRequestID, claimID)
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
		// Equal version with an empty message slot is not "already applied":
		// an E3-19 compare-and-clear recovery may have cleared the reference
		// at this exact version without yet creating its replacement (an
		// uncertain outcome, a lost commit, or a crash between the two). That
		// pending recovery must still reach the create branch below, never
		// be marked superseded merely because the version already matches.
		alreadyApplied := version == delivery.Activity.Sequence && sync.MessageID != ""
		if version > delivery.Activity.Sequence || alreadyApplied {
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
		credential, err := DecryptDiscordCredential(t.encryptionKey, ciphertext)
		if err != nil {
			return errors.New("decrypt discord delivery credential failed")
		}
		if sync.MessageID == "" {
			return createCurrent(repo, claimID, credential)
		}
		summary, err := repo.GetGameRequestSummary(ctx, delivery.Activity.GameRequestID)
		if err != nil {
			return err
		}
		message := buildDiscordMessage(summary, t.baseURL, t.instanceName)
		provider := t.webhooks.EditMessage(ctx, credential, sync.MessageID, message)
		classified := classifyDiscordEdit(provider)
		if classified.Kind == discordDeliveryRecoverMessage {
			// Compare-and-clear only the exact stale reference this read saw:
			// never a message ID/version a concurrent mutation already moved
			// past. Clearing never lowers the applied version, and the
			// replacement create below reuses this same serialization
			// boundary rather than starting a second one.
			cleared, err := repo.ClearDiscordMessageSync(ctx, id, delivery.Activity.GameRequestID, sync.MessageID, sync.AppliedVersion)
			if err != nil {
				return err
			}
			if !cleared {
				return errors.New("discord message reference already changed")
			}
			return createCurrent(repo, claimID, credential)
		}
		if classified.Kind == discordDeliveryBrokenDestination {
			return breakDestination(repo, claimID, "discord webhook no longer exists (10015 unknown webhook)")
		}
		result = discordSendResult(classified)
		if classified.Kind != discordDeliverySucceeded {
			return errDiscordSendAborted
		}
		return advance(repo, sync.MessageID)
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

func releaseDiscordCreate(ctx context.Context, repo DiscordMessageSyncRepository, destinationID, gameRequestID string, deliveryID int64) error {
	released, err := repo.ReleaseDiscordCreate(ctx, destinationID, gameRequestID, deliveryID)
	if err != nil {
		return err
	}
	if !released {
		return errors.New("discord create reservation invariant failed")
	}
	return nil
}

// discordSendResult maps a classification that SendClaimed does not handle
// with dedicated recovery/broken-destination logic of its own:
// discordDeliveryRecoverMessage (10008) and discordDeliveryBrokenDestination
// (10015) are always intercepted before reaching here, since both require
// mutating destination/sync state under the same serialization boundary
// (E3-19 recovery, E3-20 broken-destination transition).
func discordSendResult(classified discordDeliveryResult) SendResult {
	switch classified.Kind {
	case discordDeliverySucceeded:
		return SendResult{Outcome: OutcomeSucceeded}
	case discordDeliveryRetryable:
		return SendResult{Outcome: OutcomeRetryable, RetryAfter: classified.RetryAfter, Err: errors.New("discord provider retryable failure")}
	case discordDeliveryUncertain:
		return SendResult{Outcome: OutcomeUncertain, Err: errors.New("discord create outcome uncertain")}
	default:
		return SendResult{Outcome: OutcomePermanent, Err: errors.New("discord provider permanent failure")}
	}
}
