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
	ErrNotFound             = errors.New("game request not found")
	ErrGameUnavailable      = errors.New("game unavailable")
	ErrInvalidMaxPlayers    = errors.New("invalid maximum players")
	ErrInvalidStartsAt      = errors.New("invalid start time")
	ErrInvalidDescription   = errors.New("invalid description")
	ErrNotOpen              = errors.New("game request is not open")
	ErrNotPlaying           = errors.New("game request is not playing")
	ErrNotActive            = errors.New("game request is not active")
	ErrFull                 = errors.New("game request is full")
	ErrAlreadyMember        = errors.New("user is already a member")
	ErrNotMember            = errors.New("user is not a member")
	ErrCreatorCannotLeave   = errors.New("creator cannot leave game request")
	ErrNotCreator           = errors.New("user is not the game request creator")
	ErrCapacityBelowMembers = errors.New("maximum players is below current membership")
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusPlaying   Status = "playing"
	StatusClosed    Status = "closed"
	StatusCancelled Status = "cancelled"
)

// EventType identifies a completed GameRequest change that may update read-only
// delivery concerns such as the realtime web experience.
type EventType string

const (
	EventCreated                   EventType = "GameRequestCreated"
	EventJoined                    EventType = "GameRequestJoined"
	EventLeft                      EventType = "GameRequestLeft"
	EventStarted                   EventType = "GameRequestStarted"
	EventClosed                    EventType = "GameRequestClosed"
	EventCancelled                 EventType = "GameRequestCancelled"
	EventUpdated                   EventType = "GameRequestUpdated"
	EventSessionInformationChanged EventType = "GameRequestSessionInformationChanged"
)

// SessionInformationEntry is one request-local key/value entry.
type SessionInformationEntry struct {
	ID, RequestID, Key, Value string
	IsSecret                  bool
	Sequence                  int64
	CreatedAt, UpdatedAt      time.Time
}

type SessionInformation struct {
	Entries []SessionInformationEntry
	CanEdit bool
}

type SessionInformationInput struct {
	Key, Value string
	IsSecret   bool
}

var (
	ErrInvalidSessionInformationKey    = errors.New("invalid session information key")
	ErrInvalidSessionInformationValue  = errors.New("invalid session information value")
	ErrSessionInformationKeyExists     = errors.New("session information key already exists")
	ErrSessionInformationLimit         = errors.New("session information limit reached")
	ErrSessionInformationEntryNotFound = errors.New("session information entry not found")
)

type Event struct {
	Type      EventType `json:"type"`
	RequestID string    `json:"requestId"`
	GameID    string    `json:"gameId,omitempty"`
}

// ActivityKind names a durable request-local activity entry. The repository
// records one entry per successful change in the same transaction as the change
// itself, which also makes the record usable as the notification handoff: a
// rolled-back mutation leaves neither.
type ActivityKind string

const (
	ActivityCreated   ActivityKind = "created"
	ActivityJoined    ActivityKind = "joined"
	ActivityLeft      ActivityKind = "left"
	ActivityStarted   ActivityKind = "started"
	ActivityClosed    ActivityKind = "closed"
	ActivityCancelled ActivityKind = "cancelled"
	ActivityUpdated   ActivityKind = "request_updated"
)

// EventPublisher is implemented by delivery infrastructure. Publishing is a
// best-effort signal: PostgreSQL remains the source of truth for refreshed UI.
type EventPublisher interface {
	Publish(Event)
}

type GameRequest struct {
	ID          string
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
	RecordView(context.Context, string, string) error
	PostMessage(ctx context.Context, requestID, userID, body string) (Message, error)
	LobbyAccess(ctx context.Context, requestID, userID string) error
	SessionInformation(context.Context, string, string) (SessionInformation, error)
	SessionInformationAccess(context.Context, string, string) error
	CreateSessionInformationEntry(context.Context, string, string, SessionInformationInput) error
	UpdateSessionInformationEntry(context.Context, string, string, string, SessionInformationInput) (bool, error)
	DeleteSessionInformationEntry(context.Context, string, string, string) error
	UpdateDetails(context.Context, string, string, UpdateDetailsInput) (bool, error)
}

type UpdateDetailsInput struct {
	MaxPlayers  *int32
	StartsAt    time.Time
	Description string
}

