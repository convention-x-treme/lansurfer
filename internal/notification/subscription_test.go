package notification

import (
	"context"
	"errors"
	"testing"
)

type fakeSubscriptionRepository struct {
	upserted           PushSubscriptionRecord
	upsertErr          error
	subs               []PushSubscriptionRecord
	deleted            map[int64]bool
	deleteByHashCalled bool
}

func (f *fakeSubscriptionRepository) UpsertSubscription(ctx context.Context, sub PushSubscriptionRecord) (PushSubscriptionRecord, error) {
	f.upserted = sub
	if f.upsertErr != nil {
		return PushSubscriptionRecord{}, f.upsertErr
	}
	sub.ID = 1
	return sub, nil
}

func (f *fakeSubscriptionRepository) ListSubscriptions(context.Context, string) ([]PushSubscriptionRecord, error) {
	return f.subs, nil
}

func (f *fakeSubscriptionRepository) DeleteSubscription(ctx context.Context, id int64, userID string) (bool, error) {
	if f.deleted == nil {
		f.deleted = map[int64]bool{}
	}
	f.deleted[id] = true
	return true, nil
}

func (f *fakeSubscriptionRepository) DeleteSubscriptionByDeviceHash(context.Context, string, string) (bool, error) {
	f.deleteByHashCalled = true
	return true, nil
}

type fakePreferenceRepository struct {
	preference PushPreference
	setCalls   []PushPreference
}

func (f *fakePreferenceRepository) GetPreference(context.Context, string) (PushPreference, error) {
	return f.preference, nil
}

func (f *fakePreferenceRepository) SetPreference(ctx context.Context, userID string, preference PushPreference) error {
	f.setCalls = append(f.setCalls, preference)
	f.preference = preference
	return nil
}

func testEncryptionKey() []byte { return make([]byte, 32) }

func TestSubscriptionServiceEnablePersistsEncryptedSubscriptionAndSetsEnabled(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Enable(t.Context(), "user-1", "device-hash-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if subs.upserted.UserID != "user-1" || subs.upserted.Label != "This device" || subs.upserted.DeviceIDHash != "device-hash-1" {
		t.Fatalf("upserted subscription = %#v, want the given identity fields", subs.upserted)
	}
	if len(subs.upserted.EndpointCiphertext) == 0 || string(subs.upserted.EndpointCiphertext) == "https://push.example/endpoint" {
		t.Fatal("Enable() stored the endpoint in plaintext, want ciphertext")
	}
	if subs.upserted.EndpointFingerprint == "" {
		t.Fatal("Enable() did not compute an endpoint fingerprint")
	}
	if len(prefs.setCalls) != 1 || !prefs.setCalls[0].WebPushEnabled {
		t.Fatalf("SetPreference() calls = %#v, want exactly one call enabling Web Push", prefs.setCalls)
	}
}

func TestSubscriptionServiceEnablePreservesExistingAudiencePreference(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: false, NewRequestAudience: AudienceAll}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Enable(t.Context(), "user-1", "device-hash-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if len(prefs.setCalls) != 1 || prefs.setCalls[0].NewRequestAudience != AudienceAll {
		t.Fatalf("SetPreference() call = %#v, want the pre-existing audience preference kept", prefs.setCalls)
	}
}

func TestSubscriptionServiceDisableKeepsSubscriptionsAndAudience(t *testing.T) {
	subs := &fakeSubscriptionRepository{subs: []PushSubscriptionRecord{{ID: 1, Label: "This device"}}}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: true, NewRequestAudience: AudienceAll}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Disable(t.Context(), "user-1"); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if len(prefs.setCalls) != 1 || prefs.setCalls[0].WebPushEnabled || prefs.setCalls[0].NewRequestAudience != AudienceAll {
		t.Fatalf("SetPreference() call = %#v, want disabled with audience unchanged", prefs.setCalls)
	}
	devices, err := service.ListDevices(t.Context(), "user-1")
	if err != nil || len(devices) != 1 {
		t.Fatalf("ListDevices() after Disable() = %#v, %v, want the subscription kept", devices, err)
	}
}

func TestSubscriptionServiceRemoveDeviceDoesNotChangeEnabledPreference(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: true, NewRequestAudience: AudienceFollowed}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.RemoveDevice(t.Context(), "user-1", 1); err != nil {
		t.Fatalf("RemoveDevice() error = %v", err)
	}
	if len(prefs.setCalls) != 0 {
		t.Fatalf("RemoveDevice() called SetPreference() %d times, want 0 (removing a device must not touch web_push_enabled)", len(prefs.setCalls))
	}
	if !subs.deleted[1] {
		t.Fatal("RemoveDevice() did not delete the subscription")
	}
}

func TestSubscriptionServiceSetAudienceRejectsUnknownValue(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.SetAudience(t.Context(), "user-1", "everyone"); !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("SetAudience() with an unknown value error = %v, want %v", err, ErrInvalidAudience)
	}
	if len(prefs.setCalls) != 0 {
		t.Fatal("SetAudience() with an invalid value must not call SetPreference()")
	}
}
