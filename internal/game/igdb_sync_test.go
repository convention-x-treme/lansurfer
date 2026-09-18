package game

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIGDBSyncActivatesOnlyAfterCatalogCompletes(t *testing.T) {
	repo := newFakeProviderIndexRepository()
	catalog := fakeCatalog{batches: [][]ProviderGame{
		{{Reference: ProviderReference{Provider: "igdb", ExternalID: "1"}, Name: "One"}},
		{{Reference: ProviderReference{Provider: "igdb", ExternalID: "2"}, Name: "Two"}},
	}}
	service := NewIGDBSyncService(catalog, repo, time.Now, nil)
	if !service.TryStart(t.Context()) {
		t.Fatal("TryStart=false")
	}
	repo.wait(t)
	if repo.activateCount != 1 || repo.failCount != 0 || repo.activatedRecords != 2 {
		t.Fatalf("repo = %#v", repo)
	}
}

func TestIGDBSyncFailureDoesNotActivate(t *testing.T) {
	repo := newFakeProviderIndexRepository()
	catalog := fakeCatalog{err: errors.New("upstream failed")}
	service := NewIGDBSyncService(catalog, repo, time.Now, nil)
	if !service.TryStart(t.Context()) {
		t.Fatal("TryStart=false")
	}
	repo.wait(t)
	if repo.activateCount != 0 || repo.failCount != 1 {
		t.Fatalf("repo = %#v", repo)
	}
}

func TestIGDBSyncSingleFlight(t *testing.T) {
	repo := newFakeProviderIndexRepository()
	block := make(chan struct{})
	service := NewIGDBSyncService(blockingCatalog{release: block}, repo, time.Now, nil)
	if !service.TryStart(t.Context()) {
		t.Fatal("first TryStart=false")
	}
	if service.TryStart(t.Context()) {
		t.Fatal("second TryStart=true")
	}
	close(block)
	repo.wait(t)
}

func TestIGDBSyncStatusOverlaysSyncingWhileRunning(t *testing.T) {
	repo := newFakeProviderIndexRepository()
	block := make(chan struct{})
	service := NewIGDBSyncService(blockingCatalog{release: block}, repo, time.Now, nil)
	if !service.TryStart(t.Context()) {
		t.Fatal("TryStart=false")
	}

	status, err := service.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != ProviderSyncing {
		t.Fatalf("status.State = %v, want %v", status.State, ProviderSyncing)
	}

	close(block)
	repo.wait(t)
}

// TestIGDBSyncStatusOverlayPreservesCatalogAvailableWhileSyncing covers the
// refresh-after-a-successful-baseline case Step 5 calls out: Status overlays
// ProviderSyncing onto whatever the repository reports while a sync is in
// flight, but must never clobber CatalogAvailable along the way, so a
// refresh running after an earlier successful sync keeps serving that
// earlier baseline as available rather than looking like an initial sync.
func TestIGDBSyncStatusOverlayPreservesCatalogAvailableWhileSyncing(t *testing.T) {
	repo := newFakeProviderIndexRepository()
	repo.status = ProviderStatus{State: ProviderCurrent, CatalogAvailable: true}
	block := make(chan struct{})
	service := NewIGDBSyncService(blockingCatalog{release: block}, repo, time.Now, nil)
	if !service.TryStart(t.Context()) {
		t.Fatal("TryStart=false")
	}

	status, err := service.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != ProviderSyncing {
		t.Fatalf("status.State = %v, want %v", status.State, ProviderSyncing)
	}
	if !status.CatalogAvailable {
		t.Fatal("active generation must mark catalog available")
	}

	close(block)
	repo.wait(t)
}

type fakeProviderIndexRepository struct {
	mu                sync.Mutex
	activateCount     int
	failCount         int
	activatedRecords  int64
	lastFailureReason string
	nextGenerationID  int
	done              chan struct{}
	status            ProviderStatus
}

func newFakeProviderIndexRepository() *fakeProviderIndexRepository {
	return &fakeProviderIndexRepository{done: make(chan struct{}, 8)}
}

func (r *fakeProviderIndexRepository) BeginGeneration(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGenerationID++
	return "generation-1", nil
}

func (r *fakeProviderIndexRepository) StoreProviderGames(context.Context, string, []ProviderGame) error {
	return nil
}

func (r *fakeProviderIndexRepository) ActivateGeneration(_ context.Context, _ string, recordCount int64) error {
	r.mu.Lock()
	r.activateCount++
	r.activatedRecords = recordCount
	r.mu.Unlock()
	r.done <- struct{}{}
	return nil
}

func (r *fakeProviderIndexRepository) FailGeneration(_ context.Context, _ string, failure string) error {
	r.mu.Lock()
	r.failCount++
	r.lastFailureReason = failure
	r.mu.Unlock()
	r.done <- struct{}{}
	return nil
}

func (r *fakeProviderIndexRepository) Status(context.Context, time.Time) (ProviderStatus, error) {
	return r.status, nil
}

func (r *fakeProviderIndexRepository) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync completion")
	}
}

type fakeCatalog struct {
	batches [][]ProviderGame
	err     error
}

func (f fakeCatalog) Games(_ context.Context, fn func([]ProviderGame) error) error {
	if f.err != nil {
		return f.err
	}
	for _, batch := range f.batches {
		if err := fn(batch); err != nil {
			return err
		}
	}
	return nil
}

type blockingCatalog struct {
	release chan struct{}
}

func (b blockingCatalog) Games(ctx context.Context, fn func([]ProviderGame) error) error {
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return fn([]ProviderGame{{Reference: ProviderReference{Provider: "igdb", ExternalID: "1"}, Name: "Blocked"}})
}
