package game

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeDiscoveryRepository struct {
	result              DiscoverySearchResult
	items               []DiscoveryItem
	err                 error
	calls               int
	lastUser            string
	lastQuery           string
	lastLimit           int
	lastIncludeProvider bool

	findLocalItem     DiscoveryItem
	findLocalErr      error
	findLocalCalls    int
	lastFindLocalUser string
	lastFindLocalID   string

	findProviderItem     DiscoveryItem
	findProviderErr      error
	findProviderCalls    int
	lastFindProviderUser string
	lastFindProviderRef  ProviderReference
}

func (f *fakeDiscoveryRepository) Search(_ context.Context, userID, search string, limit int, includeProvider bool) (DiscoverySearchResult, error) {
	f.calls++
	f.lastUser = userID
	f.lastQuery = search
	f.lastLimit = limit
	f.lastIncludeProvider = includeProvider
	if f.err != nil {
		return DiscoverySearchResult{}, f.err
	}
	if f.items != nil {
		return DiscoverySearchResult{Items: f.items, HasMore: f.result.HasMore}, nil
	}
	return f.result, nil
}

func (f *fakeDiscoveryRepository) FindLocal(_ context.Context, userID, localGameID string) (DiscoveryItem, error) {
	f.findLocalCalls++
	f.lastFindLocalUser = userID
	f.lastFindLocalID = localGameID
	return f.findLocalItem, f.findLocalErr
}

func (f *fakeDiscoveryRepository) FindProvider(_ context.Context, userID string, reference ProviderReference) (DiscoveryItem, error) {
	f.findProviderCalls++
	f.lastFindProviderUser = userID
	f.lastFindProviderRef = reference
	return f.findProviderItem, f.findProviderErr
}

type fakeProviderStatus struct {
	status ProviderStatus
	err    error
	calls  int
}

func (f *fakeProviderStatus) Status(context.Context) (ProviderStatus, error) {
	f.calls++
	return f.status, f.err
}

func enabledProviderStatus() *fakeProviderStatus {
	return &fakeProviderStatus{status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true}}
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

func (f *fakeSubscriptions) ListFollowers(context.Context, string) ([]string, error) {
	return nil, nil
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{}, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{}, enabledProviderStatus())
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "steam", ExternalID: "100"}); !errors.Is(err, ErrInvalidProviderReference) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidProviderReference)
	}
	if materializer.calls != 0 {
		t.Fatalf("materializer.calls = %d, want 0", materializer.calls)
	}
}

func TestDiscoveryResolveWithoutMaterializerReportsNoProviderRecord(t *testing.T) {
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, &fakeSubscriptions{}, enabledProviderStatus())
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
}

func TestDiscoveryResolvePropagatesMaterializationError(t *testing.T) {
	materializer := &fakeMaterializer{err: ErrProviderRecordNotFound}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{}, enabledProviderStatus())
	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
}

func TestDiscoveryResolveRejectsWhenProviderDisabled(t *testing.T) {
	materializer := &fakeMaterializer{result: Game{ID: "local-1"}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, &fakeSubscriptions{}, &fakeProviderStatus{
		status: ProviderStatus{State: ProviderDisabled},
	})

	if _, err := service.Resolve(t.Context(), ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
	if materializer.calls != 0 {
		t.Fatalf("materializer.calls = %d, want 0", materializer.calls)
	}
}

func TestDiscoveryBrowseEmptySearchReturnsLANListWithoutProviderSearch(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	status := &fakeProviderStatus{status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true}}
	subs := &fakeSubscriptions{browse: []SubscriptionGame{{ID: "local-1", GamePresentation: GamePresentation{Name: "Local"}}}}
	service := NewDiscoveryService(repo, nil, subs, status)

	page, err := service.Browse(t.Context(), "user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 0 || len(page.Items) != 1 || page.SearchTooShort {
		t.Fatalf("page=%#v repo.calls=%d", page, repo.calls)
	}
}

func TestDiscoveryBrowseOneCharacterDoesNotSearchProvider(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
		status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true},
	})

	page, err := service.Browse(t.Context(), "user-1", " c ")
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 0 || !page.SearchTooShort {
		t.Fatalf("page=%#v calls=%d", page, repo.calls)
	}
}

func TestDiscoveryBrowsePassesProviderEnabledAndFiftyResultLimit(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
		status: ProviderStatus{State: ProviderCurrent, CatalogAvailable: true},
	})

	_, err := service.Browse(t.Context(), "user-1", "wc")
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastLimit != 50 || !repo.lastIncludeProvider {
		t.Fatalf("limit=%d includeProvider=%v", repo.lastLimit, repo.lastIncludeProvider)
	}
}

