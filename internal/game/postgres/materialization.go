package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/game/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

const (
	// These mirror the games.name, games.slug, and game_aliases.alias CHECK
	// constraints in the migrations this package owns. Materialization must
	// satisfy them itself: unlike the administrator-facing catalog service it
	// never rejects a provider record, it bounds or skips what it copies.
	materializedNameLimit  = 120
	materializedSlugLimit  = 120
	materializedAliasLimit = 120

	// maxMaterializationAttempts bounds retries after a uniqueness race with
	// a concurrent write. Slug selection is deterministic, so one retry is
	// always enough to converge; the extra attempts only absorb an unlikely
	// pile-up of unrelated administrator writes.
	maxMaterializationAttempts = 3
)

// Materialize turns an IGDB provider reference into the one authoritative
// local Game for it, creating that Game on first use (spec section 9).
//
// Only the provider reference is trusted input. The transaction reloads the
// provider row from the active generation itself, so a caller cannot inject a
// name, slug, or any other provider metadata into the local catalog, and a
// provider record that is not currently offered is never fabricated into a
// Game.
//
// Materialization deliberately does not reuse the administrator-facing
// Service/Repository create path: that path reports ErrAlreadyExists on a
// display-name or slug collision, which would wrongly reject two legitimate
// provider records that share a display name. Nothing here matches by name.
func (s *Store) Materialize(ctx context.Context, reference game.ProviderReference) (game.Game, error) {
	normalized, err := game.NewProviderReference(reference.Provider, reference.ExternalID)
	if err != nil {
		return game.Game{}, err
	}
	igdbID, err := strconv.ParseInt(normalized.ExternalID, 10, 64)
	if err != nil {
		return game.Game{}, game.ErrInvalidProviderReference
	}
	// The mapping key, the slug suffix, and the numeric provider key must all
	// describe one identity. Deriving the string form back from the parsed
	// integer guarantees that locally, whatever the caller passed in: a padded
	// "0100" must never map provider record 100 to a second local Game.
	canonicalID := strconv.FormatInt(igdbID, 10)

	for range maxMaterializationAttempts {
		materialized, err := s.materializeIGDBGame(ctx, canonicalID, igdbID)
		if err == nil {
			return materialized, nil
		}
		if !isUniqueViolation(err) {
			return game.Game{}, err
		}
		// PostgreSQL only raises a uniqueness violation once the conflicting
		// transaction has committed (an in-flight insert blocks instead), and
		// our own transaction has already rolled back by now. A fresh read
		// therefore observes the committed winner rather than a stale or
		// empty snapshot.
		winner, found, lookupErr := findMaterializedIGDBGame(ctx, s.queries, canonicalID)
		if lookupErr != nil {
			return game.Game{}, lookupErr
		}
		if found {
			return winner, nil
		}
		// The conflict came from some other Game claiming the slug we chose,
		// not from this provider record. Retry so slug selection observes the
		// now-taken slug and falls through to the deterministic suffix.
	}
	return game.Game{}, fmt.Errorf("materialize IGDB game %s: repeated uniqueness conflicts", canonicalID)
}

