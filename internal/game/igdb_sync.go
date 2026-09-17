package game

import (
	"context"
	"sync"
	"time"
)

// failureRecordTimeout bounds how long a failed sync gets to record its own
// failure once the sync's own context may already be done (for example
// because it was the context that failed, or because the application is
// shutting down). It is intentionally independent of the sync's context.
const failureRecordTimeout = 5 * time.Second

// IGDBSyncService orchestrates one full-generation rebuild of the local IGDB
// provider index at a time. Only one sync runs per process; a second request
// while one is already running is a no-op that reports the current status.
type IGDBSyncService struct {
	catalog ProviderCatalog
	repo    ProviderIndexRepository
	now     func() time.Time

	mu      sync.Mutex
	running bool
}

// NewIGDBSyncService constructs a sync service over the given provider
// catalog and index repository. now is used to derive operator status
// staleness and is normally time.Now.
func NewIGDBSyncService(catalog ProviderCatalog, repo ProviderIndexRepository, now func() time.Time) *IGDBSyncService {
	return &IGDBSyncService{catalog: catalog, repo: repo, now: now}
}

// TryStart begins a sync in a new goroutine and reports true, unless a sync
// is already running, in which case it does nothing and reports false.
func (s *IGDBSyncService) TryStart(ctx context.Context) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		s.run(ctx)
	}()
	return true
}

func (s *IGDBSyncService) run(ctx context.Context) {
	generationID, err := s.repo.BeginGeneration(ctx)
	if err != nil {
		return
	}

	var recordCount int64
	err = s.catalog.Games(ctx, func(batch []ProviderGame) error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.repo.StoreProviderGames(ctx, generationID, batch); err != nil {
			return err
		}
		recordCount += int64(len(batch))
		return nil
	})
	if err != nil {
		s.fail(generationID, err)
		return
	}

	if err := s.repo.ActivateGeneration(ctx, generationID, recordCount); err != nil {
		s.fail(generationID, err)
		return
	}
}

func (s *IGDBSyncService) fail(generationID string, cause error) {
	failCtx, cancel := context.WithTimeout(context.Background(), failureRecordTimeout)
	defer cancel()
	_ = s.repo.FailGeneration(failCtx, generationID, cause.Error())
}

// Status reports the persisted operator status, overlaid with ProviderSyncing
// while this process has a sync in flight.
func (s *IGDBSyncService) Status(ctx context.Context) (ProviderStatus, error) {
	status, err := s.repo.Status(ctx, s.now())
	if err != nil {
		return ProviderStatus{}, err
	}
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running {
		status.State = ProviderSyncing
	}
	return status, nil
}
