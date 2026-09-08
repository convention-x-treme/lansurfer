// Package postgres provides PostgreSQL persistence for the game module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/game/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: sqlcgen.New(pool)}
}

func (s *Store) List(ctx context.Context, search string) ([]game.Game, error) {
	rows, err := s.queries.ListGames(ctx, search)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	result := make([]game.Game, 0, len(rows))
	for _, row := range rows {
		databaseID, err := parseUUID(row.GID)
		if err != nil {
			return nil, fmt.Errorf("parse game ID %s: %w", row.GID, err)
		}
		aliases, err := s.queries.ListGameAliases(ctx, databaseID)
		if err != nil {
			return nil, fmt.Errorf("list aliases for game %s: %w", row.GID, err)
		}
		item := game.Game{
			ID: row.GID, Name: row.Name, Slug: row.Slug, Active: row.Active,
			CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
			Aliases: make([]game.Alias, 0, len(aliases)),
		}
		for _, alias := range aliases {
			item.Aliases = append(item.Aliases, game.Alias{ID: alias.ID, Value: alias.Alias})
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) ActiveExists(ctx context.Context, id string) (bool, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return false, nil
	}
	exists, err := s.queries.ActiveGameExists(ctx, databaseID)
	if err != nil {
		return false, fmt.Errorf("check active game: %w", err)
	}
	return exists, nil
}

func (s *Store) Create(ctx context.Context, name, slug string) (game.Game, error) {
	row, err := s.queries.CreateGame(ctx, sqlcgen.CreateGameParams{Name: name, Slug: slug})
	if isUniqueViolation(err) {
		return game.Game{}, game.ErrAlreadyExists
	}
	if err != nil {
		return game.Game{}, fmt.Errorf("create game: %w", err)
	}
	return fromGame(row.ID, row.Name, row.Slug, row.Active, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) Update(ctx context.Context, id, name, slug string) (game.Game, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return game.Game{}, game.ErrNotFound
	}
	row, err := s.queries.UpdateGame(ctx, sqlcgen.UpdateGameParams{ID: databaseID, Name: name, Slug: slug})
	if errors.Is(err, pgx.ErrNoRows) {
		return game.Game{}, game.ErrNotFound
	}
	if isUniqueViolation(err) {
		return game.Game{}, game.ErrAlreadyExists
	}
	if err != nil {
		return game.Game{}, fmt.Errorf("update game: %w", err)
	}
	return fromGame(row.ID, row.Name, row.Slug, row.Active, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) SetActive(ctx context.Context, id string, active bool) error {
	databaseID, err := parseUUID(id)
	if err != nil {
		return game.ErrNotFound
	}
	updated, err := s.queries.SetGameActive(ctx, sqlcgen.SetGameActiveParams{ID: databaseID, Active: active})
	if err != nil {
		return fmt.Errorf("set game active: %w", err)
	}
	if updated == 0 {
		return game.ErrNotFound
	}
	return nil
}

func (s *Store) AddAlias(ctx context.Context, gameID, value string) (game.Alias, error) {
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.Alias{}, game.ErrNotFound
	}
	row, err := s.queries.CreateGameAlias(ctx, sqlcgen.CreateGameAliasParams{GameID: databaseGameID, Alias: value})
	if isUniqueViolation(err) {
		return game.Alias{}, game.ErrAlreadyExists
	}
	if isForeignKeyViolation(err) {
		return game.Alias{}, game.ErrNotFound
	}
	if err != nil {
		return game.Alias{}, fmt.Errorf("create game alias: %w", err)
	}
	return game.Alias{ID: row.ID, Value: row.Alias}, nil
}

func (s *Store) RemoveAlias(ctx context.Context, gameID, aliasID string) error {
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	databaseAliasID, err := parseUUID(aliasID)
	if err != nil {
		return game.ErrAliasNotFound
	}
	deleted, err := s.queries.DeleteGameAlias(ctx, sqlcgen.DeleteGameAliasParams{ID: databaseAliasID, GameID: databaseGameID})
	if err != nil {
		return fmt.Errorf("delete game alias: %w", err)
	}
	if deleted == 0 {
		return game.ErrAliasNotFound
	}
	return nil
}

func (s *Store) Browse(ctx context.Context, userID, search string) ([]game.SubscriptionGame, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.BrowseSubscriptionGames(ctx, sqlcgen.BrowseSubscriptionGamesParams{
		UserID: databaseUserID,
		Search: search,
	})
	if err != nil {
		return nil, fmt.Errorf("browse subscription games: %w", err)
	}
	result := make([]game.SubscriptionGame, 0, len(rows))
	for _, row := range rows {
		result = append(result, game.SubscriptionGame{
			ID: row.GID, Name: row.Name, Slug: row.Slug, Subscribed: row.Subscribed,
		})
	}
	return result, nil
}

func (s *Store) MyGames(ctx context.Context, userID string) ([]game.SubscriptionGame, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.ListSubscribedGames(ctx, databaseUserID)
	if err != nil {
		return nil, fmt.Errorf("list subscribed games: %w", err)
	}
	result := make([]game.SubscriptionGame, 0, len(rows))
	for _, row := range rows {
		result = append(result, game.SubscriptionGame{
			ID: row.GID, Name: row.Name, Slug: row.Slug, Subscribed: row.Subscribed,
		})
	}
	return result, nil
}

func (s *Store) Follow(ctx context.Context, userID, gameID string) error {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return fmt.Errorf("parse user ID: %w", err)
	}
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	inserted, err := s.queries.FollowGame(ctx, sqlcgen.FollowGameParams{
		UserID: databaseUserID,
		GameID: databaseGameID,
	})
	if isForeignKeyViolation(err) {
		return game.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("follow game: %w", err)
	}
	if inserted == 0 {
		exists, err := s.queries.ActiveGameExists(ctx, databaseGameID)
		if err != nil {
			return fmt.Errorf("check active game: %w", err)
		}
		if !exists {
			return game.ErrNotFound
		}
	}
	return nil
}

func (s *Store) Unfollow(ctx context.Context, userID, gameID string) error {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return fmt.Errorf("parse user ID: %w", err)
	}
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	if err := s.queries.UnfollowGame(ctx, sqlcgen.UnfollowGameParams{
		UserID: databaseUserID,
		GameID: databaseGameID,
	}); err != nil {
		return fmt.Errorf("unfollow game: %w", err)
	}
	return nil
}

func fromGame(id, name, slug string, active bool, createdAt, updatedAt pgtype.Timestamptz) game.Game {
	return game.Game{ID: id, Name: name, Slug: slug, Active: active, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time, Aliases: []game.Alias{}}
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23503"
}
