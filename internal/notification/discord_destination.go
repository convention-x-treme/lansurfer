package notification

import (
	"context"
	"log/slog"
)

// DiscordLifecycleService centralizes the two definitive Discord
// destination lifecycle transitions: explicit administrator disconnect and
// a permanently broken/deleted webhook. Both remove local capability to
// deliver to the destination (and suppress its still-pending/claimed
// deliveries) before any remote cleanup is attempted, and neither ever
// replays historical notification_work: this type depends only on
// DiscordDestinationRepository and DiscordWebhookOAuthProvider, never on
// anything that reads notification_work or game_request_activity.
type DiscordLifecycleService struct {
	destinations  DiscordDestinationRepository
	provider      DiscordWebhookOAuthProvider
	encryptionKey []byte
	logger        *slog.Logger
}

func NewDiscordLifecycleService(destinations DiscordDestinationRepository, provider DiscordWebhookOAuthProvider, encryptionKey []byte, logger *slog.Logger) *DiscordLifecycleService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DiscordLifecycleService{destinations: destinations, provider: provider, encryptionKey: encryptionKey, logger: logger}
}

// Disconnect removes the local destination immediately: DeleteDiscordDestination
// atomically removes the destination row and suppresses its still-pending/
// claimed deliveries in one local transaction, which commits before this
// method attempts anything remote. Existing Discord channel messages are
// never touched (this method never calls anything but DeleteWebhook).
//
// Only after that local commit does Disconnect best-effort delete the
// remote webhook, decrypting the destination's credential only in memory,
// only in this immediate cleanup path. A remote cleanup failure is logged
// without secret material and never reported as an error: the local
// disconnect already succeeded and must never be rolled back or
// "restored" because of it.
func (s *DiscordLifecycleService) Disconnect(ctx context.Context) error {
	previous, previousCiphertext, err := s.destinations.GetDiscordDestination(ctx)
	if err != nil {
		return err // includes ErrDiscordDestinationNotFound
	}

	if err := s.destinations.DeleteDiscordDestination(ctx); err != nil {
		return err
	}

	if previous.State == DiscordDestinationConnected && len(previousCiphertext) > 0 {
		s.cleanupWebhook(ctx, previousCiphertext)
	}
	return nil
}

// Break transitions destinationID's connected destination to broken:
// MarkDiscordDestinationBroken atomically retains safe metadata, deletes
// the now-unusable credential ciphertext, records reason, and suppresses
// that exact generation's still-pending/claimed deliveries, all in one
// local transaction. Break never attempts a remote cleanup: a broken
// transition means the webhook is already known unusable/deleted, so
// there is nothing left to delete remotely. It returns
// ErrDiscordDestinationNotFound when destinationID is not the current
// connected destination, so a caller that raced a replacement never
// touches the new generation.
func (s *DiscordLifecycleService) Break(ctx context.Context, destinationID, reason string) error {
	broken, err := s.destinations.MarkDiscordDestinationBroken(ctx, destinationID, reason)
	if err != nil {
		return err
	}
	if !broken {
		return ErrDiscordDestinationNotFound
	}
	return nil
}

// cleanupWebhook decrypts ciphertext only in memory, in this immediate
// cleanup path, and deletes the resulting credential's remote webhook.
// Neither the decrypted credential nor the delete failure below is ever
// logged with secret material: only the safe diagnostic error message
// (never containing webhook ID/token) is recorded.
func (s *DiscordLifecycleService) cleanupWebhook(ctx context.Context, ciphertext DiscordCredentialCiphertext) {
	credential, err := DecryptDiscordCredential(s.encryptionKey, ciphertext)
	if err != nil {
		s.logger.Error("decrypt discord webhook credential for disconnect cleanup", "error", err)
		return
	}
	if err := s.provider.DeleteWebhook(ctx, credential); err != nil {
		s.logger.Error("delete discord webhook on disconnect", "error", err)
	}
}
