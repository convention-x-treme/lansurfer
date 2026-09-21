//go:build integration

package postgres

import (
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

func TestPushSubscriptionUpsertCreatesThenUpdatesInPlace(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Push Owner")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	created, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "This device", DeviceIDHash: "device-hash-1",
		EndpointFingerprint: "fingerprint-1", EndpointCiphertext: []byte("endpoint-cipher-1"),
		P256dhCiphertext: []byte("p256dh-1"), AuthCiphertext: []byte("auth-1"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() create error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("UpsertSubscription() returned a zero ID")
	}

	updated, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "Renamed device", DeviceIDHash: "device-hash-1",
		EndpointFingerprint: "fingerprint-1", EndpointCiphertext: []byte("endpoint-cipher-2"),
		P256dhCiphertext: []byte("p256dh-2"), AuthCiphertext: []byte("auth-2"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() update error = %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("UpsertSubscription() on the same fingerprint changed the row ID: %d != %d", updated.ID, created.ID)
	}

	subs, err := store.ListSubscriptions(t.Context(), owner.ID)
	if err != nil || len(subs) != 1 || subs[0].Label != "Renamed device" {
		t.Fatalf("ListSubscriptions() = %#v, %v, want exactly one row labeled %q", subs, err, "Renamed device")
	}
}

func TestPushSubscriptionUpsertReassignsOwnershipAcrossUsers(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	first, err := users.Register(t.Context(), "First Owner")
	if err != nil {
		t.Fatalf("register first user: %v", err)
	}
	second, err := users.Register(t.Context(), "Second Owner")
	if err != nil {
		t.Fatalf("register second user: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: first.ID, Label: "First's device", DeviceIDHash: "device-hash-shared",
		EndpointFingerprint: "shared-fingerprint", EndpointCiphertext: []byte("c1"),
		P256dhCiphertext: []byte("p1"), AuthCiphertext: []byte("a1"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() for first owner error = %v", err)
	}

	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: second.ID, Label: "Second's device", DeviceIDHash: "device-hash-shared",
		EndpointFingerprint: "shared-fingerprint", EndpointCiphertext: []byte("c2"),
		P256dhCiphertext: []byte("p2"), AuthCiphertext: []byte("a2"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() reassigning to second owner error = %v", err)
	}

	firstSubs, err := store.ListSubscriptions(t.Context(), first.ID)
	if err != nil || len(firstSubs) != 0 {
		t.Fatalf("ListSubscriptions(first) = %#v, %v, want none (reassigned away)", firstSubs, err)
	}
	secondSubs, err := store.ListSubscriptions(t.Context(), second.ID)
	if err != nil || len(secondSubs) != 1 || secondSubs[0].Label != "Second's device" {
		t.Fatalf("ListSubscriptions(second) = %#v, %v, want exactly the reassigned row", secondSubs, err)
	}
}

func TestPushSubscriptionDeleteIsScopedToOwner(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Delete Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	intruder, err := users.Register(t.Context(), "Delete Intruder")
	if err != nil {
		t.Fatalf("register intruder: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	sub, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "Owned device", DeviceIDHash: "device-hash-2",
		EndpointFingerprint: "fingerprint-2", EndpointCiphertext: []byte("c"),
		P256dhCiphertext: []byte("p"), AuthCiphertext: []byte("a"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() error = %v", err)
	}

	deleted, err := store.DeleteSubscription(t.Context(), sub.ID, intruder.ID)
	if err != nil || deleted {
		t.Fatalf("DeleteSubscription() by a non-owner = %v, %v, want false, nil", deleted, err)
	}
	deleted, err = store.DeleteSubscription(t.Context(), sub.ID, owner.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteSubscription() by the owner = %v, %v, want true, nil", deleted, err)
	}
	subs, err := store.ListSubscriptions(t.Context(), owner.ID)
	if err != nil || len(subs) != 0 {
		t.Fatalf("ListSubscriptions() after delete = %#v, %v, want none", subs, err)
	}
}

func TestPushSubscriptionDeleteByDeviceHash(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Logout Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "This device", DeviceIDHash: "device-hash-3",
		EndpointFingerprint: "fingerprint-3", EndpointCiphertext: []byte("c"),
		P256dhCiphertext: []byte("p"), AuthCiphertext: []byte("a"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() error = %v", err)
	}

	deleted, err := store.DeleteSubscriptionByDeviceHash(t.Context(), owner.ID, "device-hash-3")
	if err != nil || !deleted {
		t.Fatalf("DeleteSubscriptionByDeviceHash() = %v, %v, want true, nil", deleted, err)
	}
	deleted, err = store.DeleteSubscriptionByDeviceHash(t.Context(), owner.ID, "device-hash-3")
	if err != nil || deleted {
		t.Fatalf("second DeleteSubscriptionByDeviceHash() = %v, %v, want false, nil (already gone)", deleted, err)
	}
}

func TestPushPreferenceDefaultsToDisabledFollowedWhenUnset(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Preference Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	preference, err := store.GetPreference(t.Context(), owner.ID)
	if err != nil {
		t.Fatalf("GetPreference() error = %v", err)
	}
	if preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceFollowed {
		t.Fatalf("GetPreference() default = %#v, want disabled/followed", preference)
	}

	if err := store.SetPreference(t.Context(), owner.ID, notification.PushPreference{
		WebPushEnabled: true, NewRequestAudience: notification.AudienceAll,
	}); err != nil {
		t.Fatalf("SetPreference() error = %v", err)
	}
	preference, err = store.GetPreference(t.Context(), owner.ID)
	if err != nil || !preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceAll {
		t.Fatalf("GetPreference() after SetPreference() = %#v, %v, want enabled/all", preference, err)
	}

	if err := store.SetPreference(t.Context(), owner.ID, notification.PushPreference{
		WebPushEnabled: false, NewRequestAudience: notification.AudienceAll,
	}); err != nil {
		t.Fatalf("second SetPreference() error = %v", err)
	}
	preference, err = store.GetPreference(t.Context(), owner.ID)
	if err != nil || preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceAll {
		t.Fatalf("GetPreference() after disabling = %#v, %v, want disabled/all", preference, err)
	}
}
