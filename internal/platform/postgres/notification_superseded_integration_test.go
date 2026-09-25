//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

func TestDiscordSupersededDeliveryIsTerminalAndNotDiagnostic(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture := newNotificationFixture(t, pool)
	discordLifecycleDeliveryFixture(t, pool, store, fixture, []notification.Target{
		{Transport: "discord", Key: "supersede-pending"},
		{Transport: "discord", Key: "supersede-claimed"},
		{Transport: "webpush", Key: "push-pending"},
	}, []string{"pending", "claimed", "pending"})
	transport := newFakeTransport("discord", nil)
	worker := newTestWorker(store, []notification.Transport{transport})
	service := notification.NewService(store, []notification.Transport{transport})
	for _, key := range []string{"supersede-pending", "supersede-claimed"} {
		var deliveryID int64
		if err := pool.QueryRow(t.Context(), `SELECT id FROM notification_delivery WHERE target_key = $1`, key).Scan(&deliveryID); err != nil {
			t.Fatal(err)
		}
		changed, err := store.RecordSuperseded(t.Context(), deliveryID, 42)
		if err != nil || !changed {
			t.Fatalf("RecordSuperseded(%s) changed = %v, error = %v", key, changed, err)
		}
		var status, reason string
		var claim *time.Time
		if err := pool.QueryRow(t.Context(), `SELECT status, last_error, claimed_until FROM notification_delivery WHERE id = $1`, deliveryID).Scan(&status, &reason, &claim); err != nil {
			t.Fatal(err)
		}
		if status != string(notification.StatusSuperseded) || reason != "superseded by activity sequence 42" || claim != nil {
			t.Fatalf("%s: status/reason/claim = %q/%q/%v", key, status, reason, claim)
		}
		if changed, err := store.RecordSuperseded(t.Context(), deliveryID, 43); err != nil || changed {
			t.Fatalf("terminal superseded transition changed = %v, error = %v", changed, err)
		}
		if err := service.ManualRetry(t.Context(), deliveryID); !errors.Is(err, notification.ErrDeliveryNotRetryable) {
			t.Fatalf("ManualRetry(superseded) error = %v", err)
		}
		if err := service.ManualRetryUncertain(t.Context(), deliveryID); !errors.Is(err, notification.ErrDeliveryNotUncertain) {
			t.Fatalf("ManualRetryUncertain(superseded) error = %v", err)
		}
		if reopened, err := store.ReopenDelivery(t.Context(), deliveryID); err != nil || reopened {
			t.Fatalf("ReopenDelivery(superseded) = %v, %v", reopened, err)
		}
		if reopened, err := store.ReopenUncertainDelivery(t.Context(), deliveryID); err != nil || reopened {
			t.Fatalf("ReopenUncertainDelivery(superseded) = %v, %v", reopened, err)
		}
	}
	var pushID int64
	if err := pool.QueryRow(t.Context(), `SELECT id FROM notification_delivery WHERE target_key = 'push-pending'`).Scan(&pushID); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.RecordSuperseded(t.Context(), pushID, 42); err != nil || changed {
		t.Fatalf("Web Push superseded changed = %v, error = %v", changed, err)
	}
	claimed, err := store.ClaimDueDeliveries(t.Context(), 10, time.Now().Add(time.Minute))
	if err != nil || len(claimed) != 1 || claimed[0].Target.Key != "push-pending" {
		t.Fatalf("ClaimDueDeliveries = %+v, error = %v, want only Web Push", claimed, err)
	}
	worker.Tick(t.Context())
	select {
	case target := <-transport.sendCalls:
		t.Fatalf("worker retried superseded target %+v", target)
	default:
	}
	problems, err := store.ListProblemDeliveries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		if problem.Status == notification.StatusSuperseded {
			t.Fatalf("superseded listed as problem: %+v", problem)
		}
	}
}

func TestDiscordSupersessionCannotOverwriteTerminalOrBeReopenedByWorker(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	statuses := []string{"succeeded", "permanent", "exhausted", "uncertain", "suppressed", "claimed"}
	targets := make([]notification.Target, len(statuses))
	for index, status := range statuses {
		targets[index] = notification.Target{Transport: "discord", Key: "state-" + status}
	}
	fixture := newNotificationFixture(t, pool)
	discordLifecycleDeliveryFixture(t, pool, store, fixture, targets, statuses)
	for _, status := range statuses {
		key := "state-" + status
		var deliveryID int64
		if err := pool.QueryRow(t.Context(), `SELECT id FROM notification_delivery WHERE target_key = $1`, key).Scan(&deliveryID); err != nil {
			t.Fatal(err)
		}
		if status == "claimed" {
			changed, err := store.RecordSuperseded(t.Context(), deliveryID, 43)
			if err != nil || !changed {
				t.Fatalf("supersede claimed: %v, %v", changed, err)
			}
			// A worker already processing this claim must not overwrite superseded.
			for _, outcome := range []func() error{
				func() error { return store.RecordSuccess(t.Context(), deliveryID) },
				func() error { return store.RecordRetry(t.Context(), deliveryID, time.Now(), "retry") },
				func() error { return store.RecordExhausted(t.Context(), deliveryID, "exhausted") },
				func() error { return store.RecordPermanent(t.Context(), deliveryID, "permanent") },
				func() error { return store.RecordUncertain(t.Context(), deliveryID, "uncertain") },
				func() error { return store.RecordSuppressed(t.Context(), deliveryID, "ineligible") },
			} {
				if err := outcome(); err != nil {
					t.Fatal(err)
				}
				if got := deliveryStatus(t, pool, "discord", key); got != "superseded" {
					t.Fatalf("worker overwrote superseded as %q", got)
				}
			}
			continue
		}
		if changed, err := store.RecordSuperseded(t.Context(), deliveryID, 43); err != nil || changed {
			t.Fatalf("terminal %q was superseded: %v, %v", status, changed, err)
		}
		if got := deliveryStatus(t, pool, "discord", key); got != status {
			t.Fatalf("terminal %q became %q", status, got)
		}
	}
	problems, err := store.ListProblemDeliveries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[notification.DeliveryStatus]bool{
		notification.StatusPermanent: true, notification.StatusExhausted: true,
		notification.StatusUncertain: true, notification.StatusSuppressed: true,
	}
	if len(problems) != len(want) {
		t.Fatalf("diagnostic problems = %+v, want existing four terminal problem states", problems)
	}
	for _, problem := range problems {
		if !want[problem.Status] {
			t.Fatalf("unexpected diagnostic problem: %+v", problem)
		}
		delete(want, problem.Status)
	}
	if len(want) != 0 {
		t.Fatalf("missing diagnostic problem states: %+v", want)
	}
}
