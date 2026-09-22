package cover

import (
	"container/list"
	"sync"
	"time"
)

const (
	failureTTL  = 10 * time.Minute
	maxFailures = 4096
)

type failure struct {
	key     string
	addedAt time.Time
}

// failureCache remembers unknown IDs and failed fetches for failureTTL from
// insertion, bounded to maxFailures entries by evicting the oldest.
type failureCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*list.Element
	order   *list.List
}

func newFailureCache(now func() time.Time) *failureCache {
	return &failureCache{now: now, entries: map[string]*list.Element{}, order: list.New()}
}

func (c *failureCache) contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return false
	}
	if c.now().Sub(element.Value.(failure).addedAt) >= failureTTL {
		c.remove(element)
		return false
	}
	return true
}

func (c *failureCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.remove(element)
	}
	if c.order.Len() >= maxFailures {
		c.remove(c.order.Front())
	}
	c.entries[key] = c.order.PushBack(failure{key: key, addedAt: c.now()})
}

func (c *failureCache) remove(element *list.Element) {
	delete(c.entries, element.Value.(failure).key)
	c.order.Remove(element)
}
