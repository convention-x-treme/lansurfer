//go:build integration

package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

// Join and Leave stay available for the whole active round, not only before the
// Host starts it.
func TestPlayingRequestAllowsJoinAndLeave(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 3, "Join while playing")
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("join while playing: %v", err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.outsider); err != nil {
		t.Fatalf("second join while playing: %v", err)
	}
	if got := fixture.memberCount(t, request.ID); got != 3 {
		t.Fatalf("members after joins = %d, want 3", got)
	}
	if err := fixture.requests.Leave(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("leave while playing: %v", err)
	}
	if got := fixture.memberCount(t, request.ID); got != 2 {
		t.Fatalf("members after leave = %d, want 2", got)
	}
	if err := fixture.requests.Leave(t.Context(), request.ID, fixture.host); !errors.Is(err, gamerequest.ErrCreatorCannotLeave) {
		t.Fatalf("host leave while playing error = %v, want ErrCreatorCannotLeave", err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("rejoin into the freed place: %v", err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); !errors.Is(err, gamerequest.ErrAlreadyMember) {
		t.Fatalf("duplicate join error = %v, want ErrAlreadyMember", err)
	}
}

func TestPlayingRequestStillEnforcesCapacity(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 2, "Full while playing")
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("join the last place: %v", err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.outsider); !errors.Is(err, gamerequest.ErrFull) {
		t.Fatalf("join a full playing request error = %v, want ErrFull", err)
	}
}

func TestInactiveRequestRejectsJoinAndLeave(t *testing.T) {
	fixture := newPlayingFixture(t)

	closed := fixture.create(t, "Closed is inactive")
	if err := fixture.requests.Join(t.Context(), closed.ID, fixture.guest); err != nil {
		t.Fatalf("join while open: %v", err)
	}
	if err := fixture.requests.Start(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := fixture.requests.Close(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("close: %v", err)
	}
	cancelled := fixture.create(t, "Cancelled is inactive")
	if err := fixture.requests.Join(t.Context(), cancelled.ID, fixture.guest); err != nil {
		t.Fatalf("join while open: %v", err)
	}
	if err := fixture.requests.Cancel(t.Context(), cancelled.ID, fixture.host); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	for name, request := range map[string]gamerequest.GameRequest{"closed": closed, "cancelled": cancelled} {
		if err := fixture.requests.Join(t.Context(), request.ID, fixture.outsider); !errors.Is(err, gamerequest.ErrNotActive) {
			t.Errorf("join %s error = %v, want ErrNotActive", name, err)
		}
		if err := fixture.requests.Leave(t.Context(), request.ID, fixture.guest); !errors.Is(err, gamerequest.ErrNotActive) {
			t.Errorf("leave %s error = %v, want ErrNotActive", name, err)
		}
		if got := fixture.memberCount(t, request.ID); got != 2 {
			t.Errorf("%s members = %d, want 2 (unchanged)", name, got)
		}
	}
}

func (f playingFixture) memberCount(t *testing.T, requestID string) int {
	t.Helper()

	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM game_request_members WHERE game_request_id = $1`, requestID).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	return count
}

// Migration 25 must not rewrite history: a request that already holds the
// legacy finite limit 1 keeps it, and stays usable.
func TestMigration25PreservesLegacyCapacityOne(t *testing.T) {
	pool := newTestPool(t)
	migrateToVersion(t, pool, productionMigrationFS(t), 24)

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Legacy Capacity Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Legacy Capacity Game", "legacy-capacity-game")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	var requestID string
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO game_requests (game_id, creator_id, max_players, starts_at, description)
		VALUES ($1, $2, 1, now() + interval '1 hour', 'Legacy one')
		RETURNING id::text`, selectedGame.ID, host.ID).Scan(&requestID); err != nil {
		t.Fatalf("insert legacy request: %v", err)
	}

	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() to dynamic capacity: %v", err)
	}

	var maxPlayers *int32
	if err := pool.QueryRow(t.Context(), `SELECT max_players FROM game_requests WHERE id = $1`, requestID).Scan(&maxPlayers); err != nil {
		t.Fatalf("read legacy request: %v", err)
	}
	if maxPlayers == nil || *maxPlayers != 1 {
		t.Fatalf("legacy max_players = %v, want it preserved as 1", maxPlayers)
	}
}

