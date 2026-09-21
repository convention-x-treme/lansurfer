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

// RecordSuccess marks one delivery succeeded, releasing its claim lease.
func (s *Store) RecordSuccess(ctx context.Context, deliveryID int64) error {
	if err := s.queries.RecordDeliverySuccess(ctx, deliveryID); err != nil {
		return fmt.Errorf("record delivery success: %w", err)
	}
	return nil
}

// RecordRetry marks one delivery pending again, scheduled for nextAttemptAt,
// and increments its attempt count.
func (s *Store) RecordRetry(ctx context.Context, deliveryID int64, nextAttemptAt time.Time, lastError string) error {
	if err := s.queries.RecordDeliveryRetry(ctx, sqlcgen.RecordDeliveryRetryParams{
		ID: deliveryID, NextAttemptAt: pgtype.Timestamptz{Time: nextAttemptAt, Valid: true}, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery retry: %w", err)
	}
	return nil
}

// RecordExhausted marks one delivery exhausted (all automatic attempts
// used) and increments its attempt count.
func (s *Store) RecordExhausted(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryExhausted(ctx, sqlcgen.RecordDeliveryExhaustedParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery exhausted: %w", err)
	}
	return nil
}

// RecordPermanent marks one delivery permanently failed. It never touches
// attempt_count: permanent is not a retry outcome.
func (s *Store) RecordPermanent(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryPermanent(ctx, sqlcgen.RecordDeliveryPermanentParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery permanent: %w", err)
	}
	return nil
}

// RecordUncertain marks one delivery uncertain, requiring manual retry. It
// never touches attempt_count and is never automatically reclaimed.
func (s *Store) RecordUncertain(ctx context.Context, deliveryID int64, lastError string) error {
	if err := s.queries.RecordDeliveryUncertain(ctx, sqlcgen.RecordDeliveryUncertainParams{
		ID: deliveryID, LastError: nullableText(lastError),
	}); err != nil {
		return fmt.Errorf("record delivery uncertain: %w", err)
	}
	return nil
}

func (s *Store) RecordSuppressed(ctx context.Context, deliveryID int64, reason string) error {
	if err := s.queries.RecordDeliverySuppressed(ctx, sqlcgen.RecordDeliverySuppressedParams{
		ID: deliveryID, LastError: nullableText(reason),
	}); err != nil {
		return fmt.Errorf("record delivery suppressed: %w", err)
	}
	return nil
}

// ListProblemDeliveries returns every delivery in a terminal problem state
// (exhausted, permanent, uncertain, or suppressed), most recently updated
// first, for authorized diagnostics.
func (s *Store) ListProblemDeliveries(ctx context.Context) ([]notification.ProblemDelivery, error) {
	rows, err := s.queries.ListProblemDeliveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list problem deliveries: %w", err)
	}
	problems := make([]notification.ProblemDelivery, 0, len(rows))
	for _, row := range rows {
		problems = append(problems, notification.ProblemDelivery{
			DeliveryID: row.ID, Target: notification.Target{Transport: row.Transport, Key: row.TargetKey},
			Status: notification.DeliveryStatus(row.Status), AttemptCount: row.AttemptCount,
			LastError: textString(row.LastError), UpdatedAt: row.UpdatedAt.Time,
			Activity: notification.Activity{
				Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
				Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
			},
		})
	}
	return problems, nil
}

// GetDeliveryDetail returns the minimal view Service needs to validate and
// reopen one delivery for manual retry.
func (s *Store) GetDeliveryDetail(ctx context.Context, deliveryID int64) (notification.DeliveryDetail, error) {
	row, err := s.queries.GetDeliveryDetail(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.DeliveryDetail{}, notification.ErrDeliveryNotFound
	}
	if err != nil {
		return notification.DeliveryDetail{}, fmt.Errorf("get delivery detail: %w", err)
	}
	return notification.DeliveryDetail{
		DeliveryID: row.ID, Target: notification.Target{Transport: row.Transport, Key: row.TargetKey},
		Status: notification.DeliveryStatus(row.Status),
		Activity: notification.Activity{
			Sequence: row.ActivitySequence, GameRequestID: row.GameRequestID, ActorID: row.ActorID,
			Kind: notification.ActivityKind(row.Kind), OccurredAt: row.OccurredAt.Time,
		},
	}, nil
}

// ReopenDelivery reopens one exhausted or permanent delivery for a fresh
// automatic retry budget. The UPDATE's WHERE clause enforces the same status
// precondition Service already checked, atomically, so a concurrent status
// change between Service's check and this call can never reopen a delivery
// that is no longer eligible.
func (s *Store) ReopenDelivery(ctx context.Context, deliveryID int64) (bool, error) {
	_, err := s.queries.ReopenDeliveryForManualRetry(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reopen delivery for manual retry: %w", err)
	}
	return true, nil
}

