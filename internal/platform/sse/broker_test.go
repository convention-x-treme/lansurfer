package sse

import (
	"fmt"
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

func TestBrokerDropsSeventeenthUndrainedEvent(t *testing.T) {
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe()
	for i := range 17 {
		broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: fmt.Sprintf("request-%02d", i)})
	}
	for i := range 16 {
		got := <-events
		want := fmt.Sprintf("request-%02d", i)
		if got.RequestID != want {
			t.Fatalf("event %d = %q, want %q", i, got.RequestID, want)
		}
	}
	if got := len(events); got != 0 {
		t.Fatalf("buffer has %d events after drain, want 0", got)
	}
	broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: "after-drain"})
	if got := (<-events).RequestID; got != "after-drain" {
		t.Fatalf("post-drain event = %q", got)
	}
	unsubscribe()
	if _, open := <-events; open {
		t.Fatal("subscriber remains open after overflow and unsubscribe")
	}
}
