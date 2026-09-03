package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationAdvisoryLockID int64 = 0x4c414e5355524645

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Migrate applies all embedded forward migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	return migrateFS(ctx, pool, migrationFS, logger)
}

func migrateFS(ctx context.Context, pool *pgxpool.Pool, migrationFS fs.FS, logger *slog.Logger) error {
	lockConnection, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("open migration lock connection: %w", err)
	}
	defer func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelClose()
		if err := lockConnection.Close(closeCtx); err != nil {
			logger.Warn("close migration lock connection", "error", err)
		}
	}()

	if _, err := lockConnection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancelUnlock := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnlock()
		if _, err := lockConnection.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID); err != nil {
			logger.Warn("release migration advisory lock", "error", err)
		}
	}()

	database := stdlib.OpenDBFromPool(pool)
	defer database.Close()

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		database,
		migrationFS,
		goose.WithSlog(logger),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply forward migrations: %w", err)
	}
	logger.Info("database migrations complete", "applied", len(results))

	return nil
}
