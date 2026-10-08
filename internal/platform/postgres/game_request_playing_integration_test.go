//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Start is the one lifecycle change that writes two things the database has to
// keep in step: the request's status plus its actual start timestamp, and the
// `started` activity row that hands the change to notification processing. Both
// land in one locked transaction, so the persisted timestamp and the activity's
// occurred_at are the same PostgreSQL transaction clock reading — no
// application-side clock plumbing can drift them apart, and no rejected or
// duplicated Start can leave a second pair behind.
func TestGameRequestStartPersistsPlayingAtomically(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.create(t, "Atomic start")

	if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("host start: %v", err)
	}

	state := fixture.state(t, request.ID)
	if state.status != string(gamerequest.StatusPlaying) {
		t.Fatalf("status after start = %q, want %q", state.status, gamerequest.StatusPlaying)
	}
	if !state.hasStartedAt {
		t.Fatal("started_at is NULL after start, want the transaction timestamp")
	}
	if got := fixture.countActivity(t, request.ID, "started"); got != 1 {
		t.Fatalf("started activity rows = %d, want exactly 1", got)
	}
	occurredAt := fixture.startedActivityAt(t, request.ID)
	if !occurredAt.Equal(state.startedAt) {
		t.Fatalf("started activity occurred_at = %v, want it equal to started_at %v", occurredAt, state.startedAt)
	}
}

// A Host starting a request that is already playing must not re-stamp the
// actual start or grow a second `started` entry, and starting, closing,
// cancelling, joining and leaving must each keep to their own source state.
func TestGameRequestPlayingLifecycleGuardsEveryTransition(t *testing.T) {
	fixture := newPlayingFixture(t)

	started := fixture.create(t, "Started twice")
	if err := fixture.requests.Start(t.Context(), started.ID, fixture.host); err != nil {
		t.Fatalf("first start: %v", err)
	}
	firstState := fixture.state(t, started.ID)
	if err := fixture.requests.Start(t.Context(), started.ID, fixture.host); !errors.Is(err, gamerequest.ErrNotOpen) {
		t.Fatalf("second start error = %v, want ErrNotOpen", err)
	}
	if state := fixture.state(t, started.ID); !state.startedAt.Equal(firstState.startedAt) || state.status != string(gamerequest.StatusPlaying) {
		t.Fatalf("state after rejected start = %+v, want unchanged %+v", state, firstState)
	}
	if got := fixture.countActivity(t, started.ID, "started"); got != 1 {
		t.Fatalf("started activity rows after a rejected start = %d, want exactly 1", got)
	}
	if err := fixture.requests.Cancel(t.Context(), started.ID, fixture.host); !errors.Is(err, gamerequest.ErrNotOpen) {
		t.Fatalf("cancel after start error = %v, want ErrNotOpen", err)
	}

	guest := fixture.create(t, "Playing is not joinable")
	if err := fixture.requests.Join(t.Context(), guest.ID, fixture.guest); err != nil {
		t.Fatalf("join while open: %v", err)
	}
	if err := fixture.requests.Start(t.Context(), guest.ID, fixture.host); err != nil {
		t.Fatalf("start with a participant: %v", err)
	}
	// Participation stays open while a round is playing: the request is still
	// active, so Join and Leave follow the same rules as before the start.
	if err := fixture.requests.Join(t.Context(), guest.ID, fixture.outsider); err != nil {
		t.Fatalf("join while playing: %v", err)
	}
	if err := fixture.requests.Leave(t.Context(), guest.ID, fixture.guest); err != nil {
		t.Fatalf("leave while playing: %v", err)
	}

	nonHost := fixture.create(t, "Only the host starts")
	if err := fixture.requests.Start(t.Context(), nonHost.ID, fixture.guest); !errors.Is(err, gamerequest.ErrNotCreator) {
		t.Fatalf("non-host start error = %v, want ErrNotCreator", err)
	}
	if state := fixture.state(t, nonHost.ID); state.status != string(gamerequest.StatusOpen) || state.hasStartedAt {
		t.Fatalf("state after a non-host start = %+v, want open with no started_at", state)
	}

	stillOpen := fixture.create(t, "Close needs a played round")
	if err := fixture.requests.Close(t.Context(), stillOpen.ID, fixture.host); !errors.Is(err, gamerequest.ErrNotPlaying) {
		t.Fatalf("close an open request error = %v, want ErrNotPlaying", err)
	}
	if state := fixture.state(t, stillOpen.ID); state.status != string(gamerequest.StatusOpen) || state.hasStartedAt {
		t.Fatalf("state after closing an open request = %+v, want open with no started_at", state)
	}

	closed := fixture.create(t, "Close after start")
	if err := fixture.requests.Start(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("start before close: %v", err)
	}
	startedAt := fixture.state(t, closed.ID).startedAt
	if err := fixture.requests.Close(t.Context(), closed.ID, fixture.host); err != nil {
		t.Fatalf("close after start: %v", err)
	}
	state := fixture.state(t, closed.ID)
	if state.status != string(gamerequest.StatusClosed) {
		t.Fatalf("status after close = %q, want %q", state.status, gamerequest.StatusClosed)
	}
	if !state.startedAt.Equal(startedAt) {
		t.Fatalf("close moved started_at from %v to %v, want the actual start preserved", startedAt, state.startedAt)
	}
	if got := fixture.countActivity(t, closed.ID, "started"); got != 1 {
		t.Fatalf("started activity rows after close = %d, want exactly 1", got)
	}
	if got := fixture.countActivity(t, closed.ID, "closed"); got != 1 {
		t.Fatalf("closed activity rows = %d, want exactly 1", got)
	}
}

