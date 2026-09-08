package event

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateValidatesEvent(t *testing.T) {
	start := time.Date(2027, time.May, 1, 12, 0, 0, 0, time.UTC)
	repository := &recordingRepository{}
	service := NewService(repository)

	created, err := service.Create(t.Context(), "  CXT 2027  ", start, start.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "CXT 2027" || repository.createdName != "CXT 2027" {
		t.Fatalf("created event = %#v", created)
	}

	for _, test := range []struct {
		name     string
		startsAt time.Time
		endsAt   time.Time
		want     error
	}{
		{name: "", startsAt: start, endsAt: start.Add(time.Hour), want: ErrInvalidName},
		{name: "CXT", startsAt: start, endsAt: start, want: ErrInvalidTime},
		{name: "CXT", startsAt: start, endsAt: start.Add(-time.Hour), want: ErrInvalidTime},
	} {
		if _, err := service.Create(t.Context(), test.name, test.startsAt, test.endsAt); !errors.Is(err, test.want) {
			t.Errorf("Create(%q) error = %v, want %v", test.name, err, test.want)
		}
	}
}

type recordingRepository struct{ createdName string }

func (*recordingRepository) List(context.Context) ([]Event, error)   { return nil, nil }
func (*recordingRepository) Current(context.Context) (string, error) { return "event-1", nil }
func (r *recordingRepository) Create(_ context.Context, name string, startsAt, endsAt time.Time) (Event, error) {
	r.createdName = name
	return Event{Name: name, StartsAt: startsAt, EndsAt: endsAt}, nil
}
func (*recordingRepository) Update(context.Context, string, string, time.Time, time.Time) (Event, error) {
	return Event{}, nil
}
func (*recordingRepository) SetCurrent(context.Context, string) error { return nil }
