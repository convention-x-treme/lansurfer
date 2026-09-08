// Package sse provides the in-memory realtime fan-out used by a single
// LANsurfer application instance.
package sse

import (
	"sync"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

const subscriberBuffer = 16

// Broker fans GameRequest change signals out to connected browsers. Slow
// subscribers may miss a signal; every signal causes a fresh database-backed
// render, so later signals still converge on current state.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan gamerequest.Event]struct{}
	closed      bool
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[chan gamerequest.Event]struct{})}
}

func (b *Broker) Publish(event gamerequest.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (b *Broker) Subscribe() (<-chan gamerequest.Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		closed := make(chan gamerequest.Event)
		close(closed)
		return closed, func() {}
	}

	subscriber := make(chan gamerequest.Event, subscriberBuffer)
	b.subscribers[subscriber] = struct{}{}
	var once sync.Once
	return subscriber, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, exists := b.subscribers[subscriber]; exists {
				delete(b.subscribers, subscriber)
				close(subscriber)
			}
		})
	}
}

// Close disconnects all subscribers and prevents new subscriptions. It is
// used before HTTP shutdown so long-lived streams do not delay termination.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for subscriber := range b.subscribers {
		delete(b.subscribers, subscriber)
		close(subscriber)
	}
}
