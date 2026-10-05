//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
)

func TestGameRequestSessionInformationReadReturnsDatabaseErrors(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "cancelled read")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := f.requests.SessionInformation(ctx, req.ID, f.host)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
}

func TestGameRequestSessionInformationReadAccess(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "session info")
	input := gamerequest.SessionInformationInput{Key: "Server", Value: "10.20.30.40:27015", IsSecret: true}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.outsider); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider read = %v", err)
	}
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	var guestEntryID string
	for _, user := range []string{f.host, f.guest} {
		got, err := f.requests.SessionInformation(t.Context(), req.ID, user)
		if err != nil || len(got.Entries) != 1 || got.Entries[0].Value != input.Value || !got.Entries[0].IsSecret {
			t.Fatalf("read = %+v, %v", got, err)
		}
		if user == f.guest {
			guestEntryID = got.Entries[0].ID
		}
	}
	if err := f.requests.Leave(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("after leave = %v", err)
	}
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	got, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].ID != guestEntryID || got.Entries[0].Value != input.Value || !got.Entries[0].IsSecret {
		t.Fatalf("after rejoin = %+v, want secret entry %s with value %q", got, guestEntryID, input.Value)
	}
	if err := f.requests.Start(t.Context(), req.ID, f.host); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.Close(t.Context(), req.ID, f.host); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.host); !errors.Is(err, gamerequest.ErrNotActive) {
		t.Fatalf("closed read = %v", err)
	}
}

func TestGameRequestSessionInformationCreateValidationAndDatabaseConstraints(t *testing.T) {
	f := newPlayingFixture(t)
	first := f.create(t, "first")
	second := f.create(t, "second")
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: " Server ", Value: "  host "}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: strings.Repeat("😀", 40), Value: strings.Repeat("😀", 300)}); err != nil {
		t.Fatalf("create Unicode boundary entry: %v", err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "duplicate"}); !errors.Is(err, gamerequest.ErrSessionInformationKeyExists) {
		t.Fatalf("duplicate = %v", err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), second.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "another"}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{strings.Repeat("k", 41), "v"}, {"k", strings.Repeat("v", 301)}, {"k\nx", "v"}, {"k", "v\r"}, {"", "v"}, {"k", ""}} {
		if _, err := f.pool.Exec(t.Context(), `INSERT INTO game_request_session_entries(game_request_id,key,value) VALUES ($1,$2,$3)`, first.ID, pair[0], pair[1]); err == nil {
			t.Errorf("database accepted key/value lengths %d/%d", len(pair[0]), len(pair[1]))
		}
	}
	var key, value string
	if err := f.pool.QueryRow(t.Context(), `SELECT key,value FROM game_request_session_entries WHERE game_request_id=$1`, first.ID).Scan(&key, &value); err != nil || key != "Server" || value != "host" {
		t.Fatalf("stored = %q/%q, %v", key, value, err)
	}
}

