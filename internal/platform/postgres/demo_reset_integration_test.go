//go:build integration

package postgres

import (
	"slices"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/platform/legal"
	"github.com/jackc/pgx/v5"
)

func TestDemoResetEmptiesAllButKeptTables(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	var requestID, actorID string
	if err := pool.QueryRow(t.Context(), `
		WITH actor AS (INSERT INTO users(game_nick) VALUES ('DemoHost') RETURNING id),
		game AS (INSERT INTO games(name, slug) VALUES ('DemoGame', 'demo-game') RETURNING id),
		request AS (INSERT INTO game_requests(game_id, creator_id, starts_at) SELECT game.id, actor.id, now() FROM game, actor RETURNING id, creator_id)
		SELECT id::text, creator_id::text FROM request`).Scan(&requestID, &actorID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO game_request_activity(game_request_id, actor_id, kind) VALUES ('` + requestID + `', '` + actorID + `', 'created')`,
		`INSERT INTO igdb_sync_generations(status) VALUES ('syncing')`,
		`INSERT INTO metrics_access_key(singleton, key_hash) VALUES (true, decode(repeat('ab', 32), 'hex'))`,
	} {
		if _, err := pool.Exec(t.Context(), statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}

	truncated, err := TruncateForDemoReset(t.Context(), pool)
	if err != nil {
		t.Fatalf("TruncateForDemoReset() error = %v", err)
	}
	for _, table := range []string{"users", "games", "game_requests", "game_request_activity"} {
		if !slices.Contains(truncated, table) {
			t.Errorf("truncated %v lacks %s", truncated, table)
		}
	}

	rows, err := pool.Query(t.Context(), `SELECT tablename FROM pg_tables WHERE schemaname = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range DemoResetKeptTables {
		if !slices.Contains(tables, kept) {
			t.Errorf("kept table %s does not exist; update DemoResetKeptTables", kept)
		}
	}
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(DemoResetKeptTables, table) && count != 0 {
			t.Errorf("%s has %d rows after reset, want 0", table, count)
		}
		seeded := []string{"goose_db_version", "igdb_sync_generations", "legal_documents", "metrics_access_key"}
		if slices.Contains(seeded, table) && count == 0 {
			t.Errorf("kept table %s lost its rows", table)
		}
	}

	// The legal texts are re-applied after truncation; the seeded rows must
	// still exist for Save to update them.
	documents := legal.NewStore(pool)
	if err := documents.Save(t.Context(), legal.Privacy, "Demo privacy"); err != nil {
		t.Fatalf("Save() after reset error = %v", err)
	}
	if document, err := documents.Document(t.Context(), legal.Privacy); err != nil || document.Sample || document.Body != "Demo privacy" {
		t.Errorf("privacy after reset = %+v, %v", document, err)
	}
}

// TRUNCATE ... CASCADE empties every table referencing a truncated one, so a
// kept table referencing a truncated table would silently lose its rows.
func TestDemoResetKeptTablesReferenceOnlyKeptTables(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	rows, err := pool.Query(t.Context(), `
		SELECT source.relname || ' -> ' || target.relname
		FROM pg_constraint constraint_row
		JOIN pg_class source ON source.oid = constraint_row.conrelid
		JOIN pg_class target ON target.oid = constraint_row.confrelid
		WHERE constraint_row.contype = 'f'
		  AND source.relnamespace = current_schema()::regnamespace
		  AND source.relname = ANY($1) AND NOT (target.relname = ANY($1))`, DemoResetKeptTables)
	if err != nil {
		t.Fatal(err)
	}
	references, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 0 {
		t.Errorf("kept tables reference truncated tables: %v", references)
	}
}
