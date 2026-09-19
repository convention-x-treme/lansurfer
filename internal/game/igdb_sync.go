package game

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// errEmptyProviderCatalog guards against activating a generation that came
// back with zero records: activation archives every local IGDB mapping not
// present in the new generation and deletes the previous generation, so an
// empty-but-error-free provider response must not overwrite the last
// successful index.
var errEmptyProviderCatalog = errors.New("provider catalog returned no records")

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
	logger  *slog.Logger

	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

// NewIGDBSyncService constructs a sync service over the given provider
// catalog and index repository. now is used to derive operator status
// staleness and is normally time.Now. logger records sync failures that
// would otherwise be silent; if nil, slog.Default() is used.
func NewIGDBSyncService(catalog ProviderCatalog, repo ProviderIndexRepository, now func() time.Time, logger *slog.Logger) *IGDBSyncService {
	if logger == nil {
		logger = slog.Default()
	}
	return &IGDBSyncService{catalog: catalog, repo: repo, now: now, logger: logger}
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
	s.wg.Add(1)

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			s.wg.Done()
		}()
		s.run(ctx)
	}()
	return true
}

// Wait blocks until any sync started by TryStart has finished writing its
// result, or ctx is done, whichever comes first. Callers use it to avoid
// closing shared infrastructure (the database pool) out from under a sync
// goroutine's still-open transaction during shutdown.
func (s *IGDBSyncService) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (s *IGDBSyncService) run(ctx context.Context) {
	generationID, err := s.repo.BeginGeneration(ctx)
	if err != nil {
		s.logger.Error("begin IGDB sync generation", "error", err)
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
	if recordCount == 0 {
		s.fail(generationID, errEmptyProviderCatalog)
		return
	}

	if err := s.repo.ActivateGeneration(ctx, generationID, recordCount); err != nil {
		s.fail(generationID, err)
		return
	}
}

func (s *IGDBSyncService) fail(generationID string, cause error) {
	s.logger.Error("IGDB sync failed", "generation_id", generationID, "error", cause)
	failCtx, cancel := context.WithTimeout(context.Background(), failureRecordTimeout)
	defer cancel()
	if err := s.repo.FailGeneration(failCtx, generationID, cause.Error()); err != nil {
		s.logger.Error("record IGDB sync failure", "generation_id", generationID, "error", err)
	}
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
