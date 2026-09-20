package game

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"
)

// discoveryProviderRowLimit bounds a single non-empty search's page size. It
// keeps discovery pages small and predictable rather than dumping the whole
// synchronized IGDB corpus into a page. DiscoveryRepository.Search is asked
// for one row beyond this limit so DiscoveryPage.HasMore can be set without
// a separate count query.
const discoveryProviderRowLimit = 50

// DiscoveryPage is one page of Browse results: the player-visible, already
// ranked and bounded cards; whether a row past this page exists; whether the
// search itself was too short to reach the repository at all; and the
// provider status a player-facing affordance can render from (Task 4).
type DiscoveryPage struct {
	Items          []DiscoveryItem
	HasMore        bool
	SearchTooShort bool
	ProviderStatus ProviderStatus
}

// DiscoveryService combines the authoritative local Game catalog with the
// active synchronized IGDB provider index into one player-facing read model.
// It never calls IGDB directly: all provider data comes from PostgreSQL,
// synchronized in the background by the IGDB sync service.
type DiscoveryService struct {
	discovery      DiscoveryRepository
	materializer   MaterializationRepository
	subscriptions  SubscriptionRepository
	providerStatus ProviderStatusSource
}

// NewDiscoveryService wires the discovery read model. The composition root
// always wires a real materializer; Browse, MyGames, FollowLocal, and
// Unfollow never use it regardless. providerStatus is queried fresh on every
// Browse/Resolve call rather than cached on the service, so a provider
// disabled or re-enabled between requests -- or a background sync
// completing -- is reflected immediately.
func NewDiscoveryService(discovery DiscoveryRepository, materializer MaterializationRepository, subscriptions SubscriptionRepository, providerStatus ProviderStatusSource) *DiscoveryService {
	return &DiscoveryService{discovery: discovery, materializer: materializer, subscriptions: subscriptions, providerStatus: providerStatus}
}

// Browse returns local catalog entries for an empty or single-character
// search (SearchTooShort is set for the latter case, spec: searches shorter
// than two characters never reach the discovery repository), preserving the
// existing local-catalog browsing experience. A longer search always goes
// through the unified, ranked, bounded DiscoveryRepository.Search -- even
// while the provider is disabled, which simply passes includeProvider=false
// so the same ranked query returns local-sourced rows only, rather than
// falling back to a separate local-only code path. search is bounded by the
// same validateSearch rule the pre-existing local browse used, on every
// path: an over-length value produces ErrInvalidSearch without calling
// either repository.
func (s *DiscoveryService) Browse(ctx context.Context, userID, search string) (DiscoveryPage, error) {
	status, err := s.providerStatus.Status(ctx)
	if err != nil {
		return DiscoveryPage{}, err
	}

	search, err = validateSearch(search)
	if err != nil {
		return DiscoveryPage{}, err
	}

	if search == "" || utf8.RuneCountInString(search) == 1 {
		local, err := s.subscriptions.Browse(ctx, userID, "")
		if err != nil {
			return DiscoveryPage{}, err
		}
		items := make([]DiscoveryItem, 0, len(local))
		for _, item := range local {
			items = append(items, discoveryItemFromLocal(item))
		}
		return DiscoveryPage{
			Items:          items,
			SearchTooShort: search != "",
			ProviderStatus: status,
		}, nil
	}

	includeProvider := status.State != ProviderDisabled
	result, err := s.discovery.Search(ctx, userID, search, discoveryProviderRowLimit, includeProvider)
	if err != nil {
		return DiscoveryPage{}, err
	}
	return DiscoveryPage{
		Items:          result.Items,
		HasMore:        result.HasMore,
		ProviderStatus: status,
	}, nil
}

// BrowseFollowed is Browse narrowed to the Games this viewer already
// follows. It backs the merged Games screen's "Followed only" filter rather
// than a second catalog: the same ranked, bounded Browse result with
// everything unfollowed removed, so search and the filter compose instead of
// living on separate screens. An unmaterialized provider result is never
// followed, so one can never appear here. HasMore keeps Browse's meaning --
// matches exist past this page -- which the filter narrows but does not
// change.
func (s *DiscoveryService) BrowseFollowed(ctx context.Context, userID, search string) (DiscoveryPage, error) {
	page, err := s.Browse(ctx, userID, search)
	if err != nil {
		return DiscoveryPage{}, err
	}
	page.Items = slices.DeleteFunc(page.Items, func(item DiscoveryItem) bool { return !item.Subscribed })
	return page, nil
}

