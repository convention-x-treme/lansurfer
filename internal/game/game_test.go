package game

import (
	"context"
	"errors"
	"testing"
)

func TestCatalogValidation(t *testing.T) {
	repository := &recordingRepository{}
	service := NewService(repository)

	created, err := service.Create(t.Context(), "  Warcraft III  ", "warcraft-3")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "Warcraft III" || repository.createdSlug != "warcraft-3" {
		t.Fatalf("created game = %#v", created)
	}
	if _, err := service.Create(t.Context(), "Warcraft III", "Warcraft III"); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("invalid slug error = %v", err)
	}
	if _, err := service.AddAlias(t.Context(), "game", "  WC3  "); err != nil || repository.alias != "WC3" {
		t.Errorf("AddAlias() = %q, %v", repository.alias, err)
	}
	if _, err := service.AddAlias(t.Context(), "game", " "); !errors.Is(err, ErrInvalidAlias) {
		t.Errorf("empty alias error = %v", err)
	}
}

type recordingRepository struct{ createdSlug, alias string }

func (*recordingRepository) List(context.Context, string) ([]Game, error) { return nil, nil }
func (r *recordingRepository) Create(_ context.Context, name, slug string) (Game, error) {
	r.createdSlug = slug
	return Game{Name: name, Slug: slug}, nil
}
func (*recordingRepository) Update(context.Context, string, string, string) (Game, error) {
	return Game{}, nil
}
func (*recordingRepository) SetActive(context.Context, string, bool) error { return nil }
func (r *recordingRepository) AddAlias(_ context.Context, _, value string) (Alias, error) {
	r.alias = value
	return Alias{Value: value}, nil
}
func (*recordingRepository) RemoveAlias(context.Context, string, string) error { return nil }
