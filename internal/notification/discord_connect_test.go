package notification

import (
	"context"
	"errors"
	"testing"
)

type fakeDiscordDestinationRepository struct {
	destination          DiscordDestination
	credentialCiphertext DiscordCredentialCiphertext
	getErr               error
	connectErr           error
	connectCalls         int
	connectedDestination DiscordDestination
	connectedCredential  DiscordCredentialCiphertext

	markBrokenErr error
	brokenCalls   int
	brokenReason  string

	deleteErr   error
	deleteCalls int
}

func (f *fakeDiscordDestinationRepository) GetDiscordDestination(context.Context) (DiscordDestination, DiscordCredentialCiphertext, error) {
	if f.getErr != nil {
		return DiscordDestination{}, nil, f.getErr
	}
	return f.destination, f.credentialCiphertext, nil
}

func (f *fakeDiscordDestinationRepository) ConnectDiscordDestination(_ context.Context, destination DiscordDestination, credential DiscordCredentialCiphertext) error {
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connectCalls++
	f.connectedDestination, f.connectedCredential = destination, credential
	f.destination, f.credentialCiphertext = destination, credential
	return nil
}

func (f *fakeDiscordDestinationRepository) MarkDiscordDestinationBroken(_ context.Context, reason string) error {
	if f.markBrokenErr != nil {
		return f.markBrokenErr
	}
	f.brokenCalls++
	f.brokenReason = reason
	f.destination.State, f.credentialCiphertext = DiscordDestinationBroken, nil
	return nil
}

func (f *fakeDiscordDestinationRepository) DeleteDiscordDestination(context.Context) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleteCalls++
	f.destination, f.credentialCiphertext = DiscordDestination{}, nil
	return nil
}

type fakeDiscordWebhookOAuthProvider struct {
	exchangeResult DiscordWebhookOAuthResult
	exchangeErr    error

	deleteCalls       int
	deletedCredential DiscordWebhookCredential
	deleteErr         error
}

func (f *fakeDiscordWebhookOAuthProvider) AuthorizationURL(string) string { return "" }

func (f *fakeDiscordWebhookOAuthProvider) Exchange(context.Context, string) (DiscordWebhookOAuthResult, error) {
	if f.exchangeErr != nil {
		return DiscordWebhookOAuthResult{}, f.exchangeErr
	}
	return f.exchangeResult, nil
}

func (f *fakeDiscordWebhookOAuthProvider) DeleteWebhook(_ context.Context, credential DiscordWebhookCredential) error {
	f.deleteCalls++
	f.deletedCredential = credential
	return f.deleteErr
}

func testDiscordConnectFixture() (DiscordWebhookOAuthResult, []byte) {
	return DiscordWebhookOAuthResult{
		Credential: DiscordWebhookCredential{WebhookID: "new-webhook-id", WebhookToken: "new-webhook-token"},
		GuildID:    "guild-1", ChannelID: "channel-1", WebhookName: "LANsurfer",
	}, testDiscordKey(9)
}

// Initial connect (no previous destination) succeeds and never attempts a
// remote cleanup, since there is nothing to clean up.
func TestDiscordConnectServiceInitialConnectSucceeds(t *testing.T) {
	exchange, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{getErr: ErrDiscordDestinationNotFound}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if repository.connectCalls != 1 {
		t.Fatalf("ConnectDiscordDestination calls = %d, want 1", repository.connectCalls)
	}
	if repository.connectedDestination.State != DiscordDestinationConnected {
		t.Fatalf("installed destination state = %q, want connected", repository.connectedDestination.State)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 for an initial connect", provider.deleteCalls)
	}
}

// Replacement succeeds and the old webhook is deleted only after the new
// destination is installed, using the previous (not the new) credential.
func TestDiscordConnectServiceReplacementSucceedsAndCleansUpOldWebhook(t *testing.T) {
	exchange, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "old-webhook-id", WebhookToken: "old-webhook-token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-0", ChannelID: "channel-0"},
		credentialCiphertext: oldCiphertext,
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if repository.connectCalls != 1 {
		t.Fatalf("ConnectDiscordDestination calls = %d, want 1", repository.connectCalls)
	}
	if repository.connectedDestination.GuildID != "guild-1" {
		t.Fatalf("installed destination = %#v, want the new guild", repository.connectedDestination)
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("DeleteWebhook calls = %d, want exactly 1", provider.deleteCalls)
	}
	if provider.deletedCredential.WebhookID != "old-webhook-id" || provider.deletedCredential.WebhookToken != "old-webhook-token" {
		t.Fatalf("DeleteWebhook credential = %#v, want the OLD credential", provider.deletedCredential)
	}
}

