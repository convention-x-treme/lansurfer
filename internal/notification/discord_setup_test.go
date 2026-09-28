package notification

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

type fakeDiscordSetupStateRepository struct {
	createStateHash, createBrowserHash []byte
	createAdminID                      string
	createExpiresAt                    time.Time
	createErr                          error

	consumeStateHash, consumeBrowserHash []byte
	consumeAdminID                       string
	consumeNow                           time.Time
	consumeCalls                         int
	consumeResult                        DiscordSetupState
	consumeErr                           error
}

func (f *fakeDiscordSetupStateRepository) CreateDiscordSetupState(_ context.Context, stateHash, browserBindingHash []byte, initiatingAdminID string, expiresAt time.Time) (DiscordSetupState, error) {
	f.createStateHash, f.createBrowserHash = stateHash, browserBindingHash
	f.createAdminID, f.createExpiresAt = initiatingAdminID, expiresAt
	if f.createErr != nil {
		return DiscordSetupState{}, f.createErr
	}
	return DiscordSetupState{ID: "setup-1", InitiatingAdminID: initiatingAdminID, ExpiresAt: expiresAt}, nil
}

func (f *fakeDiscordSetupStateRepository) ConsumeDiscordSetupState(_ context.Context, stateHash, browserBindingHash []byte, initiatingAdminID string, now time.Time) (DiscordSetupState, error) {
	f.consumeCalls++
	f.consumeStateHash, f.consumeBrowserHash = stateHash, browserBindingHash
	f.consumeAdminID, f.consumeNow = initiatingAdminID, now
	if f.consumeErr != nil {
		return DiscordSetupState{}, f.consumeErr
	}
	return f.consumeResult, nil
}

func TestDiscordSetupServiceStartPersistsOnlyHashedSecrets(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{}
	fixedNow := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	service := NewDiscordSetupService(repository)
	service.now = func() time.Time { return fixedNow }

	state, err := service.Start(t.Context(), "admin-1", "raw-browser-binding")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if state == "" {
		t.Fatal("Start() returned an empty raw state")
	}

	wantStateHash := sha256.Sum256([]byte(state))
	if string(repository.createStateHash) != string(wantStateHash[:]) {
		t.Fatalf("persisted state hash = %x, want sha256(raw state) = %x", repository.createStateHash, wantStateHash)
	}
	wantBrowserHash := sha256.Sum256([]byte("raw-browser-binding"))
	if string(repository.createBrowserHash) != string(wantBrowserHash[:]) {
		t.Fatalf("persisted browser hash = %x, want sha256(raw browser binding) = %x", repository.createBrowserHash, wantBrowserHash)
	}
	// The raw state/browser-binding secrets themselves must never be what
	// gets persisted.
	if string(repository.createStateHash) == state {
		t.Fatal("raw state was persisted directly instead of its hash")
	}
	if repository.createAdminID != "admin-1" {
		t.Fatalf("persisted admin ID = %q", repository.createAdminID)
	}
	if !repository.createExpiresAt.Equal(fixedNow.Add(discordSetupStateLifetime)) {
		t.Fatalf("persisted expiry = %v, want %v", repository.createExpiresAt, fixedNow.Add(discordSetupStateLifetime))
	}
}

func TestDiscordSetupServiceStartGeneratesUniqueStateEachCall(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{}
	service := NewDiscordSetupService(repository)

	first, err := service.Start(t.Context(), "admin-1", "browser-1")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	second, err := service.Start(t.Context(), "admin-1", "browser-1")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if first == second {
		t.Fatal("Start() returned the same raw state twice")
	}
}

func TestDiscordSetupServiceStartRejectsMissingAdmin(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{}
	service := NewDiscordSetupService(repository)

	if _, err := service.Start(t.Context(), "", "browser"); !errors.Is(err, ErrDiscordSetupAdminRequired) {
		t.Fatalf("Start() error = %v, want ErrDiscordSetupAdminRequired", err)
	}
	if repository.createAdminID != "" {
		t.Fatal("Start() persisted a setup state despite the missing admin")
	}
}

func TestDiscordSetupServiceStartRejectsMissingBrowserBinding(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{}
	service := NewDiscordSetupService(repository)

	if _, err := service.Start(t.Context(), "admin-1", ""); !errors.Is(err, ErrDiscordSetupBrowserRequired) {
		t.Fatalf("Start() error = %v, want ErrDiscordSetupBrowserRequired", err)
	}
	if repository.createAdminID != "" {
		t.Fatal("Start() persisted a setup state despite the missing browser binding")
	}
}

func TestDiscordSetupServiceConsumeHashesBeforeLookup(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{consumeResult: DiscordSetupState{ID: "setup-1", InitiatingAdminID: "admin-1"}}
	fixedNow := time.Date(2026, 9, 25, 12, 5, 0, 0, time.UTC)
	service := NewDiscordSetupService(repository)
	service.now = func() time.Time { return fixedNow }

	result, err := service.Consume(t.Context(), "admin-1", "raw-browser-binding", "raw-state")
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if result.ID != "setup-1" {
		t.Fatalf("Consume() result = %#v", result)
	}
	wantStateHash := sha256.Sum256([]byte("raw-state"))
	if string(repository.consumeStateHash) != string(wantStateHash[:]) {
		t.Fatalf("lookup state hash = %x, want sha256(raw state) = %x", repository.consumeStateHash, wantStateHash)
	}
	if repository.consumeAdminID != "admin-1" || !repository.consumeNow.Equal(fixedNow) {
		t.Fatalf("consume args = admin %q now %v", repository.consumeAdminID, repository.consumeNow)
	}
}

func TestDiscordSetupServiceConsumePropagatesRepositoryRejection(t *testing.T) {
	repository := &fakeDiscordSetupStateRepository{consumeErr: ErrDiscordSetupStateInvalid}
	service := NewDiscordSetupService(repository)

	// This covers replay, expiry, wrong-admin, and wrong-browser-binding
	// alike: the repository is the single source of truth for the atomic
	// consume decision, and Service must not mask or reinterpret its
	// rejection.
	_, err := service.Consume(t.Context(), "admin-1", "browser", "state")
	if !errors.Is(err, ErrDiscordSetupStateInvalid) {
		t.Fatalf("Consume() error = %v, want ErrDiscordSetupStateInvalid", err)
	}
}

func TestDiscordSetupServiceConsumeRejectsMissingInputsWithoutRepositoryCall(t *testing.T) {
	tests := []struct {
		name, admin, browser, state string
		wantErr                     error
	}{
		{"missing admin", "", "browser", "state", ErrDiscordSetupAdminRequired},
		{"missing browser binding", "admin-1", "", "state", ErrDiscordSetupBrowserRequired},
		{"missing state", "admin-1", "browser", "", ErrDiscordSetupStateInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeDiscordSetupStateRepository{}
			service := NewDiscordSetupService(repository)

			_, err := service.Consume(t.Context(), test.admin, test.browser, test.state)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Consume() error = %v, want %v", err, test.wantErr)
			}
			if repository.consumeCalls != 0 {
				t.Fatal("Consume() called the repository despite invalid input")
			}
		})
	}
}
