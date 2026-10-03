package gamerequest

import (
	"errors"
	"strings"
	"testing"
)

func TestSessionInformationInputValidation(t *testing.T) {
	repository := &recordingRepository{}
	service := NewService(repository, staticGames(true), nil)
	if err := service.CreateSessionInformationEntry(t.Context(), "request-1", "host-1", SessionInformationInput{Key: "  Server  ", Value: "  10.0.0.1  "}); err != nil {
		t.Fatal(err)
	}
	if repository.sessionInput != (SessionInformationInput{Key: "Server", Value: "10.0.0.1"}) {
		t.Fatalf("input = %#v", repository.sessionInput)
	}
	for _, test := range []struct {
		input SessionInformationInput
		want  error
	}{
		{SessionInformationInput{Key: " ", Value: "value"}, ErrInvalidSessionInformationKey},
		{SessionInformationInput{Key: "key", Value: " "}, ErrInvalidSessionInformationValue},
		{SessionInformationInput{Key: "key\nx", Value: "value"}, ErrInvalidSessionInformationKey},
		{SessionInformationInput{Key: "key", Value: "value\rx"}, ErrInvalidSessionInformationValue},
		{SessionInformationInput{Key: strings.Repeat("ä", 41), Value: "value"}, ErrInvalidSessionInformationKey},
		{SessionInformationInput{Key: "key", Value: strings.Repeat("界", 301)}, ErrInvalidSessionInformationValue},
	} {
		if err := service.CreateSessionInformationEntry(t.Context(), "request-1", "host-1", test.input); !errors.Is(err, test.want) {
			t.Errorf("CreateSessionInformationEntry(%#v) error = %v, want %v", test.input, err, test.want)
		}
	}
	for _, input := range []SessionInformationInput{{Key: strings.Repeat("ä", 40), Value: "v"}, {Key: "k", Value: strings.Repeat("界", 300)}} {
		if err := service.CreateSessionInformationEntry(t.Context(), "request-1", "host-1", input); err != nil {
			t.Errorf("CreateSessionInformationEntry(%#v): %v", input, err)
		}
	}
}

func TestSessionInformationEventsOnlyPublishForRealMutations(t *testing.T) {
	repository := &recordingRepository{}
	publisher := &recordingPublisher{}
	service := NewService(repository, staticGames(true), publisher)
	input := SessionInformationInput{Key: "key", Value: "value"}
	if err := service.CreateSessionInformationEntry(t.Context(), "request-1", "host-1", input); err != nil {
		t.Fatal(err)
	}
	repository.sessionChanged = false
	if err := service.UpdateSessionInformationEntry(t.Context(), "request-1", "entry-1", "host-1", input); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events after no-op = %#v", publisher.events)
	}
	repository.sessionChanged = true
	if err := service.UpdateSessionInformationEntry(t.Context(), "request-1", "entry-1", "host-1", input); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteSessionInformationEntry(t.Context(), "request-1", "entry-1", "host-1"); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events = %#v", publisher.events)
	}
	for _, event := range publisher.events {
		if event.Type != EventSessionInformationChanged || event.RequestID != "request-1" {
			t.Errorf("event = %#v", event)
		}
	}
	repository.sessionErr = errors.New("repository failure")
	if err := service.CreateSessionInformationEntry(t.Context(), "request-1", "host-1", input); err == nil {
		t.Fatal("expected repository error")
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events after error = %#v", publisher.events)
	}
}

func TestCanSessionInformationFailsClosed(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{{ErrNotFound, false}, {ErrNotMember, false}, {ErrNotActive, false}, {nil, true}} {
		repository := &recordingRepository{sessionAccessErr: test.err}
		got, err := NewService(repository, staticGames(true), nil).CanSessionInformation(t.Context(), "request-1", "user-1")
		if err != nil || got != test.want {
			t.Errorf("CanSessionInformation() = %v, %v; want %v, nil", got, err, test.want)
		}
	}
	wantErr := errors.New("database failure")
	got, err := NewService(&recordingRepository{sessionAccessErr: wantErr}, staticGames(true), nil).CanSessionInformation(t.Context(), "request-1", "user-1")
	if got || !errors.Is(err, wantErr) {
		t.Fatalf("CanSessionInformation() = %v, %v", got, err)
	}
}