// GameLookup is implemented by the game module and keeps catalog rules with its owner.
type GameLookup interface {
	ActiveExists(context.Context, string) (bool, error)
}

type Service struct {
	repository Repository
	games      GameLookup
	publisher  EventPublisher
}

func NewService(repository Repository, games GameLookup, publisher EventPublisher) *Service {
	return &Service{repository: repository, games: games, publisher: publisher}
}

// ValidateCreateInput applies the game-independent Create validation rules
// (max players, start time, description length) and returns the trimmed
// description. It is exported so callers that must resolve a gameID (such as
// materializing a provider-referenced Game) can confirm these rules pass
// before triggering that side effect, while Service.Create itself keeps
// using it as the single source of truth for the same checks.
func ValidateCreateInput(maxPlayers *int32, startsAt time.Time, description string) (string, error) {
	description = strings.TrimSpace(description)
	if maxPlayers != nil && *maxPlayers < 2 {
		return description, ErrInvalidMaxPlayers
	}
	if startsAt.IsZero() {
		return description, ErrInvalidStartsAt
	}
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return description, ErrInvalidDescription
	}
	return description, nil
}

func (s *Service) Create(ctx context.Context, creatorID, gameID string, maxPlayers *int32, startsAt time.Time, description string) (GameRequest, error) {
	description, err := ValidateCreateInput(maxPlayers, startsAt, description)
	if err != nil {
		return GameRequest{}, err
	}
	available, err := s.games.ActiveExists(ctx, gameID)
	if err != nil {
		return GameRequest{}, err
	}
	if !available {
		return GameRequest{}, ErrGameUnavailable
	}
	created, err := s.repository.Create(ctx, GameRequest{
		GameID: gameID, CreatorID: creatorID,
		MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description, Status: StatusOpen,
	})
	if err == nil {
		s.publish(Event{Type: EventCreated, RequestID: created.ID, GameID: created.GameID})
	}
	return created, err
}

func (s *Service) Join(ctx context.Context, requestID, userID string) error {
	err := s.repository.Join(ctx, requestID, userID)
	if err == nil {
		s.publish(Event{Type: EventJoined, RequestID: requestID})
	}
	return err
}

func (s *Service) Leave(ctx context.Context, requestID, userID string) error {
	err := s.repository.Leave(ctx, requestID, userID)
	if err == nil {
		s.publish(Event{Type: EventLeft, RequestID: requestID})
	}
	return err
}

// UpdateDetails changes the mutable coordination details of a Game Request.
// A semantic no-op publishes no event.
func (s *Service) UpdateDetails(ctx context.Context, requestID, userID string, input UpdateDetailsInput) error {
	description, err := ValidateCreateInput(input.MaxPlayers, input.StartsAt, input.Description)
	if err != nil {
		return err
	}
	input.Description = description
	changed, err := s.repository.UpdateDetails(ctx, requestID, userID, input)
	if err == nil && changed {
		s.publish(Event{Type: EventUpdated, RequestID: requestID})
	}
	return err
}

// RecordView notes that a player opened a Game Request, which is what the
// navigation's "Recently viewed" list reads back. It publishes no event: a
// view is private reading history, not a change to the Game Request, and must
// never reach the realtime stream or Track E's notification handoff.
func (s *Service) RecordView(ctx context.Context, requestID, userID string) error {
	return s.repository.RecordView(ctx, requestID, userID)
}

func (s *Service) Close(ctx context.Context, requestID, userID string) error {
	return s.setStatus(ctx, requestID, userID, StatusClosed, EventClosed)
}

func (s *Service) Start(ctx context.Context, requestID, userID string) error {
	return s.setStatus(ctx, requestID, userID, StatusPlaying, EventStarted)
}

func (s *Service) Cancel(ctx context.Context, requestID, userID string) error {
	return s.setStatus(ctx, requestID, userID, StatusCancelled, EventCancelled)
}

func (s *Service) setStatus(ctx context.Context, requestID, userID string, status Status, eventType EventType) error {
	err := s.repository.SetStatus(ctx, requestID, userID, status)
	if err == nil {
		s.publish(Event{Type: eventType, RequestID: requestID})
	}
	return err
}

func (s *Service) publish(event Event) {
	if s.publisher != nil {
		s.publisher.Publish(event)
	}
}
