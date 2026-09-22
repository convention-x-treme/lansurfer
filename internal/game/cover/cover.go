// Package cover serves IGDB cover images from a local cache, fetching a
// cover LANsurfer already knows from the fixed IGDB origin on a miss, so
// player browsers never contact IGDB.
package cover

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"golang.org/x/sync/singleflight"
)

var (
	// ErrNotFound covers invalid input, unknown IDs, and failed fetches.
	ErrNotFound = errors.New("cover not found")
	// ErrBusy means every cold-miss slot is in use.
	ErrBusy = errors.New("cover fetch capacity exhausted")
)

const (
	// MemoryCapacity bounds the in-process store used without a cache directory.
	MemoryCapacity  = 64 << 20
	maxColdMisses   = 8
	coldMissTimeout = 5 * time.Second
)

var imageIDPattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

type Store interface {
	Get(size, id string) ([]byte, bool, error)
	Put(size, id string, image []byte) error
}

type Fetcher interface {
	Fetch(ctx context.Context, size, id string) ([]byte, error)
}

type Catalog interface {
	CoverImageKnown(ctx context.Context, id string) (bool, error)
}

type Service struct {
	store    Store
	fetcher  Fetcher
	catalog  Catalog
	logger   *slog.Logger
	flights  singleflight.Group
	slots    chan struct{}
	timeout  time.Duration
	failures *failureCache
}

func NewService(store Store, fetcher Fetcher, catalog Catalog, logger *slog.Logger) *Service {
	return &Service{
		store: store, fetcher: fetcher, catalog: catalog, logger: logger,
		slots:    make(chan struct{}, maxColdMisses),
		timeout:  coldMissTimeout,
		failures: newFailureCache(time.Now),
	}
}

func validSize(size string) bool { return size == "small" || size == "big" }

// Get returns the cover image bytes, ErrNotFound, ErrBusy, the caller's
// context error, or an internal error.
func (s *Service) Get(ctx context.Context, size, id string) ([]byte, error) {
	if !validSize(size) || !imageIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	key := size + "/" + id
	if image, done, err := s.lookup(size, id, key); done {
		return image, err
	}
	result := s.flights.DoChan(key, func() (any, error) { return s.fill(ctx, size, id, key) })
	select {
	case r := <-result:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]byte), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) lookup(size, id, key string) ([]byte, bool, error) {
	image, ok, err := s.store.Get(size, id)
	if err != nil {
		return nil, true, fmt.Errorf("read cached cover: %w", err)
	}
	if ok {
		return image, true, nil
	}
	if s.failures.contains(key) {
		return nil, true, ErrNotFound
	}
	return nil, false, nil
}

// fill runs once per key at a time. The shared work is detached from the
// first caller's cancellation and bounded by one deadline instead.
func (s *Service) fill(ctx context.Context, size, id, key string) ([]byte, error) {
	if image, done, err := s.lookup(size, id, key); done {
		return image, err
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()

	known, err := s.catalog.CoverImageKnown(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("look up cover %s: %w", id, err)
	}
	if !known {
		s.failures.add(key)
		return nil, ErrNotFound
	}
	image, err := s.fetcher.Fetch(ctx, size, id)
	if err != nil {
		s.failures.add(key)
		s.logger.Warn("fetch IGDB cover failed", "size", size, "image_id", id, "error", err)
		return nil, ErrNotFound
	}
	if err := s.store.Put(size, id, image); err != nil {
		s.logger.Error("cache IGDB cover failed", "size", size, "image_id", id, "error", err)
	}
	return image, nil
}
