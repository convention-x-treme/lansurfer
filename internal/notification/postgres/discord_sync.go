package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) GetDiscordMessageSync(ctx context.Context, destinationID, gameRequestID string) (notification.DiscordMessageSync, error) {
	destination, err := parseUUID(destinationID)
	if err != nil {
		return notification.DiscordMessageSync{}, fmt.Errorf("parse discord destination identity: %w", err)
	}
	request, err := parseUUID(gameRequestID)
	if err != nil {
		return notification.DiscordMessageSync{}, fmt.Errorf("parse game request identity: %w", err)
	}
	row, err := s.queries.GetDiscordMessageSync(ctx, sqlcgen.GetDiscordMessageSyncParams{DestinationID: destination, GameRequestID: request})
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.DiscordMessageSync{}, notification.ErrDiscordMessageSyncNotFound
	}
	if err != nil {
		return notification.DiscordMessageSync{}, fmt.Errorf("get discord message sync: %w", err)
	}
	return notification.DiscordMessageSync{
		DestinationID: row.DestinationID, GameRequestID: row.GameRequestID,
		MessageID: textString(row.MessageID), AppliedVersion: row.AppliedVersion,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

// AdvanceDiscordMessageSync uses one conditional PostgreSQL UPSERT. A stale
// or duplicate applied version cannot replace a stored message reference;
// the sole equal-version exception fills a reference cleared for recovery.
func (s *Store) AdvanceDiscordMessageSync(ctx context.Context, sync notification.DiscordMessageSync) (bool, error) {
	destination, err := parseUUID(sync.DestinationID)
	if err != nil {
		return false, fmt.Errorf("parse discord destination identity: %w", err)
	}
	request, err := parseUUID(sync.GameRequestID)
	if err != nil {
		return false, fmt.Errorf("parse game request identity: %w", err)
	}
	rows, err := s.queries.AdvanceDiscordMessageSync(ctx, sqlcgen.AdvanceDiscordMessageSyncParams{
		DestinationID: destination, GameRequestID: request,
		MessageID: pgtype.Text{String: sync.MessageID, Valid: true}, AppliedVersion: sync.AppliedVersion,
	})
	if err != nil {
		return false, fmt.Errorf("advance discord message sync: %w", err)
	}
	return rows == 1, nil
}

// ClearDiscordMessageSync is a compare-and-clear on both the message ID and
// version. Recovery retains the highest applied version; a later Advance at
// the same version may fill the cleared reference exactly once.
func (s *Store) ClearDiscordMessageSync(ctx context.Context, destinationID, gameRequestID, messageID string, appliedVersion int64) (bool, error) {
	destination, err := parseUUID(destinationID)
	if err != nil {
		return false, fmt.Errorf("parse discord destination identity: %w", err)
	}
	request, err := parseUUID(gameRequestID)
	if err != nil {
		return false, fmt.Errorf("parse game request identity: %w", err)
	}
	rows, err := s.queries.ClearDiscordMessageSync(ctx, sqlcgen.ClearDiscordMessageSyncParams{
		DestinationID: destination, GameRequestID: request,
		MessageID: pgtype.Text{String: messageID, Valid: true}, AppliedVersion: appliedVersion,
	})
	if err != nil {
		return false, fmt.Errorf("clear discord message sync: %w", err)
	}
	return rows == 1, nil
}

// WithDiscordMessageSyncLock holds a PostgreSQL transaction-scoped lock across
// the caller's current-state read, external create/edit, and sync write.
// Equal destination/request pairs serialize even before a sync row exists.
// The callback's repository executes on the same transaction.
func (s *Store) WithDiscordMessageSyncLock(ctx context.Context, destinationID, gameRequestID string, apply func(notification.DiscordMessageSyncRepository) error) error {
	destination, err := parseUUID(destinationID)
	if err != nil {
		return fmt.Errorf("parse discord destination identity: %w", err)
	}
	request, err := parseUUID(gameRequestID)
	if err != nil {
		return fmt.Errorf("parse game request identity: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin discord sync transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text || ':' || $2::uuid::text, 0))`, destination, request); err != nil {
		return fmt.Errorf("lock discord message sync: %w", err)
	}
	if err := apply(&Store{queries: s.queries.WithTx(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit discord sync transaction: %w", err)
	}
	return nil
}