// A failed OAuth exchange leaves any previous destination completely
// untouched: ConnectDiscordDestination must never be called.
func TestDiscordConnectServiceFailedExchangeLeavesOldDestinationActive(t *testing.T) {
	_, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{
		destination: DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-0", ChannelID: "channel-0"},
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeErr: errors.New("discord oauth exchange failed")}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err == nil {
		t.Fatal("Connect() with a failed exchange succeeded, want an error")
	}
	if repository.connectCalls != 0 {
		t.Fatalf("ConnectDiscordDestination calls = %d, want 0", repository.connectCalls)
	}
	if repository.destination.GuildID != "guild-0" {
		t.Fatalf("destination changed to %#v, want it untouched", repository.destination)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 when the exchange itself failed", provider.deleteCalls)
	}
}

// A local storage/transaction failure (encryption error surfaces the same
// way here as a store failure: Connect returns before any old-webhook
// cleanup) leaves the old destination active and never deletes the old
// remote webhook.
func TestDiscordConnectServiceStorageFailureLeavesOldDestinationActive(t *testing.T) {
	exchange, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "old-webhook-id", WebhookToken: "old-webhook-token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-0", ChannelID: "channel-0"},
		credentialCiphertext: oldCiphertext,
		connectErr:           errors.New("database unavailable"),
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err == nil {
		t.Fatal("Connect() with a storage failure succeeded, want an error")
	}
	if repository.connectCalls != 0 {
		t.Fatalf("ConnectDiscordDestination succeeded calls = %d, want 0", repository.connectCalls)
	}
	if repository.destination.GuildID != "guild-0" {
		t.Fatalf("destination changed to %#v, want it untouched", repository.destination)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 when local storage failed", provider.deleteCalls)
	}
}

// A wrong/mismatched key at the encryption boundary must fail before any
// database write, leaving the old destination active.
func TestDiscordConnectServiceEncryptionFailureLeavesOldDestinationActive(t *testing.T) {
	exchange, _ := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{
		destination: DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-0", ChannelID: "channel-0"},
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange}
	// An undersized key makes crypto.Encrypt (via EncryptDiscordCredential)
	// fail closed rather than silently truncating/padding.
	service := NewDiscordConnectService(repository, provider, []byte("too-short"), nil)

	if err := service.Connect(t.Context(), "the-code"); err == nil {
		t.Fatal("Connect() with an invalid encryption key succeeded, want an error")
	}
	if repository.connectCalls != 0 {
		t.Fatalf("ConnectDiscordDestination calls = %d, want 0", repository.connectCalls)
	}
}

// Cleanup failure of the old remote webhook must never be surfaced as a
// Connect error: the new destination is already committed and stays
// connected regardless.
func TestDiscordConnectServiceCleanupFailureLeavesNewDestinationConnected(t *testing.T) {
	exchange, key := testDiscordConnectFixture()
	oldCiphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "old-webhook-id", WebhookToken: "old-webhook-token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	repository := &fakeDiscordDestinationRepository{
		destination:          DiscordDestination{State: DiscordDestinationConnected, GuildID: "guild-0", ChannelID: "channel-0"},
		credentialCiphertext: oldCiphertext,
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange, deleteErr: errors.New("discord unavailable")}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v, want nil even though remote cleanup fails", err)
	}
	if repository.connectedDestination.GuildID != "guild-1" {
		t.Fatalf("installed destination = %#v, want the new guild still connected", repository.connectedDestination)
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("DeleteWebhook calls = %d, want exactly 1 attempted", provider.deleteCalls)
	}
}

// A previous destination already in the broken state has no remote webhook
// left (its credential ciphertext was already cleared); Connect must never
// call DeleteWebhook for it.
func TestDiscordConnectServiceSkipsCleanupForPreviouslyBrokenDestination(t *testing.T) {
	exchange, key := testDiscordConnectFixture()
	repository := &fakeDiscordDestinationRepository{
		destination: DiscordDestination{State: DiscordDestinationBroken, GuildID: "guild-0", ChannelID: "channel-0"},
	}
	provider := &fakeDiscordWebhookOAuthProvider{exchangeResult: exchange}
	service := NewDiscordConnectService(repository, provider, key, nil)

	if err := service.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0 for a previously broken destination", provider.deleteCalls)
	}
}