// Rapid lifecycle clicking reaches the same row lock, so Start and Cancel cannot
// both win: the request ends either playing with one start, or cancelled with
// no trace of a start at all.
func TestGameRequestStartAgainstCancelYieldsOneMutation(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.create(t, "Start against cancel")

	results := runConcurrently(
		func() error { return fixture.requests.Start(t.Context(), request.ID, fixture.host) },
		func() error { return fixture.requests.Cancel(t.Context(), request.ID, fixture.host) },
	)
	assertOneWinner(t, "start against cancel", results, gamerequest.ErrNotOpen)

	state := fixture.state(t, request.ID)
	startedRows := fixture.countActivity(t, request.ID, "started")
	switch state.status {
	case string(gamerequest.StatusPlaying):
		if !state.hasStartedAt || startedRows != 1 {
			t.Fatalf("playing outcome = %+v with %d started rows, want a timestamp and exactly one row", state, startedRows)
		}
		if fixture.countActivity(t, request.ID, "cancelled") != 0 {
			t.Fatal("a winning start also recorded a cancelled activity")
		}
	case string(gamerequest.StatusCancelled):
		if state.hasStartedAt || startedRows != 0 {
			t.Fatalf("cancelled outcome = %+v with %d started rows, want no timestamp and no start activity", state, startedRows)
		}
		if fixture.countActivity(t, request.ID, "cancelled") != 1 {
			t.Fatal("a winning cancel recorded no cancelled activity")
		}
	default:
		t.Fatalf("raced lifecycle left status = %q, want playing or cancelled", state.status)
	}
}

// The lifecycle invariant lives in PostgreSQL, not only in the store: a writer
// that skips the domain and rows directly still cannot put a request into a
// state the schema considers impossible.
func TestGameRequestStartedAtConstraintsRejectInvalidStates(t *testing.T) {
	fixture := newPlayingFixture(t)

	openRequest := fixture.create(t, "Invalid direct states")
	cases := []struct {
		name  string
		query string
	}{
		{"playing without a start timestamp", "UPDATE game_requests SET status = 'playing' WHERE id = $1"},
		{"open with a start timestamp", "UPDATE game_requests SET started_at = now() WHERE id = $1"},
		{"cancelled with a start timestamp", "UPDATE game_requests SET status = 'cancelled', started_at = now() WHERE id = $1"},
	}
	for _, testCase := range cases {
		if _, err := fixture.pool.Exec(t.Context(), testCase.query, openRequest.ID); err == nil {
			t.Fatalf("%s was accepted, want it rejected by the schema", testCase.name)
		} else if !isCheckConstraintViolation(err) {
			t.Fatalf("%s error = %v, want a check constraint violation", testCase.name, err)
		}
		if state := fixture.state(t, openRequest.ID); state.status != string(gamerequest.StatusOpen) || state.hasStartedAt {
			t.Fatalf("state after rejecting %s = %+v, want the row untouched", testCase.name, state)
		}
	}
}

