//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

type pushSummaryReader struct{ store *gamerequestquery.Store }

func (r pushSummaryReader) GetGameRequestSummary(ctx context.Context, id string) (notification.GameRequestSummary, error) {
	summary, err := r.store.GetGameRequestSummary(ctx, id)
	if err != nil {
		return notification.GameRequestSummary{}, err
	}
	return notification.GameRequestSummary{GameID: summary.GameID, GameName: summary.GameName, StartsAt: summary.StartsAt}, nil
}

type pushNickReader struct{ users *user.Service }

func (r pushNickReader) GetGameNick(ctx context.Context, id string) (string, error) {
	account, err := r.users.ByID(ctx, id)
	if err != nil {
		return "", err
	}
	return account.GameNick, nil
}

type pushSendCall struct {
	device  string
	payload []byte
}
type fakePushSender struct {
	calls []pushSendCall
	retry bool
}

func (s *fakePushSender) Send(_ context.Context, endpoint, _, _ string, payload []byte, ttl int) notification.WebPushSendOutcome {
	device := map[string]string{
		"https://push.example/actor":    "actor",
		"https://push.example/disabled": "disabled",
		"https://push.example/device-a": "device-a",
		"https://push.example/device-b": "device-b",
	}[endpoint]
	s.calls = append(s.calls, pushSendCall{device, append([]byte(nil), payload...)})
	if ttl != 300 {
		return notification.WebPushSendOutcome{Outcome: notification.OutcomePermanent, Err: errors.New("unexpected TTL")}
	}
	if s.retry && endpoint == "https://push.example/device-a" {
		s.retry = false
		delay := 17 * time.Minute
		return notification.WebPushSendOutcome{Outcome: notification.OutcomeRetryable, RetryAfter: &delay, Err: errors.New("rate limited")}
	}
	return notification.WebPushSendOutcome{Outcome: notification.OutcomeSucceeded}
}

