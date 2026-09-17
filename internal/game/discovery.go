package game

import (
	"context"
)

// discoveryProviderRowLimit bounds provider-only rows returned by a single
// non-empty search. It keeps discovery pages small and predictable rather
// than dumping the whole synchronized IGDB corpus into a page.
const discoveryProviderRowLimit = 50

// DiscoveryService combines the authoritative local Game catalog with the
// active synchronized IGDB provider index into one player-facing read model.
// It never calls IGDB directly: all provider data comes from PostgreSQL,
// synchronized in the background by the IGDB sync service.
type DiscoveryService struct {
	discovery     DiscoveryRepository
	materializer  MaterializationRepository
	subscriptions SubscriptionRepository
}

// NewDiscoveryService wires the discovery read model. materializer may be
// nil until an operation that requires it (materializing an unmaterialized
// provider result) is implemented; Browse, MyGames, FollowLocal, and
// Unfollow never use it.
func NewDiscoveryService(discovery DiscoveryRepository, materializer MaterializationRepository, subscriptions SubscriptionRepository) *DiscoveryService {
	return &DiscoveryService{discovery: discovery, materializer: materializer, subscriptions: subscriptions}
}

// Browse returns local catalog entries for an empty search, preserving the
// existing local-catalog experience. A non-empty search additionally reads
// the active IGDB generation, bounded to discoveryProviderRowLimit rows and
// filtered to player-visible Game Types. A provider row already
// materialized into a local Game is returned once, as that authoritative
// local Game, never duplicated as a separate provider card. search is
// bounded by the same validateSearch rule the pre-existing local browse
// used, on both the empty and non-empty path: an over-length value produces
// ErrInvalidSearch without calling either repository.
func (s *DiscoveryService) Browse(ctx context.Context, userID, search string) ([]DiscoveryItem, error) {
	search, err := validateSearch(search)
	if err != nil {
		return nil, err
	}
	if search == "" {
		local, err := s.subscriptions.Browse(ctx, userID, "")
		if err != nil {
			return nil, err
		}
		items := make([]DiscoveryItem, 0, len(local))
		for _, game := range local {
			items = append(items, discoveryItemFromLocal(game))
		}
		return items, nil
	}

	items, err := s.discovery.Search(ctx, userID, search, discoveryProviderRowLimit)
	if err != nil {
		return nil, err
	}
	result := make([]DiscoveryItem, 0, len(items))
	for _, item := range items {
		if !item.Materialized && !VisibleProviderGameType(item.GameType) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// MyGames returns the current user's followed local Games.
func (s *DiscoveryService) MyGames(ctx context.Context, userID string) ([]SubscriptionGame, error) {
	return s.subscriptions.MyGames(ctx, userID)
}

// FollowLocal follows an already-authoritative local Game by ID. Following
// an unmaterialized provider result is not implemented here; it requires
// materialization first, which lands in a later task.
func (s *DiscoveryService) FollowLocal(ctx context.Context, userID, gameID string) error {
	return s.subscriptions.Follow(ctx, userID, gameID)
}

// Unfollow removes the current user's follow of a local Game.
func (s *DiscoveryService) Unfollow(ctx context.Context, userID, gameID string) error {
	return s.subscriptions.Unfollow(ctx, userID, gameID)
}

func discoveryItemFromLocal(local SubscriptionGame) DiscoveryItem {
	return DiscoveryItem{
		LocalGameID:  local.ID,
		Name:         local.Name,
		Slug:         local.Slug,
		Subscribed:   local.Subscribed,
		Materialized: true,
	}
}
