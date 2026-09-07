package user

import (
	"context"
	"errors"
	"testing"
)

func TestRegisterValidatesAndNormalizesDisplayName(t *testing.T) {
	repository := &recordingRepository{}
	service := NewService(repository)

	created, err := service.Register(t.Context(), "  Ada Player  ")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if repository.displayName != "Ada Player" {
		t.Errorf("display name = %q", repository.displayName)
	}
	if repository.role != RoleUser {
		t.Errorf("role = %q, want %q", repository.role, RoleUser)
	}
	if created.DisplayName != "Ada Player" {
		t.Errorf("created display name = %q", created.DisplayName)
	}
}

func TestRegisterRejectsInvalidDisplayName(t *testing.T) {
	service := NewService(&recordingRepository{})
	for _, displayName := range []string{"", "   ", string(make([]rune, maxDisplayNameLength+1))} {
		_, err := service.Register(t.Context(), displayName)
		if !errors.Is(err, ErrInvalidDisplayName) {
			t.Errorf("Register(%q) error = %v, want ErrInvalidDisplayName", displayName, err)
		}
	}
}

func TestPromoteToAdmin(t *testing.T) {
	repository := &recordingRepository{}
	service := NewService(repository)

	promoted, err := service.PromoteToAdmin(t.Context(), "user-id")
	if err != nil {
		t.Fatalf("PromoteToAdmin() error = %v", err)
	}
	if repository.promotedID != "user-id" || repository.role != RoleAdmin || promoted.Role != RoleAdmin {
		t.Fatalf("promoted user = %#v, repository = %#v", promoted, repository)
	}
	if _, err := service.PromoteToAdmin(t.Context(), " "); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty user ID error = %v, want ErrNotFound", err)
	}
}

type recordingRepository struct {
	displayName string
	promotedID  string
	role        Role
}

func (r *recordingRepository) Create(_ context.Context, displayName string, role Role) (User, error) {
	r.displayName = displayName
	r.role = role
	return User{ID: "user-id", DisplayName: displayName, Role: role}, nil
}

func (*recordingRepository) ByID(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}

func (*recordingRepository) List(context.Context) ([]User, error) { return nil, nil }

func (r *recordingRepository) SetRole(_ context.Context, id string, role Role) (User, error) {
	r.promotedID = id
	r.role = role
	return User{ID: id, Role: role}, nil
}

func (*recordingRepository) Delete(context.Context, string) error {
	return nil
}
