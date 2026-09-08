// Package gamerequest provides read-only UI projections for game requests.
package gamerequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/query/gamerequest/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Request struct {
	ID               string
	EventName        string
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
}

type Member struct {
	ID          string
	DisplayName string
	JoinedAt    time.Time
}

type GameOption struct {
	ID   string
	Name string
}

type CreationOptions struct {
	EventID   string
	EventName string
	Games     []GameOption
}

type Store struct {
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: sqlcgen.New(pool)}
}

func (s *Store) ListActive(ctx context.Context, viewerID string) ([]Request, error) {
	databaseViewerID, err := parseUUID(viewerID)
	if err != nil {
		return nil, fmt.Errorf("parse viewer ID: %w", err)
	}
	rows, err := s.queries.ListActiveGameRequests(ctx, databaseViewerID)
	if err != nil {
		return nil, fmt.Errorf("list active game requests: %w", err)
	}
	result := make([]Request, 0, len(rows))
	for _, row := range rows {
		result = append(result, requestFromFields(row.RequestID, row.EventName, row.GameName, row.CreatorID, row.CreatorName,
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
	request := requestFromFields(row.RequestID, row.EventName, row.GameName, row.CreatorID, row.CreatorName,
		row.MaxPlayers, row.StartsAt, row.Description, row.Status, row.CreatedAt, row.MemberCount, row.ViewerJoined, row.ViewerSubscribed)
	members, err := s.queries.ListGameRequestMembers(ctx, databaseID)
	if err != nil {
		return Request{}, fmt.Errorf("list game request members: %w", err)
	}
	request.Members = make([]Member, 0, len(members))
	for _, member := range members {
		request.Members = append(request.Members, Member{ID: member.UserID, DisplayName: member.DisplayName, JoinedAt: member.JoinedAt.Time})
	}
	return request, nil
}

func (s *Store) CreationOptions(ctx context.Context) (CreationOptions, error) {
	event, err := s.queries.CurrentEventForGameRequest(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreationOptions{Games: []GameOption{}}, nil
	}
	if err != nil {
		return CreationOptions{}, fmt.Errorf("find current event: %w", err)
	}
	games, err := s.queries.ActiveGamesForGameRequest(ctx)
	if err != nil {
		return CreationOptions{}, fmt.Errorf("list active games: %w", err)
	}
	options := CreationOptions{EventID: event.ID, EventName: event.Name, Games: make([]GameOption, 0, len(games))}
	for _, game := range games {
		options.Games = append(options.Games, GameOption{ID: game.ID, Name: game.Name})
	}
	return options, nil
}

func requestFromFields(id, eventName, gameName, creatorID, creatorName string, maxPlayers pgtype.Int4,
	startsAt pgtype.Timestamptz, description, status string, createdAt pgtype.Timestamptz,
	memberCount int64, viewerJoined, viewerSubscribed bool,
) Request {
	return Request{
		ID: id, EventName: eventName, GameName: gameName, CreatorID: creatorID, CreatorName: creatorName,
		MaxPlayers: int4Pointer(maxPlayers), StartsAt: startsAt.Time, Description: description,
		Status: domain.Status(status), CreatedAt: createdAt.Time, MemberCount: memberCount,
		ViewerJoined: viewerJoined, ViewerSubscribed: viewerSubscribed,
		Members: []Member{},
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
