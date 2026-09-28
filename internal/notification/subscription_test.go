package notification

import (
	"context"
	"errors"
	"testing"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
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

func newTestSubscriptionService(t *testing.T, subs SubscriptionRepository, prefs PreferenceRepository) *SubscriptionService {
	t.Helper()
	service, err := NewSubscriptionService(subs, prefs, testEncryptionKey())
	if err != nil {
		t.Fatalf("NewSubscriptionService() error = %v", err)
	}
	return service
}

func TestSubscriptionServiceEnablePersistsEncryptedSubscriptionAndSetsEnabled(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := newTestSubscriptionService(t, subs, prefs)

	if err := service.Enable(t.Context(), "user-1", "device-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if subs.upserted.UserID != "user-1" || subs.upserted.Label != "This device" {
		t.Fatalf("upserted subscription = %#v, want the given identity fields", subs.upserted)
	}
	if subs.upserted.DeviceIDHash == "" || subs.upserted.DeviceIDHash == "device-1" {
		t.Fatalf("upserted DeviceIDHash = %q, want the raw device ID hashed, not stored verbatim", subs.upserted.DeviceIDHash)
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

func TestSubscriptionServiceEnableUsesDerivedKeysNotRawEncryptionKey(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	key := testEncryptionKey()
	service := newTestSubscriptionService(t, subs, prefs)

	const endpoint = "https://push.example/endpoint"
	if err := service.Enable(t.Context(), "user-1", "device-1", "This device", endpoint, "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if subs.upserted.EndpointFingerprint == platformcrypto.Fingerprint(key, endpoint) {
		t.Fatal("Enable() fingerprint equals Fingerprint(rawKey, endpoint); it must use a derived subkey, not the raw data-encryption key")
	}
	if subs.upserted.DeviceIDHash == platformcrypto.Fingerprint(key, "device-1") {
		t.Fatal("Enable() device hash equals Fingerprint(rawKey, deviceID); it must use a derived subkey, not the raw data-encryption key")
	}
}

func TestSubscriptionServiceEnablePreservesExistingAudiencePreference(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: false, NewRequestAudience: AudienceAll}}
	service := newTestSubscriptionService(t, subs, prefs)

	if err := service.Enable(t.Context(), "user-1", "device-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if len(prefs.setCalls) != 1 || prefs.setCalls[0].NewRequestAudience != AudienceAll {
		t.Fatalf("SetPreference() call = %#v, want the pre-existing audience preference kept", prefs.setCalls)
	}
}

func TestSubscriptionServiceDisableKeepsSubscriptionsAndAudience(t *testing.T) {
	subs := &fakeSubscriptionRepository{subs: []PushSubscriptionRecord{{ID: 1, Label: "This device"}}}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: true, NewRequestAudience: AudienceAll}}
	service := newTestSubscriptionService(t, subs, prefs)

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
	service := newTestSubscriptionService(t, subs, prefs)

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

func TestSubscriptionServiceRemoveDeviceByHashHashesRawDeviceID(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := newTestSubscriptionService(t, subs, prefs)

	if err := service.RemoveDeviceByHash(t.Context(), "user-1", "device-1"); err != nil {
		t.Fatalf("RemoveDeviceByHash() error = %v", err)
	}
	if !subs.deleteByHashCalled {
		t.Fatal("RemoveDeviceByHash() did not call DeleteSubscriptionByDeviceHash()")
	}
}

func TestSubscriptionServiceCurrentDeviceID(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	service := newTestSubscriptionService(t, subs, &fakePreferenceRepository{})
	subs.subs = []PushSubscriptionRecord{
		{ID: 11, UserID: "user-2", DeviceIDHash: platformcrypto.Fingerprint(service.deviceHashKey, "device-1")},
		{ID: 12, UserID: "user-1", DeviceIDHash: platformcrypto.Fingerprint(service.deviceHashKey, "device-2")},
		{ID: 13, UserID: "user-1", DeviceIDHash: platformcrypto.Fingerprint(service.deviceHashKey, "device-1")},
	}
	for _, tc := range []struct {
		user, device string
		want         int64
	}{
		{"user-1", "device-1", 13},
		{"user-1", "unknown", 0},
		{"user-1", "", 0},
		{"user-2", "device-2", 0},
	} {
		got, err := service.CurrentDeviceID(t.Context(), tc.user, tc.device)
		if err != nil || got != tc.want {
			t.Errorf("CurrentDeviceID(%q, %q) = %d, %v; want %d, nil", tc.user, tc.device, got, err, tc.want)
		}
	}
}

func TestSubscriptionServiceSetAudienceRejectsUnknownValue(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := newTestSubscriptionService(t, subs, prefs)

	if err := service.SetAudience(t.Context(), "user-1", "everyone"); !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("SetAudience() with an unknown value error = %v, want %v", err, ErrInvalidAudience)
	}
	if len(prefs.setCalls) != 0 {
		t.Fatal("SetAudience() with an invalid value must not call SetPreference()")
	}
}
