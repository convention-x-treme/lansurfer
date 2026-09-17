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
	LocalGameID      string
	Reference        ProviderReference
	Name             string
	Slug             string
	GameType         string
	PlatformNames    []string
	FirstReleaseDate *time.Time
	CoverImageID     string
	Subscribed       bool
	Materialized     bool
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
	return ProviderReference{Provider: provider, ExternalID: externalID}, nil
}

func VisibleProviderGameType(gameType string) bool {
	switch strings.ToLower(strings.TrimSpace(gameType)) {
	case "main game", "standalone expansion", "remake", "remaster", "port", "expanded game":
		return true
	default:
		return false
	}
}
