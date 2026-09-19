// Command browsertest serves LANsurfer's real HTTP stack — the same router,
// templates, assets, and SSE handler as production — against in-memory stores,
// so browser regression tests can drive the client behavior that no Go test
// reaches.
//
// The stores are fakes on purpose. These tests are about what the browser does:
// whether a submit guard holds under rapid clicking, whether a realtime stream
// is released when its page is hidden, whether an aborted request leaves a
// control stuck. Database behavior is already covered by the PostgreSQL
// integration tests, and requiring a database here would only make the browser
// tests slower and flakier.
//
// Nothing in this command is reachable from the application binary, and the
// test control plane below is mounted outside the product router, so no
// test-only route or authentication bypass exists in production code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/platform/sse"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web"
)

const viewerID = "11111111-1111-1111-1111-111111111111"

func main() {
	address := flag.String("address", "127.0.0.1:8123", "listen address")
	flag.Parse()

	state := newFixture()
	mux := http.NewServeMux()
	mux.Handle("/__test__/", state.controlPlane())
	mux.Handle("/", web.NewRouter(web.RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: state,
		Sessions:       state,
		Discovery:      state,
		GameRequests:   state,
		RequestQueries: state,
		Realtime:       state,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}))

	server := &http.Server{Addr: *address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stderr, "browser test fixture listening on %s\n", *address)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type fixtureRequest struct {
	request  requestquery.Request
	members  map[string]bool
	followed bool
}

type fixture struct {
	mu       sync.Mutex
	requests map[string]*fixtureRequest
	order    []string
	next     int

	// creates counts every Game Request that the server actually created. The
	// original defect behind issue #15 was six rapid clicks producing five
	// Game Requests, so the count is the assertion.
	creates int
	joins   int

	// failNext makes the next mutation fail, for the recovery cases.
	failNext string
	// delay stalls mutations so a test can act while one is in flight.
	delay time.Duration

	broker *sse.Broker
}

func newFixture() *fixture {
	state := &fixture{requests: map[string]*fixtureRequest{}, broker: sse.NewBroker()}
	state.seed("Warcraft III", 4, 3)
	state.seed("Quake III Arena", 0, 5)
	state.seed("Age of Empires II", 4, 4)
	return state
}

func (f *fixture) seed(gameName string, maxPlayers int32, members int64) {
	f.next++
	id := fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", f.next)
	request := requestquery.Request{
		ID: id, Game: game.GamePresentation{Name: gameName}, CreatorID: "creator-" + id, CreatorName: "Host",
		StartsAt: time.Now().Add(time.Hour), Status: gamerequest.StatusOpen,
		MemberCount: members, CreatedAt: time.Now().Add(-time.Hour),
		Members:  []requestquery.Member{{ID: "creator-" + id, DisplayName: "Host", JoinedAt: time.Now().Add(-time.Hour)}},
		Activity: []requestquery.Activity{{Kind: gamerequest.ActivityCreated, ActorID: "creator-" + id, ActorName: "Host", OccurredAt: time.Now().Add(-time.Hour)}},
	}
	if maxPlayers > 0 {
		limit := maxPlayers
		request.MaxPlayers = &limit
	}
	f.requests[id] = &fixtureRequest{request: request, members: map[string]bool{}}
	f.order = append(f.order, id)
}

// --- authentication -------------------------------------------------------
//
// Any session cookie resolves to the same player. The fixture never sees a
// credential, and the product's own authentication middleware, CSRF middleware,
// and session cookie handling run unchanged.

func (*fixture) Available() bool { return true }

func (*fixture) Providers() []auth.ProviderAvailability {
	return []auth.ProviderAvailability{{Kind: "oidc", DisplayName: "Test IdP"}}
}

func (*fixture) Begin(context.Context, auth.OAuthStartInput) (auth.OAuthStart, error) {
	return auth.OAuthStart{}, nil
}

func (*fixture) Complete(context.Context, auth.OAuthCompletion) (auth.OAuthResult, error) {
	return auth.OAuthResult{}, nil
}

func (*fixture) Logout(context.Context, string) error { return nil }

func (*fixture) Authenticate(context.Context, string) (user.User, error) {
	return user.User{ID: viewerID, DisplayName: "Ada Player", Role: user.RoleUser}, nil
}

// --- realtime -------------------------------------------------------------

