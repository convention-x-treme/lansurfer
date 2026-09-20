// Package postgres provides PostgreSQL persistence for the notification module.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: sqlcgen.New(pool)}
}

// InitializeBaseline runs once, ever, on the first successful call: it marks
// the singleton notification_baseline row and, in the same transaction,
// materializes every already-visible origin=live activity row as baseline
// (never-deliverable) work. Because the marker insert and the baseline scan
// share one transaction, a crash between them leaves neither committed, so a
// retried call after a crash safely redoes the whole thing rather than
// leaving some pre-existing history undiscovered and eligible for delivery.
// The composition root calls this exactly once, synchronously, before the
// HTTP server or the Worker starts, which also means no new live activity
// can be committed concurrently with this scan.
func (s *Store) InitializeBaseline(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin baseline transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	inserted, err := queries.InsertBaselineMarker(ctx)
	if err != nil {
		return fmt.Errorf("insert baseline marker: %w", err)
	}
	if inserted == 0 {
		return nil // an earlier successful call already initialized the baseline
	}
	if err := queries.MaterializeBaselineWork(ctx); err != nil {
		return fmt.Errorf("materialize baseline work: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit baseline transaction: %w", err)
	}
	return nil
}

// DiscoverActivity finds live game_request_activity rows that have not yet
// been materialized into notification_work, via a NOT EXISTS anti-join on
// activity_sequence rather than a high-watermark cursor. sequence values are
// assigned at insert time, not commit time, so a transaction that inserted
// at a lower sequence can still commit after one that inserted at a higher
// sequence; a cursor advanced past the higher sequence would then skip the
// lower one forever, while the anti-join finds it on the next call.
func (s *Store) DiscoverActivity(ctx context.Context, limit int) ([]notification.Activity, error) {
	rows, err := s.queries.DiscoverNewActivity(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("discover new activity: %w", err)
	}
	activities := make([]notification.Activity, 0, len(rows))
	for _, row := range rows {
		activities = append(activities, notification.Activity{
			Sequence: row.Sequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
			Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
		})
	}
	return activities, nil
}

// MaterializeWork inserts one notification_work row for activity and zero or
// more notification_delivery rows beneath it, all in one transaction. If the
// work row already exists (a concurrent or repeated discovery pass), it
// leaves the existing row and its deliveries untouched rather than
// duplicating them.
func (s *Store) MaterializeWork(ctx context.Context, activity notification.Activity, targets []notification.Target) error {
	gameRequestID, err := parseUUID(activity.GameRequestID)
	if err != nil {
		return fmt.Errorf("parse game request ID: %w", err)
	}
	actorID, err := parseUUID(activity.ActorID)
	if err != nil {
		return fmt.Errorf("parse actor ID: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin materialize work transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)

	workID, err := queries.InsertNotificationWork(ctx, sqlcgen.InsertNotificationWorkParams{
		ActivitySequence: activity.Sequence, GameRequestID: gameRequestID, ActorID: actorID,
		Kind: string(activity.Kind), OccurredAt: pgtype.Timestamptz{Time: activity.OccurredAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // another materialization already handled this activity row
	}
	if err != nil {
		return fmt.Errorf("insert notification work: %w", err)
	}
	for _, target := range targets {
		if err := queries.InsertNotificationDelivery(ctx, sqlcgen.InsertNotificationDeliveryParams{
			WorkID: workID, Transport: target.Transport, TargetKey: target.Key,
		}); err != nil {
			return fmt.Errorf("insert notification delivery: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit materialize work transaction: %w", err)
	}
	return nil
}

// ClaimDueDeliveries atomically claims up to limit deliveries that are
// either newly pending and due, or claimed by an earlier lease that has
// since expired (a crashed or stalled worker), and extends their lease to
// leaseUntil. The underlying query's FOR UPDATE SKIP LOCKED row-selection
// runs inside one implicit statement-level transaction, so concurrent
// callers never see or wait on rows another in-flight call has already
// locked: each one claims a disjoint set of rows, or none if nothing is
// left.
func (s *Store) ClaimDueDeliveries(ctx context.Context, limit int, leaseUntil time.Time) ([]notification.ClaimedDelivery, error) {
	rows, err := s.queries.ClaimDueDeliveries(ctx, sqlcgen.ClaimDueDeliveriesParams{
		RowLimit: int32(limit), ClaimedUntil: pgtype.Timestamptz{Time: leaseUntil, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("claim due deliveries: %w", err)
	}
	claimed := make([]notification.ClaimedDelivery, 0, len(rows))
	for _, row := range rows {
		claimed = append(claimed, notification.ClaimedDelivery{
			DeliveryID:   row.ID,
			Target:       notification.Target{Transport: row.Transport, Key: row.TargetKey},
			AttemptCount: row.AttemptCount,
			Activity: notification.Activity{
				Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
				Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
			},
		})
	}
	return claimed, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func nullableText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
