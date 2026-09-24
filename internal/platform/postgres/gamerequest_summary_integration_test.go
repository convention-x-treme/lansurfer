//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

func TestGameRequestSummaryReturnsCurrentViewerIndependentSnapshot(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	creator, err := users.Register(t.Context(), "Snapshot Creator")
	if err != nil {
		t.Fatalf("register creator: %v", err)
	}
	member, err := users.Register(t.Context(), "Snapshot Member")
	if err != nil {
		t.Fatalf("register member: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Snapshot Game", "snapshot-game")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	readStore := gamerequestquery.NewStore(pool)
	startsAt := time.Date(2026, 10, 1, 18, 30, 0, 0, time.UTC)

	create := func(description string, maxPlayers *int32) gamerequest.GameRequest {
		t.Helper()
		request, err := requests.Create(t.Context(), creator.ID, selectedGame.ID, maxPlayers, startsAt, description)
		if err != nil {
			t.Fatalf("create request %q: %v", description, err)
		}
		return request
	}
	assertSnapshot := func(request gamerequest.GameRequest, status gamerequest.Status, memberCount int64, maxPlayers *int32) {
		t.Helper()
		got, err := readStore.GetGameRequestSummary(t.Context(), request.ID)
		if err != nil {
			t.Fatalf("GetGameRequestSummary(%q): %v", request.ID, err)
		}
		if got.RequestID != request.ID || got.GameID != selectedGame.ID || got.GameName != selectedGame.Name ||
			!got.StartsAt.Equal(startsAt) || got.Status != status || got.MemberCount != memberCount ||
			got.CreatorGameNick != creator.GameNick || got.Description != request.Description {
			t.Fatalf("snapshot = %#v, want request=%q game=%q status=%q members=%d creator=%q description=%q", got, request.ID, selectedGame.Name, status, memberCount, creator.GameNick, request.Description)
		}
		if (got.MaxPlayers == nil) != (maxPlayers == nil) || (maxPlayers != nil && *got.MaxPlayers != *maxPlayers) {
			t.Fatalf("snapshot MaxPlayers = %v, want %v", got.MaxPlayers, maxPlayers)
		}
	}

	three := int32(3)
	open := create("Open request description", &three)
	assertSnapshot(open, gamerequest.StatusOpen, 1, &three)

	two := int32(2)
	full := create("Full request description", &two)
	if err := requests.Join(t.Context(), full.ID, member.ID); err != nil {
		t.Fatalf("fill request: %v", err)
	}
	assertSnapshot(full, gamerequest.StatusOpen, 2, &two)

	unlimited := create("Unlimited request description", nil)
	assertSnapshot(unlimited, gamerequest.StatusOpen, 1, nil)

	closed := create("Closed request description", &two)
	if err := requests.Close(t.Context(), closed.ID, creator.ID); err != nil {
		t.Fatalf("close request: %v", err)
	}
	assertSnapshot(closed, gamerequest.StatusClosed, 1, &two)

	cancelled := create("Cancelled request description", &two)
	if err := requests.Cancel(t.Context(), cancelled.ID, creator.ID); err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	assertSnapshot(cancelled, gamerequest.StatusCancelled, 1, &two)
}
