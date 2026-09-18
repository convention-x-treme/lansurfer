package game

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeDiscoveryRepository struct {
	items     []DiscoveryItem
	err       error
	calls     int
	lastUser  string
	lastQuery string
	lastLimit int
}

func (f *fakeDiscoveryRepository) Search(_ context.Context, userID, search string, limit int) ([]DiscoveryItem, error) {
	f.calls++
	f.lastUser = userID
	f.lastQuery = search
	f.lastLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

type fakeSubscriptions struct {
	browse       []SubscriptionGame
	browseErr    error
	myGames      []SubscriptionGame
	myGamesErr   error
	followErr    error
	unfollowErr  error
	followedUser string
	followedGame string
	unfollowUser string
	unfollowGame string
	browseCalls  int
}

func (f *fakeSubscriptions) Browse(_ context.Context, _, _ string) ([]SubscriptionGame, error) {
	f.browseCalls++
	if f.browseErr != nil {
		return nil, f.browseErr
	}
	return f.browse, nil
}

func (f *fakeSubscriptions) MyGames(context.Context, string) ([]SubscriptionGame, error) {
	if f.myGamesErr != nil {
		return nil, f.myGamesErr
	}
	return f.myGames, nil
}

func (f *fakeSubscriptions) Follow(_ context.Context, userID, gameID string) error {
	f.followedUser, f.followedGame = userID, gameID
	return f.followErr
}

func (f *fakeSubscriptions) Unfollow(_ context.Context, userID, gameID string) error {
	f.unfollowUser, f.unfollowGame = userID, gameID
	return f.unfollowErr
}

type fakeMaterializer struct {
	result    Game
	err       error
	calls     int
	lastRefer ProviderReference
}

func (f *fakeMaterializer) Materialize(_ context.Context, reference ProviderReference) (Game, error) {
	f.calls++
	f.lastRefer = reference
	return f.result, f.err
}

func TestDiscoveryResolveNormalizesReferenceBeforeMaterializing(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1", Name: "Local", Slug: "local"}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{})
	got, err := service.Resolve(t.Context(), ProviderReference{Provider: " IGDB ", ExternalID: " 100 "})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != "local-1" {
		t.Fatalf("got = %#v", got)
	}
	want := ProviderReference{Provider: "igdb", ExternalID: "100"}
	if materializer.calls != 1 || materializer.lastRefer != want {
		t.Fatalf("calls=%d reference=%#v, want 1/%#v", materializer.calls, materializer.lastRefer, want)
	}
}

func TestDiscoveryResolveRejectsInvalidReference(t *testing.T) {
	materializer := &fakeMaterializer{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{})
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "steam", ExternalID: "100"}); !errors.Is(err, ErrInvalidProviderReference) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidProviderReference)
	}
	if materializer.calls != 0 {
		t.Fatalf("materializer.calls = %d, want 0", materializer.calls)
	}
}

func TestDiscoveryResolveWithoutMaterializerReportsNoProviderRecord(t *testing.T) {
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, &fakeSubscriptions{})
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
}

func TestDiscoveryResolvePropagatesMaterializationError(t *testing.T) {
	materializer := &fakeMaterializer{err: ErrProviderRecordNotFound}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{})
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
}

func TestDiscoveryBrowseFiltersHiddenTypes(t *testing.T) {
	repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
		{Reference: ProviderReference{Provider: "igdb", ExternalID: "1"}, Name: "Visible", GameType: "Main Game"},
		{Reference: ProviderReference{Provider: "igdb", ExternalID: "2"}, Name: "Hidden", GameType: "DLC Addon"},
	}}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
	got, err := service.Browse(t.Context(), "user-1", "game")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Visible" {
		t.Fatalf("got=%#v", got)
	}
}

func TestDiscoveryBrowseEmptySearchUsesLocalCatalogOnly(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	subs := &fakeSubscriptions{browse: []SubscriptionGame{{ID: "local-1", Name: "Local", Slug: "local"}}}
	service := NewDiscoveryService(repo, nil, subs)
	got, err := service.Browse(t.Context(), "user-1", "")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if repo.calls != 0 || len(got) != 1 || got[0].LocalGameID != "local-1" {
		t.Fatalf("repo.calls=%d got=%#v", repo.calls, got)
	}
}

func TestDiscoveryBrowseTrimsSearchBeforeDecidingLocalOnly(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	subs := &fakeSubscriptions{browse: []SubscriptionGame{{ID: "local-1", Name: "Local", Slug: "local"}}}
	service := NewDiscoveryService(repo, nil, subs)
	got, err := service.Browse(t.Context(), "user-1", "   ")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if repo.calls != 0 || len(got) != 1 {
		t.Fatalf("repo.calls=%d got=%#v", repo.calls, got)
	}
}

func TestDiscoveryBrowseKeepsMaterializedRegardlessOfType(t *testing.T) {
	repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
		{LocalGameID: "local-1", Reference: ProviderReference{Provider: "igdb", ExternalID: "9"}, Name: "Already local", GameType: "DLC Addon", Materialized: true},
	}}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
	got, err := service.Browse(t.Context(), "user-1", "game")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(got) != 1 || got[0].LocalGameID != "local-1" {
		t.Fatalf("got=%#v", got)
	}
}

func TestDiscoveryBrowsePassesBoundedLimit(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
	if _, err := service.Browse(t.Context(), "user-1", "wc3"); err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if repo.calls != 1 || repo.lastUser != "user-1" || repo.lastQuery != "wc3" || repo.lastLimit != 50 {
		t.Fatalf("calls=%d user=%q query=%q limit=%d", repo.calls, repo.lastUser, repo.lastQuery, repo.lastLimit)
	}
}

