package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DemoResetKeptTables survive a demo reset. Everything else is truncated, so
// tables added by future migrations reset by default instead of leaking data
// across days. No kept table may reference a truncated one: CASCADE would
// empty it (pinned by the integration test).
var DemoResetKeptTables = []string{
	"goose_db_version",
	// IGDB provider index and sync state avoid a daily baseline resync.
	"igdb_game_index",
	"igdb_game_index_multiplayer_modes",
	"igdb_provider_state",
	"igdb_sync_generations",
	// The rows are seeded by migration and overwritten from files after the
	// truncation; legal.Store.Save only updates existing rows.
	"legal_documents",
	// Rotation is blocked in demo mode; keeps the operator's scrape working.
	"metrics_access_key",
	// Guest Access changes are blocked in demo mode; the operator's enabled
	// flag and invite survive so the shared link keeps working after a reset.
	"auth_guest_access",
	// Legacy prototype table, empty on a demo instance.
	"events",
}

// TruncateForDemoReset empties every base table in the current schema except
// DemoResetKeptTables in one transaction and returns the truncated names. It
// is an operator command for a demo database, reachable only through
// app.ResetDemo, and never runs inside the HTTP process.
func TruncateForDemoReset(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin demo reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = current_schema() AND NOT (tablename = ANY($1))
		ORDER BY tablename`, DemoResetKeptTables)
	if err != nil {
		return nil, fmt.Errorf("list demo reset tables: %w", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list demo reset tables: %w", err)
	}
	if len(tables) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(tables))
	for i, table := range tables {
		quoted[i] = pgx.Identifier{table}.Sanitize()
	}
	if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(quoted, ", ")+" RESTART IDENTITY CASCADE"); err != nil {
		return nil, fmt.Errorf("truncate demo tables: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit demo reset: %w", err)
	}
	return tables, nil
}
