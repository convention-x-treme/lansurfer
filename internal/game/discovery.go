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
	discovery       DiscoveryRepository
	materializer    MaterializationRepository
	subscriptions   SubscriptionRepository
	providerEnabled bool
}

// NewDiscoveryService wires the discovery read model. The composition root
// always wires a real materializer; Browse, MyGames, FollowLocal, and
// Unfollow never use it regardless. IGDB-backed search and materialization
// start enabled; the composition root calls DisableProvider when IGDB
// credentials are not configured.
func NewDiscoveryService(discovery DiscoveryRepository, materializer MaterializationRepository, subscriptions SubscriptionRepository) *DiscoveryService {
	return &DiscoveryService{discovery: discovery, materializer: materializer, subscriptions: subscriptions, providerEnabled: true}
}

// DisableProvider turns off IGDB-backed search and materialization for the
// rest of this service's lifetime. A non-empty Browse then falls back to the
// local-catalog-only search that Browse already uses for an empty query, and
// Resolve (and therefore Follow) reports ErrProviderRecordNotFound instead
// of materializing a Game from provider data. This matters even after IGDB
// was previously enabled and successfully synchronized: the active
// generation persisted in PostgreSQL by that earlier synchronization must
// stop being player-visible/materializable the moment the provider is
// disabled, not just stop refreshing (spec: "disabled IGDB preserves
// current local search behavior").
func (s *DiscoveryService) DisableProvider() {
	s.providerEnabled = false
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
	if search == "" || !s.providerEnabled {
		local, err := s.subscriptions.Browse(ctx, userID, search)
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

// Resolve turns an opaque provider reference into the one authoritative
// local Game for it, materializing that Game on first use. Only the provider
// and external ID cross this boundary: the materializer reloads the provider
// row from the active index itself, so browser-supplied names, slugs, or
// other provider metadata can never reach the local catalog. Concurrent
// Resolve calls for the same reference converge on a single local Game.
func (s *DiscoveryService) Resolve(ctx context.Context, ref ProviderReference) (Game, error) {
	normalized, err := NewProviderReference(ref.Provider, ref.ExternalID)
	if err != nil {
		return Game{}, err
	}
	if !s.providerEnabled || s.materializer == nil {
		return Game{}, ErrProviderRecordNotFound
	}
	return s.materializer.Materialize(ctx, normalized)
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
		LocalGameID:  local.ID,
		Name:         local.Name,
		Slug:         local.Slug,
		Subscribed:   local.Subscribed,
		Materialized: true,
	}
}
