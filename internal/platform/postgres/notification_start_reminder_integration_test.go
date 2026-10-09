//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

func TestMaterializeStartReminderRechecksScheduleAndConsumesUniqueOpportunity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	startsAt := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Microsecond)
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, startsAt, "Reminder")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	targets := []notification.Target{{Transport: "webpush", Key: "device-a"}}
	reminder := notification.StartReminder{GameRequestID: created.ID, StartsAt: startsAt}
	for _, err := range runConcurrently(
		func() error { return store.MaterializeStartReminder(t.Context(), reminder, targets) },
		func() error { return store.MaterializeStartReminder(t.Context(), reminder, targets) },
	) {
		if err != nil {
			t.Fatalf("concurrent materialize: %v", err)
		}
	}
	var parents, deliveries int
	if err := store.MaterializeStartReminder(t.Context(), reminder, targets); err != nil {
		t.Fatalf("duplicate materialize: %v", err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_start_reminder WHERE game_request_id = $1", created.ID).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery d JOIN notification_start_reminder r ON r.id=d.start_reminder_id WHERE r.game_request_id = $1", created.ID).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if parents != 1 || deliveries != 1 {
		t.Fatalf("parent/delivery rows = %d/%d, want 1/1", parents, deliveries)
	}

	stale := notification.StartReminder{GameRequestID: created.ID, StartsAt: startsAt.Add(time.Minute)}
	if err := store.MaterializeStartReminder(t.Context(), stale, targets); err != nil {
		t.Fatalf("stale materialize: %v", err)
	}
	if err := fixture.requests.UpdateDetails(t.Context(), created.ID, fixture.host.ID, gamerequest.UpdateDetailsInput{StartsAt: startsAt.Add(time.Minute), Description: "Reminder"}); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	var staleParents int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_start_reminder WHERE game_request_id=$1 AND starts_at=$2", created.ID, stale.StartsAt).Scan(&staleParents); err != nil {
		t.Fatal(err)
	}
	if staleParents != 0 {
		t.Fatalf("stale schedule parents = %d, want 0", staleParents)
	}

	zero := notification.StartReminder{GameRequestID: created.ID, StartsAt: startsAt.Add(time.Minute)}
	if err := store.MaterializeStartReminder(t.Context(), zero, nil); err != nil {
		t.Fatalf("zero-target materialize: %v", err)
	}
	if err := store.MaterializeStartReminder(t.Context(), zero, targets); err != nil {
		t.Fatalf("repeat zero-target materialize: %v", err)
	}
	var zeroParent, zeroDeliveries int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_start_reminder WHERE game_request_id=$1 AND starts_at=$2", created.ID, zero.StartsAt).Scan(&zeroParent); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery d JOIN notification_start_reminder r ON r.id=d.start_reminder_id WHERE r.game_request_id=$1 AND r.starts_at=$2", created.ID, zero.StartsAt).Scan(&zeroDeliveries); err != nil {
		t.Fatal(err)
	}
	if zeroParent != 1 || zeroDeliveries != 0 {
		t.Fatalf("zero-target parent/delivery rows = %d/%d, want 1/0", zeroParent, zeroDeliveries)
	}

	cancelled, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, startsAt.Add(time.Hour), "Cancelled")
	if err != nil {
		t.Fatalf("create cancelled request: %v", err)
	}
	if err := fixture.requests.Cancel(t.Context(), cancelled.ID, fixture.host.ID); err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	if err := store.MaterializeStartReminder(t.Context(), notification.StartReminder{GameRequestID: cancelled.ID, StartsAt: cancelled.StartsAt}, targets); err != nil {
		t.Fatalf("materialize cancelled request: %v", err)
	}
	var rejected int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_start_reminder WHERE game_request_id=$1", cancelled.ID).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected != 0 {
		t.Fatalf("cancelled request reminder rows = %d, want 0", rejected)
	}

	deleted, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, startsAt.Add(2*time.Hour), "Deleted")
	if err != nil {
		t.Fatalf("create deleted request: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM game_requests WHERE id=$1", deleted.ID); err != nil {
		t.Fatalf("delete request: %v", err)
	}
	if err := store.MaterializeStartReminder(t.Context(), notification.StartReminder{GameRequestID: deleted.ID, StartsAt: deleted.StartsAt}, targets); err != nil {
		t.Fatalf("materialize deleted request: %v", err)
	}

	rollbackStart := startsAt.Add(2 * time.Minute)
	if err := fixture.requests.UpdateDetails(t.Context(), created.ID, fixture.host.ID, gamerequest.UpdateDetailsInput{StartsAt: rollbackStart, Description: "Reminder"}); err != nil {
		t.Fatalf("reschedule for rollback: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `ALTER TABLE notification_delivery
		ADD CONSTRAINT reminder_test_force_rollback CHECK (target_key <> 'force-reminder-rollback')`); err != nil {
		t.Fatalf("add rollback constraint: %v", err)
	}
	if err := store.MaterializeStartReminder(t.Context(), notification.StartReminder{GameRequestID: created.ID, StartsAt: rollbackStart},
		[]notification.Target{{Transport: "webpush", Key: "force-reminder-rollback"}}); err == nil {
		t.Fatal("materialize with forced delivery insert failure succeeded")
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_start_reminder WHERE game_request_id=$1 AND starts_at=$2", created.ID, rollbackStart).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected != 0 {
		t.Fatalf("rolled-back reminder parents = %d, want 0", rejected)
	}
}

func TestStartReminderReservationClaimRecoveryAndDiagnostics(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate(): %v", err)
	}
	fixture := newNotificationFixture(t, pool)
	startsAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, startsAt, "Reminder")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	reminder := notification.StartReminder{GameRequestID: created.ID, StartsAt: startsAt}
	if err := store.MaterializeStartReminder(t.Context(), reminder, []notification.Target{{Transport: "webpush", Key: "reserved"}, {Transport: "webpush", Key: "unreserved"}}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	var reservedID, unreservedID int64
	if err := pool.QueryRow(t.Context(), "SELECT id FROM notification_delivery WHERE target_key='reserved'").Scan(&reservedID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT id FROM notification_delivery WHERE target_key='unreserved'").Scan(&unreservedID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET status='claimed', claimed_until=now()-interval '1 second' WHERE id IN ($1,$2)", reservedID, unreservedID); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.ReserveStartReminderSend(t.Context(), reservedID); err != nil || ok {
		t.Fatalf("expired reservation = %v, %v; want false", ok, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET claimed_until=now()+interval '1 minute' WHERE id=$1", reservedID); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.ReserveStartReminderSend(t.Context(), reservedID); err != nil || !ok {
		t.Fatalf("reserve = %v, %v", ok, err)
	}
	if ok, err := store.ReserveStartReminderSend(t.Context(), reservedID); err != nil || ok {
		t.Fatalf("duplicate reserve = %v, %v; want false", ok, err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET claimed_until=now()-interval '1 second' WHERE id=$1", reservedID); err != nil {
		t.Fatal(err)
	}

	restartedStore := notificationpostgres.NewStore(pool)
	claimed, err := restartedStore.ClaimDueDeliveries(t.Context(), 10, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim after repository restart: %v", err)
	}
	if len(claimed) != 1 || claimed[0].DeliveryID != unreservedID || claimed[0].StartReminder == nil || claimed[0].Activity.GameRequestID != "" {
		t.Fatalf("claimed rows = %#v, want only unreserved reminder", claimed)
	}
	var status, reason string
	if err := pool.QueryRow(t.Context(), "SELECT status,last_error FROM notification_delivery WHERE id=$1", reservedID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "uncertain" || reason != "start reminder send outcome is unknown" {
		t.Fatalf("reserved expired state = %s/%q", status, reason)
	}
	problems, err := store.ListProblemDeliveries(t.Context())
	if err != nil {
		t.Fatalf("problems: %v", err)
	}
	found := false
	for _, problem := range problems {
		if problem.DeliveryID == reservedID && problem.StartReminder != nil && problem.Activity.GameRequestID == "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reserved reminder missing from problem contexts: %#v", problems)
	}
	detail, err := store.GetDeliveryDetail(t.Context(), reservedID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.StartReminder == nil || detail.Activity.GameRequestID != "" {
		t.Fatalf("detail context = %#v", detail)
	}
	if reopened, err := store.ReopenUncertainDelivery(t.Context(), reservedID); err != nil || !reopened {
		t.Fatalf("manual retry reopen = %v, %v", reopened, err)
	}
	var marker *time.Time
	if err := pool.QueryRow(t.Context(), "SELECT send_started_at FROM notification_delivery WHERE id=$1", reservedID).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != nil {
		t.Fatalf("manual retry marker = %v, want NULL", marker)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET send_started_at=now() WHERE id=$1", unreservedID); err != nil {
		t.Fatal(err)
	}
	if err := restartedStore.RecordRetry(t.Context(), unreservedID, time.Now().Add(time.Minute), "retry"); err != nil {
		t.Fatalf("record retry: %v", err)
	}
	var retryMarker *time.Time
	if err := pool.QueryRow(t.Context(), "SELECT send_started_at FROM notification_delivery WHERE id=$1", unreservedID).Scan(&retryMarker); err != nil {
		t.Fatal(err)
	}
	if retryMarker != nil {
		t.Fatalf("retry marker = %v, want NULL", retryMarker)
	}
}
