package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

// ListUsers returns local accounts for operator administration.
func ListUsers(ctx context.Context, cfg config.Config, logger *slog.Logger) ([]user.User, error) {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}

	accounts, err := user.NewService(userpostgres.NewStore(pool)).List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return accounts, nil
}

// PromoteAdmin grants the local administrator role to one existing user.
func PromoteAdmin(ctx context.Context, cfg config.Config, userID string, logger *slog.Logger) (user.User, error) {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return user.User{}, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return user.User{}, fmt.Errorf("apply database migrations: %w", err)
	}

	account, err := user.NewService(userpostgres.NewStore(pool)).PromoteToAdmin(ctx, userID)
	if err != nil {
		return user.User{}, fmt.Errorf("promote user: %w", err)
	}
	return account, nil
}