// Migration 23 must not invent history. A request that closed before the
// playing lifecycle existed stays closed with no actual start, because no
// recorded start ever happened.
func TestMigration23KeepsLegacyClosedRequestWithoutStartedAt(t *testing.T) {
	pool := newTestPool(t)
	migrateToVersion(t, pool, productionMigrationFS(t), 22)

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Legacy Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Legacy Game", "legacy-game")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	created, err := requests.Create(t.Context(), host.ID, selectedGame.ID, nil, time.Now().UTC().Add(time.Hour), "Closed before playing")
	if err != nil {
		t.Fatalf("create request under the pre-playing schema: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE game_requests SET status = 'closed' WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("close the legacy request directly: %v", err)
	}

	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() to the playing lifecycle: %v", err)
	}

	var status string
	var startedAt pgtype.Timestamptz
	if err := pool.QueryRow(t.Context(), `SELECT status, started_at FROM game_requests WHERE id = $1`, created.ID).Scan(&status, &startedAt); err != nil {
		t.Fatalf("read the legacy request: %v", err)
	}
	if status != string(gamerequest.StatusClosed) {
		t.Fatalf("legacy status = %q, want it still %q", status, gamerequest.StatusClosed)
	}
	if startedAt.Valid {
		t.Fatalf("legacy started_at = %v, want NULL: the migration must not invent a start time", startedAt.Time)
	}
	fixture := playingFixture{pool: pool}
	if got := fixture.countActivity(t, created.ID, "started"); got != 0 {
		t.Fatalf("legacy request gained %d started activity rows, want none", got)
	}
}

// A `playing` request is still an active round. The read models that mean "the
// Game Requests happening now" — discovery in every scope, the installation-wide
// navigation count, and the Free places capacity arithmetic — keep it, while a
// terminal `closed` one leaves them. Trending gains exactly the one `started`
// activity Start wrote, because a session really beginning is recent activity
// under the existing six-hour definition and needs no separate scoring rule.
func TestPlayingRequestRemainsActiveInTheReadModels(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)

	playing := fixture.create(t, "Playing while others look")
	if err := fixture.requests.Join(t.Context(), playing.ID, fixture.guest); err != nil {
		t.Fatalf("join as guest: %v", err)
	}
	// Baseline before Start: this request's own `created` and `joined` rows are
	// already scored, and they belong to one Game Request.
	before := trendingEntry(t, queries, "Playing Game")
	if before.ActivityCount != 2 || before.RequestCount != 1 {
		t.Fatalf("trending before start = %d entries across %d requests, want 2 across 1",
			before.ActivityCount, before.RequestCount)
	}

	if err := fixture.requests.Start(t.Context(), playing.ID, fixture.host); err != nil {
		t.Fatalf("host start: %v", err)
	}

	started := trendingEntry(t, queries, "Playing Game")
	if started.ActivityCount != before.ActivityCount+1 {
		t.Fatalf("trending activity after start = %d, want %d plus the started entry",
			started.ActivityCount, before.ActivityCount)
	}
	if started.RequestCount != before.RequestCount {
		t.Fatalf("trending request count after start = %d, want it unchanged at %d",
			started.RequestCount, before.RequestCount)
	}

	// Two more playing requests, one with a place free and one exactly full, so
	// the capacity filter is exercised on the status the discovery list just
	// started to include.
	spare := fixture.createLimited(t, 3, "Playing with room")
	if err := fixture.requests.Join(t.Context(), spare.ID, fixture.guest); err != nil {
		t.Fatalf("join spare request as guest: %v", err)
	}
	full := fixture.createLimited(t, 2, "Playing and full")
	if err := fixture.requests.Join(t.Context(), full.ID, fixture.guest); err != nil {
		t.Fatalf("join full request as guest: %v", err)
	}
	for _, request := range []gamerequest.GameRequest{spare, full} {
		if err := fixture.requests.Start(t.Context(), request.ID, fixture.host); err != nil {
			t.Fatalf("start %q: %v", request.ID, err)
		}
	}

	all := activeRequests(t, queries, fixture.outsider, gamerequestquery.Filter{})
	for _, request := range []gamerequest.GameRequest{playing, spare, full} {
		found := findActive(all, request.ID)
		if found == nil {
			t.Fatalf("playing request %s missing from All discovery: %#v", request.ID, all)
		}
		if found.Status != gamerequest.StatusPlaying {
			t.Fatalf("discovery status for %s = %q, want %q", request.ID, found.Status, gamerequest.StatusPlaying)
		}
	}
	if joined := activeRequests(t, queries, fixture.guest, gamerequestquery.Filter{Scope: gamerequestquery.ScopeJoined}); len(joined) != 3 {
		t.Fatalf("Joined scope for a participant of all three = %#v, want 3 playing requests", joined)
	}

	withPlaces := activeRequests(t, queries, fixture.guest, gamerequestquery.Filter{OpenPlacesOnly: true})
	if findActive(withPlaces, spare.ID) == nil {
		t.Fatalf("Free places dropped a playing request with a place left: %#v", withPlaces)
	}
	if findActive(withPlaces, full.ID) != nil {
		t.Fatalf("Free places kept a playing request that is full: %#v", withPlaces)
	}

	total, err := queries.CountActive(t.Context())
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if total != 3 {
		t.Fatalf("CountActive = %d, want all three playing requests", total)
	}

	// Ending the session is what takes a request out of the active set.
	if err := fixture.requests.Close(t.Context(), playing.ID, fixture.host); err != nil {
		t.Fatalf("close after start: %v", err)
	}
	if found := findActive(activeRequests(t, queries, fixture.outsider, gamerequestquery.Filter{}), playing.ID); found != nil {
		t.Fatalf("closed request still in the active discovery list: %#v", found)
	}
	if total, err := queries.CountActive(t.Context()); err != nil || total != 2 {
		t.Fatalf("CountActive after close = %d, %v, want 2", total, err)
	}
}

