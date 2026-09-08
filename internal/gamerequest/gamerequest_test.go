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
	service := NewService(repository, staticGames(true), staticEvent("event-1"))
	startsAt := time.Date(2027, time.May, 1, 18, 0, 0, 0, time.UTC)
	maximum := int32(4)

	created, err := service.Create(t.Context(), "user-1", "game-1", &maximum, startsAt, "  Casual players welcome.  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.EventID != "event-1" || created.GameID != "game-1" || created.CreatorID != "user-1" ||
		created.Description != "Casual players welcome." || created.Status != StatusOpen {
		t.Fatalf("created request = %#v", created)
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

func TestCreateRequiresActiveGameAndCurrentEvent(t *testing.T) {
	startsAt := time.Now()
	if _, err := NewService(&recordingRepository{}, staticGames(false), staticEvent("event-1")).Create(
		t.Context(), "user-1", "game-1", nil, startsAt, "",
	); !errors.Is(err, ErrGameUnavailable) {
		t.Errorf("inactive game error = %v", err)
	}
	if _, err := NewService(&recordingRepository{}, staticGames(true), staticEvent("")).Create(
		t.Context(), "user-1", "game-1", nil, startsAt, "",
	); !errors.Is(err, ErrNoCurrentEvent) {
		t.Errorf("missing current event error = %v", err)
	}
}

type recordingRepository struct{ created GameRequest }

func (r *recordingRepository) Create(_ context.Context, request GameRequest) (GameRequest, error) {
	r.created = request
	request.ID = "request-1"
	return request, nil
}
func (*recordingRepository) Join(context.Context, string, string) error              { return nil }
func (*recordingRepository) Leave(context.Context, string, string) error             { return nil }
func (*recordingRepository) SetStatus(context.Context, string, string, Status) error { return nil }

type staticGames bool

func (g staticGames) ActiveExists(context.Context, string) (bool, error) { return bool(g), nil }

type staticEvent string

func (e staticEvent) Current(context.Context) (string, error) { return string(e), nil }
