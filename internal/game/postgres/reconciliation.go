package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/game/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const adminIGDBSearchLimit = 25

func (s *Store) SearchIGDB(ctx context.Context, search string) ([]game.AdminProviderGame, error) {
	rows, err := s.queries.SearchActiveIGDBGamesForAdmin(ctx, search)
	if err != nil {
		return nil, fmt.Errorf("search active IGDB catalog: %w", err)
	}
	result := make([]game.AdminProviderGame, 0, min(len(rows), adminIGDBSearchLimit))
	modeRows, err := s.providerModesForResults(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result = append(result, game.AdminProviderGame{
			ExternalID: row.ExternalID, Name: row.Name, Slug: row.Slug, GameType: row.GameType,
			PlatformNames: nonNilStrings(row.PlatformNames), FirstReleaseDate: timestamptzPointer(row.FirstReleaseDate),
			CoverImageID: row.CoverImageID, LinkedGameID: row.LinkedGameID, LinkedGameName: row.LinkedGameName,
			Summary: row.Summary, GenreNames: nonNilStrings(row.GenreNames), GameModeNames: nonNilStrings(row.GameModeNames),
			MultiplayerModes: modeRows[row.ExternalID],
		})
	}
	return result, nil
}

// providerModesForResults loads the active generation's multiplayer modes
// for the search results in one query. Numeric IGDB IDs parse back from the
// text external IDs the search returned; a failure here is a programming
// error, not user input.
func (s *Store) providerModesForResults(ctx context.Context, rows []sqlcgen.SearchActiveIGDBGamesForAdminRow) (map[string][]game.MultiplayerMode, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		id, err := strconv.ParseInt(row.ExternalID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse external ID %s: %w", row.ExternalID, err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return map[string][]game.MultiplayerMode{}, nil
	}
	modeRows, err := s.queries.IGDBMultiplayerModesForExternalIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load IGDB multiplayer modes: %w", err)
	}
	return providerModeRows(modeRows), nil
}

// LinkIGDB links only an identity selected by the administrator. All provider
// metadata is reloaded inside the transaction from the active generation.
func (s *Store) LinkIGDB(ctx context.Context, gameID string, reference game.ProviderReference) error {
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	igdbID, err := strconv.ParseInt(reference.ExternalID, 10, 64)
	if err != nil {
		return game.ErrInvalidProviderReference
	}
	err = s.linkIGDB(ctx, databaseGameID, gameID, reference.ExternalID, igdbID)
	if !isUniqueViolation(err) {
		return err
	}

	// A concurrent link won the unique provider/external-ID constraint. Read
	// its committed owner outside the rolled-back transaction and translate it
	// into the same useful conflict the non-racing path returns.
	owner, ownerErr := s.queries.IGDBLinkOwner(ctx, reference.ExternalID)
	if ownerErr != nil {
		return fmt.Errorf("resolve concurrent IGDB link conflict: %w", ownerErr)
	}
	if owner.GameID != gameID {
		return &game.IGDBLinkConflict{GameID: owner.GameID, GameName: owner.GameName}
	}
	return s.linkIGDB(ctx, databaseGameID, gameID, reference.ExternalID, igdbID)
}

func (s *Store) linkIGDB(ctx context.Context, databaseGameID pgtype.UUID, gameID, externalID string, igdbID int64) error {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin IGDB link transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := s.queries.WithTx(transaction)

	if _, err := queries.LockAdminGame(ctx, databaseGameID); errors.Is(err, pgx.ErrNoRows) {
		return game.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock game for IGDB link: %w", err)
	}
	if _, err := queries.ActiveIGDBProviderGame(ctx, igdbID); errors.Is(err, pgx.ErrNoRows) {
		return game.ErrProviderRecordNotFound
	} else if err != nil {
		return fmt.Errorf("load active IGDB record: %w", err)
	}

	current, err := queries.AdminGameIGDBLink(ctx, databaseGameID)
	if err == nil {
		if current != externalID {
			return game.ErrGameAlreadyLinked
		}
		if err := queries.RestoreIGDBExternalID(ctx, externalID); err != nil {
			return fmt.Errorf("restore IGDB link: %w", err)
		}
		if err := queries.UpsertIGDBEnrichmentForExternalID(ctx, externalID); err != nil {
			return fmt.Errorf("refresh IGDB enrichment: %w", err)
		}
		if err := queries.ClearIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
			return fmt.Errorf("clear IGDB multiplayer modes: %w", err)
		}
		if err := queries.InsertIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
			return fmt.Errorf("insert IGDB multiplayer modes: %w", err)
		}
		return commitReconciliation(ctx, transaction, "link IGDB record")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load game IGDB link: %w", err)
	}

	owner, err := queries.IGDBLinkOwner(ctx, externalID)
	if err == nil {
		return &game.IGDBLinkConflict{GameID: owner.GameID, GameName: owner.GameName}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load IGDB link owner: %w", err)
	}
	if err := queries.CreateIGDBExternalID(ctx, sqlcgen.CreateIGDBExternalIDParams{
		GameID: databaseGameID, ExternalID: externalID,
	}); err != nil {
		return fmt.Errorf("create IGDB link: %w", err)
	}
	if err := queries.UpsertIGDBEnrichmentForExternalID(ctx, externalID); err != nil {
		return fmt.Errorf("store IGDB enrichment: %w", err)
	}
	if err := queries.ClearIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
		return fmt.Errorf("clear IGDB multiplayer modes: %w", err)
	}
	if err := queries.InsertIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
		return fmt.Errorf("insert IGDB multiplayer modes: %w", err)
	}
	return commitReconciliation(ctx, transaction, "link IGDB record")
}

