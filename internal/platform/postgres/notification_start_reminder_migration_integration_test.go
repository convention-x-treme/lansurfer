//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestStartReminderMigrationDownPreservesActivityDeliveriesAndCanReapply(t *testing.T) {
	pool := newTestPool(t)
	migrationFS := productionMigrationFS(t)
	migrateToVersion(t, pool, migrationFS, 29)

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS, goose.WithSlog(testLogger()))
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.UpTo(t.Context(), 30); err != nil {
		t.Fatalf("apply reminder migration: %v", err)
	}

	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Rollback test")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	var sequence int64
	if err := pool.QueryRow(t.Context(), `
		SELECT sequence FROM game_request_activity WHERE game_request_id = $1 AND kind = 'created'
	`, request.ID).Scan(&sequence); err != nil {
		t.Fatalf("read activity sequence: %v", err)
	}
	var workID int64
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at)
		SELECT $1, $2, $3, 'created', occurred_at FROM game_request_activity WHERE sequence = $1
		RETURNING id
	`, sequence, request.ID, fixture.host.ID).Scan(&workID); err != nil {
		t.Fatalf("insert activity work: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_delivery (work_id, transport, target_key) VALUES ($1, 'webpush', 'activity-target')
	`, workID); err != nil {
		t.Fatalf("insert activity delivery: %v", err)
	}
	var reminderID int64
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO notification_start_reminder (game_request_id, starts_at) VALUES ($1, $2)
		RETURNING id
	`, request.ID, request.StartsAt).Scan(&reminderID); err != nil {
		t.Fatalf("insert reminder: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_delivery (start_reminder_id, transport, target_key)
		VALUES ($1, 'webpush', 'reminder-target')
	`, reminderID); err != nil {
		t.Fatalf("insert reminder delivery: %v", err)
	}

	if _, err := provider.DownTo(t.Context(), 29); err != nil {
		t.Fatalf("roll back reminder migration: %v", err)
	}
	var activityDeliveries int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM notification_delivery WHERE target_key = 'activity-target'
	`).Scan(&activityDeliveries); err != nil || activityDeliveries != 1 {
		t.Fatalf("activity deliveries after down = %d, %v; want 1 and no error", activityDeliveries, err)
	}
	var workIDNullable string
	if err := pool.QueryRow(t.Context(), `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'notification_delivery' AND column_name = 'work_id'
	`).Scan(&workIDNullable); err != nil || workIDNullable != "NO" {
		t.Fatalf("work_id nullable after down = %q, %v; want NO", workIDNullable, err)
	}
	var remindersTableExists bool
	if err := pool.QueryRow(t.Context(), `
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'notification_start_reminder')
	`).Scan(&remindersTableExists); err != nil || remindersTableExists {
		t.Fatalf("reminder table exists after down = %v, %v; want false", remindersTableExists, err)
	}

	if _, err := provider.UpTo(t.Context(), 30); err != nil {
		t.Fatalf("reapply reminder migration: %v", err)
	}
	var newReminderID int64
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO notification_start_reminder (game_request_id, starts_at) VALUES ($1, $2)
		RETURNING id
	`, request.ID, request.StartsAt.Add(time.Minute)).Scan(&newReminderID); err != nil {
		t.Fatalf("insert reminder after re-up: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_delivery (start_reminder_id, transport, target_key)
		VALUES ($1, 'webpush', 'reapplied-reminder-target')
	`, newReminderID); err != nil {
		t.Fatalf("insert reminder delivery after re-up: %v", err)
	}
	var remainingDeliveries int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_delivery`).Scan(&remainingDeliveries); err != nil || remainingDeliveries != 2 {
		t.Fatalf("deliveries after re-up = %d, %v; want preserved activity plus new reminder", remainingDeliveries, err)
	}
}
