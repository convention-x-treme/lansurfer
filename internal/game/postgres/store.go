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
	pool    *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: sqlcgen.New(pool), pool: pool}
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
		modeRows, err := s.queries.EnrichmentMultiplayerModesForGame(ctx, databaseID)
		if err != nil {
			return nil, fmt.Errorf("list multiplayer modes for game %s: %w", row.GID, err)
		}
		item := game.Game{
			ID: row.GID, Name: row.Name, Slug: row.Slug, Active: row.Active,
			IGDBExternalID: row.IgdbExternalID, IGDBEnriched: row.IgdbEnriched,
			IGDBSummary:    row.EnrichmentSummary,
			IGDBGenreNames: nonNilStrings(row.GenreNames), IGDBGameModeNames: nonNilStrings(row.GameModeNames),
			IGDBMultiplayerModes: enrichmentModeRows(modeRows),
			GameRequestCount:     row.GameRequestCount, SubscriptionCount: row.SubscriptionCount,
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
			ID:               row.GID,
			GamePresentation: presentation(row.Name, row.Slug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate),
			Subscribed:       row.Subscribed,
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
			ID:               row.GID,
			GamePresentation: presentation(row.Name, row.Slug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate),
			Subscribed:       row.Subscribed,
		})
	}
	return result, nil
}

// CountCatalog reports how many active local Games exist, which is the number
// the navigation states beside "Games".
func (s *Store) CountCatalog(ctx context.Context) (int64, error) {
	total, err := s.queries.CountActiveGames(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active games: %w", err)
	}
	return total, nil
}

// CountFollowed reports how many active Games the viewer follows, matching
// what My Games itself lists.
func (s *Store) CountFollowed(ctx context.Context, userID string) (int64, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return 0, fmt.Errorf("parse user ID: %w", err)
	}
	total, err := s.queries.CountSubscribedGames(ctx, databaseUserID)
	if err != nil {
		return 0, fmt.Errorf("count subscribed games: %w", err)
	}
	return total, nil
}

// textValue reads a possibly-NULL text column into a plain string, empty
// when NULL. game_igdb_enrichment columns are NULL until a local Game has
// materialized via IGDB (LEFT JOIN with no matching row), so this is the
// normal shape for a local-only Game, not an error case.
func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

// presentation maps one Browse/My Games row into GamePresentation, shared by
// both queries so the two player-facing projections cannot drift. name and
// slug always come from the authoritative local Game; the remaining fields
// come from the LEFT JOINed game_igdb_enrichment snapshot and are zero for a
// Game never materialized via IGDB.
func presentation(name, slug string, gameType, cover pgtype.Text, platforms []string, release pgtype.Timestamptz) game.GamePresentation {
	return game.GamePresentation{
		Name:             name,
		Slug:             slug,
		GameType:         textValue(gameType),
		PlatformNames:    nonNilStrings(platforms),
		FirstReleaseDate: timestamptzPointer(release),
		CoverImageID:     textValue(cover),
	}
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
