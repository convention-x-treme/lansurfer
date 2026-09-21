// Package gamerequest provides read-only UI projections for game requests.
package gamerequest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/game"
	domain "github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/query/gamerequest/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TrendingWindow is the rolling window the Trending read model ranks over. The
// UI states it next to the ranking, so both come from this one value.
const TrendingWindow = 6 * time.Hour

// trendingLimit keeps the rail compact; a sparse or empty ranking during a
// quiet period is the intended result, not a reason to widen the window.
const trendingLimit = 5

// maxSearchLength matches the game catalog's own search bound.
const maxSearchLength = 120

// ErrInvalidSearch reports a search term the read model refuses to run.
var ErrInvalidSearch = errors.New("invalid search term")

type Request struct {
	ID               string
	GameID           string
	Game             game.GamePresentation
	CreatorID        string
	CreatorGameNick  string
	MaxPlayers       *int32
	StartsAt         time.Time
	Description      string
	Status           domain.Status
	CreatedAt        time.Time
	MemberCount      int64
	ViewerJoined     bool
	ViewerSubscribed bool
	Members          []Member
	Activity         []Activity
}

type Member struct {
	ID       string
	GameNick string
	JoinedAt time.Time
}

// Activity is one durable request-local entry, newest first.
type Activity struct {
	Kind          domain.ActivityKind
	ActorID       string
	ActorGameNick string
	OccurredAt    time.Time
}

// Scope is the audience a player is looking at. The four values are mutually
// exclusive intents rather than combinable facets: asking for requests you
// created and requests for games you follow at once has no useful answer.
type Scope string

const (
	// ScopeAll is the default view and the zero value, so an absent or
	// unrecognized parameter simply leaves the list unnarrowed.
	ScopeAll     Scope = ""
	ScopeMyGames Scope = "my-games"
	ScopeJoined  Scope = "joined"
	ScopeMine    Scope = "mine"
)

// ParseScope resolves a request parameter to a scope, falling back to the
// default view for anything it does not recognize.
func ParseScope(value string) Scope {
	switch Scope(value) {
	case ScopeMyGames, ScopeJoined, ScopeMine:
		return Scope(value)
	default:
		return ScopeAll
	}
}

// Filter carries the stable 1.0 Game Request discovery filters. Scope selects
// the audience; free places is a separate toggle because it narrows any of
// them.
type Filter struct {
	Search         string
	Scope          Scope
	OpenPlacesOnly bool
}

// Active reports whether any filter narrows the list.
func (f Filter) Active() bool {
	return f.Search != "" || f.Scope != ScopeAll || f.OpenPlacesOnly
}

// TrendingGame is one entry of the request-activity ranking.
type TrendingGame struct {
	GameID        string
	GameName      string
	ActivityCount int64
	RequestCount  int64
}

// RequestSummary is the viewer-independent projection of a Game Request for
// system callers, such as the notification worker, that never personalize
// for a viewer and so never carry a viewer ID.
type RequestSummary struct {
	GameID   string
	GameName string
	StartsAt time.Time
	Status   domain.Status
}

type Store struct {
	queries *sqlcgen.Queries
	now     func() time.Time
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: sqlcgen.New(pool), now: time.Now}
}

