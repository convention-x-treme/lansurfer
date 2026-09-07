// Package user owns LANsurfer users and their local roles.
package user

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxDisplayNameLength = 100

var (
	// ErrNotFound indicates that a local user does not exist.
	ErrNotFound = errors.New("user not found")
	// ErrInvalidDisplayName indicates that a display name is empty or too long.
	ErrInvalidDisplayName = errors.New("invalid display name")
)

// Role is a deliberately small local authorization role.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// User is LANsurfer's local user, independent of any identity provider.
type User struct {
	ID          string
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

// Register creates a normal local user for a newly authenticated identity.
func (s *Service) Register(ctx context.Context, displayName string) (User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || utf8.RuneCountInString(displayName) > maxDisplayNameLength {
		return User{}, ErrInvalidDisplayName
	}
	return s.repository.Create(ctx, displayName, RoleUser)
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