func TestRequestUpdatedActivityMetadataConstraints(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.create(t, "Metadata constraints")
	oldStart := time.Now().UTC().Truncate(time.Microsecond)
	newStart := oldStart.Add(time.Hour)

	insert := func(kind string, previousMax, newMax any, previousStart, nextStart any, descriptionChanged bool) error {
		_, err := fixture.pool.Exec(t.Context(), `
			INSERT INTO game_request_activity (
				game_request_id, actor_id, kind, previous_max_players, new_max_players,
				previous_starts_at, new_starts_at, description_changed
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			request.ID, fixture.host, kind, previousMax, newMax, previousStart, nextStart, descriptionChanged)
		return err
	}

	for name, err := range map[string]error{
		"joined with metadata":       insert("joined", 4, 6, nil, nil, false),
		"update without differences": insert("request_updated", nil, nil, nil, nil, false),
		"equal capacity":             insert("request_updated", 4, 4, nil, nil, false),
		"partial time pair":          insert("request_updated", nil, nil, oldStart, nil, false),
		"equal times":                insert("request_updated", nil, nil, oldStart, oldStart, false),
		"new capacity below 2":       insert("request_updated", 4, 1, nil, nil, false),
		"previous capacity 0":        insert("request_updated", 0, 4, nil, nil, false),
		"unrelated metadata":         insert("created", nil, nil, nil, nil, true),
	} {
		if err == nil || !isCheckConstraintViolation(err) {
			t.Errorf("%s error = %v, want a check constraint violation", name, err)
		}
	}

	for name, err := range map[string]error{
		"legacy 1 to 2":       insert("request_updated", 1, 2, nil, nil, false),
		"finite to unlimited": insert("request_updated", 6, nil, nil, nil, false),
		"unlimited to finite": insert("request_updated", nil, 8, nil, nil, false),
		"start only":          insert("request_updated", nil, nil, oldStart, newStart, false),
		"description only":    insert("request_updated", nil, nil, nil, nil, true),
		"multiple dimensions": insert("request_updated", 4, 6, oldStart, newStart, true),
	} {
		if err != nil {
			t.Errorf("%s error = %v, want accepted", name, err)
		}
	}
}

func TestHostUpdatesOpenDetailsAndPlayingDetails(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "Original")
	newStart := request.StartsAt.Add(time.Hour)

	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, gamerequest.UpdateDetailsInput{
		MaxPlayers: new(int32(6)), StartsAt: newStart, Description: "Updated",
	}); err != nil {
		t.Fatalf("update open request: %v", err)
	}
	if got := fixture.maxPlayers(t, request.ID); got == nil || *got != 6 {
		t.Fatalf("open max_players = %v, want 6", got)
	}
	if got := fixture.requestStart(t, request.ID); !got.Equal(newStart) {
		t.Fatalf("open starts_at = %v, want %v", got, newStart)
	}
	if got := fixture.requestDescription(t, request.ID); got != "Updated" {
		t.Fatalf("open description = %q, want Updated", got)
	}

	var gameID string
	if err := fixture.pool.QueryRow(t.Context(), `SELECT game_id::text FROM game_requests WHERE id = $1`, request.ID).Scan(&gameID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	startedAt := fixture.actualStartedAt(t, request.ID)
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, gamerequest.UpdateDetailsInput{
		MaxPlayers: new(int32(5)), StartsAt: newStart, Description: "Playing details",
	}); err != nil {
		t.Fatalf("update playing capacity and description: %v", err)
	}
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, gamerequest.UpdateDetailsInput{
		MaxPlayers: new(int32(4)), StartsAt: newStart.Add(time.Hour), Description: "Must not save",
	}); !errors.Is(err, gamerequest.ErrInvalidStartsAt) {
		t.Fatalf("playing start edit error = %v, want ErrInvalidStartsAt", err)
	}

	var gotGameID string
	if err := fixture.pool.QueryRow(t.Context(), `SELECT game_id::text FROM game_requests WHERE id = $1`, request.ID).Scan(&gotGameID); err != nil {
		t.Fatal(err)
	}
	if gotGameID != gameID {
		t.Errorf("game_id changed from %s to %s", gameID, gotGameID)
	}
	if got := fixture.actualStartedAt(t, request.ID); !got.Equal(startedAt) {
		t.Errorf("started_at changed from %v to %v", startedAt, got)
	}
	if got := fixture.maxPlayers(t, request.ID); got == nil || *got != 5 {
		t.Errorf("playing max_players = %v, want 5 after rejected partial update", got)
	}
	if got := fixture.requestDescription(t, request.ID); got != "Playing details" {
		t.Errorf("playing description = %q after rejected partial update, want Playing details", got)
	}
}

func TestDetailsNoOpWritesNothingAndUpdateActivityIsTyped(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "Original")
	input := gamerequest.UpdateDetailsInput{
		MaxPlayers: new(int32(6)), StartsAt: request.StartsAt.Add(time.Hour), Description: "Changed",
	}
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, input); err != nil {
		t.Fatal(err)
	}
	if got := fixture.countActivity(t, request.ID, "request_updated"); got != 1 {
		t.Fatalf("request_updated rows = %d, want one", got)
	}
	var previousMax, newMax *int32
	var previousStart, newStart *time.Time
	var descriptionChanged bool
	var activityDescription string
	if err := fixture.pool.QueryRow(t.Context(), `
		SELECT previous_max_players, new_max_players, previous_starts_at, new_starts_at, description_changed,
		       row_to_json(game_request_activity)::text
		FROM game_request_activity
		WHERE game_request_id = $1 AND kind = 'request_updated'`, request.ID).
		Scan(&previousMax, &newMax, &previousStart, &newStart, &descriptionChanged, &activityDescription); err != nil {
		t.Fatal(err)
	}
	if !sameInt32Pointer(previousMax, new(int32(4))) || !sameInt32Pointer(newMax, new(int32(6))) {
		t.Fatalf("capacity metadata = %v -> %v, want 4 -> 6", previousMax, newMax)
	}
	if previousStart == nil || !previousStart.Equal(request.StartsAt) || newStart == nil || !newStart.Equal(input.StartsAt) {
		t.Fatalf("start metadata = %v -> %v, want %v -> %v", previousStart, newStart, request.StartsAt, input.StartsAt)
	}
	if !descriptionChanged || strings.Contains(activityDescription, "Changed") {
		t.Fatalf("description metadata leaked text or lost changed marker: %q, changed=%v", activityDescription, descriptionChanged)
	}

	updatedAt := fixture.updatedAt(t, request.ID)
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, input); err != nil {
		t.Fatalf("no-op UpdateDetails: %v", err)
	}
	if got := fixture.updatedAt(t, request.ID); !got.Equal(updatedAt) {
		t.Errorf("no-op moved updated_at from %v to %v", updatedAt, got)
	}
	if got := fixture.countActivity(t, request.ID, "request_updated"); got != 1 {
		t.Errorf("no-op added request_updated rows: %d, want 1", got)
	}
}

func TestDetailActivityProjectsRequestUpdateMetadata(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)
	request := fixture.createLimited(t, 4, "Read-model history")
	input := gamerequest.UpdateDetailsInput{
		MaxPlayers: new(int32(6)), StartsAt: request.StartsAt.Add(time.Hour), Description: "Updated description",
	}
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host, input); err != nil {
		t.Fatal(err)
	}
	detail, err := queries.ByID(t.Context(), request.ID, fixture.host)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Activity) != 2 || detail.Activity[0].Kind != gamerequest.ActivityUpdated {
		t.Fatalf("activity = %#v, want request_updated plus created", detail.Activity)
	}
	entry := detail.Activity[0]
	if !sameInt32Pointer(entry.PreviousMaxPlayers, new(int32(4))) || !sameInt32Pointer(entry.NewMaxPlayers, new(int32(6))) {
		t.Errorf("capacity projection = %v -> %v, want 4 -> 6", entry.PreviousMaxPlayers, entry.NewMaxPlayers)
	}
	if entry.PreviousStartsAt == nil || !entry.PreviousStartsAt.Equal(request.StartsAt) ||
		entry.NewStartsAt == nil || !entry.NewStartsAt.Equal(input.StartsAt) || !entry.DescriptionChanged {
		t.Errorf("request update projection = %+v", entry)
	}
	created := detail.Activity[1]
	if created.PreviousMaxPlayers != nil || created.NewMaxPlayers != nil ||
		created.PreviousStartsAt != nil || created.NewStartsAt != nil || created.DescriptionChanged {
		t.Errorf("created activity has update metadata: %+v", created)
	}
}

func TestHostUpdatesCapacityWhileOpenAndPlaying(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "Capacity while open and playing")

	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(6))); err != nil {
		t.Fatalf("raise capacity while open: %v", err)
	}
	if got := fixture.maxPlayers(t, request.ID); got == nil || *got != 6 {
		t.Fatalf("max_players after open edit = %v, want 6", got)
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(5))); err != nil {
		t.Fatalf("lower capacity while playing: %v", err)
	}
	if got := fixture.maxPlayers(t, request.ID); got == nil || *got != 5 {
		t.Fatalf("max_players after playing edit = %v, want 5", got)
	}
	if got := fixture.countActivity(t, request.ID, "request_updated"); got != 2 {
		t.Fatalf("request_updated rows = %d, want 2", got)
	}
}

func TestCapacityCanReturnToUnlimited(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "Remove the limit")

	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, nil); err != nil {
		t.Fatalf("remove capacity: %v", err)
	}
	if got := fixture.maxPlayers(t, request.ID); got != nil {
		t.Fatalf("max_players after removal = %d, want NULL", *got)
	}
	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(8))); err != nil {
		t.Fatalf("set capacity again: %v", err)
	}
	if got := fixture.maxPlayers(t, request.ID); got == nil || *got != 8 {
		t.Fatalf("max_players after re-setting = %v, want 8", got)
	}
}

func TestCapacityRejectsNonHostAndInactiveRequests(t *testing.T) {
	fixture := newPlayingFixture(t)

	open := fixture.createLimited(t, 4, "Only the Host edits")
	if err := fixture.requests.Join(t.Context(), open.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := fixture.updateCapacity(t.Context(), open.ID, fixture.guest, new(int32(6))); !errors.Is(err, gamerequest.ErrNotCreator) {
		t.Fatalf("participant edit error = %v, want ErrNotCreator", err)
	}
	if err := fixture.updateCapacity(t.Context(), open.ID, fixture.outsider, nil); !errors.Is(err, gamerequest.ErrNotCreator) {
		t.Fatalf("outsider edit error = %v, want ErrNotCreator", err)
	}
	fixture.assertCapacityUntouched(t, open.ID, 4)

	closed := fixture.createLimited(t, 4, "Closed is not editable")
	if err := fixture.requests.Start(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := fixture.requests.Close(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("close: %v", err)
	}
	cancelled := fixture.createLimited(t, 4, "Cancelled is not editable")
	if err := fixture.requests.Cancel(t.Context(), cancelled.ID, fixture.host); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	for name, request := range map[string]gamerequest.GameRequest{"closed": closed, "cancelled": cancelled} {
		if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(6))); !errors.Is(err, gamerequest.ErrNotActive) {
			t.Errorf("%s edit error = %v, want ErrNotActive", name, err)
		}
		fixture.assertCapacityUntouched(t, request.ID, 4)
	}
}

func TestCapacityCannotDropBelowCurrentMembership(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "Not below members")
	for _, userID := range []string{fixture.guest, fixture.outsider} {
		if err := fixture.requests.Join(t.Context(), request.ID, userID); err != nil {
			t.Fatalf("join: %v", err)
		}
	}

	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(2))); !errors.Is(err, gamerequest.ErrCapacityBelowMembers) {
		t.Fatalf("reduce below members error = %v, want ErrCapacityBelowMembers", err)
	}
	fixture.assertCapacityUntouched(t, request.ID, 4)
	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(3))); err != nil {
		t.Fatalf("reduce to exactly the member count: %v", err)
	}
}

func TestCapacityNoOpWritesNothing(t *testing.T) {
	fixture := newPlayingFixture(t)
	limited := fixture.createLimited(t, 4, "Same finite value")
	unlimited := fixture.create(t, "Unlimited to unlimited")

	limitedBefore := fixture.updatedAt(t, limited.ID)
	unlimitedBefore := fixture.updatedAt(t, unlimited.ID)
	if err := fixture.updateCapacity(t.Context(), limited.ID, fixture.host, new(int32(4))); err != nil {
		t.Fatalf("same finite value: %v", err)
	}
	if err := fixture.updateCapacity(t.Context(), unlimited.ID, fixture.host, nil); err != nil {
		t.Fatalf("unlimited to unlimited: %v", err)
	}

	for name, request := range map[string]gamerequest.GameRequest{"finite": limited, "unlimited": unlimited} {
		if got := fixture.countActivity(t, request.ID, "request_updated"); got != 0 {
			t.Errorf("%s no-op wrote %d request_updated rows, want none", name, got)
		}
	}
	if got := fixture.updatedAt(t, limited.ID); !got.Equal(limitedBefore) {
		t.Errorf("finite no-op moved updated_at from %v to %v", limitedBefore, got)
	}
	if got := fixture.updatedAt(t, unlimited.ID); !got.Equal(unlimitedBefore) {
		t.Errorf("unlimited no-op moved updated_at from %v to %v", unlimitedBefore, got)
	}
}

func TestCapacityChangeStoresBeforeAndAfterValues(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 4, "History values")

	for _, next := range []*int32{new(int32(6)), nil, new(int32(8))} {
		if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, next); err != nil {
			t.Fatalf("update capacity: %v", err)
		}
	}

	got := fixture.capacityHistory(t, request.ID)
	want := []capacityChange{
		{previous: new(int32(4)), next: new(int32(6))},
		{previous: new(int32(6)), next: nil},
		{previous: nil, next: new(int32(8))},
	}
	if len(got) != len(want) {
		t.Fatalf("capacity history = %+v, want %d rows", got, len(want))
	}
	for index := range want {
		if !sameInt32Pointer(got[index].previous, want[index].previous) || !sameInt32Pointer(got[index].next, want[index].next) {
			t.Errorf("capacity history[%d] = %s, want %s", index, got[index], want[index])
		}
	}
	var otherKindsWithMetadata int
	if err := fixture.pool.QueryRow(t.Context(), `
		SELECT count(*) FROM game_request_activity
		WHERE game_request_id = $1 AND kind <> 'request_updated'
		  AND (previous_max_players IS NOT NULL OR new_max_players IS NOT NULL)`, request.ID).Scan(&otherKindsWithMetadata); err != nil {
		t.Fatalf("count metadata on other kinds: %v", err)
	}
	if otherKindsWithMetadata != 0 {
		t.Fatalf("%d non-capacity activity rows carry capacity metadata", otherKindsWithMetadata)
	}
}

// A request created before the finite minimum became 2 may hold 1. The Host can
// move it to unlimited or to 2 or more, and history records the legacy 1 as the
// old value, but a freshly submitted 1 is rejected.
func TestLegacyCapacityOneCanBeChangedButNotResubmitted(t *testing.T) {
	fixture := newPlayingFixture(t)
	toUnlimited := fixture.insertLegacyCapacityOne(t, "Legacy to unlimited")
	toFinite := fixture.insertLegacyCapacityOne(t, "Legacy to finite")
	resubmitted := fixture.insertLegacyCapacityOne(t, "Legacy resubmitted")

	if err := fixture.updateCapacity(t.Context(), resubmitted, fixture.host, new(int32(1))); !errors.Is(err, gamerequest.ErrInvalidMaxPlayers) {
		t.Fatalf("resubmitting 1 error = %v, want ErrInvalidMaxPlayers", err)
	}
	fixture.assertCapacityUntouched(t, resubmitted, 1)
	if err := fixture.updateCapacity(t.Context(), toUnlimited, fixture.host, nil); err != nil {
		t.Fatalf("legacy 1 to unlimited: %v", err)
	}
	if err := fixture.updateCapacity(t.Context(), toFinite, fixture.host, new(int32(2))); err != nil {
		t.Fatalf("legacy 1 to 2: %v", err)
	}

	history := fixture.capacityHistory(t, toFinite)
	if len(history) != 1 || !sameInt32Pointer(history[0].previous, new(int32(1))) || !sameInt32Pointer(history[0].next, new(int32(2))) {
		t.Fatalf("legacy history = %+v, want 1 -> 2", history)
	}
}

func TestConcurrentJoinsCannotExceedPlayingCapacity(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 2, "Last place race")
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}

	results := runConcurrently(
		func() error { return fixture.requests.Join(t.Context(), request.ID, fixture.guest) },
		func() error { return fixture.requests.Join(t.Context(), request.ID, fixture.outsider) },
	)
	assertOneWinner(t, "joins for the last place", results, gamerequest.ErrFull)
	fixture.assertWithinCapacity(t, request.ID)
}

func TestConcurrentJoinAndCapacityReductionPreserveInvariant(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 3, "Join against reduction")
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}

	results := runConcurrently(
		func() error { return fixture.requests.Join(t.Context(), request.ID, fixture.outsider) },
		func() error {
			return fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(2)))
		},
	)
	// Either lock order is legal; exactly one failure kind belongs to each loser.
	if err := results[0]; err != nil && !errors.Is(err, gamerequest.ErrFull) {
		t.Fatalf("join error = %v, want nil or ErrFull", err)
	}
	if err := results[1]; err != nil && !errors.Is(err, gamerequest.ErrCapacityBelowMembers) {
		t.Fatalf("reduction error = %v, want nil or ErrCapacityBelowMembers", err)
	}
	if results[0] == nil && results[1] == nil {
		t.Fatal("join and reduction to 2 both succeeded: 3 members would exceed the limit")
	}
	fixture.assertWithinCapacity(t, request.ID)
}

func TestConcurrentLeaveAndCapacityReductionPreserveInvariant(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.createLimited(t, 3, "Leave against reduction")
	for _, userID := range []string{fixture.guest, fixture.outsider} {
		if err := fixture.requests.Join(t.Context(), request.ID, userID); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}

	results := runConcurrently(
		func() error { return fixture.requests.Leave(t.Context(), request.ID, fixture.guest) },
		func() error {
			return fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(2)))
		},
	)
	if results[0] != nil {
		t.Fatalf("leave error = %v, want success in either lock order", results[0])
	}
	if err := results[1]; err != nil && !errors.Is(err, gamerequest.ErrCapacityBelowMembers) {
		t.Fatalf("reduction error = %v, want nil or ErrCapacityBelowMembers", err)
	}
	fixture.assertWithinCapacity(t, request.ID)
}

type capacityChange struct{ previous, next *int32 }

func (c capacityChange) String() string {
	return formatCapacity(c.previous) + " -> " + formatCapacity(c.next)
}

func formatCapacity(value *int32) string {
	if value == nil {
		return "unlimited"
	}
	return strconv.Itoa(int(*value))
}

func sameInt32Pointer(a, b *int32) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (f playingFixture) maxPlayers(t *testing.T, requestID string) *int32 {
	t.Helper()

	var maxPlayers *int32
	if err := f.pool.QueryRow(t.Context(), `SELECT max_players FROM game_requests WHERE id = $1`, requestID).Scan(&maxPlayers); err != nil {
		t.Fatalf("read max_players: %v", err)
	}
	return maxPlayers
}

func (f playingFixture) updatedAt(t *testing.T, requestID string) time.Time {
	t.Helper()

	var updatedAt time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT updated_at FROM game_requests WHERE id = $1`, requestID).Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	return updatedAt
}

func (f playingFixture) requestStart(t *testing.T, requestID string) time.Time {
	t.Helper()
	var value time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT starts_at FROM game_requests WHERE id = $1`, requestID).Scan(&value); err != nil {
		t.Fatalf("read starts_at: %v", err)
	}
	return value
}

func (f playingFixture) actualStartedAt(t *testing.T, requestID string) time.Time {
	t.Helper()
	var value time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT started_at FROM game_requests WHERE id = $1`, requestID).Scan(&value); err != nil {
		t.Fatalf("read started_at: %v", err)
	}
	return value
}

