package mediaproxy

import (
	"container/list"
	"sync"
)

type memoryEntry struct {
	key   string
	image []byte
}

// MemoryStore is a byte-bounded LRU used when no cache directory is set.
// Its contents are lost on restart.
type MemoryStore struct {
	mu       sync.Mutex
	capacity int
	used     int
	entries  map[string]*list.Element
	order    *list.List // front = most recently used
}

func NewMemoryStore(capacity int) *MemoryStore {
	return &MemoryStore{capacity: capacity, entries: map[string]*list.Element{}, order: list.New()}
}

func (m *MemoryStore) Get(size, id string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	element, ok := m.entries[size+"/"+id]
	if !ok {
		return nil, false, nil
	}
	m.order.MoveToFront(element)
	return element.Value.(memoryEntry).image, true, nil
}

func (m *MemoryStore) Put(size, id string, image []byte) error {
	if len(image) > m.capacity {
		return nil
	}
	key := size + "/" + id
	m.mu.Lock()
	defer m.mu.Unlock()
	if element, ok := m.entries[key]; ok {
		m.remove(element)
	}
	for m.used+len(image) > m.capacity {
		m.remove(m.order.Back())
	}
	m.entries[key] = m.order.PushFront(memoryEntry{key: key, image: image})
	m.used += len(image)
	return nil
}

func (m *MemoryStore) remove(element *list.Element) {
	entry := element.Value.(memoryEntry)
	delete(m.entries, entry.key)
	m.used -= len(entry.image)
	m.order.Remove(element)
}
