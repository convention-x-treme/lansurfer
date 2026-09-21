package notification

import (
	"context"
	"errors"
	"fmt"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

var ErrInvalidAudience = errors.New("audience must be \"followed\" or \"all\"")

// SubscriptionService is the only place plaintext Push subscription material
// and the data-encryption key meet: it encrypts on the way into
// SubscriptionRepository and never decrypts, since the settings surface
// never displays raw endpoint/p256dh/auth. It also owns the product rules
// around enable/disable/device removal that keep "web_push_enabled",
// "has at least one device", and "audience preference" independent states.
type SubscriptionService struct {
	subscriptions SubscriptionRepository
	preferences   PreferenceRepository
	encryptionKey []byte
}

func NewSubscriptionService(subscriptions SubscriptionRepository, preferences PreferenceRepository, encryptionKey []byte) *SubscriptionService {
	return &SubscriptionService{subscriptions: subscriptions, preferences: preferences, encryptionKey: encryptionKey}
}

// Enable runs the complete enable sequence for one browser subscription:
// persist (or reassign) it, and set web_push_enabled=true while preserving
// whatever audience preference the user already had.
func (s *SubscriptionService) Enable(ctx context.Context, userID, deviceIDHash, label, endpoint, p256dh, auth string) error {
	endpointCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, endpoint)
	if err != nil {
		return fmt.Errorf("encrypt endpoint: %w", err)
	}
	p256dhCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, p256dh)
	if err != nil {
		return fmt.Errorf("encrypt p256dh: %w", err)
	}
	authCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, auth)
	if err != nil {
		return fmt.Errorf("encrypt auth: %w", err)
	}
	fingerprint := platformcrypto.Fingerprint(s.encryptionKey, endpoint)

	if _, err := s.subscriptions.UpsertSubscription(ctx, PushSubscriptionRecord{
		UserID: userID, Label: label, DeviceIDHash: deviceIDHash, EndpointFingerprint: fingerprint,
		EndpointCiphertext: endpointCiphertext, P256dhCiphertext: p256dhCiphertext, AuthCiphertext: authCiphertext,
	}); err != nil {
		return fmt.Errorf("upsert push subscription: %w", err)
	}

	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.WebPushEnabled = true
	return s.preferences.SetPreference(ctx, userID, current)
}

// Disable pauses Push delivery without deleting any device subscription, so
// re-enabling can reuse them.
func (s *SubscriptionService) Disable(ctx context.Context, userID string) error {
	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.WebPushEnabled = false
	return s.preferences.SetPreference(ctx, userID, current)
}

// SetAudience updates only the new-request audience preference, leaving
// web_push_enabled untouched.
func (s *SubscriptionService) SetAudience(ctx context.Context, userID, audience string) error {
	if audience != AudienceFollowed && audience != AudienceAll {
		return ErrInvalidAudience
	}
	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.NewRequestAudience = audience
	return s.preferences.SetPreference(ctx, userID, current)
}

// RemoveDevice deletes one subscription the caller owns. It never changes
// web_push_enabled: "enabled with zero devices" is a valid, distinct state.
func (s *SubscriptionService) RemoveDevice(ctx context.Context, userID string, subscriptionID int64) error {
	if _, err := s.subscriptions.DeleteSubscription(ctx, subscriptionID, userID); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

// RemoveDeviceByHash deletes the caller's subscription for one browser
// device, identified by the opaque device cookie's hash. Used by logout.
func (s *SubscriptionService) RemoveDeviceByHash(ctx context.Context, userID, deviceIDHash string) error {
	if _, err := s.subscriptions.DeleteSubscriptionByDeviceHash(ctx, userID, deviceIDHash); err != nil {
		return fmt.Errorf("delete push subscription by device hash: %w", err)
	}
	return nil
}

func (s *SubscriptionService) ListDevices(ctx context.Context, userID string) ([]PushSubscriptionRecord, error) {
	return s.subscriptions.ListSubscriptions(ctx, userID)
}

func (s *SubscriptionService) Preference(ctx context.Context, userID string) (PushPreference, error) {
	return s.preferences.GetPreference(ctx, userID)
}
