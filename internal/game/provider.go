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
	Summary           string
	GenreNames        []string
	GameModeNames     []string
	MultiplayerModes  []MultiplayerMode
}

// MultiplayerMode is one platform-specific multiplayer capability record
// (issue #46). Player limits are nil when IGDB does not report them: nil is
// "not reported" and is deliberately distinct from a zero-player limit,
// which IGDB never sends (0 is its unset representation).
type MultiplayerMode struct {
	PlatformIGDBID    int64
	PlatformName      string
	CampaignCoop      bool
	DropIn            bool
	LanCoop           bool
	OfflineCoop       bool
	OnlineCoop        bool
	SplitScreen       bool
	SplitScreenOnline bool
	OfflineCoopMax    *int
	OfflineMax        *int
	OnlineCoopMax     *int
	OnlineMax         *int
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
	// RecordCount is the record count of the last successfully activated
	// baseline generation, not of whatever sync may be running right now.
	RecordCount int64
	// StagedRecords is how many provider records have already been written
	// into the in-progress generation. It is 0 whenever no generation is
	// still being staged, so the admin status page renders it only while
	// State reports ProviderSyncing; it is the signal that tells a running
	// full sync apart from a hung one.
	StagedRecords    int64
	LastError        string
	CatalogAvailable bool
}

// ProviderStatusSource reports the current IGDB provider status.
// DiscoveryService queries it on every Browse/Resolve call rather than
// caching an enabled/disabled flag, so it stays correct across a provider
// being disabled or re-enabled, or a background sync completing, without
// restarting the service. *IGDBSyncService satisfies this (via its Status
// method) so the composition root wires the real sync service directly.
type ProviderStatusSource interface {
	Status(context.Context) (ProviderStatus, error)
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

// DiscoverySearchResult is one bounded page of a unified, ranked local+
// provider search. HasMore is true only when a row past the requested limit
// actually exists; the repository fetches one extra row to detect it rather
// than issuing a separate count query.
type DiscoverySearchResult struct {
	Items   []DiscoveryItem
	HasMore bool
}

// DiscoveryRepository.Search's final bool parameter gates whether
// unmaterialized provider rows may be considered at all: false restricts the
// unified query to local-sourced rows (an already-materialized local Game
// still matches by its current local name/alias), exactly like the provider
// being disabled did before this contract existed, but through the same
// ranked query rather than a separate local-only code path.
//
// FindLocal and FindProvider (Task 5) are targeted single-row read-only
// lookups for the Game Request composer, each keyed by its own precise
// identity (a local Game ID, or one provider reference) rather than
// deduplicating across a ranked candidate set the way Search does. FindLocal
// takes (ctx, userID, localGameID) and reports the local Game's current
// presentation, last-known-good enrichment, and the given viewer's own
// Follow state; it never returns Reference. FindProvider takes (ctx, userID,
// ProviderReference) and reports either the linked local Game's presentation
// (already materialized) or the active generation's own provider
// presentation (not yet materialized) -- never both, and never a row at all
// for a provider record mapped only to a deactivated local Game.
type DiscoveryRepository interface {
	Search(context.Context, string, string, int, bool) (DiscoverySearchResult, error)
	FindLocal(context.Context, string, string) (DiscoveryItem, error)
	FindProvider(context.Context, string, ProviderReference) (DiscoveryItem, error)
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
// hidden until explicitly reviewed here. A supported type alone is not
// enough: VisibleProviderRecord also hides IGDB versions/editions.
var visibleProviderGameTypes = []string{
	"main game", "expansion", "standalone expansion", "remake", "remaster", "port", "expanded game",
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

// VisibleProviderRecord is the full player-visible policy for an
// unmaterialized provider record: a supported Game Type and no IGDB
// version_parent (a version_parent marks a commercial edition/version of
// another record). parent_game deliberately plays no part, so expansions stay
// discoverable. Already materialized local Games are never subject to it.
func VisibleProviderRecord(gameType string, hasVersionParent bool) bool {
	return !hasVersionParent && VisibleProviderGameType(gameType)
}

// VisibleProviderGameTypes returns the normalized (lowercase) Game Type
// values considered player-visible, for read-model queries that must filter
// by type before paging/limiting provider rows (rather than after, which
// could let a page fill entirely with hidden-type matches). The returned
// slice is a copy; callers may not mutate the canonical policy through it.
func VisibleProviderGameTypes() []string {
	return append([]string(nil), visibleProviderGameTypes...)
}