// The connection-exhaustion regression is only observable as a count of the
// streams the server is holding. The product broker reports that itself, so the
// fixture passes both methods straight through.
func (f *fixture) Subscribe() (<-chan gamerequest.Event, func()) { return f.broker.Subscribe() }

func (f *fixture) Subscribers() int { return f.broker.Subscribers() }

// --- game request commands ------------------------------------------------

func (f *fixture) Create(_ context.Context, _, gameID string, maxPlayers *int32, startsAt time.Time, description string) (gamerequest.GameRequest, error) {
	if err := f.beforeMutation("create"); err != nil {
		return gamerequest.GameRequest{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	f.next++
	id := fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", f.next)
	f.requests[id] = &fixtureRequest{
		request: requestquery.Request{
			ID: id, Game: game.GamePresentation{Name: gameName(gameID)}, CreatorID: viewerID, CreatorName: "Ada Player",
			MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description,
			Status: gamerequest.StatusOpen, MemberCount: 1, CreatedAt: time.Now(),
			Members:  []requestquery.Member{{ID: viewerID, DisplayName: "Ada Player", JoinedAt: time.Now()}},
			Activity: []requestquery.Activity{{Kind: gamerequest.ActivityCreated, ActorID: viewerID, ActorName: "Ada Player", OccurredAt: time.Now()}},
		},
		members: map[string]bool{},
	}
	f.order = append(f.order, id)
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventCreated, RequestID: id, GameID: gameID})
	return gamerequest.GameRequest{ID: id, GameID: gameID, CreatorID: viewerID}, nil
}

func (f *fixture) Join(_ context.Context, requestID, userID string) error {
	if err := f.beforeMutation("join"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.members[userID] {
		return gamerequest.ErrAlreadyMember
	}
	if entry.request.MaxPlayers != nil && entry.request.MemberCount >= int64(*entry.request.MaxPlayers) {
		return gamerequest.ErrFull
	}
	f.joins++
	entry.members[userID] = true
	entry.request.MemberCount++
	entry.request.ViewerJoined = true
	entry.request.Members = append(entry.request.Members, requestquery.Member{ID: userID, DisplayName: "Ada Player", JoinedAt: time.Now()})
	entry.request.Activity = append([]requestquery.Activity{{Kind: gamerequest.ActivityJoined, ActorID: userID, ActorName: "Ada Player", OccurredAt: time.Now()}}, entry.request.Activity...)
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventJoined, RequestID: requestID})
	return nil
}

func (f *fixture) Leave(_ context.Context, requestID, userID string) error {
	if err := f.beforeMutation("leave"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if !entry.members[userID] {
		return gamerequest.ErrNotMember
	}
	delete(entry.members, userID)
	entry.request.MemberCount--
	entry.request.ViewerJoined = false
	entry.request.Activity = append([]requestquery.Activity{{Kind: gamerequest.ActivityLeft, ActorID: userID, ActorName: "Ada Player", OccurredAt: time.Now()}}, entry.request.Activity...)
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventLeft, RequestID: requestID})
	return nil
}

func (f *fixture) Close(ctx context.Context, requestID, userID string) error {
	return f.setStatus(ctx, requestID, userID, gamerequest.StatusClosed)
}

func (f *fixture) Cancel(ctx context.Context, requestID, userID string) error {
	return f.setStatus(ctx, requestID, userID, gamerequest.StatusCancelled)
}

func (f *fixture) setStatus(_ context.Context, requestID, _ string, status gamerequest.Status) error {
	if err := f.beforeMutation("status"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.Status != gamerequest.StatusOpen {
		return gamerequest.ErrNotOpen
	}
	entry.request.Status = status
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventClosed, RequestID: requestID})
	return nil
}

// beforeMutation applies whatever failure or stall the current test asked for.
func (f *fixture) beforeMutation(kind string) error {
	f.mu.Lock()
	failure, delay := f.failNext, f.delay
	if failure != "" {
		f.failNext = ""
	}
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	switch failure {
	case "conflict":
		return gamerequest.ErrAlreadyMember
	case "error":
		return fmt.Errorf("%s failed on purpose", kind)
	}
	return nil
}

// --- game request queries -------------------------------------------------

