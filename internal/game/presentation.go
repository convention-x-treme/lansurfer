package game

import "time"

// GamePresentation is the reusable player-facing presentation of one local
// Game: local name/slug plus the last-known-good IGDB enrichment snapshot
// (cover image, game type, platform names, first release date) persisted in
// game_igdb_enrichment. A local Game never materialized via IGDB has a zero
// GamePresentation for every enrichment field: local name/slug still apply,
// nothing else does. It carries no provider identity; that stays on the
// outer discovery/local types that embed it.
type GamePresentation struct {
	Name             string
	Slug             string
	GameType         string
	PlatformNames    []string
	FirstReleaseDate *time.Time
	CoverImageID     string
}
