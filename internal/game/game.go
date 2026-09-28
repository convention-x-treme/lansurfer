// Package game owns LANsurfer's authoritative local game catalog.
package game

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxNameLength   = 120
	maxSlugLength   = 120
	maxAliasLength  = 120
	maxSearchLength = 120
)

var (
	ErrNotFound            = errors.New("game not found")
	ErrAliasNotFound       = errors.New("game alias not found")
	ErrAlreadyExists       = errors.New("game or alias already exists")
	ErrInvalidName         = errors.New("invalid game name")
	ErrInvalidSlug         = errors.New("invalid game slug")
	ErrInvalidAlias        = errors.New("invalid game alias")
	ErrInvalidSearch       = errors.New("invalid game search")
	ErrSameGame            = errors.New("source and target game are the same")
	ErrGameAlreadyLinked   = errors.New("game already has an IGDB link")
	ErrIGDBNotLinked       = errors.New("game has no IGDB link")
	ErrIGDBAlreadyLinked   = errors.New("IGDB record is linked to another game")
	ErrConflictingIGDBLink = errors.New("games have conflicting IGDB links")
	ErrGameHasRequests     = errors.New("game has game requests")
	ErrGameHasFollowers    = errors.New("game has followers")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Alias struct {
	ID    string
	Value string
}

type Game struct {
	ID             string
	Name           string
	Slug           string
	Active         bool
	Aliases        []Alias
	IGDBExternalID string
	IGDBEnriched   bool
	// IGDBSlug addresses the canonical igdb.com page for the linked record.
	// It comes from the active synchronized provider index rather than from
	// the enrichment snapshot, so it is empty whenever that index cannot
	// currently confirm the record.
	IGDBSlug             string
	IGDBCoverImageID     string
	IGDBSummary          string
	IGDBGenreNames       []string
	IGDBGameModeNames    []string
	IGDBMultiplayerModes []MultiplayerMode
	GameRequestCount     int64
	SubscriptionCount    int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// AdminProviderGame is one active-generation IGDB record offered for an
// explicit administrator link. LinkedGameID/Name expose an existing mapping
// so the UI can route the administrator to merge instead of surfacing a
// uniqueness error.
type AdminProviderGame struct {
	ExternalID       string
	Name             string
	Slug             string
	GameType         string
	PlatformNames    []string
	FirstReleaseDate *time.Time
	CoverImageID     string
	Summary          string
	GenreNames       []string
	GameModeNames    []string
	MultiplayerModes []MultiplayerMode
	LinkedGameID     string
	LinkedGameName   string
}

// IGDBLinkConflict carries the existing owner of an IGDB identity. It still
// matches ErrIGDBAlreadyLinked for ordinary error translation.
type IGDBLinkConflict struct {
	GameID   string
	GameName string
}

func (e *IGDBLinkConflict) Error() string { return ErrIGDBAlreadyLinked.Error() }
func (e *IGDBLinkConflict) Is(target error) bool {
	return target == ErrIGDBAlreadyLinked
}

// SubscriptionGame is the user-facing catalog projection with the current
// user's subscription state.
type SubscriptionGame struct {
	ID string
	GamePresentation
	Subscribed bool
}

type Repository interface {
	List(context.Context, string) ([]Game, error)
	ActiveExists(context.Context, string) (bool, error)
	Create(context.Context, string, string) (Game, error)
	Update(context.Context, string, string, string) (Game, error)
	SetActive(context.Context, string, bool) error
	AddAlias(context.Context, string, string) (Alias, error)
	RemoveAlias(context.Context, string, string) error
	SearchIGDB(context.Context, string) ([]AdminProviderGame, error)
	LinkIGDB(context.Context, string, ProviderReference) error
	UnlinkIGDB(context.Context, string) error
	Merge(context.Context, string, string) error
	Delete(context.Context, string) error
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

type SubscriptionRepository interface {
	Browse(context.Context, string, string) ([]SubscriptionGame, error)
	MyGames(context.Context, string) ([]SubscriptionGame, error)
	Follow(context.Context, string, string) error
	Unfollow(context.Context, string, string) error
	CountCatalog(context.Context) (int64, error)
	CountFollowed(context.Context, string) (int64, error)
	ListFollowers(context.Context, string) ([]string, error)
}

type SubscriptionService struct {
	repository SubscriptionRepository
}

func NewSubscriptionService(repository SubscriptionRepository) *SubscriptionService {
	return &SubscriptionService{repository: repository}
}

func (s *SubscriptionService) Browse(ctx context.Context, userID, search string) ([]SubscriptionGame, error) {
	search, err := validateSearch(search)
	if err != nil {
		return nil, err
	}
	return s.repository.Browse(ctx, userID, search)
}

func (s *SubscriptionService) MyGames(ctx context.Context, userID string) ([]SubscriptionGame, error) {
	return s.repository.MyGames(ctx, userID)
}

// CountCatalog and CountFollowed back the navigation's Games/My Games counts.
// They are counts, not the lists themselves, so every screen can state them
// without loading a catalog it is not showing.
func (s *SubscriptionService) CountCatalog(ctx context.Context) (int64, error) {
	return s.repository.CountCatalog(ctx)
}

func (s *SubscriptionService) CountFollowed(ctx context.Context, userID string) (int64, error) {
	return s.repository.CountFollowed(ctx, userID)
}

func (s *SubscriptionService) Follow(ctx context.Context, userID, gameID string) error {
	return s.repository.Follow(ctx, userID, gameID)
}

func (s *SubscriptionService) Unfollow(ctx context.Context, userID, gameID string) error {
	return s.repository.Unfollow(ctx, userID, gameID)
}

// ListFollowers returns the user IDs following gameID, for Track E2's
// notification recipient materialization (Task 4).
func (s *SubscriptionService) ListFollowers(ctx context.Context, gameID string) ([]string, error) {
	return s.repository.ListFollowers(ctx, gameID)
}

func (s *Service) List(ctx context.Context, search string) ([]Game, error) {
	search, err := validateSearch(search)
	if err != nil {
		return nil, err
	}
	return s.repository.List(ctx, search)
}

func (s *Service) ActiveExists(ctx context.Context, id string) (bool, error) {
	return s.repository.ActiveExists(ctx, id)
}

func validateSearch(search string) (string, error) {
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > maxSearchLength {
		return "", ErrInvalidSearch
	}
	return search, nil
}

func (s *Service) Create(ctx context.Context, name, slug string) (Game, error) {
	name, slug, err := validateGame(name, slug)
	if err != nil {
		return Game{}, err
	}
	return s.repository.Create(ctx, name, slug)
}

func (s *Service) Update(ctx context.Context, id, name, slug string) (Game, error) {
	name, slug, err := validateGame(name, slug)
	if err != nil {
		return Game{}, err
	}
	return s.repository.Update(ctx, id, name, slug)
}

func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	return s.repository.SetActive(ctx, id, active)
}

func (s *Service) AddAlias(ctx context.Context, gameID, value string) (Alias, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxAliasLength {
		return Alias{}, ErrInvalidAlias
	}
	return s.repository.AddAlias(ctx, gameID, value)
}

func (s *Service) RemoveAlias(ctx context.Context, gameID, aliasID string) error {
	return s.repository.RemoveAlias(ctx, gameID, aliasID)
}

func (s *Service) SearchIGDB(ctx context.Context, search string) ([]AdminProviderGame, error) {
	search, err := validateSearch(search)
	if err != nil {
		return nil, err
	}
	if search == "" {
		return []AdminProviderGame{}, nil
	}
	return s.repository.SearchIGDB(ctx, search)
}

func (s *Service) LinkIGDB(ctx context.Context, gameID, externalID string) error {
	reference, err := NewProviderReference("igdb", externalID)
	if err != nil {
		return err
	}
	return s.repository.LinkIGDB(ctx, gameID, reference)
}

func (s *Service) UnlinkIGDB(ctx context.Context, gameID string) error {
	return s.repository.UnlinkIGDB(ctx, gameID)
}

// Merge moves source into target. Argument order stays explicit throughout
// the stack because reversing it would preserve the wrong local identity.
func (s *Service) Merge(ctx context.Context, sourceID, targetID string) error {
	if sourceID == targetID {
		return ErrSameGame
	}
	return s.repository.Merge(ctx, sourceID, targetID)
}

func (s *Service) Delete(ctx context.Context, gameID string) error {
	return s.repository.Delete(ctx, gameID)
}

func validateGame(name, slug string) (string, string, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(slug)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return "", "", ErrInvalidName
	}
	if !ValidSlug(slug) {
		return "", "", ErrInvalidSlug
	}
	return name, slug, nil
}

// ValidSlug reports whether value satisfies the local Game slug invariant,
// which the games.slug CHECK constraint enforces in PostgreSQL as well.
// Materialization uses it to decide whether a provider-supplied slug can be
// adopted verbatim before falling back to a deterministic suffixed slug.
func ValidSlug(value string) bool {
	return utf8.RuneCountInString(value) <= maxSlugLength && slugPattern.MatchString(value)
}