func (f *fixture) ListActive(_ context.Context, _ string, filter requestquery.Filter) ([]requestquery.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]requestquery.Request, 0, len(f.order))
	for _, id := range f.order {
		entry := f.requests[id]
		if entry.request.Status != gamerequest.StatusOpen {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(entry.request.Game.Name), strings.ToLower(filter.Search)) {
			continue
		}
		switch filter.Scope {
		case requestquery.ScopeMyGames:
			if !entry.followed {
				continue
			}
		case requestquery.ScopeJoined:
			if !entry.request.ViewerJoined {
				continue
			}
		case requestquery.ScopeMine:
			if entry.request.CreatorID != viewerID {
				continue
			}
		}
		if filter.OpenPlacesOnly && entry.request.MaxPlayers != nil && entry.request.MemberCount >= int64(*entry.request.MaxPlayers) {
			continue
		}
		result = append(result, entry.request)
	}
	return result, nil
}

func (f *fixture) ByID(_ context.Context, id, _ string) (requestquery.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[id]
	if !ok {
		return requestquery.Request{}, gamerequest.ErrNotFound
	}
	return entry.request, nil
}

// CountOpen is the whole installation's open total, never the filtered subset,
// which is what the navigation badge states.
func (f *fixture) CountOpen(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var total int64
	for _, entry := range f.requests {
		if entry.request.Status == gamerequest.StatusOpen {
			total++
		}
	}
	return total, nil
}

func (f *fixture) Trending(context.Context) ([]requestquery.TrendingGame, error) {
	return []requestquery.TrendingGame{{GameID: "game-1", GameName: "Warcraft III", ActivityCount: 4, RequestCount: 2}}, nil
}

// --- game discovery ---------------------------------------------------
//
// Browser tests never exercise the IGDB provider path (no fixture Game is
// unmaterialized), so Browse only ever returns already-materialized local
// Games and Follow(ProviderReference) is unreachable from these tests.

func (*fixture) Browse(context.Context, string, string) (game.DiscoveryPage, error) {
	return game.DiscoveryPage{Items: []game.DiscoveryItem{{LocalGameID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Materialized: true}}}, nil
}

// Select is the Game Request composer's read-only lookup (Task 7). Browser
// tests only ever select an already-local Game by ID; the provider path is
// unreachable here for the same reason Follow's provider path is.
func (*fixture) Select(_ context.Context, _ string, selection game.GameSelection) (game.DiscoveryItem, error) {
	if selection.LocalGameID == "" {
		return game.DiscoveryItem{}, game.ErrProviderRecordNotFound
	}
	return game.DiscoveryItem{
		LocalGameID:      selection.LocalGameID,
		GamePresentation: game.GamePresentation{Name: gameName(selection.LocalGameID)},
		Materialized:     true,
	}, nil
}

func (*fixture) MyGames(context.Context, string) ([]game.SubscriptionGame, error) {
	return nil, nil
}

func (*fixture) FollowLocal(context.Context, string, string) error { return nil }
func (*fixture) Unfollow(context.Context, string, string) error    { return nil }

func (*fixture) Follow(context.Context, string, game.ProviderReference) (game.Game, error) {
	return game.Game{}, game.ErrProviderRecordNotFound
}

// --- test control plane ---------------------------------------------------

func (f *fixture) controlPlane() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__test__/state", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		payload := map[string]int{"creates": f.creates, "joins": f.joins, "streams": f.broker.Subscribers(), "requests": len(f.order)}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/__test__/publish", func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Query().Get("request")
		if requestID == "" {
			f.mu.Lock()
			if len(f.order) > 0 {
				requestID = f.order[0]
			}
			f.mu.Unlock()
		}
		eventType := gamerequest.EventType(r.URL.Query().Get("type"))
		if eventType == "" {
			eventType = gamerequest.EventJoined
		}
		f.broker.Publish(gamerequest.Event{Type: eventType, RequestID: requestID})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/__test__/mode", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.failNext = r.URL.Query().Get("fail")
		if milliseconds, err := strconv.Atoi(r.URL.Query().Get("delay")); err == nil {
			f.delay = time.Duration(milliseconds) * time.Millisecond
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/__test__/reset", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		fresh := newFixture()
		f.requests, f.order, f.next = fresh.requests, fresh.order, fresh.next
		f.creates, f.joins, f.failNext, f.delay = 0, 0, "", 0
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func gameName(gameID string) string {
	if gameID == "game-2" {
		return "Quake III Arena"
	}
	return "Warcraft III"
}