func (f playingFixture) requestDescription(t *testing.T, requestID string) string {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(t.Context(), `SELECT description FROM game_requests WHERE id = $1`, requestID).Scan(&value); err != nil {
		t.Fatalf("read description: %v", err)
	}
	return value
}

func (f playingFixture) updateCapacity(ctx context.Context, requestID, userID string, maxPlayers *int32) error {
	var startsAt time.Time
	var description string
	if err := f.pool.QueryRow(ctx, `SELECT starts_at, description FROM game_requests WHERE id = $1`, requestID).
		Scan(&startsAt, &description); err != nil {
		return err
	}
	return f.requests.UpdateDetails(ctx, requestID, userID, gamerequest.UpdateDetailsInput{
		MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description,
	})
}

func (f playingFixture) capacityHistory(t *testing.T, requestID string) []capacityChange {
	t.Helper()

	rows, err := f.pool.Query(t.Context(), `
		SELECT previous_max_players, new_max_players FROM game_request_activity
		WHERE game_request_id = $1 AND kind = 'request_updated' AND origin = 'live'
		ORDER BY sequence`, requestID)
	if err != nil {
		t.Fatalf("read capacity history: %v", err)
	}
	defer rows.Close()
	var history []capacityChange
	for rows.Next() {
		var change capacityChange
		if err := rows.Scan(&change.previous, &change.next); err != nil {
			t.Fatalf("scan capacity history: %v", err)
		}
		history = append(history, change)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate capacity history: %v", err)
	}
	return history
}

