package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/game/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const providerStaleAfter = 36 * time.Hour

func (s *Store) BeginGeneration(ctx context.Context) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin IGDB generation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	// A leftover 'syncing' row can only be abandoned: single-instance 1.0
	// and IGDBSyncService.TryStart's in-process mutex mean this call is
	// never racing a still-running sync, so any 'syncing' row found here is
	// a previous process incarnation that exited before completing or
	// failing its generation.
	if err := queries.FailAbandonedIGDBGenerations(ctx); err != nil {
		return "", fmt.Errorf("fail abandoned IGDB generations: %w", err)
	}
	id, err := queries.BeginIGDBGeneration(ctx)
	if err != nil {
		return "", fmt.Errorf("begin IGDB generation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit IGDB generation: %w", err)
	}
	return id, nil
}

func (s *Store) StoreProviderGames(ctx context.Context, generationID string, games []game.ProviderGame) error {
	databaseGenerationID, err := parseUUID(generationID)
	if err != nil {
		return fmt.Errorf("parse IGDB generation ID: %w", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin IGDB staging transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	for _, providerGame := range games {
		parameters, err := storeIGDBProviderGameParams(databaseGenerationID, providerGame)
		if err != nil {
			return err
		}
		stored, err := queries.StoreIGDBProviderGame(ctx, parameters)
		if err != nil {
			return fmt.Errorf("store IGDB provider game %s: %w", providerGame.Reference.ExternalID, err)
		}
		if stored != 1 {
			return fmt.Errorf("store IGDB provider game %s: generation is not syncing", providerGame.Reference.ExternalID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit IGDB staging: %w", err)
	}
	return nil
}

func (s *Store) ActivateGeneration(ctx context.Context, generationID string, recordCount int64) error {
	databaseGenerationID, err := parseUUID(generationID)
	if err != nil {
		return fmt.Errorf("parse IGDB generation ID: %w", err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin IGDB activation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	updated, err := queries.CompleteIGDBGeneration(ctx, sqlcgen.CompleteIGDBGenerationParams{
		RecordCount:  recordCount,
		GenerationID: databaseGenerationID,
	})
	if err != nil {
		return fmt.Errorf("complete IGDB generation: %w", err)
	}
	if updated != 1 {
		return errors.New("complete IGDB generation: generation is not syncing")
	}
	if err := queries.ActivateIGDBGeneration(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("activate IGDB generation: %w", err)
	}
	if err := queries.ArchiveMissingIGDBExternalIDs(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("archive missing IGDB external IDs: %w", err)
	}
	if err := queries.RestorePresentIGDBExternalIDs(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("restore present IGDB external IDs: %w", err)
	}
	if err := queries.DeleteInactiveIGDBGenerations(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("delete inactive IGDB generations: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit IGDB activation: %w", err)
	}
	return nil
}

func (s *Store) FailGeneration(ctx context.Context, generationID, failure string) error {
	databaseGenerationID, err := parseUUID(generationID)
	if err != nil {
		return fmt.Errorf("parse IGDB generation ID: %w", err)
	}
	failure = boundedOneLine(failure, 500)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin IGDB failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	_, err = queries.FailIGDBGeneration(ctx, sqlcgen.FailIGDBGenerationParams{
		Error:        pgtype.Text{String: failure, Valid: true},
		GenerationID: databaseGenerationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("fail IGDB generation: generation is not syncing")
	}
	if err != nil {
		return fmt.Errorf("fail IGDB generation: %w", err)
	}
	if err := queries.RecordIGDBGenerationFailure(ctx, sqlcgen.RecordIGDBGenerationFailureParams{
		Error:        pgtype.Text{String: failure, Valid: true},
		GenerationID: databaseGenerationID,
	}); err != nil {
		return fmt.Errorf("record IGDB generation failure: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit IGDB failure: %w", err)
	}
	return nil
}

func (s *Store) Status(ctx context.Context, now time.Time) (game.ProviderStatus, error) {
	row, err := s.queries.IGDBProviderStatus(ctx)
	if err != nil {
		return game.ProviderStatus{}, fmt.Errorf("load IGDB provider status: %w", err)
	}
	status := game.ProviderStatus{
		State:       game.ProviderNotSynced,
		LastAttempt: timestamptzPointer(row.LastAttemptAt),
		LastSuccess: timestamptzPointer(row.LastSuccessAt),
		RecordCount: row.RecordCount,
	}
	if row.LastError.Valid {
		status.LastError = row.LastError.String
	}
	if row.LastAttemptSucceeded.Valid && !row.LastAttemptSucceeded.Bool {
		status.State = game.ProviderFailed
	} else if row.ActiveGenerationID.Valid && row.LastSuccessAt.Valid {
		status.State = game.ProviderCurrent
		if row.LastSuccessAt.Time.Before(now.Add(-providerStaleAfter)) {
			status.State = game.ProviderStale
		}
	}
	return status, nil
}

// Search combines authoritative local Games (matched by name/alias, exactly
// as before this feature) with the active IGDB generation from PostgreSQL;
// it never calls the IGDB provider. Empty search is handled entirely by the
// caller (DiscoveryService) using the local catalog, so this method is only
// invoked for a non-empty search.
func (s *Store) Search(ctx context.Context, userID, search string, limit int) ([]game.DiscoveryItem, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.SearchDiscoveryGames(ctx, sqlcgen.SearchDiscoveryGamesParams{
		UserID:           databaseUserID,
		VisibleGameTypes: game.VisibleProviderGameTypes(),
		Search:           search,
		RowLimit:         int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search discovery games: %w", err)
	}
	result := make([]game.DiscoveryItem, 0, len(rows))
	for _, row := range rows {
		var reference game.ProviderReference
		if row.ExternalID != "" {
			reference, err = game.NewProviderReference("igdb", row.ExternalID)
			if err != nil {
				return nil, fmt.Errorf("parse IGDB external ID %s: %w", row.ExternalID, err)
			}
		}
		result = append(result, game.DiscoveryItem{
			LocalGameID:      row.LocalGameID,
			Reference:        reference,
			Name:             row.Name,
			Slug:             row.Slug,
			GameType:         row.GameType.String,
			PlatformNames:    row.PlatformNames,
			FirstReleaseDate: timestamptzPointer(row.FirstReleaseDate),
			CoverImageID:     row.CoverImageID.String,
			Subscribed:       row.Subscribed,
			Materialized:     row.Materialized,
		})
	}
	return result, nil
}

func storeIGDBProviderGameParams(generationID pgtype.UUID, providerGame game.ProviderGame) (sqlcgen.StoreIGDBProviderGameParams, error) {
	reference, err := game.NewProviderReference(providerGame.Reference.Provider, providerGame.Reference.ExternalID)
	if err != nil {
		return sqlcgen.StoreIGDBProviderGameParams{}, fmt.Errorf("validate provider reference: %w", err)
	}
	igdbID, err := strconv.ParseInt(reference.ExternalID, 10, 64)
	if err != nil {
		return sqlcgen.StoreIGDBProviderGameParams{}, fmt.Errorf("parse IGDB ID: %w", err)
	}
	parentID, err := optionalPositiveInt64(providerGame.ParentExternalID)
	if err != nil {
		return sqlcgen.StoreIGDBProviderGameParams{}, fmt.Errorf("parse parent IGDB ID: %w", err)
	}
	versionParentID, err := optionalPositiveInt64(providerGame.VersionParentID)
	if err != nil {
		return sqlcgen.StoreIGDBProviderGameParams{}, fmt.Errorf("parse version parent IGDB ID: %w", err)
	}
	checksum, err := optionalUUID(providerGame.Checksum)
	if err != nil {
		return sqlcgen.StoreIGDBProviderGameParams{}, fmt.Errorf("parse IGDB checksum: %w", err)
	}
	return sqlcgen.StoreIGDBProviderGameParams{
		GenerationID:        generationID,
		IgdbID:              igdbID,
		Name:                providerGame.Name,
		Slug:                providerGame.Slug,
		GameType:            optionalText(providerGame.GameType),
		FirstReleaseDate:    optionalTime(providerGame.FirstReleaseDate),
		ParentIgdbID:        parentID,
		VersionParentIgdbID: versionParentID,
		PlatformNames:       nonNilStrings(providerGame.PlatformNames),
		AlternativeNames:    nonNilStrings(providerGame.AlternativeNames),
		CoverImageID:        optionalText(providerGame.CoverImageID),
		IgdbUpdatedAt:       optionalTime(providerGame.ProviderUpdatedAt),
		Checksum:            checksum,
		SearchText:          normalizedSearchText(providerGame.Name, providerGame.AlternativeNames),
	}, nil
}

func optionalPositiveInt64(value string) (pgtype.Int8, error) {
	if value == "" {
		return pgtype.Int8{}, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return pgtype.Int8{}, game.ErrInvalidProviderReference
	}
	return pgtype.Int8{Int64: parsed, Valid: true}, nil
}

func optionalUUID(value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func optionalTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func timestamptzPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func normalizedSearchText(name string, alternativeNames []string) string {
	values := append([]string{name}, alternativeNames...)
	return strings.ToLower(strings.Join(values, " "))
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func boundedOneLine(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
