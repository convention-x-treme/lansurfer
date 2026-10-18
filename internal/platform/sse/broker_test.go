package sse

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestBrokerPublishesAndUnsubscribes(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe()
	want := gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "request-1"}
	broker.Publish(want)

	select {
	case got := <-events:
		if got != want {
			t.Fatalf("event = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not delivered")
	}

	unsubscribe()
	if _, open := <-events; open {
		t.Fatal("subscriber remains open after unsubscribe")
	}
}

func TestBrokerCloseDisconnectsAndRejectsSubscribers(t *testing.T) {
	broker := NewBroker()
	events, _ := broker.Subscribe()
	broker.Close()

	if _, open := <-events; open {
		t.Fatal("subscriber remains open after close")
	}
	afterClose, _ := broker.Subscribe()
	if _, open := <-afterClose; open {
		t.Fatal("subscription after close is open")
	}
	broker.Publish(gamerequest.Event{Type: gamerequest.EventCreated})
}

func TestBrokerCountsOpenSubscribers(t *testing.T) {
	broker := NewBroker()
	if got := broker.Subscribers(); got != 0 {
		t.Fatalf("new broker Subscribers() = %d", got)
	}

	_, first := broker.Subscribe()
	_, second := broker.Subscribe()
	if got := broker.Subscribers(); got != 2 {
		t.Errorf("Subscribers() with two streams open = %d", got)
	}

	first()
	first()
	if got := broker.Subscribers(); got != 1 {
		t.Errorf("Subscribers() after one release = %d", got)
	}

	second()
	if got := broker.Subscribers(); got != 0 {
		t.Errorf("Subscribers() after all released = %d", got)
	}
}

// A subscriber that cannot keep up loses its stream instead of silently
// missing a signal; its EventSource reconnects and resynchronizes.
func TestBrokerOverflowDisconnectsOnlySlowSubscribers(t *testing.T) {
	broker := NewBroker()
	slow, unsubscribeSlow := broker.Subscribe()
	healthy, unsubscribeHealthy := broker.Subscribe()
	t.Cleanup(unsubscribeHealthy)
	receive := func(want string) {
		t.Helper()
		select {
		case got, open := <-healthy:
			if !open || got.RequestID != want {
				t.Fatalf("healthy subscriber received %q (open=%v), want %q", got.RequestID, open, want)
			}
		default:
			t.Fatalf("healthy subscriber did not receive %q", want)
		}
	}

	for i := range 16 {
		want := fmt.Sprintf("request-%02d", i)
		broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: want})
		receive(want)
	}
	if got := broker.Subscribers(); got != 2 {
		t.Fatalf("Subscribers() with full buffer = %d, want 2", got)
	}

	broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "overflow"})
	receive("overflow")
	select {
	case got, open := <-slow:
		if open {
			t.Fatalf("slow subscriber received buffered %q, want closed stream", got.RequestID)
		}
	default:
		t.Fatal("slow subscriber is not closed after overflow")
	}
	if got := broker.Subscribers(); got != 1 {
		t.Fatalf("Subscribers() after overflow = %d, want 1", got)
	}

	unsubscribeSlow()
	unsubscribeSlow()
	if got := broker.Subscribers(); got != 1 {
		t.Fatalf("Subscribers() after stale unsubscribe = %d, want 1", got)
	}
	broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "after-overflow"})
	receive("after-overflow")

	fresh, unsubscribeFresh := broker.Subscribe()
	t.Cleanup(unsubscribeFresh)
	broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "fresh"})
	receive("fresh")
	select {
	case got, open := <-fresh:
		if !open || got.RequestID != "fresh" {
			t.Fatalf("fresh subscriber received %q (open=%v), want fresh", got.RequestID, open)
		}
	default:
		t.Fatal("fresh subscriber did not receive an event after overflow")
	}
}

func TestBrokerConcurrentPublishUnsubscribeAndClose(t *testing.T) {
	for range 25 {
		broker := NewBroker()
		first, unsubscribeFirst := broker.Subscribe()
		second, unsubscribeSecond := broker.Subscribe()
		start := make(chan struct{})
		var wg sync.WaitGroup
		run := func(work func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				work()
			}()
		}
		for range 2 {
			run(func() {
				for i := range 32 {
					broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: fmt.Sprintf("request-%02d", i)})
				}
			})
		}
		run(func() {
			for range 2 {
				unsubscribeFirst()
				unsubscribeSecond()
			}
		})
		run(func() {
			broker.Close()
			broker.Close()
		})
		close(start)
		wg.Wait()

		for _, events := range []<-chan gamerequest.Event{first, second} {
			drained := time.After(time.Second)
			for open := true; open; {
				select {
				case _, open = <-events:
				case <-drained:
					t.Fatal("subscriber is still open after close")
				}
			}
		}
		if got := broker.Subscribers(); got != 0 {
			t.Fatalf("Subscribers() after close = %d, want 0", got)
		}
		late, _ := broker.Subscribe()
		if _, open := <-late; open {
			t.Fatal("subscription after close is open")
		}
		broker.Publish(gamerequest.Event{Type: gamerequest.EventCreated})
		if got := broker.Subscribers(); got != 0 {
			t.Fatalf("Subscribers() after late publish = %d, want 0", got)
		}
	}
}