func (s *Store) ListActive(ctx context.Context, viewerID string, filter Filter) ([]Request, error) {
	search := strings.TrimSpace(filter.Search)
	// Counted in characters, as the game catalog counts its own search term and
	// as the browser's maxlength counts. Bytes would reject a shorter term than
	// the player was allowed to type.
	if utf8.RuneCountInString(search) > maxSearchLength {
		return nil, ErrInvalidSearch
	}
	databaseViewerID, err := parseUUID(viewerID)
	if err != nil {
		return nil, fmt.Errorf("parse viewer ID: %w", err)
	}
	rows, err := s.queries.ListActiveGameRequests(ctx, sqlcgen.ListActiveGameRequestsParams{
		ViewerID:       databaseViewerID,
		Search:         search,
		Scope:          string(filter.Scope),
		OpenPlacesOnly: filter.OpenPlacesOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("list active game requests: %w", err)
	}
	result := make([]Request, 0, len(rows))
	for _, row := range rows {
		result = append(result, requestFromFields(requestFields{
			ID: row.RequestID, GameID: row.GameID,
			Game:      gamePresentationFromRow(row.GameName, row.GameSlug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate),
			CreatorID: row.CreatorID, CreatorGameNick: row.CreatorGameNick.String,
			MaxPlayers: row.MaxPlayers, StartsAt: row.StartsAt, Description: row.Description, Status: row.Status,
			CreatedAt: row.CreatedAt, MemberCount: row.MemberCount, ViewerJoined: row.ViewerJoined, ViewerSubscribed: row.ViewerSubscribed,
		}))
	}
	return result, nil
}

func (s *Store) ByID(ctx context.Context, id, viewerID string) (Request, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return Request{}, domain.ErrNotFound
	}
	databaseViewerID, err := parseUUID(viewerID)
	if err != nil {
		return Request{}, fmt.Errorf("parse viewer ID: %w", err)
	}
	row, err := s.queries.GetGameRequest(ctx, sqlcgen.GetGameRequestParams{ID: databaseID, ViewerID: databaseViewerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, domain.ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("get game request: %w", err)
	}
	request := requestFromFields(requestFields{
		ID: row.RequestID, GameID: row.GameID,
		Game:      gamePresentationFromRow(row.GameName, row.GameSlug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate),
		CreatorID: row.CreatorID, CreatorGameNick: row.CreatorGameNick.String,
		MaxPlayers: row.MaxPlayers, StartsAt: row.StartsAt, Description: row.Description, Status: row.Status,
		CreatedAt: row.CreatedAt, MemberCount: row.MemberCount, ViewerJoined: row.ViewerJoined, ViewerSubscribed: row.ViewerSubscribed,
	})
	members, err := s.queries.ListGameRequestMembers(ctx, databaseID)
	if err != nil {
		return Request{}, fmt.Errorf("list game request members: %w", err)
	}
	request.Members = make([]Member, 0, len(members))
	for _, member := range members {
		request.Members = append(request.Members, Member{ID: member.UserID, GameNick: member.MemberGameNick.String, JoinedAt: member.JoinedAt.Time})
	}
	activity, err := s.queries.ListGameRequestActivity(ctx, databaseID)
	if err != nil {
		return Request{}, fmt.Errorf("list game request activity: %w", err)
	}
	request.Activity = make([]Activity, 0, len(activity))
	for _, entry := range activity {
		request.Activity = append(request.Activity, Activity{
			Kind: domain.ActivityKind(entry.Kind), ActorID: entry.ActorID,
			ActorGameNick: entry.ActorGameNick.String, OccurredAt: entry.OccurredAt.Time,
		})
	}
	return request, nil
}

// Trending ranks games by Game Request activity inside TrendingWindow.
func (s *Store) Trending(ctx context.Context) ([]TrendingGame, error) {
	rows, err := s.queries.TrendingGames(ctx, sqlcgen.TrendingGamesParams{
		Since:       pgtype.Timestamptz{Time: s.now().Add(-TrendingWindow), Valid: true},
		ResultLimit: trendingLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("rank trending games: %w", err)
	}
	result := make([]TrendingGame, 0, len(rows))
	for _, row := range rows {
		result = append(result, TrendingGame{
			GameID: row.GameID, GameName: row.GameName,
			ActivityCount: row.ActivityCount, RequestCount: row.RequestCount,
		})
	}
	return result, nil
}

// CountOpen reports how many Game Requests are open on this installation,
// independent of any discovery filter, so the navigation badge keeps one
// meaning whether or not the list beside it is narrowed.
func (s *Store) CountOpen(ctx context.Context) (int64, error) {
	total, err := s.queries.CountOpenGameRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("count open game requests: %w", err)
	}
	return total, nil
}

// GetGameRequestSummary resolves a Game Request for a system caller with no
// viewer to personalize for, unlike ByID.
func (s *Store) GetGameRequestSummary(ctx context.Context, gameRequestID string) (RequestSummary, error) {
	databaseID, err := parseUUID(gameRequestID)
	if err != nil {
		return RequestSummary{}, domain.ErrNotFound
	}
	row, err := s.queries.GetGameRequestSummary(ctx, databaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestSummary{}, domain.ErrNotFound
	}
	if err != nil {
		return RequestSummary{}, fmt.Errorf("get game request summary: %w", err)
	}
	return RequestSummary{
		GameID: row.GameID, GameName: row.GameName,
		StartsAt: row.StartsAt.Time, Status: domain.Status(row.Status),
	}, nil
}

// requestFields is the input to requestFromFields, built once by each sqlc
// row mapper (ListActive, ByID) so a new presentation field cannot silently
// reorder past the existing positional fields the way an expanding
// positional requestFromFields signature would invite.
type requestFields struct {
	ID               string
	GameID           string
	Game             game.GamePresentation
	CreatorID        string
	CreatorGameNick  string
	MaxPlayers       pgtype.Int4
	StartsAt         pgtype.Timestamptz
	Description      string
	Status           string
	CreatedAt        pgtype.Timestamptz
	MemberCount      int64
	ViewerJoined     bool
	ViewerSubscribed bool
}

func requestFromFields(fields requestFields) Request {
	return Request{
		ID: fields.ID, GameID: fields.GameID, Game: fields.Game,
		CreatorID: fields.CreatorID, CreatorGameNick: fields.CreatorGameNick,
		MaxPlayers: int4Pointer(fields.MaxPlayers), StartsAt: fields.StartsAt.Time, Description: fields.Description,
		Status: domain.Status(fields.Status), CreatedAt: fields.CreatedAt.Time, MemberCount: fields.MemberCount,
		ViewerJoined: fields.ViewerJoined, ViewerSubscribed: fields.ViewerSubscribed,
		Members:  []Member{},
		Activity: []Activity{},
	}
}

// gamePresentationFromRow maps one row's local name/slug plus its LEFT
// JOINed game_igdb_enrichment snapshot into game.GamePresentation, shared by
// ListActive and ByID so the two projections cannot drift. The enrichment
// columns are NULL for a Game never materialized via IGDB, which is the
// normal shape for a local-only Game, not an error case.
func gamePresentationFromRow(name, slug string, gameType, cover pgtype.Text, platforms []string, release pgtype.Timestamptz) game.GamePresentation {
	return game.GamePresentation{
		Name:             name,
		Slug:             slug,
		GameType:         textValue(gameType),
		PlatformNames:    nonNilStrings(platforms),
		FirstReleaseDate: timestamptzPointer(release),
		CoverImageID:     textValue(cover),
	}
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func timestamptzPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func int4Pointer(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}