// assertCapacityUntouched proves a rejected edit left the stored maximum and the
// capacity history as they were.
func (f playingFixture) assertCapacityUntouched(t *testing.T, requestID string, want int32) {
	t.Helper()

	if got := f.maxPlayers(t, requestID); got == nil || *got != want {
		t.Fatalf("max_players = %v after a rejected edit, want %d", got, want)
	}
	if got := f.countActivity(t, requestID, "request_updated"); got != 0 {
		t.Fatalf("rejected edit left %d request_updated rows", got)
	}
}

func (f playingFixture) assertWithinCapacity(t *testing.T, requestID string) {
	t.Helper()

	var withinCapacity bool
	var members int
	if err := f.pool.QueryRow(t.Context(), `
		SELECT r.max_players IS NULL OR m.count <= r.max_players, m.count
		FROM game_requests r,
		     LATERAL (SELECT count(*) FROM game_request_members WHERE game_request_id = r.id) m
		WHERE r.id = $1`, requestID).Scan(&withinCapacity, &members); err != nil {
		t.Fatalf("read capacity invariant: %v", err)
	}
	if !withinCapacity {
		t.Fatalf("request committed %d members above its maximum %v", members, f.maxPlayers(t, requestID))
	}
}

func (f playingFixture) insertLegacyCapacityOne(t *testing.T, description string) string {
	t.Helper()

	var requestID string
	if err := f.pool.QueryRow(t.Context(), `
		WITH created AS (
			INSERT INTO game_requests (game_id, creator_id, max_players, starts_at, description)
			VALUES ($1, $2, 1, $3, $4)
			RETURNING id
		), member AS (
			INSERT INTO game_request_members (game_request_id, user_id)
			SELECT id, $2 FROM created
		)
		SELECT id::text FROM created`, f.gameID, f.host, f.startsAt, description).Scan(&requestID); err != nil {
		t.Fatalf("insert legacy capacity-one request: %v", err)
	}
	return requestID
}

