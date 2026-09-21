package game

import (
	"errors"
	"time"
)

// ErrInvalidGameSelection reports a GameSelection that identifies zero or
// more than one Game: DiscoveryService.Select requires exactly one of
// LocalGameID or Reference to be set.
var ErrInvalidGameSelection = errors.New("invalid game selection")

// GameSelection identifies exactly one Game -- either an already-
// authoritative local Game by ID, or an IGDB provider record that may or
// may not be materialized yet -- for a read-only composer lookup (Task 5:
// DiscoveryService.Select). Exactly one of LocalGameID or Reference must be
// set; the zero value of both, or both set together, is invalid.
type GameSelection struct {
	LocalGameID string
	Reference   ProviderReference
}

// GamePresentation is the reusable player-facing presentation of one local
// Game: local name/slug plus the last-known-good IGDB enrichment snapshot
// (cover image, game type, platform names, first release date, summary,
// genre names) persisted in game_igdb_enrichment. A local Game never
// materialized via IGDB has a zero GamePresentation for every enrichment
// field: local name/slug still apply, nothing else does. It carries no
// provider identity; that stays on the outer discovery/local types that
// embed it.
type GamePresentation struct {
	Name             string
	Slug             string
	GameType         string
	PlatformNames    []string
	FirstReleaseDate *time.Time
	CoverImageID     string
	Summary          string
	GenreNames       []string
}