// HasCreated backs the composer's first-request hint: only creating a Game
// Request counts, joining one does not, and a request that later ended still
// counts.
func TestHasCreatedCountsOnlyCreatedRequestsInAnyStatus(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)
	hasCreated := func(userID string) bool {
		t.Helper()
		created, err := queries.HasCreated(t.Context(), userID)
		if err != nil {
			t.Fatalf("HasCreated: %v", err)
		}
		return created
	}

	if hasCreated(fixture.host) {
		t.Fatal("host has created before any Game Request exists")
	}
	request := fixture.create(t, "First round")
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest); err != nil {
		t.Fatalf("join as guest: %v", err)
	}
	if err := fixture.requests.Cancel(t.Context(), request.ID, fixture.host); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if !hasCreated(fixture.host) {
		t.Fatal("cancelled request no longer counts as created")
	}
	if hasCreated(fixture.guest) {
		t.Fatal("joining counted as creating")
	}
}

func activeRequests(t *testing.T, queries *gamerequestquery.Store, viewerID string, filter gamerequestquery.Filter) []gamerequestquery.Request {
	t.Helper()

	requests, err := queries.ListActive(t.Context(), viewerID, filter)
	if err != nil {
		t.Fatalf("ListActive(%+v) as %s: %v", filter, viewerID, err)
	}
	return requests
}

func findActive(requests []gamerequestquery.Request, requestID string) *gamerequestquery.Request {
	for index, request := range requests {
		if request.ID == requestID {
			return &requests[index]
		}
	}
	return nil
}

func trendingEntry(t *testing.T, queries *gamerequestquery.Store, gameName string) gamerequestquery.TrendingGame {
	t.Helper()

	entries, err := queries.Trending(t.Context())
	if err != nil {
		t.Fatalf("Trending: %v", err)
	}
	for _, entry := range entries {
		if entry.GameName == gameName {
			return entry
		}
	}
	t.Fatalf("trending has no entry for %q: %#v", gameName, entries)
	return gamerequestquery.TrendingGame{}
}

// createLimited exists because fixture.create leaves capacity unlimited, and the
// Free places arithmetic on a `playing` request needs a limit to compare against.
func (f playingFixture) createLimited(t *testing.T, maxPlayers int32, description string) gamerequest.GameRequest {
	t.Helper()

	request, err := f.requests.Create(t.Context(), f.host, f.gameID, &maxPlayers, f.startsAt, description)
	if err != nil {
		t.Fatalf("create limited game request %q: %v", description, err)
	}
	return request
}

