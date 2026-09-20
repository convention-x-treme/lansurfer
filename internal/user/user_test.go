package user

import (
	"context"
	"errors"
	"testing"
)

func TestRegisterClaimsTrimmedGameNick(t *testing.T) {
	repository := &recordingRepository{}
	service := NewService(repository)

	created, err := service.Register(t.Context(), "  Ada Player  ")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if repository.gameNick != "Ada Player" {
		t.Errorf("game nick = %q", repository.gameNick)
	}
	if repository.role != RoleUser {
		t.Errorf("role = %q, want %q", repository.role, RoleUser)
	}
	if created.GameNick != "Ada Player" {
		t.Errorf("created game nick = %q", created.GameNick)
	}
}

func TestValidateGameNickRejectsInvalidUnicode(t *testing.T) {
	for _, gameNick := range []string{"", "   ", string(make([]rune, 33)), "not\x00printable", "invisible\u200bformat"} {
		if _, err := ValidateGameNick(gameNick); !errors.Is(err, ErrInvalidGameNick) {
			t.Errorf("ValidateGameNick(%q) error = %v, want ErrInvalidGameNick", gameNick, err)
		}
	}
}

func TestValidateGameNickPreservesInternalSpacingAndUnicode(t *testing.T) {
	gameNick, err := ValidateGameNick("\u2003Night  Otter\u2003")
	if err != nil {
		t.Fatalf("ValidateGameNick() error = %v", err)
	}
	if gameNick != "Night  Otter" {
		t.Fatalf("game nick = %q, want preserved internal spaces", gameNick)
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
	gameNick   string
	promotedID string
	role       Role
}

func (r *recordingRepository) Create(_ context.Context, gameNick string, role Role) (User, error) {
	r.gameNick = gameNick
	r.role = role
	return User{ID: "user-id", GameNick: gameNick, Role: role}, nil
}

func (*recordingRepository) ByID(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}

func (*recordingRepository) List(context.Context) ([]User, error) { return nil, nil }

func (r *recordingRepository) SetGameNick(_ context.Context, id, gameNick string) (User, error) {
	r.promotedID = id
	r.gameNick = gameNick
	return User{ID: id, GameNick: gameNick, Role: r.role}, nil
}

func (r *recordingRepository) SetRole(_ context.Context, id string, role Role) (User, error) {
	r.promotedID = id
	r.role = role
	return User{ID: id, Role: role}, nil
}

func (*recordingRepository) Delete(context.Context, string) error {
	return nil
}
