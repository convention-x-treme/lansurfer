// Package gamerequest owns game request creation, membership, and lifecycle behavior.
package gamerequest

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxDescriptionLength = 1000

var (
	ErrNotFound           = errors.New("game request not found")
	ErrNoCurrentEvent     = errors.New("no current event")
	ErrGameUnavailable    = errors.New("game unavailable")
	ErrInvalidMaxPlayers  = errors.New("invalid maximum players")
	ErrInvalidStartsAt    = errors.New("invalid start time")
	ErrInvalidDescription = errors.New("invalid description")
	ErrNotOpen            = errors.New("game request is not open")
	ErrFull               = errors.New("game request is full")
	ErrAlreadyMember      = errors.New("user is already a member")
	ErrNotMember          = errors.New("user is not a member")
	ErrCreatorCannotLeave = errors.New("creator cannot leave game request")
	ErrNotCreator         = errors.New("user is not the game request creator")
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusClosed    Status = "closed"
	StatusCancelled Status = "cancelled"
)

type GameRequest struct {
	ID          string
	EventID     string
	GameID      string
	CreatorID   string
	MaxPlayers  *int32
	StartsAt    time.Time
	Description string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Repository interface {
	Create(context.Context, GameRequest) (GameRequest, error)
	Join(context.Context, string, string) error
	Leave(context.Context, string, string) error
	SetStatus(context.Context, string, string, Status) error
}

// GameLookup is implemented by the game module and keeps catalog rules with its owner.
type GameLookup interface {
	ActiveExists(context.Context, string) (bool, error)
}

// EventLookup is implemented by the event module and keeps current-event selection with its owner.
type EventLookup interface {
	Current(context.Context) (string, error)
}

type Service struct {
	repository Repository
	games      GameLookup
	events     EventLookup
}

func NewService(repository Repository, games GameLookup, events EventLookup) *Service {
	return &Service{repository: repository, games: games, events: events}
}

func (s *Service) Create(ctx context.Context, creatorID, gameID string, maxPlayers *int32, startsAt time.Time, description string) (GameRequest, error) {
	description = strings.TrimSpace(description)
	if maxPlayers != nil && *maxPlayers < 1 {
		return GameRequest{}, ErrInvalidMaxPlayers
	}
	if startsAt.IsZero() {
		return GameRequest{}, ErrInvalidStartsAt
	}
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return GameRequest{}, ErrInvalidDescription
	}
	available, err := s.games.ActiveExists(ctx, gameID)
	if err != nil {
		return GameRequest{}, err
	}
	if !available {
		return GameRequest{}, ErrGameUnavailable
	}
	eventID, err := s.events.Current(ctx)
	if err != nil {
		return GameRequest{}, err
	}
	if eventID == "" {
		return GameRequest{}, ErrNoCurrentEvent
	}
	return s.repository.Create(ctx, GameRequest{
		EventID: eventID, GameID: gameID, CreatorID: creatorID,
		MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description, Status: StatusOpen,
	})
}

func (s *Service) Join(ctx context.Context, requestID, userID string) error {
	return s.repository.Join(ctx, requestID, userID)
}

func (s *Service) Leave(ctx context.Context, requestID, userID string) error {
	return s.repository.Leave(ctx, requestID, userID)
}

func (s *Service) Close(ctx context.Context, requestID, userID string) error {
	return s.repository.SetStatus(ctx, requestID, userID, StatusClosed)
}

func (s *Service) Cancel(ctx context.Context, requestID, userID string) error {
	return s.repository.SetStatus(ctx, requestID, userID, StatusCancelled)
}