type playingFixture struct {
	pool                  *pgxpool.Pool
	requests              *gamerequest.Service
	host, guest, outsider string
	gameID                string
	startsAt              time.Time
}

func newPlayingFixture(t *testing.T) playingFixture {
	t.Helper()

	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Playing Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Playing Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}
	outsider, err := users.Register(t.Context(), "Playing Outsider")
	if err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Playing Game", "playing-game")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	return playingFixture{
		pool:     pool,
		requests: gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil),
		host:     host.ID,
		guest:    guest.ID,
		outsider: outsider.ID,
		gameID:   selectedGame.ID,
		startsAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond),
	}
}

// create opens an unlimited request with a planned time in the future, so the
// only reason a Start could be refused is the lifecycle itself.
func (f playingFixture) create(t *testing.T, description string) gamerequest.GameRequest {
	t.Helper()

	request, err := f.requests.Create(t.Context(), f.host, f.gameID, nil, f.startsAt, description)
	if err != nil {
		t.Fatalf("create game request %q: %v", description, err)
	}
	return request
}

type playingState struct {
	status       string
	startedAt    time.Time
	hasStartedAt bool
}

func (f playingFixture) state(t *testing.T, requestID string) playingState {
	t.Helper()

	var status string
	var startedAt pgtype.Timestamptz
	if err := f.pool.QueryRow(t.Context(), `SELECT status, started_at FROM game_requests WHERE id = $1`, requestID).Scan(&status, &startedAt); err != nil {
		t.Fatalf("read game request state: %v", err)
	}
	return playingState{status: status, startedAt: startedAt.Time, hasStartedAt: startedAt.Valid}
}

func (f playingFixture) countActivity(t *testing.T, requestID, kind string) int {
	t.Helper()

	var count int
	if err := f.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM game_request_activity WHERE game_request_id = $1 AND kind = $2 AND origin = 'live'`,
		requestID, kind).Scan(&count); err != nil {
		t.Fatalf("count %q activity: %v", kind, err)
	}
	return count
}

func (f playingFixture) startedActivityAt(t *testing.T, requestID string) time.Time {
	t.Helper()

	var occurredAt time.Time
	if err := f.pool.QueryRow(t.Context(),
		`SELECT occurred_at FROM game_request_activity WHERE game_request_id = $1 AND kind = 'started' AND origin = 'live'`,
		requestID).Scan(&occurredAt); err != nil {
		t.Fatalf("read started activity timestamp: %v", err)
	}
	return occurredAt
}

func isCheckConstraintViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23514"
}

// The landing page's badges count only `open` requests and each player once,
// however many open requests they sit in; a started round leaves both counts.
func TestLandingCountsOpenRequestsAndDistinctPlayers(t *testing.T) {
	fixture := newPlayingFixture(t)
	queries := gamerequestquery.NewStore(fixture.pool)

	first := fixture.create(t, "Landing one")
	if err := fixture.requests.Join(t.Context(), first.ID, fixture.guest); err != nil {
		t.Fatalf("join first as guest: %v", err)
	}
	fixture.create(t, "Landing two")

	landing := func() gamerequestquery.LandingGame {
		t.Helper()
		games, err := queries.Landing(t.Context())
		if err != nil {
			t.Fatalf("Landing: %v", err)
		}
		for _, entry := range games {
			if entry.GameName == "Playing Game" {
				return entry
			}
		}
		t.Fatalf("landing has no entry for the Game: %#v", games)
		return gamerequestquery.LandingGame{}
	}

	if got := landing(); got.OpenRequests != 2 || got.Players != 2 {
		t.Fatalf("landing = %d open, %d players; want 2 open, 2 players (host counted once)", got.OpenRequests, got.Players)
	}

	if err := fixture.requests.Start(t.Context(), first.ID, fixture.host); err != nil {
		t.Fatalf("start first: %v", err)
	}
	if got := landing(); got.OpenRequests != 1 || got.Players != 1 {
		t.Fatalf("landing after start = %d open, %d players; want 1 open, 1 player", got.OpenRequests, got.Players)
	}
}
