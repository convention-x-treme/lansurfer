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

func TestSubscriptionSearchValidationAndWrites(t *testing.T) {
	repository := &recordingSubscriptionRepository{}
	service := NewSubscriptionService(repository)

	if _, err := service.Browse(t.Context(), "user-1", "  wc3  "); err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if repository.search != "wc3" {
		t.Fatalf("browse search = %q, want wc3", repository.search)
	}
	if _, err := service.Browse(t.Context(), "user-1", string(make([]byte, maxSearchLength+1))); !errors.Is(err, ErrInvalidSearch) {
		t.Errorf("long search error = %v, want ErrInvalidSearch", err)
	}
	if err := service.Follow(t.Context(), "user-1", "game-1"); err != nil {
		t.Fatalf("Follow() error = %v", err)
	}
	if err := service.Unfollow(t.Context(), "user-1", "game-1"); err != nil {
		t.Fatalf("Unfollow() error = %v", err)
	}
	if repository.followed != "user-1/game-1" || repository.unfollowed != "user-1/game-1" {
		t.Fatalf("writes = %q, %q", repository.followed, repository.unfollowed)
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

type recordingSubscriptionRepository struct{ search, followed, unfollowed string }

func (r *recordingSubscriptionRepository) Browse(_ context.Context, _, search string) ([]SubscriptionGame, error) {
	r.search = search
	return nil, nil
}
func (*recordingSubscriptionRepository) MyGames(context.Context, string) ([]SubscriptionGame, error) {
	return nil, nil
}
func (r *recordingSubscriptionRepository) Follow(_ context.Context, userID, gameID string) error {
	r.followed = userID + "/" + gameID
	return nil
}
func (r *recordingSubscriptionRepository) Unfollow(_ context.Context, userID, gameID string) error {
	r.unfollowed = userID + "/" + gameID
	return nil
}