func TestGameRequestSessionInformationUpdateDeleteAndOrdering(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "mutation")
	ctx := t.Context()
	for _, input := range []gamerequest.SessionInformationInput{{Key: "Server", Value: "one"}, {Key: "Lobby", Value: "two"}, {Key: "Meeting", Value: "three"}} {
		if err := f.requests.CreateSessionInformationEntry(ctx, req.ID, f.host, input); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || len(rows.Entries) != 3 {
		t.Fatalf("entries = %+v, %v", rows, err)
	}
	original := rows.Entries[1]
	if err := f.requests.UpdateSessionInformationEntry(ctx, req.ID, original.ID, f.host, gamerequest.SessionInformationInput{Key: "Room", Value: "changed", IsSecret: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.UpdateSessionInformationEntry(ctx, req.ID, original.ID, f.host, gamerequest.SessionInformationInput{Key: " Room ", Value: " changed ", IsSecret: true}); err != nil {
		t.Fatal(err)
	}
	rows, err = f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || rows.Entries[1].ID != original.ID || rows.Entries[1].Sequence != original.Sequence || rows.Entries[1].Key != "Room" || !rows.Entries[1].IsSecret {
		t.Fatalf("updated entries = %+v, %v", rows, err)
	}
	deleted := rows.Entries[0].ID
	if err := f.requests.DeleteSessionInformationEntry(ctx, req.ID, deleted, f.host); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.DeleteSessionInformationEntry(ctx, req.ID, deleted, f.host); !errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound) {
		t.Fatalf("repeated delete = %v", err)
	}
}

type sessionInformationEvents struct {
	events []gamerequest.Event
}

func (p *sessionInformationEvents) Publish(event gamerequest.Event) {
	p.events = append(p.events, event)
}

func TestGameRequestSessionInformationSecretOnlyTransitionsPersist(t *testing.T) {
	f := newPlayingFixture(t)
	ctx := t.Context()
	req := f.create(t, "secret-only transitions")
	publisher := &sessionInformationEvents{}
	service := gamerequest.NewService(gamerequestpostgres.NewStore(f.pool), nil, publisher)
	input := gamerequest.SessionInformationInput{Key: "Password", Value: "hunter2"}
	if err := service.CreateSessionInformationEntry(ctx, req.ID, f.host, input); err != nil {
		t.Fatal(err)
	}
	publisher.events = nil
	entry := func() gamerequest.SessionInformationEntry {
		t.Helper()
		info, err := service.SessionInformation(ctx, req.ID, f.host)
		if err != nil || len(info.Entries) != 1 {
			t.Fatalf("session information = %+v, %v", info, err)
		}
		return info.Entries[0]
	}
	setOldTimestamp := func() {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `UPDATE game_request_session_entries SET updated_at='2000-01-01T00:00:00Z' WHERE game_request_id=$1`, req.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, secret := range []bool{true, false} {
		setOldTimestamp()
		before := entry()
		if err := service.UpdateSessionInformationEntry(ctx, req.ID, before.ID, f.host, gamerequest.SessionInformationInput{Key: " Password ", Value: " hunter2 ", IsSecret: secret}); err != nil {
			t.Fatal(err)
		}
		after := entry()
		if after.IsSecret != secret || after.Key != "Password" || after.Value != "hunter2" || after.ID != before.ID || after.Sequence != before.Sequence {
			t.Fatalf("entry after IsSecret=%t: before=%+v after=%+v", secret, before, after)
		}
		if !after.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("updated_at did not advance: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
		}
		if len(publisher.events) != 1 || publisher.events[0] != (gamerequest.Event{Type: gamerequest.EventSessionInformationChanged, RequestID: req.ID}) {
			t.Fatalf("published events = %#v", publisher.events)
		}
		publisher.events = nil
	}
}

func TestGameRequestSessionInformationNormalizedNoOpTouchesNothing(t *testing.T) {
	f := newPlayingFixture(t)
	ctx := t.Context()
	req := f.create(t, "session information no-op")
	publisher := &sessionInformationEvents{}
	service := gamerequest.NewService(gamerequestpostgres.NewStore(f.pool), nil, publisher)
	if err := service.CreateSessionInformationEntry(ctx, req.ID, f.host, gamerequest.SessionInformationInput{Key: "Password", Value: "hunter2"}); err != nil {
		t.Fatal(err)
	}
	publisher.events = nil
	if _, err := f.pool.Exec(ctx, `UPDATE game_request_session_entries SET updated_at='2000-01-01T00:00:00Z' WHERE game_request_id=$1`, req.ID); err != nil {
		t.Fatal(err)
	}
	before, err := service.SessionInformation(ctx, req.ID, f.host)
	if err != nil {
		t.Fatal(err)
	}
	var activitiesBefore, workBefore, deliveriesBefore int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM game_request_activity WHERE game_request_id=$1`, req.ID).Scan(&activitiesBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notification_work WHERE game_request_id=$1`, req.ID).Scan(&workBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1`, req.ID).Scan(&deliveriesBefore); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateSessionInformationEntry(ctx, req.ID, before.Entries[0].ID, f.host, gamerequest.SessionInformationInput{Key: " Password ", Value: " hunter2 ", IsSecret: false}); err != nil {
		t.Fatal(err)
	}
	after, err := service.SessionInformation(ctx, req.ID, f.host)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || after.Entries[0].ID != before.Entries[0].ID ||
		after.Entries[0].Sequence != before.Entries[0].Sequence ||
		after.Entries[0].Key != before.Entries[0].Key ||
		after.Entries[0].Value != before.Entries[0].Value ||
		after.Entries[0].IsSecret != before.Entries[0].IsSecret {
		t.Fatalf("no-op changed entry: before=%+v after=%+v", before.Entries[0], after.Entries)
	}
	if after.Entries[0].UpdatedAt != before.Entries[0].UpdatedAt {
		t.Fatalf("no-op changed updated_at: before=%s after=%s", before.Entries[0].UpdatedAt, after.Entries[0].UpdatedAt)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("no-op published events: %#v", publisher.events)
	}
	var activitiesAfter, workAfter, deliveriesAfter int
	for _, count := range []struct {
		query string
		dest  *int
	}{
		{`SELECT count(*) FROM game_request_activity WHERE game_request_id=$1`, &activitiesAfter},
		{`SELECT count(*) FROM notification_work WHERE game_request_id=$1`, &workAfter},
		{`SELECT count(*) FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1`, &deliveriesAfter},
	} {
		if err := f.pool.QueryRow(ctx, count.query, req.ID).Scan(count.dest); err != nil {
			t.Fatal(err)
		}
	}
	if activitiesAfter != activitiesBefore || workAfter != workBefore || deliveriesAfter != deliveriesBefore {
		t.Fatalf("no-op persistence changed activity/work/deliveries: %d/%d/%d -> %d/%d/%d", activitiesBefore, workBefore, deliveriesBefore, activitiesAfter, workAfter, deliveriesAfter)
	}
}

func TestGameRequestSessionInformationLimitSerializesConcurrentCreates(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "limit")
	for i := range 11 {
		if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: fmt.Sprintf("key-%d", i), Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	results := runConcurrently(func() error {
		return f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "race-1", Value: "v"})
	}, func() error {
		return f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "race-2", Value: "v"})
	})
	var successes, limits int
	for _, err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, gamerequest.ErrSessionInformationLimit) {
			limits++
		} else {
			t.Fatalf("create error = %v", err)
		}
	}
	if successes != 1 || limits != 1 {
		t.Fatalf("successes/limits = %d/%d", successes, limits)
	}
}

func TestGameRequestSessionInformationHostScopeLifecycleAndCascade(t *testing.T) {
	f := newPlayingFixture(t)
	ctx := t.Context()
	req := f.create(t, "host scope")
	other := f.create(t, "other request")
	if err := f.requests.Join(ctx, req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.CreateSessionInformationEntry(ctx, req.ID, f.host, gamerequest.SessionInformationInput{Key: "Lobby", Value: "secret"}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || len(rows.Entries) != 1 {
		t.Fatalf("entries = %+v, %v", rows, err)
	}
	entryID := rows.Entries[0].ID
	input := gamerequest.SessionInformationInput{Key: "Lobby", Value: "changed"}
	for _, err := range []error{
		f.requests.CreateSessionInformationEntry(ctx, req.ID, f.guest, input),
		func() error { return f.requests.UpdateSessionInformationEntry(ctx, req.ID, entryID, f.guest, input) }(),
		f.requests.DeleteSessionInformationEntry(ctx, req.ID, entryID, f.guest),
	} {
		if !errors.Is(err, gamerequest.ErrNotCreator) {
			t.Fatalf("participant mutation error = %v, want ErrNotCreator", err)
		}
	}
	if err := f.requests.UpdateSessionInformationEntry(ctx, other.ID, entryID, f.host, input); !errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound) {
		t.Fatalf("cross-request update = %v, want entry not found", err)
	}
	if err := f.requests.DeleteSessionInformationEntry(ctx, other.ID, entryID, f.host); !errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound) {
		t.Fatalf("cross-request delete = %v, want entry not found", err)
	}
	var activitiesBefore, activitiesAfter int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM game_request_activity WHERE game_request_id=$1`, req.ID).Scan(&activitiesBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.CreateSessionInformationEntry(ctx, req.ID, f.host, gamerequest.SessionInformationInput{Key: "Activity", Value: "before"}); err != nil {
		t.Fatal(err)
	}
	rows, err = f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || len(rows.Entries) != 2 {
		t.Fatalf("entries before activity check = %+v, %v", rows, err)
	}
	activityEntryID := rows.Entries[1].ID
	if err := f.requests.UpdateSessionInformationEntry(ctx, req.ID, activityEntryID, f.host, gamerequest.SessionInformationInput{Key: "Activity", Value: "after"}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.DeleteSessionInformationEntry(ctx, req.ID, activityEntryID, f.host); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM game_request_activity WHERE game_request_id=$1`, req.ID).Scan(&activitiesAfter); err != nil {
		t.Fatal(err)
	}
	if activitiesAfter != activitiesBefore {
		t.Fatalf("session-information activity count = %d, before %d", activitiesAfter, activitiesBefore)
	}
	if err := f.requests.CreateSessionInformationEntry(ctx, req.ID, f.host, gamerequest.SessionInformationInput{Key: "Cascade", Value: "value"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM game_requests WHERE id=$1`, req.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM game_request_session_entries WHERE game_request_id=$1`, req.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("session-information rows after request delete = %d, %v", remaining, err)
	}
}

func TestGameRequestSessionInformationMutationSerializesWithClose(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "close race")
	if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "Lobby", Value: "before"}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.Start(t.Context(), req.ID, f.host); err != nil {
		t.Fatal(err)
	}
	rows, err := f.requests.SessionInformation(t.Context(), req.ID, f.host)
	if err != nil || len(rows.Entries) != 1 {
		t.Fatalf("entries = %+v, %v", rows, err)
	}
	entryID := rows.Entries[0].ID
	if err := f.requests.UpdateSessionInformationEntry(t.Context(), req.ID, entryID, f.host, gamerequest.SessionInformationInput{Key: "Lobby", Value: "playing"}); err != nil {
		t.Fatalf("update while playing: %v", err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "Temporary", Value: "playing"}); err != nil {
		t.Fatalf("create while playing: %v", err)
	}
	rows, err = f.requests.SessionInformation(t.Context(), req.ID, f.host)
	if err != nil || len(rows.Entries) != 2 {
		t.Fatalf("entries while playing = %+v, %v", rows, err)
	}
	if err := f.requests.DeleteSessionInformationEntry(t.Context(), req.ID, rows.Entries[1].ID, f.host); err != nil {
		t.Fatalf("delete while playing: %v", err)
	}
	results := runConcurrently(func() error {
		return f.requests.UpdateSessionInformationEntry(t.Context(), req.ID, entryID, f.host, gamerequest.SessionInformationInput{Key: "Lobby", Value: "after"})
	}, func() error {
		return f.requests.Close(t.Context(), req.ID, f.host)
	})
	if results[1] != nil {
		t.Fatalf("close error = %v", results[1])
	}
	if results[0] != nil && !errors.Is(results[0], gamerequest.ErrNotActive) {
		t.Fatalf("concurrent update error = %v, want success or ErrNotActive", results[0])
	}
	if state := f.state(t, req.ID); state.status != string(gamerequest.StatusClosed) {
		t.Fatalf("status after close race = %q", state.status)
	}
	for _, mutate := range []func() error{
		func() error {
			return f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "Late", Value: "value"})
		},
		func() error {
			return f.requests.UpdateSessionInformationEntry(t.Context(), req.ID, entryID, f.host, gamerequest.SessionInformationInput{Key: "Lobby", Value: "late"})
		},
		func() error { return f.requests.DeleteSessionInformationEntry(t.Context(), req.ID, entryID, f.host) },
	} {
		if err := mutate(); !errors.Is(err, gamerequest.ErrNotActive) {
			t.Fatalf("terminal mutation error = %v, want ErrNotActive", err)
		}
	}
}
