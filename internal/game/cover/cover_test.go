package cover

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeStore struct {
	mu     sync.Mutex
	images map[string][]byte
	putErr error
	onGet  func() // optional barrier hook, called before each Get reads
}

func (s *fakeStore) Get(size, id string) ([]byte, bool, error) {
	if s.onGet != nil {
		s.onGet()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	image, ok := s.images[size+"/"+id]
	return image, ok, nil
}

func (s *fakeStore) Put(size, id string, image []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images[size+"/"+id] = image
	return nil
}

type fakeCatalog struct {
	calls atomic.Int32
	known func(ctx context.Context, id string) (bool, error)
}

func (c *fakeCatalog) CoverImageKnown(ctx context.Context, id string) (bool, error) {
	c.calls.Add(1)
	if c.known == nil {
		return true, nil
	}
	return c.known(ctx, id)
}

type fakeFetcher struct {
	calls atomic.Int32
	fetch func(ctx context.Context, size, id string) ([]byte, error)
}

func (f *fakeFetcher) Fetch(ctx context.Context, size, id string) ([]byte, error) {
	f.calls.Add(1)
	if f.fetch == nil {
		return []byte("image-" + id), nil
	}
	return f.fetch(ctx, size, id)
}

func newTestService() (*Service, *fakeStore, *fakeCatalog, *fakeFetcher) {
	store := &fakeStore{images: map[string][]byte{}}
	catalog := &fakeCatalog{}
	fetcher := &fakeFetcher{}
	return NewService(store, fetcher, catalog, slog.New(slog.NewTextHandler(io.Discard, nil))), store, catalog, fetcher
}

func TestGetRejectsInvalidInputWithoutAccess(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	for _, input := range []struct{ size, id string }{
		{"big", "../x"}, {"big", "%2f"}, {"big", "CO1ABC"}, {"big", ""},
		{"big", strings.Repeat("a", 33)}, {"big", "co1.abc"}, {"medium", "co1abc"},
	} {
		if _, err := service.Get(t.Context(), input.size, input.id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q, %q) error = %v, want ErrNotFound", input.size, input.id, err)
		}
	}
	if catalog.calls.Load() != 0 || fetcher.calls.Load() != 0 {
		t.Errorf("catalog/fetch calls = %d/%d, want 0/0", catalog.calls.Load(), fetcher.calls.Load())
	}
}

func TestGetServesHitWithoutCatalogOrFetch(t *testing.T) {
	service, store, catalog, fetcher := newTestService()
	store.images["big/co1"] = []byte("cached")
	image, err := service.Get(t.Context(), "big", "co1")
	if err != nil || string(image) != "cached" {
		t.Fatalf("Get = %q, %v", image, err)
	}
	if catalog.calls.Load() != 0 || fetcher.calls.Load() != 0 {
		t.Error("cache hit reached catalog or fetcher")
	}
}

func TestGetFetchesAndStoresKnownMiss(t *testing.T) {
	service, store, _, fetcher := newTestService()
	image, err := service.Get(t.Context(), "big", "co1")
	if err != nil || string(image) != "image-co1" {
		t.Fatalf("Get = %q, %v", image, err)
	}
	if string(store.images["big/co1"]) != "image-co1" {
		t.Error("fetched image not stored")
	}
	if _, err := service.Get(t.Context(), "big", "co1"); err != nil || fetcher.calls.Load() != 1 {
		t.Errorf("second Get error = %v, fetches = %d, want nil/1", err, fetcher.calls.Load())
	}
}

func TestGetUnknownIDIsNotFetchedAndIsRemembered(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	catalog.known = func(context.Context, string) (bool, error) { return false, nil }
	for range 2 {
		if _, err := service.Get(t.Context(), "big", "co1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get error = %v, want ErrNotFound", err)
		}
	}
	if catalog.calls.Load() != 1 || fetcher.calls.Load() != 0 {
		t.Errorf("catalog/fetch calls = %d/%d, want 1/0", catalog.calls.Load(), fetcher.calls.Load())
	}
}