func TestDiscoveryBrowseRejectsOverLongSearch(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	subs := &fakeSubscriptions{}
	service := NewDiscoveryService(repo, nil, subs)
	overLong := strings.Repeat("a", maxSearchLength+1)
	_, err := service.Browse(t.Context(), "user-1", overLong)
	if !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidSearch)
	}
	if repo.calls != 0 || subs.browseCalls != 0 {
		t.Fatalf("repo.calls=%d subs.browseCalls=%d, want 0/0", repo.calls, subs.browseCalls)
	}
}

func TestDiscoveryBrowsePropagatesRepositoryError(t *testing.T) {
	failure := errors.New("search failed")
	repo := &fakeDiscoveryRepository{err: failure}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{})
	if _, err := service.Browse(t.Context(), "user-1", "wc3"); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
}

func TestDiscoveryMyGamesDelegatesToSubscriptions(t *testing.T) {
	subs := &fakeSubscriptions{myGames: []SubscriptionGame{{ID: "local-1", Name: "Local"}}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs)
	got, err := service.MyGames(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("MyGames: %v", err)
	}
	if len(got) != 1 || got[0].ID != "local-1" {
		t.Fatalf("got=%#v", got)
	}
}

func TestDiscoveryFollowLocalDelegatesToSubscriptions(t *testing.T) {
	subs := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs)
	if err := service.FollowLocal(t.Context(), "user-1", "local-1"); err != nil {
		t.Fatalf("FollowLocal: %v", err)
	}
	if subs.followedUser != "user-1" || subs.followedGame != "local-1" {
		t.Fatalf("followedUser=%q followedGame=%q", subs.followedUser, subs.followedGame)
	}
}

func TestDiscoveryFollowMaterializesThenSubscribes(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1", Name: "Game", Slug: "game"}}
	subscriptions := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
	got, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if got.ID != "local-1" || subscriptions.followedGame != "local-1" || subscriptions.followedUser != "user-1" {
		t.Fatalf("got=%#v subs=%#v", got, subscriptions)
	}
}

func TestDiscoveryFollowPropagatesResolveErrorWithoutSubscribing(t *testing.T) {
	materializer := &fakeMaterializer{err: ErrProviderRecordNotFound}
	subscriptions := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
	if _, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
	if subscriptions.followedGame != "" {
		t.Fatalf("followedGame = %q, want empty", subscriptions.followedGame)
	}
}

func TestDiscoveryFollowRejectsInvalidReferenceWithoutMaterializing(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1"}}
	subscriptions := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
	if _, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider: "steam", ExternalID: "100"}); !errors.Is(err, ErrInvalidProviderReference) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidProviderReference)
	}
	if materializer.calls != 0 || subscriptions.followedGame != "" {
		t.Fatalf("materializer.calls=%d followedGame=%q", materializer.calls, subscriptions.followedGame)
	}
}

func TestDiscoveryFollowPropagatesSubscriptionError(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1"}}
	failure := errors.New("follow failed")
	subscriptions := &fakeSubscriptions{followErr: failure}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions)
	if _, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
}

func TestDiscoveryBrowseWithProviderDisabledUsesLocalSearchOnlyEvenForNonEmptySearch(t *testing.T) {
	repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
		{Reference: ProviderReference{Provider: "igdb", ExternalID: "1"}, Name: "Stale Provider Result", GameType: "Main Game"},
	}}
	subs := &fakeSubscriptions{browse: []SubscriptionGame{{ID: "local-1", Name: "Local Only", Slug: "local-only"}}}
	service := NewDiscoveryService(repo, nil, subs)
	service.DisableProvider()

	got, err := service.Browse(t.Context(), "user-1", "game")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("repo.calls = %d, want 0: a disabled provider must never be queried", repo.calls)
	}
	if len(got) != 1 || got[0].LocalGameID != "local-1" || got[0].Reference != (ProviderReference{}) {
		t.Fatalf("got = %#v", got)
	}
}

func TestDiscoveryResolveWithProviderDisabledReportsNoProviderRecordWithoutMaterializing(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1", Name: "Game", Slug: "game"}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{})
	service.DisableProvider()

	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
	if materializer.calls != 0 {
		t.Fatalf("materializer.calls = %d, want 0: disabled provider must never materialize stale generations", materializer.calls)
	}
}

func TestDiscoveryBrowseEmptySearchKeepsLocalEnrichment(t *testing.T) {
	release := time.Date(2002, 7, 3, 0, 0, 0, 0, time.UTC)
	subs := &fakeSubscriptions{browse: []SubscriptionGame{{
		ID: "local-1",
		GamePresentation: GamePresentation{
			Name: "Warcraft III", Slug: "warcraft-iii",
			CoverImageID: "co1abc", GameType: "Main Game",
			PlatformNames:    []string{"PC (Microsoft Windows)"},
			FirstReleaseDate: &release,
		},
	}}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs)

	got, err := service.Browse(t.Context(), "user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("items = %#v", got)
	}
	if got[0].CoverImageID != "co1abc" || got[0].GameType != "Main Game" {
		t.Fatalf("item = %#v", got[0])
	}
}

func TestDiscoveryUnfollowDelegatesToSubscriptions(t *testing.T) {
	subs := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs)
	if err := service.Unfollow(t.Context(), "user-1", "local-1"); err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if subs.unfollowUser != "user-1" || subs.unfollowGame != "local-1" {
		t.Fatalf("unfollowUser=%q unfollowGame=%q", subs.unfollowUser, subs.unfollowGame)
	}
}