// materializeIGDBGame performs one attempt inside a single transaction. The
// default Read Committed isolation is deliberate: correctness rests on the
// unique (lower(provider), external_id) index, which is enforced regardless of
// isolation level, rather than on serialization failures the caller would then
// have to retry differently.
func (s *Store) materializeIGDBGame(ctx context.Context, externalID string, igdbID int64) (game.Game, error) {
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return game.Game{}, fmt.Errorf("begin IGDB materialization transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := s.queries.WithTx(transaction)

	existing, found, err := findMaterializedIGDBGame(ctx, queries, externalID)
	if err != nil {
		return game.Game{}, err
	}
	if found {
		if err := queries.UpsertIGDBEnrichmentForExternalID(ctx, externalID); err != nil {
			return game.Game{}, fmt.Errorf("refresh IGDB enrichment for %s: %w", externalID, err)
		}
		if err := queries.ClearIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
			return game.Game{}, fmt.Errorf("clear IGDB multiplayer modes for %s: %w", externalID, err)
		}
		if err := queries.InsertIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
			return game.Game{}, fmt.Errorf("insert IGDB multiplayer modes for %s: %w", externalID, err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return game.Game{}, fmt.Errorf("commit IGDB materialization: %w", err)
		}
		return existing, nil
	}

	// A provider record that dropped out of the active generation between
	// this read and the commit is harmless: the local Game stays
	// authoritative and the next generation swap archives the identity.
	providerRow, err := queries.ActiveIGDBProviderGame(ctx, igdbID)
	if errors.Is(err, pgx.ErrNoRows) {
		return game.Game{}, game.ErrProviderRecordNotFound
	}
	if err != nil {
		return game.Game{}, fmt.Errorf("load active IGDB provider game %s: %w", externalID, err)
	}

	name, err := materializedGameName(providerRow.Name)
	if err != nil {
		return game.Game{}, fmt.Errorf("materialize IGDB game %s: %w", externalID, err)
	}
	slug, err := materializedGameSlug(ctx, queries, providerRow.Slug, providerRow.Name, externalID)
	if err != nil {
		return game.Game{}, err
	}

	created, err := queries.CreateGame(ctx, sqlcgen.CreateGameParams{Name: name, Slug: slug})
	if err != nil {
		return game.Game{}, fmt.Errorf("create local game for IGDB %s: %w", externalID, err)
	}
	createdID, err := parseUUID(created.ID)
	if err != nil {
		return game.Game{}, fmt.Errorf("parse created game ID %s: %w", created.ID, err)
	}
	if aliases := materializedAliases(providerRow.AlternativeNames); len(aliases) > 0 {
		if err := queries.CreateMaterializedGameAliases(ctx, sqlcgen.CreateMaterializedGameAliasesParams{
			GameID:  createdID,
			Aliases: aliases,
		}); err != nil {
			return game.Game{}, fmt.Errorf("copy aliases for IGDB game %s: %w", externalID, err)
		}
	}
	if err := queries.CreateIGDBExternalID(ctx, sqlcgen.CreateIGDBExternalIDParams{
		GameID:     createdID,
		ExternalID: externalID,
	}); err != nil {
		return game.Game{}, fmt.Errorf("map IGDB external ID %s: %w", externalID, err)
	}
	if err := queries.UpsertIGDBEnrichmentForExternalID(ctx, externalID); err != nil {
		return game.Game{}, fmt.Errorf("store IGDB enrichment for %s: %w", externalID, err)
	}
	if err := queries.ClearIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
		return game.Game{}, fmt.Errorf("clear IGDB multiplayer modes for %s: %w", externalID, err)
	}
	if err := queries.InsertIGDBEnrichmentMultiplayerModesForExternalID(ctx, externalID); err != nil {
		return game.Game{}, fmt.Errorf("insert IGDB multiplayer modes for %s: %w", externalID, err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return game.Game{}, fmt.Errorf("commit IGDB materialization: %w", err)
	}
	return fromGame(created.ID, created.Name, created.Slug, created.Active, created.CreatedAt, created.UpdatedAt), nil
}

// findMaterializedIGDBGame reads the local Game an IGDB external ID already
// maps to, clearing the archive tombstone when the provider record is offered
// again (spec section 9 step 2). Aliases are not loaded; like the catalog
// create path, only List hydrates them.
func findMaterializedIGDBGame(ctx context.Context, queries *sqlcgen.Queries, externalID string) (game.Game, bool, error) {
	row, err := queries.FindIGDBMaterializedGame(ctx, externalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return game.Game{}, false, nil
	}
	if err != nil {
		return game.Game{}, false, fmt.Errorf("load materialized IGDB game %s: %w", externalID, err)
	}
	if row.Archived {
		if err := queries.RestoreIGDBExternalID(ctx, externalID); err != nil {
			return game.Game{}, false, fmt.Errorf("restore IGDB external ID %s: %w", externalID, err)
		}
	}
	return fromGame(row.ID, row.Name, row.Slug, row.Active, row.CreatedAt, row.UpdatedAt), true, nil
}

