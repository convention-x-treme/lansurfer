// Package gamerequest provides read-only UI projections for game requests.
package gamerequest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	GameName         string
	CreatorID        string
	CreatorName      string
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
	ID          string
	DisplayName string
	JoinedAt    time.Time
}

// Activity is one durable request-local entry, newest first.
type Activity struct {
	Kind       domain.ActivityKind
	ActorID    string
	ActorName  string
	OccurredAt time.Time
}

type GameOption struct {
	ID   string
	Name string
}

// Filter carries the stable 1.0 Game Request discovery filters.
type Filter struct {
	Search         string
	FollowedOnly   bool
	OpenPlacesOnly bool
}

// Active reports whether any filter narrows the list.
func (f Filter) Active() bool {
	return f.Search != "" || f.FollowedOnly || f.OpenPlacesOnly
}

// TrendingGame is one entry of the request-activity ranking.
type TrendingGame struct {
	GameID        string
	GameName      string
	ActivityCount int64
	RequestCount  int64
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
	if len(search) > maxSearchLength {
		return nil, ErrInvalidSearch
	}
	databaseViewerID, err := parseUUID(viewerID)
	if err != nil {
		return nil, fmt.Errorf("parse viewer ID: %w", err)
	}
	rows, err := s.queries.ListActiveGameRequests(ctx, sqlcgen.ListActiveGameRequestsParams{
		ViewerID:       databaseViewerID,
		Search:         search,
		FollowedOnly:   filter.FollowedOnly,
		OpenPlacesOnly: filter.OpenPlacesOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("list active game requests: %w", err)
	}
	result := make([]Request, 0, len(rows))
	for _, row := range rows {
		result = append(result, requestFromFields(row.RequestID, row.GameName, row.CreatorID, row.CreatorName,
			row.MaxPlayers, row.StartsAt, row.Description, row.Status, row.CreatedAt, row.MemberCount, row.ViewerJoined, row.ViewerSubscribed))
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
	request := requestFromFields(row.RequestID, row.GameName, row.CreatorID, row.CreatorName,
		row.MaxPlayers, row.StartsAt, row.Description, row.Status, row.CreatedAt, row.MemberCount, row.ViewerJoined, row.ViewerSubscribed)
	members, err := s.queries.ListGameRequestMembers(ctx, databaseID)
	if err != nil {
		return Request{}, fmt.Errorf("list game request members: %w", err)
	}
	request.Members = make([]Member, 0, len(members))
	for _, member := range members {
		request.Members = append(request.Members, Member{ID: member.UserID, DisplayName: member.DisplayName, JoinedAt: member.JoinedAt.Time})
	}
	activity, err := s.queries.ListGameRequestActivity(ctx, databaseID)
	if err != nil {
		return Request{}, fmt.Errorf("list game request activity: %w", err)
	}
	request.Activity = make([]Activity, 0, len(activity))
	for _, entry := range activity {
		request.Activity = append(request.Activity, Activity{
			Kind: domain.ActivityKind(entry.Kind), ActorID: entry.ActorID,
			ActorName: entry.ActorName, OccurredAt: entry.OccurredAt.Time,
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

// CreationOptions lists the games a player may request a round of. The
// installation itself is the event context, so no Event selection is involved.
func (s *Store) CreationOptions(ctx context.Context) ([]GameOption, error) {
	games, err := s.queries.ActiveGamesForGameRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active games: %w", err)
	}
	options := make([]GameOption, 0, len(games))
	for _, game := range games {
		options = append(options, GameOption{ID: game.ID, Name: game.Name})
	}
	return options, nil
}

func requestFromFields(id, gameName, creatorID, creatorName string, maxPlayers pgtype.Int4,
	startsAt pgtype.Timestamptz, description, status string, createdAt pgtype.Timestamptz,
	memberCount int64, viewerJoined, viewerSubscribed bool,
) Request {
	return Request{
		ID: id, GameName: gameName, CreatorID: creatorID, CreatorName: creatorName,
		MaxPlayers: int4Pointer(maxPlayers), StartsAt: startsAt.Time, Description: description,
		Status: domain.Status(status), CreatedAt: createdAt.Time, MemberCount: memberCount,
		ViewerJoined: viewerJoined, ViewerSubscribed: viewerSubscribed,
		Members:  []Member{},
		Activity: []Activity{},
	}
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