// The detail read model carries the typed before/after values so the Activity
// section can state the concrete change; NULL stays "unlimited".
func TestDetailActivityProjectsCapacityChanges(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)
	request := fixture.createLimited(t, 4, "Projected history")

	for _, next := range []*int32{new(int32(6)), nil, new(int32(8))} {
		if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, next); err != nil {
			t.Fatalf("update capacity: %v", err)
		}
	}

	detail, err := queries.ByID(t.Context(), request.ID, fixture.host)
	if err != nil {
		t.Fatalf("read detail: %v", err)
	}
	// Newest first: unlimited -> 8, 6 -> unlimited, 4 -> 6, then created.
	want := []capacityChange{
		{previous: nil, next: new(int32(8))},
		{previous: new(int32(6)), next: nil},
		{previous: new(int32(4)), next: new(int32(6))},
	}
	if len(detail.Activity) != len(want)+1 {
		t.Fatalf("activity = %+v, want %d capacity entries plus created", detail.Activity, len(want))
	}
	for index, change := range want {
		entry := detail.Activity[index]
		if entry.Kind != gamerequest.ActivityUpdated {
			t.Fatalf("activity[%d] kind = %q, want request_updated", index, entry.Kind)
		}
		if !sameInt32Pointer(entry.PreviousMaxPlayers, change.previous) || !sameInt32Pointer(entry.NewMaxPlayers, change.next) {
			t.Errorf("activity[%d] = %s -> %s, want %s", index,
				formatCapacity(entry.PreviousMaxPlayers), formatCapacity(entry.NewMaxPlayers), change)
		}
	}
	created := detail.Activity[len(want)]
	if created.Kind != gamerequest.ActivityCreated || created.PreviousMaxPlayers != nil || created.NewMaxPlayers != nil {
		t.Errorf("created entry = %+v, want no capacity metadata", created)
	}
}

