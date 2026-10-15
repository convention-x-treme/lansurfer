// Package mediaproxy serves remote images LANsurfer already knows from a
// local cache, fetching them server-side on a miss, so player browsers never
// contact third-party image hosts. IGDB covers and provider avatars are each
// one Service instance with their own catalog, fetcher, and buckets.
package mediaproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"golang.org/x/sync/singleflight"
)

var (
	// ErrNotFound covers invalid input, unknown IDs, and failed fetches.
	ErrNotFound = errors.New("media not found")
	// ErrBusy means every cold-miss slot is in use.
	ErrBusy = errors.New("media fetch capacity exhausted")
)

const (
	// MemoryCapacity bounds the in-process store used without a cache directory.
	MemoryCapacity  = 64 << 20
	maxColdMisses   = 8
	coldMissTimeout = 5 * time.Second
)

var imageIDPattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// Store keys images by bucket (e.g. a cover bucket or "avatar") and ID.
type Store interface {
	Get(bucket, id string) ([]byte, bool, error)
	Put(bucket, id string, image []byte) error
	IDs(bucket string) ([]string, error)
	Delete(bucket, id string) error
}

type Fetcher interface {
	Fetch(ctx context.Context, bucket, id string) ([]byte, error)
}

// KnownFunc reports whether id belongs to the owning catalog. It runs only on
// a cold miss and restricts which upstream images can ever be fetched.
type KnownFunc func(ctx context.Context, id string) (bool, error)

type Service struct {
	store    Store
	fetcher  Fetcher
	known    KnownFunc
	buckets  []string
	logger   *slog.Logger
	flights  singleflight.Group
	slots    chan struct{}
	timeout  time.Duration
	failures *failureCache
}

func NewService(store Store, fetcher Fetcher, known KnownFunc, logger *slog.Logger, buckets ...string) *Service {
	return &Service{
		store: store, fetcher: fetcher, known: known, buckets: buckets, logger: logger,
		slots:    make(chan struct{}, maxColdMisses),
		timeout:  coldMissTimeout,
		failures: newFailureCache(time.Now),
	}
}

func (s *Service) validBucket(bucket string) bool { return slices.Contains(s.buckets, bucket) }

// Get returns the image bytes, ErrNotFound, ErrBusy, the caller's
// context error, or an internal error.
func (s *Service) Get(ctx context.Context, bucket, id string) ([]byte, error) {
	if !s.validBucket(bucket) || !imageIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	key := bucket + "/" + id
	if image, done, err := s.lookup(bucket, id, key); done {
		return image, err
	}
	result := s.flights.DoChan(key, func() (any, error) { return s.fill(ctx, bucket, id, key) })
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

func (s *Service) lookup(bucket, id, key string) ([]byte, bool, error) {
	image, ok, err := s.store.Get(bucket, id)
	if err != nil {
		return nil, true, fmt.Errorf("read cached image: %w", err)
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
func (s *Service) fill(ctx context.Context, bucket, id, key string) ([]byte, error) {
	if image, done, err := s.lookup(bucket, id, key); done {
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

	known, err := s.known(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("look up image %s: %w", id, err)
	}
	if !known {
		s.failures.add(key)
		return nil, ErrNotFound
	}
	image, err := s.fetcher.Fetch(ctx, bucket, id)
	if err != nil {
		s.failures.add(key)
		s.logger.Warn("fetch remote image failed", "bucket", bucket, "image_id", id, "error", err)
		return nil, ErrNotFound
	}
	if err := s.store.Put(bucket, id, image); err != nil {
		s.logger.Error("cache remote image failed", "bucket", bucket, "image_id", id, "error", err)
	}
	return image, nil
}

// Prune deletes every cached image in this Service's buckets that the
// catalog no longer knows. For avatars that means the picture is no longer
// any user's selection: changed, unlinked, or the account deleted. A Put
// racing a fresh selection may be deleted too; the next request refetches.
func (s *Service) Prune(ctx context.Context) error {
	for _, bucket := range s.buckets {
		ids, err := s.store.IDs(bucket)
		if err != nil {
			return fmt.Errorf("list cached images: %w", err)
		}
		for _, id := range ids {
			known, err := s.known(ctx, id)
			if err != nil {
				return fmt.Errorf("look up image %s: %w", id, err)
			}
			if known {
				continue
			}
			if err := s.store.Delete(bucket, id); err != nil {
				return fmt.Errorf("delete cached image %s: %w", id, err)
			}
		}
	}
	return nil
}
