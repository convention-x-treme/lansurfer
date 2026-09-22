package postgres

import (
	"context"
	"encoding/json"
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

	// Clean up terminal generations left behind by earlier attempts before
	// starting a new one, so a full-sync retry that fails late does not
	// leave its staged igdb_game_index rows in PostgreSQL forever. This
	// must run before FailAbandonedIGDBGenerations/BeginIGDBGeneration
	// below, using the active generation as it stood before either of
	// those write anything in this transaction, so it can never delete the
	// very generation this call is about to fail-abandon or create.
	activeGenerationID, err := queries.ActiveIGDBGenerationID(ctx)
	if err != nil {
		return "", fmt.Errorf("load active IGDB generation: %w", err)
	}
	if err := queries.DeleteInactiveIGDBGenerations(ctx, activeGenerationID); err != nil {
		return "", fmt.Errorf("delete inactive IGDB generations: %w", err)
	}

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
		if len(providerGame.MultiplayerModes) > 0 {
			igdbID, err := strconv.ParseInt(providerGame.Reference.ExternalID, 10, 64)
			if err != nil {
				return fmt.Errorf("store IGDB multiplayer modes %s: parse IGDB ID: %w", providerGame.Reference.ExternalID, err)
			}
			modes, err := multiplayerModesJSON(providerGame.MultiplayerModes)
			if err != nil {
				return fmt.Errorf("store IGDB multiplayer modes %s: encode: %w", providerGame.Reference.ExternalID, err)
			}
			if err := queries.StoreIGDBMultiplayerModes(ctx, sqlcgen.StoreIGDBMultiplayerModesParams{
				GenerationID: databaseGenerationID,
				IgdbID:       igdbID,
				Modes:        modes,
			}); err != nil {
				return fmt.Errorf("store IGDB multiplayer modes %s: %w", providerGame.Reference.ExternalID, err)
			}
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
	if err := queries.RefreshIGDBEnrichmentFromGeneration(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("refresh IGDB enrichment: %w", err)
	}
	if err := queries.ClearIGDBEnrichmentMultiplayerModesFromGeneration(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("clear IGDB multiplayer modes: %w", err)
	}
	if err := queries.InsertIGDBEnrichmentMultiplayerModesFromGeneration(ctx, databaseGenerationID); err != nil {
		return fmt.Errorf("insert IGDB multiplayer modes: %w", err)
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
		State:            game.ProviderNotSynced,
		LastAttempt:      timestamptzPointer(row.LastAttemptAt),
		LastSuccess:      timestamptzPointer(row.LastSuccessAt),
		RecordCount:      row.RecordCount,
		StagedRecords:    row.StagedRecordCount,
		CatalogAvailable: row.ActiveGenerationID.Valid,
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

// Search combines authoritative local Games (matched by name/alias) with the
// active IGDB generation from PostgreSQL into one ranked, bounded page; it
// never calls the IGDB provider. Empty/single-character search is handled
// entirely by the caller (DiscoveryService) using the local catalog, so this
// method is only invoked for a two-or-more-character search. It fetches one
// row beyond limit to detect DiscoverySearchResult.HasMore without a
// separate count query, and trims that extra row before returning.
// includeProvider=false restricts the ranked query to local-sourced rows
// only, exactly what a disabled provider needs, through the same query
// rather than a separate local-only code path.
func (s *Store) Search(ctx context.Context, userID, search string, limit int, includeProvider bool) (game.DiscoverySearchResult, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return game.DiscoverySearchResult{}, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.SearchDiscoveryGames(ctx, sqlcgen.SearchDiscoveryGamesParams{
		UserID:           databaseUserID,
		Search:           search,
		IncludeProvider:  includeProvider,
		VisibleGameTypes: game.VisibleProviderGameTypes(),
		FetchLimit:       int32(limit + 1),
	})
	if err != nil {
		return game.DiscoverySearchResult{}, fmt.Errorf("search discovery games: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]game.DiscoveryItem, 0, len(rows))
	for _, row := range rows {
		var reference game.ProviderReference
		if row.ExternalID != "" {
			reference, err = game.NewProviderReference("igdb", row.ExternalID)
			if err != nil {
				return game.DiscoverySearchResult{}, fmt.Errorf("parse IGDB external ID %s: %w", row.ExternalID, err)
			}
		}
		items = append(items, game.DiscoveryItem{
			LocalGameID:      row.LocalGameID,
			Reference:        reference,
			GamePresentation: presentation(row.Name, row.Slug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate, row.Summary, row.GenreNames),
			Subscribed:       row.Subscribed,
			Materialized:     row.Materialized,
		})
	}
	return game.DiscoverySearchResult{Items: items, HasMore: hasMore}, nil
}

// FindLocal is the read-only composer lookup for DiscoveryRepository
// (Task 5, GameSelection.LocalGameID): one active local Game by ID, its
// retained last-known-good enrichment, and the given viewer's own Follow
// state. It never sets Reference -- like the existing local Browse/MyGames
// projections, a local ID lookup does not surface the local Game's provider
// mapping. No row (missing or inactive Game) reports game.ErrNotFound.
func (s *Store) FindLocal(ctx context.Context, userID, localGameID string) (game.DiscoveryItem, error) {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return game.DiscoveryItem{}, fmt.Errorf("parse user ID: %w", err)
	}
	databaseGameID, err := parseUUID(localGameID)
	if err != nil {
		return game.DiscoveryItem{}, game.ErrNotFound
	}
	row, err := s.queries.FindLocalDiscoveryGame(ctx, sqlcgen.FindLocalDiscoveryGameParams{
		UserID: databaseUserID,
		GameID: databaseGameID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return game.DiscoveryItem{}, game.ErrNotFound
	}
	if err != nil {
		return game.DiscoveryItem{}, fmt.Errorf("find local discovery game: %w", err)
	}
	return game.DiscoveryItem{
		LocalGameID:      row.GID,
		GamePresentation: presentation(row.Name, row.Slug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate, row.Summary, row.GenreNames),
		Subscribed:       row.Subscribed,
		Materialized:     true,
	}, nil
}

// FindProvider is the read-only composer lookup for DiscoveryRepository
// (Task 5, GameSelection.Reference): the provider row must belong to the
// active generation. If it already maps to an active local Game, this
// returns that Game's own presentation and the viewer's Follow state
// (Materialized=true); otherwise it returns the active generation's own
// provider presentation, unmaterialized (Materialized=false). A provider
// record absent from the active generation, or mapped only to a
// deactivated local Game, reports game.ErrProviderRecordNotFound rather
// than fabricating or resurrecting a result -- see
// FindProviderDiscoveryGame's own comment for why those two cases collapse
// to "no row" together.
func (s *Store) FindProvider(ctx context.Context, userID string, reference game.ProviderReference) (game.DiscoveryItem, error) {
	normalized, err := game.NewProviderReference(reference.Provider, reference.ExternalID)
	if err != nil {
		return game.DiscoveryItem{}, err
	}
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return game.DiscoveryItem{}, fmt.Errorf("parse user ID: %w", err)
	}
	igdbID, err := strconv.ParseInt(normalized.ExternalID, 10, 64)
	if err != nil {
		return game.DiscoveryItem{}, game.ErrInvalidProviderReference
	}
	row, err := s.queries.FindProviderDiscoveryGame(ctx, sqlcgen.FindProviderDiscoveryGameParams{
		UserID: databaseUserID,
		IgdbID: igdbID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return game.DiscoveryItem{}, game.ErrProviderRecordNotFound
	}
	if err != nil {
		return game.DiscoveryItem{}, fmt.Errorf("find provider discovery game: %w", err)
	}
	rowReference, err := game.NewProviderReference("igdb", row.ExternalID)
	if err != nil {
		return game.DiscoveryItem{}, fmt.Errorf("parse IGDB external ID %s: %w", row.ExternalID, err)
	}
	return game.DiscoveryItem{
		LocalGameID:      row.LocalGameID,
		Reference:        rowReference,
		GamePresentation: presentation(row.Name, row.Slug, row.GameType, row.CoverImageID, row.PlatformNames, row.FirstReleaseDate, row.Summary, row.GenreNames),
		Subscribed:       row.Subscribed,
		Materialized:     row.Materialized,
	}, nil
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
		Summary:             optionalText(providerGame.Summary),
		GenreNames:          nonNilStrings(providerGame.GenreNames),
		GameModeNames:       nonNilStrings(providerGame.GameModeNames),
	}, nil
}

// multiplayerModeJSON is the transport shape the StoreIGDBMultiplayerModes
// jsonb_to_recordset column list expects. Pointers encode "not reported":
// nil player limits and an absent platform become SQL NULL (issue #46).
type multiplayerModeJSON struct {
	PlatformIGDBID    *int64 `json:"platform_igdb_id"`
	PlatformName      string `json:"platform_name"`
	CampaignCoop      bool   `json:"campaign_coop"`
	DropIn            bool   `json:"drop_in"`
	LanCoop           bool   `json:"lan_coop"`
	OfflineCoop       bool   `json:"offline_coop"`
	OnlineCoop        bool   `json:"online_coop"`
	SplitScreen       bool   `json:"split_screen"`
	SplitScreenOnline bool   `json:"split_screen_online"`
	OfflineCoopMax    *int   `json:"offline_coop_max"`
	OfflineMax        *int   `json:"offline_max"`
	OnlineCoopMax     *int   `json:"online_coop_max"`
	OnlineMax         *int   `json:"online_max"`
}

func multiplayerModesJSON(modes []game.MultiplayerMode) ([]byte, error) {
	payload := make([]multiplayerModeJSON, len(modes))
	for i, mode := range modes {
		row := multiplayerModeJSON{
			PlatformName:      mode.PlatformName,
			CampaignCoop:      mode.CampaignCoop,
			DropIn:            mode.DropIn,
			LanCoop:           mode.LanCoop,
			OfflineCoop:       mode.OfflineCoop,
			OnlineCoop:        mode.OnlineCoop,
			SplitScreen:       mode.SplitScreen,
			SplitScreenOnline: mode.SplitScreenOnline,
			OfflineCoopMax:    mode.OfflineCoopMax,
			OfflineMax:        mode.OfflineMax,
			OnlineCoopMax:     mode.OnlineCoopMax,
			OnlineMax:         mode.OnlineMax,
		}
		if mode.PlatformIGDBID > 0 {
			platformID := mode.PlatformIGDBID
			row.PlatformIGDBID = &platformID
		}
		payload[i] = row
	}
	return json.Marshal(payload)
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

func enrichmentModeRows(rows []sqlcgen.EnrichmentMultiplayerModesForGameRow) []game.MultiplayerMode {
	modes := make([]game.MultiplayerMode, 0, len(rows))
	for _, row := range rows {
		modes = append(modes, game.MultiplayerMode{
			PlatformIGDBID:    row.PlatformIgdbID.Int64,
			PlatformName:      row.PlatformName.String,
			CampaignCoop:      row.CampaignCoop,
			DropIn:            row.DropIn,
			LanCoop:           row.LanCoop,
			OfflineCoop:       row.OfflineCoop,
			OnlineCoop:        row.OnlineCoop,
			SplitScreen:       row.SplitScreen,
			SplitScreenOnline: row.SplitScreenOnline,
			OfflineCoopMax:    optionalInt(row.OfflineCoopMax),
			OfflineMax:        optionalInt(row.OfflineMax),
			OnlineCoopMax:     optionalInt(row.OnlineCoopMax),
			OnlineMax:         optionalInt(row.OnlineMax),
		})
	}
	return modes
}

func providerModeRows(rows []sqlcgen.IGDBMultiplayerModesForExternalIDsRow) map[string][]game.MultiplayerMode {
	modes := make(map[string][]game.MultiplayerMode, len(rows))
	for _, row := range rows {
		modes[row.ExternalID] = append(modes[row.ExternalID], game.MultiplayerMode{
			PlatformIGDBID:    row.PlatformIgdbID.Int64,
			PlatformName:      row.PlatformName.String,
			CampaignCoop:      row.CampaignCoop,
			DropIn:            row.DropIn,
			LanCoop:           row.LanCoop,
			OfflineCoop:       row.OfflineCoop,
			OnlineCoop:        row.OnlineCoop,
			SplitScreen:       row.SplitScreen,
			SplitScreenOnline: row.SplitScreenOnline,
			OfflineCoopMax:    optionalInt(row.OfflineCoopMax),
			OfflineMax:        optionalInt(row.OfflineMax),
			OnlineCoopMax:     optionalInt(row.OnlineCoopMax),
			OnlineMax:         optionalInt(row.OnlineMax),
		})
	}
	return modes
}

func optionalInt(value pgtype.Int4) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int32)
	return &result
}

// CoverImageKnown reports whether any synchronized provider record or local
// enrichment snapshot references imageID, so the cover proxy only ever
// fetches covers LANsurfer itself renders.
func (s *Store) CoverImageKnown(ctx context.Context, imageID string) (bool, error) {
	known, err := s.queries.CoverImageKnown(ctx, imageID)
	if err != nil {
		return false, fmt.Errorf("look up IGDB cover image: %w", err)
	}
	return known, nil
}
