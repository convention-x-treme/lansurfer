package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	"github.com/convention-x-treme/lansurfer/internal/platform/legal"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
)

// DemoResetResult summarizes one demo reset.
type DemoResetResult struct {
	TruncatedTables []string
	LegalDocuments  []legal.Kind
}

// ResetDemo empties a demo database and re-applies the legal texts from
// legalDir (terms.md, privacy.md, and imprint.md when the imprint is enabled).
// It refuses outside demo mode so it can never empty a production database,
// and it validates every legal file before touching data. The HTTP process
// must be stopped meanwhile; restarting it re-initializes in-memory state.
func ResetDemo(ctx context.Context, cfg config.Config, legalDir string, logger *slog.Logger) (DemoResetResult, error) {
	if cfg.Demo == nil {
		return DemoResetResult{}, errors.New("demo reset refused: LANSURFER_DEMO_MODE is not true")
	}
	kinds := []legal.Kind{legal.Terms, legal.Privacy}
	if cfg.ImprintEnabled {
		kinds = append(kinds, legal.Imprint)
	}
	bodies := make(map[legal.Kind]string, len(kinds))
	for _, kind := range kinds {
		path := filepath.Join(legalDir, string(kind)+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			return DemoResetResult{}, fmt.Errorf("read legal file: %w", err)
		}
		body, err := legal.Normalize(string(data))
		if err != nil {
			return DemoResetResult{}, fmt.Errorf("legal file %s: %w", path, err)
		}
		bodies[kind] = body
	}

	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return DemoResetResult{}, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return DemoResetResult{}, fmt.Errorf("apply database migrations: %w", err)
	}

	truncated, err := platformpostgres.TruncateForDemoReset(ctx, pool)
	if err != nil {
		return DemoResetResult{}, err
	}
	documents := legal.NewStore(pool)
	for _, kind := range kinds {
		if err := documents.Save(ctx, kind, bodies[kind]); err != nil {
			return DemoResetResult{}, fmt.Errorf("save legal document %s: %w", kind, err)
		}
	}
	return DemoResetResult{TruncatedTables: truncated, LegalDocuments: kinds}, nil
}
