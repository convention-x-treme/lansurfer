// Package postgres provides PostgreSQL infrastructure for LANsurfer.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	initialRetryDelay = 250 * time.Millisecond
	maximumRetryDelay = 5 * time.Second
	pingTimeout       = 3 * time.Second
)

// Connect creates and verifies a PostgreSQL pool, retrying until ctx expires.
func Connect(ctx context.Context, databaseURL string, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	retryDelay := initialRetryDelay
	for attempt := 1; ; attempt++ {
		pool, connectErr := pgxpool.NewWithConfig(ctx, poolConfig.Copy())
		if connectErr == nil {
			pingCtx, cancelPing := context.WithTimeout(ctx, pingTimeout)
			connectErr = pool.Ping(pingCtx)
			cancelPing()
			if connectErr == nil {
				logger.Info("connected to PostgreSQL", "attempt", attempt)
				return pool, nil
			}
			pool.Close()
		}

		logger.Warn("PostgreSQL is not ready", "attempt", attempt, "retry_in", retryDelay, "error", connectErr)

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("wait for PostgreSQL: %w", ctx.Err())
		case <-timer.C:
		}

		retryDelay = min(retryDelay*2, maximumRetryDelay)
	}
}
