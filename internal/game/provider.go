package game

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidProviderReference = errors.New("invalid provider reference")
var ErrProviderRecordNotFound = errors.New("provider record not found")

type ProviderReference struct {
	Provider   string
	ExternalID string
}

type ProviderGame struct {
	Reference         ProviderReference
	Name              string
	Slug              string
	GameType          string
	FirstReleaseDate  *time.Time
	ParentExternalID  string
	VersionParentID   string
	PlatformNames     []string
	AlternativeNames  []string
	CoverImageID      string
	ProviderUpdatedAt *time.Time
	Checksum          string
}

type ProviderState string

const (
	ProviderDisabled  ProviderState = "disabled"
	ProviderNotSynced ProviderState = "not synced"
	ProviderSyncing   ProviderState = "syncing"
	ProviderCurrent   ProviderState = "current"
	ProviderStale     ProviderState = "stale"
	ProviderFailed    ProviderState = "failed"
)

type ProviderStatus struct {
	State       ProviderState
	LastAttempt *time.Time
	LastSuccess *time.Time
	RecordCount int64
	LastError   string
}

type ProviderCatalog interface {
	Games(context.Context, func([]ProviderGame) error) error
}

type ProviderIndexRepository interface {
	BeginGeneration(context.Context) (string, error)
	StoreProviderGames(context.Context, string, []ProviderGame) error
	ActivateGeneration(context.Context, string, int64) error
	FailGeneration(context.Context, string, string) error
	Status(context.Context, time.Time) (ProviderStatus, error)
}

type DiscoveryItem struct {
	LocalGameID string
	Reference   ProviderReference
	GamePresentation
	Subscribed   bool
	Materialized bool
}

type DiscoveryRepository interface {
	Search(context.Context, string, string, int) ([]DiscoveryItem, error)
}

type MaterializationRepository interface {
	Materialize(context.Context, ProviderReference) (Game, error)
}

func NewProviderReference(provider, externalID string) (ProviderReference, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	externalID = strings.TrimSpace(externalID)
	if provider != "igdb" || externalID == "" {
		return ProviderReference{}, ErrInvalidProviderReference
	}
	for _, char := range externalID {
		if char < '0' || char > '9' {
			return ProviderReference{}, ErrInvalidProviderReference
		}
	}
	value, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil || value <= 0 {
		return ProviderReference{}, ErrInvalidProviderReference
	}
	// Canonicalize to the decimal form the provider index itself stores
	// (igdb_id::text), so a padded value such as "0100" can never become a
	// second identity for provider record 100. The external ID is the key for
	// the game_external_ids mapping and for the deterministic materialization
	// slug, and both must agree with the numeric provider key. Canonicalizing
	// also bounds the string to at most 19 digits.
	return ProviderReference{Provider: provider, ExternalID: strconv.FormatInt(value, 10)}, nil
}

// visibleProviderGameTypes is the single source of truth for the
// player-visible Game Type policy (spec: "Player-visible Game Type
// policy"). Values are normalized (trimmed, lowercase) IGDB semantic
// game_type.type strings. Unknown or newly introduced types default to
// hidden until explicitly reviewed here.
var visibleProviderGameTypes = []string{
	"main game", "standalone expansion", "remake", "remaster", "port", "expanded game",
}

func VisibleProviderGameType(gameType string) bool {
	gameType = strings.ToLower(strings.TrimSpace(gameType))
	for _, visible := range visibleProviderGameTypes {
		if gameType == visible {
			return true
		}
	}
	return false
}

// VisibleProviderGameTypes returns the normalized (lowercase) Game Type
// values considered player-visible, for read-model queries that must filter
// by type before paging/limiting provider rows (rather than after, which
// could let a page fill entirely with hidden-type matches). The returned
// slice is a copy; callers may not mutate the canonical policy through it.
func VisibleProviderGameTypes() []string {
	return append([]string(nil), visibleProviderGameTypes...)
}
