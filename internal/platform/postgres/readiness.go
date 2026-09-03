package postgres

import (
	"context"

	"github.com/convention-x-treme/lansurfer/internal/platform/postgres/sqlcgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReadyCheck returns a database-backed readiness function.
func ReadyCheck(pool *pgxpool.Pool) func(context.Context) error {
	queries := sqlcgen.New(pool)

	return func(ctx context.Context) error {
		_, err := queries.HealthCheck(ctx)
		return err
	}
}
