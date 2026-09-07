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

type recordingRepository struct {
	displayName string
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

func (*recordingRepository) Delete(context.Context, string) error {
	return nil
}