func TestDiscoveryBrowseDisabledProviderStillUsesUnifiedLocalSearch(t *testing.T) {
	repo := &fakeDiscoveryRepository{items: []DiscoveryItem{
		{LocalGameID: "local-1", GamePresentation: GamePresentation{Name: "Local"}, Materialized: true},
	}}
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, &fakeProviderStatus{
		status: ProviderStatus{State: ProviderDisabled},
	})

	page, err := service.Browse(t.Context(), "user-1", "wc")
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.lastIncludeProvider {
		t.Fatalf("calls=%d includeProvider=%v, want 1/false: a disabled provider still searches through the unified query, gated to local-sourced rows only, never through provider rows", repo.calls, repo.lastIncludeProvider)
	}
	if len(page.Items) != 1 || !page.Items[0].Materialized {
		t.Fatalf("page=%#v, want only the local-sourced item the fake returned", page)
	}
}

// TestDiscoveryBrowseReturnsProviderStatusUnchanged proves DiscoveryPage
// carries whatever ProviderStatusSource reports, verbatim, on both the
// short-circuited local-only path and the repository-backed path, so a
// player-facing "why is this all I see" affordance (Task 4) can render it
// without DiscoveryService reinterpreting or dropping fields.
func TestDiscoveryBrowseReturnsProviderStatusUnchanged(t *testing.T) {
	status := ProviderStatus{State: ProviderStale, RecordCount: 42, CatalogAvailable: true}
	statusSource := &fakeProviderStatus{status: status}

	t.Run("empty search", func(t *testing.T) {
		service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, &fakeSubscriptions{}, statusSource)
		page, err := service.Browse(t.Context(), "user-1", "")
		if err != nil {
			t.Fatal(err)
		}
		if page.ProviderStatus != status {
			t.Fatalf("ProviderStatus = %#v, want %#v", page.ProviderStatus, status)
		}
	})

	t.Run("non-empty search", func(t *testing.T) {
		service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, &fakeSubscriptions{}, statusSource)
		page, err := service.Browse(t.Context(), "user-1", "wc3")
		if err != nil {
			t.Fatal(err)
		}
		if page.ProviderStatus != status {
			t.Fatalf("ProviderStatus = %#v, want %#v", page.ProviderStatus, status)
		}
	})
}

func TestDiscoveryBrowseRejectsOverLongSearch(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	subs := &fakeSubscriptions{}
	service := NewDiscoveryService(repo, nil, subs, enabledProviderStatus())
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
	service := NewDiscoveryService(repo, nil, &fakeSubscriptions{}, enabledProviderStatus())
	if _, err := service.Browse(t.Context(), "user-1", "wc3"); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
}

func TestDiscoveryMyGamesDelegatesToSubscriptions(t *testing.T) {
	subs := &fakeSubscriptions{myGames: []SubscriptionGame{{ID: "local-1", GamePresentation: GamePresentation{Name: "Local"}}}}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions, enabledProviderStatus())
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, materializer, subscriptions, enabledProviderStatus())
	if _, err := service.Follow(t.Context(), "user-1", ProviderReference{Provider: "igdb", ExternalID: "100"}); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
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
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs, enabledProviderStatus())

	page, err := service.Browse(t.Context(), "user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %#v", page.Items)
	}
	if page.Items[0].CoverImageID != "co1abc" || page.Items[0].GameType != "Main Game" {
		t.Fatalf("item = %#v", page.Items[0])
	}
}

func TestDiscoveryUnfollowDelegatesToSubscriptions(t *testing.T) {
	subs := &fakeSubscriptions{}
	service := NewDiscoveryService(&fakeDiscoveryRepository{}, nil, subs, enabledProviderStatus())
	if err := service.Unfollow(t.Context(), "user-1", "local-1"); err != nil {
		t.Fatalf("Unfollow: %v", err)
	}
	if subs.unfollowUser != "user-1" || subs.unfollowGame != "local-1" {
		t.Fatalf("unfollowUser=%q unfollowGame=%q", subs.unfollowUser, subs.unfollowGame)
	}
}