func TestGetStoreWriteErrorStillReturnsImage(t *testing.T) {
	service, store, _, _ := newTestService()
	store.putErr = errors.New("disk full")
	if image, err := service.Get(t.Context(), "big", "co1"); err != nil || string(image) != "image-co1" {
		t.Fatalf("Get = %q, %v", image, err)
	}
}

func TestGetCoalescesConcurrentMisses(t *testing.T) {
	service, _, _, fetcher := newTestService()
	release := make(chan struct{})
	fetcher.fetch = func(context.Context, string, string) ([]byte, error) {
		<-release
		return []byte("image"), nil
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Get(context.Background(), "big", "co1"); err != nil {
				t.Errorf("Get error = %v", err)
			}
		}()
	}
	for fetcher.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if fetcher.calls.Load() != 1 {
		t.Errorf("fetches = %d, want 1", fetcher.calls.Load())
	}
}

// A caller that missed the cache before another flight completed must use
// that flight's result from the in-flight recheck instead of fetching again.
func TestGetDelayedCallerUsesCompletedFlightResult(t *testing.T) {
	for name, known := range map[string]bool{"positive": true, "negative": false} {
		t.Run(name, func(t *testing.T) {
			service, store, catalog, fetcher := newTestService()
			catalog.known = func(context.Context, string) (bool, error) { return known, nil }
			paused, resume := make(chan struct{}), make(chan struct{})
			var first atomic.Bool
			store.onGet = func() {
				if first.CompareAndSwap(false, true) {
					close(paused)
					<-resume
				}
			}
			delayed := make(chan error, 1)
			go func() {
				_, err := service.Get(context.Background(), "big", "co1")
				delayed <- err
			}()
			<-paused
			_, _ = service.Get(t.Context(), "big", "co1") // completes a full flight
			close(resume)
			err := <-delayed
			if known && err != nil || !known && !errors.Is(err, ErrNotFound) {
				t.Fatalf("delayed Get error = %v", err)
			}
			if catalog.calls.Load() != 1 || fetcher.calls.Load() > 1 {
				t.Errorf("catalog/fetch calls = %d/%d, want 1/≤1", catalog.calls.Load(), fetcher.calls.Load())
			}
		})
	}
}

func TestGetRejectsNewColdKeysWhenSaturated(t *testing.T) {
	service, store, catalog, _ := newTestService()
	store.images["big/cached"] = []byte("cached")
	release := make(chan struct{})
	entered := make(chan struct{}, maxColdMisses)
	catalog.known = func(context.Context, string) (bool, error) {
		entered <- struct{}{}
		<-release
		return true, nil
	}
	var wg sync.WaitGroup
	for i := range maxColdMisses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Get(context.Background(), "big", fmt.Sprintf("co%d", i)); err != nil {
				t.Errorf("admitted Get error = %v", err)
			}
		}()
	}
	for range maxColdMisses {
		<-entered
	}
	if _, err := service.Get(t.Context(), "big", "coninth"); !errors.Is(err, ErrBusy) {
		t.Fatalf("ninth Get error = %v, want ErrBusy", err)
	}
	if catalog.calls.Load() != maxColdMisses {
		t.Errorf("catalog calls = %d, want %d", catalog.calls.Load(), maxColdMisses)
	}
	if _, err := service.Get(t.Context(), "big", "cached"); err != nil {
		t.Errorf("hit during saturation error = %v", err)
	}
	joined := make(chan error, 1)
	go func() { _, err := service.Get(context.Background(), "big", "co0"); joined <- err }()
	close(release)
	wg.Wait()
	if err := <-joined; err != nil {
		t.Errorf("joining caller error = %v", err)
	}
	catalog.known = nil
	if _, err := service.Get(t.Context(), "big", "coninth"); err != nil {
		t.Errorf("retry after release error = %v (busy must not be remembered)", err)
	}
}

