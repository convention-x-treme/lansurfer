// Package event owns LANsurfer events and selection of the current event.
package event

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxNameLength = 120

var (
	ErrNotFound    = errors.New("event not found")
	ErrInvalidName = errors.New("invalid event name")
	ErrInvalidTime = errors.New("invalid event time range")
)

type Event struct {
	ID        string
	Name      string
	StartsAt  time.Time
	EndsAt    time.Time
	IsCurrent bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository interface {
	List(context.Context) ([]Event, error)
	Current(context.Context) (string, error)
	Create(context.Context, string, time.Time, time.Time) (Event, error)
	Update(context.Context, string, string, time.Time, time.Time) (Event, error)
	SetCurrent(context.Context, string) error
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context) ([]Event, error) {
	return s.repository.List(ctx)
}

func (s *Service) Current(ctx context.Context) (string, error) {
	return s.repository.Current(ctx)
}

func (s *Service) Create(ctx context.Context, name string, startsAt, endsAt time.Time) (Event, error) {
	name, err := validate(name, startsAt, endsAt)
	if err != nil {
		return Event{}, err
	}
	return s.repository.Create(ctx, name, startsAt, endsAt)
}

func (s *Service) Update(ctx context.Context, id, name string, startsAt, endsAt time.Time) (Event, error) {
	name, err := validate(name, startsAt, endsAt)
	if err != nil {
		return Event{}, err
	}
	return s.repository.Update(ctx, id, name, startsAt, endsAt)
}

func (s *Service) SetCurrent(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrNotFound
	}
	return s.repository.SetCurrent(ctx, id)
}

func validate(name string, startsAt, endsAt time.Time) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return "", ErrInvalidName
	}
	if startsAt.IsZero() || endsAt.IsZero() || !endsAt.After(startsAt) {
		return "", ErrInvalidTime
	}
	return name, nil
}