// TestDiscoverySelectLocalIsReadOnly proves a LocalGameID-only selection
// reads through DiscoveryRepository.FindLocal and never touches the
// materializer: Select is a read-only composer lookup, not a creation path.
func TestDiscoverySelectLocalIsReadOnly(t *testing.T) {
	repo := &fakeDiscoveryRepository{
		findLocalItem: DiscoveryItem{LocalGameID: "local-1", GamePresentation: GamePresentation{Name: "Local"}, Materialized: true},
	}
	materializer := &fakeMaterializer{}
	service := NewDiscoveryService(repo, materializer, &fakeSubscriptions{}, enabledProviderStatus())

	got, err := service.Select(t.Context(), "user-1", GameSelection{LocalGameID: "local-1"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.LocalGameID != "local-1" {
		t.Fatalf("got = %#v", got)
	}
	if repo.findLocalCalls != 1 || repo.lastFindLocalUser != "user-1" || repo.lastFindLocalID != "local-1" {
		t.Fatalf("findLocalCalls=%d user=%q id=%q, want 1/user-1/local-1", repo.findLocalCalls, repo.lastFindLocalUser, repo.lastFindLocalID)
	}
	if repo.findProviderCalls != 0 || materializer.calls != 0 {
		t.Fatalf("findProviderCalls=%d materializer.calls=%d, want 0/0", repo.findProviderCalls, materializer.calls)
	}
}

// TestDiscoverySelectProviderIsReadOnly proves a provider-reference-only
// selection normalizes the reference and reads through
// DiscoveryRepository.FindProvider without ever calling the materializer.
func TestDiscoverySelectProviderIsReadOnly(t *testing.T) {
	repo := &fakeDiscoveryRepository{
		findProviderItem: DiscoveryItem{
			Reference:        ProviderReference{Provider: "igdb", ExternalID: "100"},
			GamePresentation: GamePresentation{Name: "Provider Game"},
		},
	}
	materializer := &fakeMaterializer{}
	service := NewDiscoveryService(repo, materializer, &fakeSubscriptions{}, enabledProviderStatus())

	got, err := service.Select(t.Context(), "user-1", GameSelection{Reference: ProviderReference{Provider: " IGDB ", ExternalID: " 100 "}})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Reference.ExternalID != "100" {
		t.Fatalf("got = %#v", got)
	}
	want := ProviderReference{Provider: "igdb", ExternalID: "100"}
	if repo.findProviderCalls != 1 || repo.lastFindProviderUser != "user-1" || repo.lastFindProviderRef != want {
		t.Fatalf("findProviderCalls=%d user=%q ref=%#v, want 1/user-1/%#v", repo.findProviderCalls, repo.lastFindProviderUser, repo.lastFindProviderRef, want)
	}
	if repo.findLocalCalls != 0 || materializer.calls != 0 {
		t.Fatalf("findLocalCalls=%d materializer.calls=%d, want 0/0", repo.findLocalCalls, materializer.calls)
	}
}

// TestDiscoverySelectRejectsMissingOrAmbiguousIdentity proves Select
// validates exactly one identity source before touching either repository
// method or the materializer: neither LocalGameID nor Reference set, and
// both set, are equally invalid.
func TestDiscoverySelectRejectsMissingOrAmbiguousIdentity(t *testing.T) {
	tests := []GameSelection{
		{},
		{LocalGameID: "local-1", Reference: ProviderReference{Provider: "igdb", ExternalID: "100"}},
	}
	for _, selection := range tests {
		repo := &fakeDiscoveryRepository{}
		materializer := &fakeMaterializer{}
		service := NewDiscoveryService(repo, materializer, &fakeSubscriptions{}, enabledProviderStatus())

		if _, err := service.Select(t.Context(), "user-1", selection); !errors.Is(err, ErrInvalidGameSelection) {
			t.Fatalf("Select(%#v): err = %v, want %v", selection, err, ErrInvalidGameSelection)
		}
		if repo.findLocalCalls != 0 || repo.findProviderCalls != 0 || materializer.calls != 0 {
			t.Fatalf("Select(%#v): findLocalCalls=%d findProviderCalls=%d materializer.calls=%d, want 0/0/0",
				selection, repo.findLocalCalls, repo.findProviderCalls, materializer.calls)
		}
	}
}

// TestDiscoverySelectProviderRejectsDisabledProvider proves a disabled
// provider rejects a provider-reference selection with
// ErrProviderRecordNotFound before ever calling FindProvider, matching
// Resolve's existing disabled-provider gate.
func TestDiscoverySelectProviderRejectsDisabledProvider(t *testing.T) {
	repo := &fakeDiscoveryRepository{}
	materializer := &fakeMaterializer{}
	service := NewDiscoveryService(repo, materializer, &fakeSubscriptions{}, &fakeProviderStatus{
		status: ProviderStatus{State: ProviderDisabled},
	})

	if _, err := service.Select(t.Context(), "user-1", GameSelection{Reference: ProviderReference{Provider: "igdb", ExternalID: "100"}}); !errors.Is(err, ErrProviderRecordNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrProviderRecordNotFound)
	}
	if repo.findProviderCalls != 0 {
		t.Fatalf("findProviderCalls = %d, want 0", repo.findProviderCalls)
	}
}