// materializedGameName bounds the current provider display name to the
// games.name CHECK. Duplicate display names are expected and allowed: the
// global case-insensitive unique name index was dropped precisely so two
// provider records that share a name can become two local Games.
func materializedGameName(providerName string) (string, error) {
	name := strings.TrimSpace(providerName)
	if utf8.RuneCountInString(name) > materializedNameLimit {
		name = strings.TrimSpace(string([]rune(name)[:materializedNameLimit]))
	}
	if name == "" {
		return "", errors.New("provider record has no usable name")
	}
	return name, nil
}

// materializedGameSlug picks the local slug: the provider's own slug when it
// is valid and unused, otherwise the deterministic suffixed slug. It never
// adopts an existing Game by slug; a taken slug only ever moves selection on.
func materializedGameSlug(ctx context.Context, queries *sqlcgen.Queries, providerSlug, providerName, externalID string) (string, error) {
	preferred := strings.TrimSpace(providerSlug)
	if game.ValidSlug(preferred) {
		taken, err := queries.GameSlugTaken(ctx, preferred)
		if err != nil {
			return "", fmt.Errorf("check local slug %s: %w", preferred, err)
		}
		if !taken {
			return preferred, nil
		}
	}
	fallback := suffixedProviderSlug(providerName, externalID)
	taken, err := queries.GameSlugTaken(ctx, fallback)
	if err != nil {
		return "", fmt.Errorf("check local slug %s: %w", fallback, err)
	}
	if taken {
		return "", fmt.Errorf("materialize IGDB game %s: deterministic slug %s is already in use", externalID, fallback)
	}
	return fallback, nil
}

// suffixedProviderSlug builds the deterministic fallback slug. The
// "igdb-<external ID>" suffix is what makes it unique, so only the name-derived
// base is truncated to stay inside the 120-character games.slug CHECK. Two
// provider records sharing a display name (and therefore a base, and often a
// provider slug) still produce two distinct local slugs.
func suffixedProviderSlug(providerName, externalID string) string {
	suffix := "igdb-" + externalID
	base := slugifyProviderName(providerName)
	budget := materializedSlugLimit - len(suffix) - len("-")
	if base == "" || budget < 1 {
		return suffix
	}
	if len(base) > budget {
		base = strings.Trim(base[:budget], "-")
	}
	if base == "" {
		return suffix
	}
	return base + "-" + suffix
}

// slugifyProviderName reduces a provider display name to the ASCII
// lowercase/digit/hyphen shape games.slug requires. Characters outside that
// set collapse into single separators, which is why the suffix rather than
// the base is what guarantees uniqueness.
func slugifyProviderName(value string) string {
	var builder strings.Builder
	separated := false
	for _, character := range strings.ToLower(value) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
			separated = false
			continue
		}
		if builder.Len() > 0 && !separated {
			builder.WriteByte('-')
			separated = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// materializedAliases keeps the provider alternative names that satisfy local
// alias validation and deduplicates them the way the
// (game_id, lower(btrim(alias))) unique index does. Over-long or empty values
// are skipped rather than truncated, and an alias that still conflicts is
// absorbed by ON CONFLICT DO NOTHING, so copying aliases can never abort
// materialization.
func materializedAliases(values []string) []string {
	aliases := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		alias := strings.TrimSpace(value)
		if alias == "" || utf8.RuneCountInString(alias) > materializedAliasLimit {
			continue
		}
		key := strings.ToLower(alias)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, alias)
	}
	return aliases
}
