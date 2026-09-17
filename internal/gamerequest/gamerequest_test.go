package gamerequest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateValidatesAndUsesOwnedLookups(t *testing.T) {
	repository := &recordingRepository{}
	publisher := &recordingPublisher{}
	service := NewService(repository, staticGames(true), publisher)
	startsAt := time.Date(2027, time.May, 1, 18, 0, 0, 0, time.UTC)
	maximum := int32(4)

	created, err := service.Create(t.Context(), "user-1", "game-1", &maximum, startsAt, "  Casual players welcome.  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.GameID != "game-1" || created.CreatorID != "user-1" ||
		created.Description != "Casual players welcome." || created.Status != StatusOpen {
		t.Fatalf("created request = %#v", created)
	}
	if len(publisher.events) != 1 || publisher.events[0] != (Event{Type: EventCreated, RequestID: "request-1", GameID: "game-1"}) {
		t.Fatalf("published event = %#v", publisher.events)
	}

	invalidMaximum := int32(0)
	if _, err := service.Create(t.Context(), "user-1", "game-1", &invalidMaximum, startsAt, ""); !errors.Is(err, ErrInvalidMaxPlayers) {
		t.Errorf("invalid maximum error = %v", err)
	}
	if _, err := service.Create(t.Context(), "user-1", "game-1", nil, time.Time{}, ""); !errors.Is(err, ErrInvalidStartsAt) {
		t.Errorf("zero start error = %v", err)
	}
	if _, err := service.Create(t.Context(), "user-1", "game-1", nil, startsAt, strings.Repeat("x", maxDescriptionLength+1)); !errors.Is(err, ErrInvalidDescription) {
		t.Errorf("long description error = %v", err)
	}
}

// Creating a Game Request needs an active Game and nothing else: the
// installation itself is the 1.0 event context.
func TestCreateRequiresActiveGameOnly(t *testing.T) {
	startsAt := time.Now()
	if _, err := NewService(&recordingRepository{}, staticGames(false), nil).Create(
		t.Context(), "user-1", "game-1", nil, startsAt, "",
	); !errors.Is(err, ErrGameUnavailable) {
		t.Errorf("inactive game error = %v", err)
	}
	if _, err := NewService(&recordingRepository{}, staticGames(true), nil).Create(
		t.Context(), "user-1", "game-1", nil, startsAt, "",
	); err != nil {
		t.Errorf("Create() without an Event error = %v", err)
	}
}

func TestPublishesSuccessfulLifecycleChangesOnly(t *testing.T) {
	repository := &recordingRepository{}
	publisher := &recordingPublisher{}
	service := NewService(repository, staticGames(true), publisher)

	if err := service.Join(t.Context(), "request-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Leave(t.Context(), "request-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), "request-1", "user-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(t.Context(), "request-2", "user-1"); err != nil {
		t.Fatal(err)
	}

	want := []Event{
		{Type: EventJoined, RequestID: "request-1"},
		{Type: EventLeft, RequestID: "request-1"},
		{Type: EventClosed, RequestID: "request-1"},
		{Type: EventCancelled, RequestID: "request-2"},
	}
	if len(publisher.events) != len(want) {
		t.Fatalf("events = %#v", publisher.events)
	}
	for index := range want {
		if publisher.events[index] != want[index] {
			t.Errorf("event %d = %#v, want %#v", index, publisher.events[index], want[index])
		}
	}

	repository.joinErr = ErrFull
	if err := service.Join(t.Context(), "request-3", "user-1"); !errors.Is(err, ErrFull) {
		t.Fatalf("Join() error = %v", err)
	}
	if len(publisher.events) != len(want) {
		t.Error("failed change published an event")
	}
}

type recordingRepository struct {
	created GameRequest
	joinErr error
}

func (r *recordingRepository) Create(_ context.Context, request GameRequest) (GameRequest, error) {
	r.created = request
	request.ID = "request-1"
	return request, nil
}
func (r *recordingRepository) Join(context.Context, string, string) error            { return r.joinErr }
func (*recordingRepository) Leave(context.Context, string, string) error             { return nil }
func (*recordingRepository) SetStatus(context.Context, string, string, Status) error { return nil }

type staticGames bool

func (g staticGames) ActiveExists(context.Context, string) (bool, error) { return bool(g), nil }

type recordingPublisher struct{ events []Event }

func (p *recordingPublisher) Publish(event Event) { p.events = append(p.events, event) }
