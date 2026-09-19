// Package user owns LANsurfer users and their local roles.
package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxGameNickLength = 32

var (
	// ErrNotFound indicates that a local user does not exist.
	ErrNotFound = errors.New("user not found")
	// ErrInvalidGameNick indicates that a Game Nick is not a printable,
	// non-empty, 1–32-code-point public identity.
	ErrInvalidGameNick = errors.New("invalid game nick")
	// ErrGameNickTaken indicates that another player has claimed the same
	// case-insensitive Game Nick.
	ErrGameNickTaken = errors.New("game nick already taken")
)

// Role is a deliberately small local authorization role.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// User is LANsurfer's local user, independent of any identity provider.
type User struct {
	ID       string
	GameNick string
	// DisplayName is a compatibility projection while web/read-model callers
	// are migrated to GameNick. It is always derived from GameNick and is not
	// persisted separately.
	DisplayName string
	Role        Role
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repository persists local users. The interface belongs to the consuming
// user module; concrete storage remains outside its domain model.
type Repository interface {
	Create(context.Context, string, Role) (User, error)
	ByID(context.Context, string) (User, error)
	List(context.Context) ([]User, error)
	SetGameNick(context.Context, string, string) (User, error)
	SetRole(context.Context, string, Role) (User, error)
	Delete(context.Context, string) error
}

// Service owns local user behavior.
type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Register creates a normal local user with its required public Game Nick.
func (s *Service) Register(ctx context.Context, gameNick string) (User, error) {
	gameNick, err := ValidateGameNick(gameNick)
	if err != nil {
		return User{}, err
	}
	return s.repository.Create(ctx, gameNick, RoleUser)
}

// ValidateGameNick trims only outer Unicode whitespace and preserves the
// player's spelling and internal spacing.
func ValidateGameNick(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxGameNickLength {
		return "", ErrInvalidGameNick
	}
	for _, r := range value {
		if !unicode.IsPrint(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", ErrInvalidGameNick
		}
	}
	return value, nil
}

// ClaimGameNick gives an upgraded local user its required public identity.
func (s *Service) ClaimGameNick(ctx context.Context, userID, gameNick string) (User, error) {
	gameNick, err := ValidateGameNick(gameNick)
	if err != nil {
		return User{}, err
	}
	return s.repository.SetGameNick(ctx, userID, gameNick)
}

// RenameGameNick changes a claimed public identity under the same validation
// and database uniqueness invariant as the initial claim.
func (s *Service) RenameGameNick(ctx context.Context, userID, gameNick string) (User, error) {
	return s.ClaimGameNick(ctx, userID, gameNick)
}

func (s *Service) ByID(ctx context.Context, id string) (User, error) {
	return s.repository.ByID(ctx, id)
}

// List returns local users for operator administration.
func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.repository.List(ctx)
}

// PromoteToAdmin grants the deliberately small local administrator role.
func (s *Service) PromoteToAdmin(ctx context.Context, id string) (User, error) {
	if strings.TrimSpace(id) == "" {
		return User{}, ErrNotFound
	}
	return s.repository.SetRole(ctx, id, RoleAdmin)
}

// RollbackRegistration removes a user created during an identity-linking
// attempt that could not be completed.
func (s *Service) RollbackRegistration(ctx context.Context, id string) error {
	return s.repository.Delete(ctx, id)
}