// ReopenUncertainDelivery reopens one uncertain delivery. Its UPDATE only
// matches status = 'uncertain', so it can never reopen an exhausted or
// permanent delivery, symmetrically to how ReopenDelivery can never reopen
// an uncertain one.
func (s *Store) ReopenUncertainDelivery(ctx context.Context, deliveryID int64) (bool, error) {
	_, err := s.queries.ReopenUncertainDeliveryForManualRetry(ctx, deliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reopen uncertain delivery for manual retry: %w", err)
	}
	return true, nil
}

// UpsertSubscription persists sub, atomically reassigning ownership if
// EndpointFingerprint already belongs to a different user (the ON CONFLICT
// clause on the unique endpoint_fingerprint column).
func (s *Store) UpsertSubscription(ctx context.Context, sub notification.PushSubscriptionRecord) (notification.PushSubscriptionRecord, error) {
	userID, err := parseUUID(sub.UserID)
	if err != nil {
		return notification.PushSubscriptionRecord{}, fmt.Errorf("parse user ID: %w", err)
	}
	row, err := s.queries.UpsertPushSubscription(ctx, sqlcgen.UpsertPushSubscriptionParams{
		UserID: userID, Label: sub.Label, DeviceIDHash: sub.DeviceIDHash,
		EndpointFingerprint: sub.EndpointFingerprint, EndpointCiphertext: sub.EndpointCiphertext,
		P256dhCiphertext: sub.P256dhCiphertext, AuthCiphertext: sub.AuthCiphertext,
	})
	if err != nil {
		return notification.PushSubscriptionRecord{}, fmt.Errorf("upsert push subscription: %w", err)
	}
	sub.ID, sub.CreatedAt, sub.UpdatedAt = row.ID, row.CreatedAt.Time, row.UpdatedAt.Time
	return sub, nil
}

// ListSubscriptions returns userID's subscriptions without ciphertext (the
// query never selects it; only UpsertSubscription round-trips it, and only
// SubscriptionService ever decrypts it).
func (s *Store) ListSubscriptions(ctx context.Context, userID string) ([]notification.PushSubscriptionRecord, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.ListPushSubscriptionsByUser(ctx, parsedUserID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	subs := make([]notification.PushSubscriptionRecord, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, notification.PushSubscriptionRecord{
			ID: row.ID, UserID: userID, Label: row.Label, DeviceIDHash: row.DeviceIDHash,
			EndpointFingerprint: row.EndpointFingerprint, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return subs, nil
}

// DeleteSubscription removes one subscription scoped to its owner; the
// WHERE clause's user_id match means a non-owner's delete affects zero rows
// rather than another user's subscription.
func (s *Store) DeleteSubscription(ctx context.Context, subscriptionID int64, userID string) (bool, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return false, fmt.Errorf("parse user ID: %w", err)
	}
	deleted, err := s.queries.DeletePushSubscription(ctx, sqlcgen.DeletePushSubscriptionParams{
		ID: subscriptionID, UserID: parsedUserID,
	})
	if err != nil {
		return false, fmt.Errorf("delete push subscription: %w", err)
	}
	return deleted > 0, nil
}

func (s *Store) DeleteSubscriptionByDeviceHash(ctx context.Context, userID, deviceIDHash string) (bool, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return false, fmt.Errorf("parse user ID: %w", err)
	}
	deleted, err := s.queries.DeletePushSubscriptionByDeviceHash(ctx, sqlcgen.DeletePushSubscriptionByDeviceHashParams{
		UserID: parsedUserID, DeviceIDHash: deviceIDHash,
	})
	if err != nil {
		return false, fmt.Errorf("delete push subscription by device hash: %w", err)
	}
	return deleted > 0, nil
}

// GetPreference returns the zero-value PushPreference's intended default
// (disabled, followed-only) when userID has no stored row yet. It returns
// that default explicitly rather than a bare struct literal because
// PushPreference's Go zero value for NewRequestAudience is "", not
// "followed": returning notification.PushPreference{} here would leak that
// empty string as the caller-visible default instead of AudienceFollowed.
func (s *Store) GetPreference(ctx context.Context, userID string) (notification.PushPreference, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return notification.PushPreference{}, fmt.Errorf("parse user ID: %w", err)
	}
	row, err := s.queries.GetPushPreference(ctx, parsedUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.PushPreference{NewRequestAudience: notification.AudienceFollowed}, nil
	}
	if err != nil {
		return notification.PushPreference{}, fmt.Errorf("get push preference: %w", err)
	}
	return notification.PushPreference{WebPushEnabled: row.WebPushEnabled, NewRequestAudience: row.NewRequestAudience}, nil
}

func (s *Store) SetPreference(ctx context.Context, userID string, preference notification.PushPreference) error {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return fmt.Errorf("parse user ID: %w", err)
	}
	if err := s.queries.UpsertPushPreference(ctx, sqlcgen.UpsertPushPreferenceParams{
		UserID: parsedUserID, WebPushEnabled: preference.WebPushEnabled, NewRequestAudience: preference.NewRequestAudience,
	}); err != nil {
		return fmt.Errorf("set push preference: %w", err)
	}
	return nil
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
