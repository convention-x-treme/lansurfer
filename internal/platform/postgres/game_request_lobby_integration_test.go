//go:build integration

package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
)

func TestGameRequestLobbyCombinesSourcesAndRequiresMembership(t *testing.T) {
	fixture := newPlayingFixture(t)
	ctx := t.Context()
	request := fixture.create(t, "Lobby projection")
	queries := requestquery.NewStore(fixture.pool)
	if _, err := queries.Lobby(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("empty host Lobby: %v", err)
	}
	if _, err := queries.Lobby(ctx, request.ID, fixture.outsider); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider Lobby error = %v, want ErrNotMember", err)
	}
	if err := fixture.requests.Join(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.guest, "ready"); err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := fixture.requests.Start(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	lobby, err := queries.Lobby(ctx, request.ID, fixture.guest)
	if err != nil {
		t.Fatalf("read Lobby: %v", err)
	}
	if len(lobby.Entries) != 4 {
		t.Fatalf("entries = %d, want create, join, chat, and start", len(lobby.Entries))
	}
	if lobby.Entries[0].Source != requestquery.LobbySourceActivity || lobby.Entries[1].Activity.Kind != gamerequest.ActivityJoined || lobby.Entries[2].Source != requestquery.LobbySourceChat || lobby.Entries[2].Message.Body != "ready" || lobby.Entries[3].Activity.Kind != gamerequest.ActivityStarted {
		t.Fatalf("entries = %+v, want chronological Activity, Activity, Chat, Activity", lobby.Entries)
	}
	if err := fixture.requests.Leave(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if _, err := queries.Lobby(ctx, request.ID, fixture.guest); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("Lobby after leave error = %v, want ErrNotMember", err)
	}
}

func TestGameRequestLobbyLimitsAfterCombiningEqualTimestampEntries(t *testing.T) {
	fixture := newPlayingFixture(t)
	ctx := t.Context()
	request := fixture.create(t, "Lobby boundary")
	queries := requestquery.NewStore(fixture.pool)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if _, err := fixture.pool.Exec(ctx, `UPDATE game_request_activity SET occurred_at = $2 WHERE game_request_id = $1`, request.ID, at.Add(-time.Hour)); err != nil {
		t.Fatalf("age fixture activity: %v", err)
	}
	for i := range 100 {
		actor := fixture.host
		if i%2 == 1 {
			actor = fixture.guest
		}
		if _, err := fixture.pool.Exec(ctx, `INSERT INTO game_request_activity (game_request_id, actor_id, kind, occurred_at) VALUES ($1, $2, 'joined', $3)`, request.ID, actor, at); err != nil {
			t.Fatalf("insert Activity %d: %v", i, err)
		}
	}
	for i := 1; i <= 101; i++ {
		messageID := fmt.Sprintf("00000000-0000-0000-0000-%012x", i)
		if _, err := fixture.pool.Exec(ctx, `INSERT INTO game_request_messages (id, game_request_id, author_id, body, created_at) VALUES ($1, $2, $3, $4, $5)`, messageID, request.ID, fixture.host, fmt.Sprintf("message-%03d", i), at); err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
	}
	lobby, err := queries.Lobby(ctx, request.ID, fixture.host)
	if err != nil {
		t.Fatalf("read Lobby: %v", err)
	}
	if len(lobby.Entries) != 200 {
		t.Fatalf("entries = %d, want 200 total", len(lobby.Entries))
	}
	for i := range 99 {
		entry := lobby.Entries[i]
		if entry.Source != requestquery.LobbySourceActivity {
			t.Fatalf("entry %d source = %d, want Activity", i, entry.Source)
		}
		wantActor := fixture.guest
		if (i+1)%2 == 0 {
			wantActor = fixture.host
		}
		if entry.Activity.ActorID != wantActor {
			t.Fatalf("Activity %d actor = %s, want %s (oldest selected sequence first)", i, entry.Activity.ActorID, wantActor)
		}
	}
	for i := range 101 {
		entry := lobby.Entries[99+i]
		wantID := fmt.Sprintf("00000000-0000-0000-0000-%012x", i+1)
		if entry.Source != requestquery.LobbySourceChat || entry.Message.ID != wantID {
			t.Fatalf("entry %d = %+v, want Chat %s", 99+i, entry, wantID)
		}
	}
}

func TestGameRequestLobbyOrdersActivitySequenceNumerically(t *testing.T) {
	fixture := newPlayingFixture(t)
	ctx := t.Context()
	request := fixture.create(t, "Lobby sequence")
	queries := requestquery.NewStore(fixture.pool)
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if _, err := fixture.pool.Exec(ctx, `UPDATE game_request_activity SET occurred_at = $2 WHERE game_request_id = $1`, request.ID, at.Add(-time.Hour)); err != nil {
		t.Fatalf("age fixture activity: %v", err)
	}
	_, err := fixture.pool.Exec(ctx, `
INSERT INTO game_request_activity (sequence, game_request_id, actor_id, kind, occurred_at)
OVERRIDING SYSTEM VALUE
VALUES (2, $1, $2, 'joined', $4), (10, $1, $3, 'left', $4)`,
		request.ID, fixture.host, fixture.guest, at)
	if err != nil {
		t.Fatalf("insert numbered Activity: %v", err)
	}
	for i, id := range []string{"00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000001"} {
		if _, err := fixture.pool.Exec(ctx, `INSERT INTO game_request_messages (id, game_request_id, author_id, body, created_at) VALUES ($1, $2, $3, $4, $5)`, id, request.ID, fixture.host, fmt.Sprintf("chat-%d", i), at); err != nil {
			t.Fatalf("insert tied message: %v", err)
		}
	}
	lobby, err := queries.Lobby(ctx, request.ID, fixture.host)
	if err != nil {
		t.Fatalf("read Lobby: %v", err)
	}
	if len(lobby.Entries) != 5 ||
		lobby.Entries[0].Activity.Kind != gamerequest.ActivityCreated ||
		lobby.Entries[1].Activity.ActorID != fixture.host ||
		lobby.Entries[2].Activity.ActorID != fixture.guest ||
		lobby.Entries[3].Message.ID != "00000000-0000-0000-0000-000000000001" ||
		lobby.Entries[4].Message.ID != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("entries = %+v, want older creation, Activity sequence 2, 10, then Chat UUID order", lobby.Entries)
	}
}

func lobbyActivities(t *testing.T, queries *requestquery.Store, requestID, viewerID string) []requestquery.Activity {
	t.Helper()
	lobby, err := queries.Lobby(t.Context(), requestID, viewerID)
	if err != nil {
		t.Fatalf("read Lobby: %v", err)
	}
	activities := make([]requestquery.Activity, 0, len(lobby.Entries))
	for _, entry := range lobby.Entries {
		if entry.Source == requestquery.LobbySourceActivity {
			activities = append(activities, entry.Activity)
		}
	}
	return activities
}