// Resolve turns an opaque provider reference into the one authoritative
// local Game for it, materializing that Game on first use. Only the provider
// and external ID cross this boundary: the materializer reloads the provider
// row from the active index itself, so browser-supplied names, slugs, or
// other provider metadata can never reach the local catalog. Concurrent
// Resolve calls for the same reference converge on a single local Game. A
// disabled provider rejects with ErrProviderRecordNotFound without ever
// calling the materializer, even for a generation that synchronized
// successfully before the provider was disabled: a configured-but-not-yet-
// synced provider (no active generation) is a different case and continues
// through to materialization, which reports ErrProviderRecordNotFound itself
// once it finds no active provider row.
func (s *DiscoveryService) Resolve(ctx context.Context, ref ProviderReference) (Game, error) {
	normalized, err := NewProviderReference(ref.Provider, ref.ExternalID)
	if err != nil {
		return Game{}, err
	}
	status, err := s.providerStatus.Status(ctx)
	if err != nil {
		return Game{}, err
	}
	if status.State == ProviderDisabled || s.materializer == nil {
		return Game{}, ErrProviderRecordNotFound
	}
	return s.materializer.Materialize(ctx, normalized)
}

// Select is a read-only composer lookup (Task 5) for one already-presented
// Game: it never materializes, regardless of which identity the selection
// carries -- unlike Follow/Resolve, it never calls the materializer, so a
// player can review an unmaterialized provider choice (e.g. in a Game
// Request composer) without that alone creating a local Game. Exactly one
// of GameSelection.LocalGameID or GameSelection.Reference must be set;
// either both set or neither set is ErrInvalidGameSelection before either
// repository method or the provider status is even consulted for the local
// case.
//
// A local selection reads straight through DiscoveryRepository.FindLocal. A
// provider selection normalizes the reference, rejects a disabled provider
// exactly like Resolve does (ErrProviderRecordNotFound, without ever
// calling FindProvider), and otherwise reads through
// DiscoveryRepository.FindProvider.
func (s *DiscoveryService) Select(ctx context.Context, userID string, selection GameSelection) (DiscoveryItem, error) {
	hasLocal := strings.TrimSpace(selection.LocalGameID) != ""
	hasProvider := selection.Reference != (ProviderReference{})
	if hasLocal == hasProvider {
		return DiscoveryItem{}, ErrInvalidGameSelection
	}
	if hasLocal {
		return s.discovery.FindLocal(ctx, userID, selection.LocalGameID)
	}

	normalized, err := NewProviderReference(selection.Reference.Provider, selection.Reference.ExternalID)
	if err != nil {
		return DiscoveryItem{}, err
	}
	status, err := s.providerStatus.Status(ctx)
	if err != nil {
		return DiscoveryItem{}, err
	}
	if status.State == ProviderDisabled {
		return DiscoveryItem{}, ErrProviderRecordNotFound
	}
	return s.discovery.FindProvider(ctx, userID, normalized)
}

// Follow materializes the authoritative local Game for an IGDB discovery
// result (if it is not already materialized) and then follows it, so a
// player can follow a provider search result without the browser ever
// needing to know a local Game ID (spec: "Follow and Game Request
// integration"). It orchestrates Resolve and the subscription follow inside
// the Game capability so this stays a single explicit call for callers.
func (s *DiscoveryService) Follow(ctx context.Context, userID string, ref ProviderReference) (Game, error) {
	item, err := s.Resolve(ctx, ref)
	if err != nil {
		return Game{}, err
	}
	if err := s.subscriptions.Follow(ctx, userID, item.ID); err != nil {
		return Game{}, err
	}
	return item, nil
}

// MyGames returns the current user's followed local Games.
func (s *DiscoveryService) MyGames(ctx context.Context, userID string) ([]SubscriptionGame, error) {
	return s.subscriptions.MyGames(ctx, userID)
}

// CountCatalog and CountFollowed state the navigation's Games/My Games
// totals. CountCatalog covers the authoritative local catalog only: the
// synchronized provider index is reachable through search, but it is not a
// catalog this installation owns or can meaningfully total.
func (s *DiscoveryService) CountCatalog(ctx context.Context) (int64, error) {
	return s.subscriptions.CountCatalog(ctx)
}

func (s *DiscoveryService) CountFollowed(ctx context.Context, userID string) (int64, error) {
	return s.subscriptions.CountFollowed(ctx, userID)
}

// FollowLocal follows an already-authoritative local Game by ID. Following
// an unmaterialized provider result goes through Follow instead, which
// materializes the Game first.
func (s *DiscoveryService) FollowLocal(ctx context.Context, userID, gameID string) error {
	return s.subscriptions.Follow(ctx, userID, gameID)
}

// Unfollow removes the current user's follow of a local Game.
func (s *DiscoveryService) Unfollow(ctx context.Context, userID, gameID string) error {
	return s.subscriptions.Unfollow(ctx, userID, gameID)
}

func discoveryItemFromLocal(local SubscriptionGame) DiscoveryItem {
	return DiscoveryItem{
		LocalGameID:      local.ID,
		GamePresentation: local.GamePresentation,
		Subscribed:       local.Subscribed,
		Materialized:     true,
	}
}