func (s *Store) UnlinkIGDB(ctx context.Context, gameID string) error {
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin IGDB unlink transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := s.queries.WithTx(transaction)
	if _, err := queries.LockAdminGame(ctx, databaseGameID); errors.Is(err, pgx.ErrNoRows) {
		return game.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock game for IGDB unlink: %w", err)
	}
	if err := queries.DeleteGameIGDBEnrichment(ctx, databaseGameID); err != nil {
		return fmt.Errorf("delete IGDB enrichment: %w", err)
	}
	deleted, err := queries.DeleteGameIGDBLink(ctx, databaseGameID)
	if err != nil {
		return fmt.Errorf("delete IGDB link: %w", err)
	}
	if deleted == 0 {
		return game.ErrIGDBNotLinked
	}
	return commitReconciliation(ctx, transaction, "unlink IGDB record")
}

func (s *Store) Merge(ctx context.Context, sourceID, targetID string) error {
	source, err := parseUUID(sourceID)
	if err != nil {
		return game.ErrNotFound
	}
	target, err := parseUUID(targetID)
	if err != nil {
		return game.ErrNotFound
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin game merge transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := s.queries.WithTx(transaction)
	locked, err := queries.LockGamesForMerge(ctx, sqlcgen.LockGamesForMergeParams{SourceID: source, TargetID: target})
	if err != nil {
		return fmt.Errorf("lock games for merge: %w", err)
	}
	if len(locked) != 2 {
		return game.ErrNotFound
	}

	sourceLink, sourceLinked, err := adminGameIGDBLink(ctx, queries, source)
	if err != nil {
		return err
	}
	_, targetLinked, err := adminGameIGDBLink(ctx, queries, target)
	if err != nil {
		return err
	}
	if sourceLinked && targetLinked {
		return game.ErrConflictingIGDBLink
	}

	if err := queries.MoveGameRequests(ctx, sqlcgen.MoveGameRequestsParams{TargetID: target, SourceID: source}); err != nil {
		return fmt.Errorf("move game requests: %w", err)
	}
	if err := queries.CopyGameSubscriptions(ctx, sqlcgen.CopyGameSubscriptionsParams{TargetID: target, SourceID: source}); err != nil {
		return fmt.Errorf("copy game subscriptions: %w", err)
	}
	if err := queries.DeleteGameSubscriptions(ctx, source); err != nil {
		return fmt.Errorf("delete source game subscriptions: %w", err)
	}
	if err := queries.CopyGameAliases(ctx, sqlcgen.CopyGameAliasesParams{TargetID: target, SourceID: source}); err != nil {
		return fmt.Errorf("copy game aliases: %w", err)
	}
	if sourceLinked {
		if err := queries.DeleteGameIGDBEnrichment(ctx, target); err != nil {
			return fmt.Errorf("clear target IGDB enrichment: %w", err)
		}
		if err := queries.MoveGameIGDBLink(ctx, sqlcgen.MoveGameIGDBLinkParams{TargetID: target, SourceID: source}); err != nil {
			return fmt.Errorf("move IGDB link %s: %w", sourceLink, err)
		}
		if err := queries.MoveGameIGDBEnrichment(ctx, sqlcgen.MoveGameIGDBEnrichmentParams{TargetID: target, SourceID: source}); err != nil {
			return fmt.Errorf("move IGDB enrichment: %w", err)
		}
	}
	deleted, err := queries.DeleteGame(ctx, source)
	if err != nil {
		return fmt.Errorf("delete merged source game: %w", err)
	}
	if deleted != 1 {
		return game.ErrNotFound
	}
	return commitReconciliation(ctx, transaction, "merge games")
}

func (s *Store) Delete(ctx context.Context, gameID string) error {
	databaseGameID, err := parseUUID(gameID)
	if err != nil {
		return game.ErrNotFound
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin game deletion transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := s.queries.WithTx(transaction)
	if _, err := queries.LockAdminGame(ctx, databaseGameID); errors.Is(err, pgx.ErrNoRows) {
		return game.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock game for deletion: %w", err)
	}
	usage, err := queries.GameUsage(ctx, databaseGameID)
	if err != nil {
		return fmt.Errorf("load game deletion guards: %w", err)
	}
	if usage.GameRequestCount > 0 {
		return game.ErrGameHasRequests
	}
	if usage.SubscriptionCount > 0 {
		return game.ErrGameHasFollowers
	}
	deleted, err := queries.DeleteGame(ctx, databaseGameID)
	if err != nil {
		return fmt.Errorf("delete game: %w", err)
	}
	if deleted != 1 {
		return game.ErrNotFound
	}
	return commitReconciliation(ctx, transaction, "delete game")
}

func adminGameIGDBLink(ctx context.Context, queries *sqlcgen.Queries, gameID pgtype.UUID) (string, bool, error) {
	externalID, err := queries.AdminGameIGDBLink(ctx, gameID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load game IGDB link: %w", err)
	}
	return externalID, true, nil
}

func commitReconciliation(ctx context.Context, transaction pgx.Tx, operation string) error {
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}
