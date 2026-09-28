package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDiscordTargetsGenerationScoped(t *testing.T) {
	oldID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	newID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	repo := &fakeDiscordDestinationRepository{getErr: ErrDiscordDestinationNotFound}
	transport := NewDiscordTransport(repo)
	activity := Activity{GameRequestID: "existing-request", Kind: ActivityJoined}
	if transport.Name() != "discord" {
		t.Fatalf("name = %s", transport.Name())
	}
	targets, err := transport.Targets(t.Context(), activity)
	if err != nil || len(targets) != 0 {
		t.Fatalf("absent = %v, %v", targets, err)
	}
	repo.getErr = nil
	repo.destination = DiscordDestination{ID: oldID, State: DiscordDestinationConnected, GuildID: "guild-secret", ChannelID: "channel-secret"}
	repo.credentialCiphertext = DiscordCredentialCiphertext("encrypted-webhook-secret")
	old, err := transport.Targets(t.Context(), activity)
	if err != nil || len(old) != 1 || old[0].Transport != "discord" {
		t.Fatalf("connected = %v, %v", old, err)
	}
	for _, secret := range []string{"webhook-secret", "guild-secret", "channel-secret"} {
		if strings.Contains(old[0].Key, secret) {
			t.Fatalf("target leaked %s", secret)
		}
	}
	again, err := transport.Targets(t.Context(), activity)
	if err != nil || len(again) != 1 || again[0] != old[0] {
		t.Fatalf("unstable target = %v, %v", again, err)
	}
	dest, credential, err := transport.ResolveTarget(t.Context(), old[0])
	if err != nil || dest.ID != oldID || string(credential) != "encrypted-webhook-secret" {
		t.Fatalf("resolve = %v, %q, %v", dest, credential, err)
	}
	repo.destination.ID = newID // replacement installs a different generation, same singleton row
	repo.credentialCiphertext = DiscordCredentialCiphertext("new-encrypted-webhook-secret")
	replaced, err := transport.Targets(t.Context(), activity)
	if err != nil || len(replaced) != 1 || replaced[0].Key == old[0].Key {
		t.Fatalf("replacement = %v, %v", replaced, err)
	}
	if _, _, err := transport.ResolveTarget(t.Context(), old[0]); !errors.Is(err, ErrNoLongerEligible) {
		t.Fatalf("old target resolved to new destination: %v", err)
	}
	repo.destination.State = DiscordDestinationBroken
	broken, err := transport.Targets(t.Context(), activity)
	if err != nil || len(broken) != 0 {
		t.Fatalf("broken = %v, %v", broken, err)
	}
	repo.destination = DiscordDestination{ID: oldID, State: DiscordDestinationConnected} // reconnect gets fresh ID in real storage; simulate distinct ID next
	repo.destination.ID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	reconnected, err := transport.Targets(t.Context(), activity)
	if err != nil || len(reconnected) != 1 || reconnected[0].Key == replaced[0].Key || reconnected[0].Key == old[0].Key {
		t.Fatalf("reconnect = %v, %v", reconnected, err)
	}
	if _, _, err := transport.ResolveTarget(t.Context(), replaced[0]); !errors.Is(err, ErrNoLongerEligible) {
		t.Fatalf("broken generation resolved after reconnect: %v", err)
	}
}

func TestDiscordTargetRejectsInvalidOrStaleIdentity(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	repo := &fakeDiscordDestinationRepository{destination: DiscordDestination{ID: id, State: DiscordDestinationConnected}}
	transport := NewDiscordTransport(repo)
	for _, key := range []string{"", "instance", id, "d1:", "d1:" + strings.ToUpper(id), "d1:" + id + "/new", "d1:00000000-0000-0000-0000-00000000000g"} {
		if _, _, err := transport.ResolveTarget(t.Context(), Target{Transport: "discord", Key: key}); !errors.Is(err, ErrNoLongerEligible) {
			t.Errorf("accepted invalid key %q: %v", key, err)
		}
	}
	if _, _, err := transport.ResolveTarget(t.Context(), Target{Transport: "webpush", Key: "d1:" + id}); !errors.Is(err, ErrNoLongerEligible) {
		t.Errorf("accepted wrong transport: %v", err)
	}
	repo.destination.State = DiscordDestinationBroken
	if _, _, err := transport.ResolveTarget(context.Background(), Target{Transport: "discord", Key: "d1:" + id}); !errors.Is(err, ErrNoLongerEligible) {
		t.Errorf("resolved broken destination: %v", err)
	}
}