func TestGetCanceledCallerDoesNotCancelSharedFetch(t *testing.T) {
	service, _, _, fetcher := newTestService()
	release := make(chan struct{})
	fetcher.fetch = func(ctx context.Context, _, _ string) ([]byte, error) {
		<-release
		return []byte("image"), ctx.Err()
	}
	canceled, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := service.Get(canceled, "big", "co1"); first <- err }()
	for fetcher.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { _, err := service.Get(context.Background(), "big", "co1"); second <- err }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller error = %v", err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatalf("waiting caller error = %v", err)
	}
	if fetcher.calls.Load() != 1 {
		t.Errorf("fetches = %d, want 1", fetcher.calls.Load())
	}
}

func TestGetSharesOneDeadlineAcrossCatalogAndFetch(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	service.timeout = 50 * time.Millisecond
	catalog.known = func(ctx context.Context, _ string) (bool, error) {
		<-ctx.Done()
		return false, ctx.Err()
	}
	_, err := service.Get(t.Context(), "big", "co1")
	if err == nil || errors.Is(err, ErrNotFound) || fetcher.calls.Load() != 0 {
		t.Fatalf("Get error = %v, fetches = %d; want internal error and no fetch", err, fetcher.calls.Load())
	}
	if service.failures.contains("big/co1") {
		t.Error("catalog deadline was negative-cached")
	}

	var catalogDeadline, fetchDeadline time.Time
	catalog.known = func(ctx context.Context, _ string) (bool, error) {
		catalogDeadline, _ = ctx.Deadline()
		return true, nil
	}
	fetcher.fetch = func(ctx context.Context, _, _ string) ([]byte, error) {
		fetchDeadline, _ = ctx.Deadline()
		return []byte("image"), nil
	}
	if _, err := service.Get(t.Context(), "big", "co2"); err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if catalogDeadline.IsZero() || !catalogDeadline.Equal(fetchDeadline) {
		t.Errorf("deadlines catalog=%v fetch=%v, want one shared deadline", catalogDeadline, fetchDeadline)
	}
	if _, err := service.Get(t.Context(), "big", "co3"); err != nil {
		t.Errorf("slot not released after deadline: %v", err)
	}
}

func TestGetRemembersFetchFailureForTenMinutes(t *testing.T) {
	service, _, catalog, fetcher := newTestService()
	now := time.Unix(0, 0)
	service.failures.now = func() time.Time { return now }
	fetcher.fetch = func(context.Context, string, string) ([]byte, error) { return nil, errors.New("offline") }
	for range 2 {
		if _, err := service.Get(t.Context(), "big", "co1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get error = %v, want ErrNotFound", err)
		}
	}
	if fetcher.calls.Load() != 1 {
		t.Fatalf("fetches = %d, want 1", fetcher.calls.Load())
	}
	now = now.Add(9 * time.Minute)
	_, _ = service.Get(t.Context(), "big", "co1") // read must not extend expiry
	now = now.Add(time.Minute)
	_, _ = service.Get(t.Context(), "big", "co1")
	if fetcher.calls.Load() != 2 {
		t.Errorf("fetches after 10 min = %d, want 2", fetcher.calls.Load())
	}

	catalog.known = func(context.Context, string) (bool, error) { return false, errors.New("db down") }
	_, _ = service.Get(t.Context(), "big", "co9")
	catalog.known = nil
	fetcher.fetch = nil
	if _, err := service.Get(t.Context(), "big", "co9"); err != nil {
		t.Errorf("catalog error was remembered: %v", err)
	}
}

func TestFailureCacheIsBounded(t *testing.T) {
	cache := newFailureCache(time.Now)
	for i := range maxFailures + 1 {
		cache.add(fmt.Sprintf("big/co%d", i))
	}
	if cache.order.Len() != maxFailures {
		t.Errorf("entries = %d, want %d", cache.order.Len(), maxFailures)
	}
	if cache.contains("big/co0") {
		t.Error("oldest entry not evicted")
	}
	if !cache.contains(fmt.Sprintf("big/co%d", maxFailures)) {
		t.Error("newest entry missing")
	}
}