func TestWebPushWorkerPostgresEndToEnd(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	fixture := newNotificationFixture(t, pool)
	users := user.NewService(userpostgres.NewStore(pool))
	disabled, err := users.Register(t.Context(), "Disabled Push")
	if err != nil {
		t.Fatal(err)
	}
	newOwner, err := users.Register(t.Context(), "New Push Owner")
	if err != nil {
		t.Fatal(err)
	}
	gameStore := gamepostgres.NewStore(pool)
	followers := game.NewSubscriptionService(gameStore)
	for _, id := range []string{fixture.host.ID, fixture.guest.ID, disabled.ID, newOwner.ID} {
		if err := followers.Follow(t.Context(), id, fixture.gameID); err != nil {
			t.Fatal(err)
		}
	}
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	subscriptions, err := notification.NewSubscriptionService(store, store, key)
	if err != nil {
		t.Fatal(err)
	}
	enable := func(id, device, endpoint string) {
		t.Helper()
		if err := subscriptions.Enable(t.Context(), id, device, "Device", endpoint, "browser-key", "browser-auth"); err != nil {
			t.Fatal(err)
		}
	}
	enable(fixture.host.ID, "actor-device", "https://push.example/actor")
	enable(fixture.guest.ID, "device-a", "https://push.example/device-a")
	enable(fixture.guest.ID, "device-b", "https://push.example/device-b")
	enable(disabled.ID, "disabled-device", "https://push.example/disabled")
	if err := subscriptions.Disable(t.Context(), disabled.ID); err != nil {
		t.Fatal(err)
	}
	queries := gamerequestquery.NewStore(pool)
	sender := &fakePushSender{retry: true}
	transport := notification.NewWebPushTransport(store, store, store, followers, queries, pushSummaryReader{queries}, pushNickReader{users}, "https://lan.example", "Camp Nova", key, sender)
	worker := newTestWorker(store, []notification.Transport{transport})
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Private description")
	if err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	rows, err := pool.Query(t.Context(), `SELECT d.status, d.target_key FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1 ORDER BY d.id`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	var keys []string
	for rows.Next() {
		var status, target string
		if err := rows.Scan(&status, &target); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, status)
		keys = append(keys, target)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(keys) != 2 || !reflect.DeepEqual(statuses, []string{"pending", "succeeded"}) {
		t.Fatalf("eligible deliveries = %v; want one scheduled retry and one succeeded", statuses)
	}
	if len(sender.calls) != 2 {
		t.Fatalf("provider sends = %d; want 2", len(sender.calls))
	}
	devices := map[string]int{}
	for _, call := range sender.calls {
		devices[call.device]++
	}
	if !reflect.DeepEqual(devices, map[string]int{"device-a": 1, "device-b": 1}) {
		t.Fatalf("eligible device sends = %v; want one per device", devices)
	}
	for _, call := range sender.calls {
		var payload map[string]any
		if err := json.Unmarshal(call.payload, &payload); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"version": float64(1), "kind": "created", "game": "Notification Game", "url": "https://lan.example/game-requests/" + created.ID, "instance": "Camp Nova", "title": "New Notification Game Game Request"}
		for key, value := range want {
			if payload[key] != value {
				t.Fatalf("payload %s = %v; want %v", key, payload[key], value)
			}
		}
		allowed := map[string]bool{"version": true, "kind": true, "game": true, "starts_at": true, "url": true, "instance": true, "title": true, "body": true}
		for key := range payload {
			if !allowed[key] {
				t.Fatalf("unexpected payload field %q", key)
			}
		}
	}
	var nextAttempt time.Time
	if err := pool.QueryRow(t.Context(), "SELECT next_attempt_at FROM notification_delivery WHERE status='pending'").Scan(&nextAttempt); err != nil {
		t.Fatal(err)
	}
	if delay := time.Until(nextAttempt); delay < 16*time.Minute || delay > 18*time.Minute {
		t.Fatalf("provider Retry-After delay = %v", delay)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE notification_delivery SET next_attempt_at=now() WHERE status='pending'"); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	if len(sender.calls) != 3 {
		t.Fatalf("retry sends = %d; want 3", len(sender.calls))
	}
	var succeeded int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery WHERE status='succeeded'").Scan(&succeeded); err != nil || succeeded != 2 {
		t.Fatalf("successful deliveries = %d, err = %v", succeeded, err)
	}

	// Membership drives lifecycle notifications independently of discovery follows.
	if err := fixture.requests.Join(t.Context(), created.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	if len(sender.calls) != 4 || sender.calls[3].device != "actor" {
		t.Fatalf("joined activity sends = %d; want one to existing participant, not joining actor", len(sender.calls)-3)
	}
	var joined map[string]any
	if err := json.Unmarshal(sender.calls[3].payload, &joined); err != nil {
		t.Fatal(err)
	}
	if joined["kind"] != "joined" || joined["actor_game_nick"] != fixture.guest.GameNick || joined["title"] != fixture.guest.GameNick+" joined Notification Game" {
		t.Fatalf("joined payload lacks public actor Game Nick: %v", joined["title"])
	}
	for field := range joined {
		if !map[string]bool{"version": true, "kind": true, "game": true, "starts_at": true, "url": true, "instance": true, "title": true, "body": true, "actor_game_nick": true}[field] {
			t.Fatalf("unexpected joined payload field %q", field)
		}
	}

	// Materialize a new live activity with the same real transport, then transfer
	// one endpoint to a different eligible owner before the worker can send.
	later, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Later")
	if err != nil {
		t.Fatal(err)
	}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("new activity count = %d, err = %v", len(activities), err)
	}
	targets, err := transport.Targets(t.Context(), activities[0])
	if err != nil || len(targets) != 2 {
		t.Fatalf("materialized targets = %d, err = %v", len(targets), err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], targets); err != nil {
		t.Fatal(err)
	}
	enable(newOwner.ID, "new-device", "https://push.example/device-b")
	sendsBefore := len(sender.calls)
	worker.Tick(t.Context())
	if len(sender.calls) != sendsBefore+1 {
		t.Fatalf("post-reassignment sends = %d; want only original owner's remaining device", len(sender.calls)-sendsBefore)
	}
	if sender.calls[len(sender.calls)-1].device != "device-a" {
		t.Fatal("old target redirected to new owner")
	}
	var suppressed int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1 AND d.status='suppressed'", later.ID).Scan(&suppressed); err != nil || suppressed != 1 {
		t.Fatalf("reassigned target suppressed = %d, err = %v", suppressed, err)
	}

	// A legacy/malformed materialized target must not reach the sender.
	obsolete, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Obsolete")
	if err != nil {
		t.Fatal(err)
	}
	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 || activities[0].GameRequestID != obsolete.ID {
		t.Fatalf("obsolete activity count = %d, err = %v", len(activities), err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], []notification.Target{{Transport: "webpush", Key: "legacy-numeric-target"}}); err != nil {
		t.Fatal(err)
	}
	sendsBefore = len(sender.calls)
	worker.Tick(t.Context())
	if len(sender.calls) != sendsBefore {
		t.Fatal("malformed target caused provider send")
	}
	if err := pool.QueryRow(t.Context(), "SELECT status FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1", obsolete.ID).Scan(&statuses[0]); err != nil || statuses[0] != "suppressed" {
		t.Fatalf("obsolete status = %q, err = %v", statuses[0], err)
	}
}
