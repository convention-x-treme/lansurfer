package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
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
	if err := apply(&Store{queries: s.queries.WithTx(tx), tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit discord sync transaction: %w", err)
	}
	return nil
}

// These reads run through the transaction-bound Store passed by
// WithDiscordMessageSyncLock, never through the pool.
func (s *Store) GetGameRequestSummary(ctx context.Context, requestID string) (notification.GameRequestSummary, error) {
	if s.tx == nil {
		return notification.GameRequestSummary{}, errors.New("discord summary read requires sync transaction")
	}
	summary, err := gamerequestquery.NewStoreWithTx(s.tx).GetGameRequestSummary(ctx, requestID)
	if err != nil {
		return notification.GameRequestSummary{}, fmt.Errorf("get discord current request: %w", err)
	}
	return notification.GameRequestSummary{
		RequestID: summary.RequestID, GameID: summary.GameID, GameName: summary.GameName,
		StartsAt: summary.StartsAt, Status: string(summary.Status), MemberCount: summary.MemberCount,
		MaxPlayers: summary.MaxPlayers, CreatorGameNick: summary.CreatorGameNick, Description: summary.Description,
	}, nil
}

func (s *Store) ClaimedDiscordDelivery(ctx context.Context, deliveryID, sequence int64, gameRequestID string, target notification.Target) (int64, error) {
	id, err := parseUUID(gameRequestID)
	if err != nil {
		return 0, fmt.Errorf("parse game request identity: %w", err)
	}
	claimed, err := s.queries.ClaimedDiscordDelivery(ctx, sqlcgen.ClaimedDiscordDeliveryParams{
		DeliveryID: deliveryID, ActivitySequence: sequence, GameRequestID: id, TargetKey: target.Key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get claimed discord delivery: %w", err)
	}
	return claimed, nil
}

func (s *Store) NewerDiscordDelivery(ctx context.Context, sequence int64, gameRequestID string, target notification.Target) (int64, error) {
	id, err := parseUUID(gameRequestID)
	if err != nil {
		return 0, fmt.Errorf("parse game request identity: %w", err)
	}
	version, err := s.queries.NewerDiscordDelivery(ctx, sqlcgen.NewerDiscordDeliveryParams{
		ActivitySequence: sequence, GameRequestID: id, TargetKey: target.Key,
	})
	if err != nil {
		return 0, fmt.Errorf("get newer discord delivery: %w", err)
	}
	return version, nil
}

// ReserveDiscordCreate commits on its own pooled connection, never on the
// sync transaction: the reservation must be durable before Discord sees the
// create. False means an earlier create outcome is still unrecorded.
func (s *Store) ReserveDiscordCreate(ctx context.Context, destinationID, gameRequestID string, deliveryID int64) (bool, error) {
	if s.tx != nil {
		return false, errors.New("discord create reservation must commit outside the sync transaction")
	}
	destination, request, err := parseDiscordSyncKey(destinationID, gameRequestID)
	if err != nil {
		return false, err
	}
	rows, err := s.queries.ReserveDiscordCreate(ctx, sqlcgen.ReserveDiscordCreateParams{DestinationID: destination, GameRequestID: request, OwnerDeliveryID: deliveryID})
	if err != nil {
		return false, fmt.Errorf("reserve discord create: %w", err)
	}
	return rows == 1, nil
}

// ReleaseDiscordCreate runs on the sync transaction, so the release commits
// atomically with the recorded message ID or proven provider rejection.
func (s *Store) ReleaseDiscordCreate(ctx context.Context, destinationID, gameRequestID string, deliveryID int64) (bool, error) {
	destination, request, err := parseDiscordSyncKey(destinationID, gameRequestID)
	if err != nil {
		return false, err
	}
	rows, err := s.queries.ReleaseDiscordCreate(ctx, sqlcgen.ReleaseDiscordCreateParams{DestinationID: destination, GameRequestID: request, OwnerDeliveryID: deliveryID})
	if err != nil {
		return false, fmt.Errorf("release discord create: %w", err)
	}
	return rows == 1, nil
}

// ReopenUncertainDiscordDeliveryWithReservation atomically rechecks the
// connected generation and claim; a reservation can be overridden only by
// its owning delivery. No-reservation Discord retries use this same boundary.
func (s *Store) ReopenUncertainDiscordDeliveryWithReservation(ctx context.Context, deliveryID int64, destinationID string) error {
	destination, err := parseUUID(destinationID)
	if err != nil {
		return fmt.Errorf("parse discord destination identity: %w", err)
	}
	row, err := s.queries.ReopenUncertainDiscordDeliveryWithReservation(ctx, sqlcgen.ReopenUncertainDiscordDeliveryWithReservationParams{
		ID: deliveryID, DestinationID: destination,
	})
	if err != nil {
		return fmt.Errorf("reopen uncertain discord delivery with reservation: %w", err)
	}
	if !row.GenerationMatches {
		return notification.ErrNoLongerEligible
	}
	if row.ReservationFound && row.DeliveryUncertain && !row.Reopened {
		return notification.ErrDiscordReservationNotOwned
	}
	if !row.Reopened {
		return notification.ErrDeliveryNotUncertain
	}
	return nil
}

func parseDiscordSyncKey(destinationID, gameRequestID string) (pgtype.UUID, pgtype.UUID, error) {
	destination, err := parseUUID(destinationID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("parse discord destination identity: %w", err)
	}
	request, err := parseUUID(gameRequestID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("parse game request identity: %w", err)
	}
	return destination, request, nil
}