// Free places follows the live capacity of a playing request, and a Host
// configuration change is not popularity: Trending does not count it.
func TestCapacityChangesFollowFreePlacesAndStayOutOfTrending(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)
	request := fixture.createLimited(t, 2, "Capacity and discovery")
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	freePlaces := gamerequestquery.Filter{OpenPlacesOnly: true}

	if findActive(activeRequests(t, queries, fixture.outsider, freePlaces), request.ID) != nil {
		t.Fatal("Free places kept a full playing request")
	}
	before := trendingEntry(t, queries, "Playing Game")

	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(3))); err != nil {
		t.Fatalf("raise capacity: %v", err)
	}
	if findActive(activeRequests(t, queries, fixture.outsider, freePlaces), request.ID) == nil {
		t.Fatal("Free places dropped a playing request after capacity was raised")
	}
	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, nil); err != nil {
		t.Fatalf("remove capacity: %v", err)
	}
	if err := fixture.updateCapacity(t.Context(), request.ID, fixture.host, new(int32(2))); err != nil {
		t.Fatalf("lower capacity back to the member count: %v", err)
	}
	if findActive(activeRequests(t, queries, fixture.outsider, freePlaces), request.ID) != nil {
		t.Fatal("Free places kept a playing request after capacity was lowered to full")
	}

	after := trendingEntry(t, queries, "Playing Game")
	if after.ActivityCount != before.ActivityCount || after.RequestCount != before.RequestCount {
		t.Fatalf("trending after 3 capacity changes = %d/%d, want unchanged %d/%d",
			after.ActivityCount, after.RequestCount, before.ActivityCount, before.RequestCount)
	}
}
