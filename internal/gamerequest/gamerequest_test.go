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

func TestValidateCreateInputFiniteMinimum(t *testing.T) {
	startsAt := time.Date(2027, time.May, 1, 18, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		maximum *int32
		want    error
	}{
		{"unlimited", nil, nil},
		{"zero pointer", new(int32(0)), ErrInvalidMaxPlayers},
		{"one", new(int32(1)), ErrInvalidMaxPlayers},
		{"two", new(int32(2)), nil},
	} {
		if _, err := ValidateCreateInput(tc.maximum, startsAt, ""); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// Creating a Game Request needs an active Game and nothing else: the
// installation itself is the event context.
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
	if err := service.Start(t.Context(), "request-1", "user-1"); err != nil {
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
		{Type: EventStarted, RequestID: "request-1"},
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
	repository.statusErr = errors.New("status update failed")
	if err := service.Start(t.Context(), "request-3", "user-1"); !errors.Is(err, repository.statusErr) {
		t.Fatalf("Start() error = %v", err)
	}
	if len(publisher.events) != len(want) {
		t.Error("failed start published an event")
	}
}

func TestUpdateDetailsPublishesEachRealChangeOnce(t *testing.T) {
	startsAt := time.Date(2026, time.October, 2, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input UpdateDetailsInput
	}{
		{name: "capacity only", input: UpdateDetailsInput{MaxPlayers: new(int32(6)), StartsAt: startsAt, Description: "same"}},
		{name: "start only", input: UpdateDetailsInput{StartsAt: startsAt.Add(time.Hour), Description: "same"}},
		{name: "description only", input: UpdateDetailsInput{StartsAt: startsAt, Description: "changed"}},
		{name: "multiple fields", input: UpdateDetailsInput{MaxPlayers: new(int32(6)), StartsAt: startsAt.Add(time.Hour), Description: "changed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &recordingRepository{detailsChanged: true}
			publisher := &recordingPublisher{}
			service := NewService(repository, staticGames(true), publisher)

			if err := service.UpdateDetails(t.Context(), "request-1", "host-1", tt.input); err != nil {
				t.Fatal(err)
			}
			if repository.detailsCalls != 1 || repository.detailsInput != tt.input {
				t.Fatalf("repository input = %#v, calls = %d; want %#v, 1", repository.detailsInput, repository.detailsCalls, tt.input)
			}
			if want := []Event{{Type: EventUpdated, RequestID: "request-1"}}; len(publisher.events) != 1 || publisher.events[0] != want[0] {
				t.Fatalf("events = %#v, want %#v", publisher.events, want)
			}
		})
	}
}

func TestUpdateDetailsPublishesOnlySuccessfulChanges(t *testing.T) {
	repository := &recordingRepository{}
	publisher := &recordingPublisher{}
	service := NewService(repository, staticGames(true), publisher)
	input := UpdateDetailsInput{StartsAt: time.Date(2026, time.October, 2, 18, 30, 0, 0, time.UTC), Description: "text"}

	if err := service.UpdateDetails(t.Context(), "request-1", "host-1", input); err != nil {
		t.Fatalf("no-op error = %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("no-op published events: %#v", publisher.events)
	}

	repository.detailsErr = ErrCapacityBelowMembers
	if err := service.UpdateDetails(t.Context(), "request-1", "host-1", input); !errors.Is(err, ErrCapacityBelowMembers) {
		t.Fatalf("error = %v, want %v", err, ErrCapacityBelowMembers)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("failed update published events: %#v", publisher.events)
	}
}

func TestUpdateDetailsValidatesAndTrimsInput(t *testing.T) {
	validStart := time.Date(2026, time.October, 2, 18, 30, 0, 0, time.UTC)
	for _, value := range []int32{-1, 0, 1} {
		repository := &recordingRepository{}
		service := NewService(repository, staticGames(true), &recordingPublisher{})
		input := UpdateDetailsInput{MaxPlayers: &value, StartsAt: validStart}
		if err := service.UpdateDetails(t.Context(), "request-1", "host-1", input); !errors.Is(err, ErrInvalidMaxPlayers) {
			t.Errorf("UpdateDetails(max=%d) error = %v, want %v", value, err, ErrInvalidMaxPlayers)
		}
		if repository.detailsCalls != 0 {
			t.Errorf("repository called for invalid max %d", value)
		}
	}

	for _, input := range []UpdateDetailsInput{
		{StartsAt: time.Time{}},
		{StartsAt: validStart, Description: strings.Repeat("x", 1001)},
	} {
		repository := &recordingRepository{}
		service := NewService(repository, staticGames(true), &recordingPublisher{})
		if err := service.UpdateDetails(t.Context(), "request-1", "host-1", input); err == nil {
			t.Errorf("UpdateDetails(%#v) succeeded, want validation error", input)
		}
		if repository.detailsCalls != 0 {
			t.Errorf("repository called for invalid input %#v", input)
		}
	}

	repository := &recordingRepository{}
	service := NewService(repository, staticGames(true), &recordingPublisher{})
	input := UpdateDetailsInput{StartsAt: validStart, Description: "  trimmed  "}
	if err := service.UpdateDetails(t.Context(), "request-1", "host-1", input); err != nil {
		t.Fatal(err)
	}
	if repository.detailsInput.Description != "trimmed" {
		t.Errorf("description = %q, want trimmed", repository.detailsInput.Description)
	}
}

func TestCanReadLobbyUsesMembershipOnly(t *testing.T) {
	backendErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name    string
		err     error
		want    bool
		wantErr error
	}{
		{name: "member", want: true},
		{name: "non-member", err: ErrNotMember, want: false},
		{name: "missing", err: ErrNotFound, want: false, wantErr: ErrNotFound},
		{name: "backend failure", err: backendErr, want: false, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &recordingRepository{lobbyAccessErr: tc.err}
			got, err := NewService(repository, staticGames(true), nil).CanReadLobby(t.Context(), "request", "user")
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("CanReadLobby() = %v, %v; want %v, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func (r *recordingRepository) LobbyAccess(context.Context, string, string) error {
	return r.lobbyAccessErr
}

type recordingRepository struct {
	created          GameRequest
	joinErr          error
	status           Status
	statusErr        error
	detailsInput     UpdateDetailsInput
	detailsChanged   bool
	detailsErr       error
	detailsCalls     int
	sessionInput     SessionInformationInput
	sessionChanged   bool
	sessionErr       error
	sessionAccessErr error
	lobbyAccessErr   error
}

func (r *recordingRepository) SessionInformation(context.Context, string, string) (SessionInformation, error) {
	return SessionInformation{}, r.sessionErr
}
func (r *recordingRepository) SessionInformationAccess(context.Context, string, string) error {
	return r.sessionAccessErr
}
func (r *recordingRepository) CreateSessionInformationEntry(_ context.Context, _, _ string, input SessionInformationInput) error {
	r.sessionInput = input
	return r.sessionErr
}
func (r *recordingRepository) UpdateSessionInformationEntry(_ context.Context, _, _, _ string, input SessionInformationInput) (bool, error) {
	r.sessionInput = input
	return r.sessionChanged, r.sessionErr
}
func (r *recordingRepository) DeleteSessionInformationEntry(context.Context, string, string, string) error {
	return r.sessionErr
}

func (r *recordingRepository) UpdateDetails(_ context.Context, _, _ string, input UpdateDetailsInput) (bool, error) {
	r.detailsCalls++
	r.detailsInput = input
	return r.detailsChanged, r.detailsErr
}

func (r *recordingRepository) Create(_ context.Context, request GameRequest) (GameRequest, error) {
	r.created = request
	request.ID = "request-1"
	return request, nil
}
func (r *recordingRepository) Join(context.Context, string, string) error               { return r.joinErr }
func (*recordingRepository) Leave(context.Context, string, string) error                { return nil }
func (*recordingRepository) RemoveMember(context.Context, string, string, string) error { return nil }
func (r *recordingRepository) SetStatus(_ context.Context, _, _ string, status Status) error {
	r.status = status
	return r.statusErr
}
func (*recordingRepository) RecordView(context.Context, string, string) error { return nil }
func (*recordingRepository) PostMessage(context.Context, string, string, string) (Message, error) {
	return Message{}, nil
}

type staticGames bool

func (g staticGames) ActiveExists(context.Context, string) (bool, error) { return bool(g), nil }

type recordingPublisher struct{ events []Event }

func (p *recordingPublisher) Publish(event Event) { p.events = append(p.events, event) }
