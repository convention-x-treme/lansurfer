package notification

import (
	"errors"
	"testing"
)

// Disconnect removes the local destination and, only afterward, attempts a
// best-effort remote cleanup using the credential that was active before
// the local removal.
func TestDiscordLifecycleServiceDisconnectRemovesDestinationAndCleansUpWebhook(t *testing.T) {
	_, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "webhook-id-1", WebhookToken: "webhook-token-1"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-1", ChannelID: "channel-1"},
		credentialCiphertext: oldCiphertext,
	}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if repository.deleteCalls != 1 {
		t.Fatalf("DeleteDiscordDestination calls = %d, want 1", repository.deleteCalls)
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("DeleteWebhook calls = %d, want 1", provider.deleteCalls)
	}
	if provider.deletedCredential.WebhookID != "webhook-id-1" {
		t.Fatalf("DeleteWebhook credential = %#v, want the destination's credential", provider.deletedCredential)
	}
}

// A missing destination surfaces ErrDiscordDestinationNotFound and never
// attempts a local delete or remote cleanup.
func TestDiscordLifecycleServiceDisconnectWithNoDestinationFails(t *testing.T) {
	_, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{getErr: ErrDiscordDestinationNotFound}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Disconnect(t.Context()); !errors.Is(err, ErrDiscordDestinationNotFound) {
		t.Fatalf("Disconnect() error = %v, want ErrDiscordDestinationNotFound", err)
	}
	if repository.deleteCalls != 0 {
		t.Fatalf("DeleteDiscordDestination calls = %d, want 0", repository.deleteCalls)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0", provider.deleteCalls)
	}
}

// A local delete failure must never attempt a remote cleanup: the local
// disconnect never committed.
func TestDiscordLifecycleServiceDisconnectLocalFailureSkipsRemoteCleanup(t *testing.T) {
	_, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "webhook-id-1", WebhookToken: "webhook-token-1"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-1", ChannelID: "channel-1"},
		credentialCiphertext: oldCiphertext,
		deleteErr:            errors.New("database unavailable"),
	}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Disconnect(t.Context()); err == nil {
		t.Fatal("Disconnect() with a local delete failure succeeded, want an error")
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 when the local delete failed", provider.deleteCalls)
	}
}

// A remote cleanup failure on disconnect must never be surfaced as an
// error and must never restore the local destination: the local removal
// already succeeded.
func TestDiscordLifecycleServiceDisconnectCleanupFailureStillDisconnected(t *testing.T) {
	_, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "webhook-id-1", WebhookToken: "webhook-token-1"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-1", ChannelID: "channel-1"},
		credentialCiphertext: oldCiphertext,
	}
	provider := &fakeDiscordWebhookOAuthProvider{deleteErr: errors.New("discord unavailable")}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v, want nil even though remote cleanup fails", err)
	}
	if repository.deleteCalls != 1 {
		t.Fatalf("DeleteDiscordDestination calls = %d, want 1", repository.deleteCalls)
	}
}

// A previously broken destination has no remote webhook left: Disconnect
// must never call DeleteWebhook for it.
func TestDiscordLifecycleServiceDisconnectSkipsCleanupForBrokenDestination(t *testing.T) {
	_, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{
		destination: DiscordDestination{State: DiscordDestinationBroken, GuildID: "guild-1", ChannelID: "channel-1"},
	}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 for an already-broken destination", provider.deleteCalls)
	}
}

// Break delegates directly to the repository's atomic broken transition
// and never attempts any remote call: a broken destination's webhook is
// already known unusable/deleted.
func TestDiscordLifecycleServiceBreakNeverAttemptsRemoteCleanup(t *testing.T) {
	_, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{
		destination: DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-1", ChannelID: "channel-1"},
	}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Break(t.Context(), "webhook deleted (10015)"); err != nil {
		t.Fatalf("Break() error = %v", err)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0: Break never contacts the provider", provider.deleteCalls)
	}
}

// Break propagates a repository failure (for example, no connected
// destination to break) without swallowing it.
func TestDiscordLifecycleServiceBreakPropagatesRepositoryFailure(t *testing.T) {
	_, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{markBrokenErr: ErrDiscordDestinationNotFound}
	provider := &fakeDiscordWebhookOAuthProvider{}
	service := NewDiscordLifecycleService(repository, provider, key, nil)

	if err := service.Break(t.Context(), "reason"); !errors.Is(err, ErrDiscordDestinationNotFound) {
		t.Fatalf("Break() error = %v, want ErrDiscordDestinationNotFound", err)
	}
}
