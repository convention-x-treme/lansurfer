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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/platform/sse"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web"
)

const (
	viewerID      = "11111111-1111-1111-1111-111111111111"
	otherViewerID = "22222222-2222-2222-2222-222222222222"
)

func main() {
	address := flag.String("address", "127.0.0.1:8123", "listen address")
	flag.Parse()

	state := newFixture()
	mux := http.NewServeMux()
	mux.Handle("/__test__/", state.controlPlane())
	mux.Handle("/", web.NewRouter(web.RouterDependencies{
		Ready:                      func(context.Context) error { return nil },
		Authentication:             state,
		Sessions:                   state,
		Identities:                 state,
		Discovery:                  state,
		GameRequests:               state,
		Chat:                       state,
		LobbyAccess:                state,
		LobbyQueries:               state,
		SessionInformation:         state,
		RequestQueries:             state,
		NavigationQueries:          state,
		NavigationCatalog:          state,
		Realtime:                   state,
		RealtimeSessionInformation: state,
		Logger:                     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}))

	server := &http.Server{Addr: *address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(os.Stderr, "browser test fixture listening on %s\n", *address)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type fixtureRequest struct {
	request            requestquery.Request
	members            map[string]bool
	followed           bool
	activity           []requestquery.Activity
	messages           []gamerequest.Message
	sessionInformation []gamerequest.SessionInformationEntry
	nextSessionEntry   int64
}

type fixture struct {
	mu       sync.Mutex
	requests map[string]*fixtureRequest
	order    []string
	next     int
	viewed   []requestquery.RecentRequest

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
	state.seed("Warcraft III", 4, 3, gamerequest.StatusOpen)
	state.seed("Quake III Arena", 0, 5, gamerequest.StatusOpen)
	// A request somebody else already started that still has one free place:
	// the viewer is a participant candidate here, never its Host.
	request := state.seed("Age of Empires II", 4, 3, gamerequest.StatusPlaying)
	startedAt := time.Now().Add(-3 * time.Minute)
	request.request.StartedAt = &startedAt
	return state
}

func (f *fixture) seed(gameName string, maxPlayers int32, members int64, status gamerequest.Status) *fixtureRequest {
	f.next++
	id := fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", f.next)
	request := requestquery.Request{
		ID: id, Game: game.GamePresentation{Name: gameName}, CreatorID: "creator-" + id, CreatorGameNick: "Host",
		StartsAt: time.Now().Add(time.Hour), Status: status,
		MemberCount: members, CreatedAt: time.Now().Add(-time.Hour),
		Members: []requestquery.Member{{ID: "creator-" + id, GameNick: "Host", JoinedAt: time.Now().Add(-time.Hour)}},
	}
	if maxPlayers > 0 {
		limit := maxPlayers
		request.MaxPlayers = &limit
	}
	entry := &fixtureRequest{
		request: request,
		members: map[string]bool{request.CreatorID: true},
		activity: []requestquery.Activity{{
			Kind: gamerequest.ActivityCreated, ActorID: request.CreatorID,
			ActorGameNick: "Host", OccurredAt: request.CreatedAt,
		}},
	}
	f.requests[id] = entry
	f.order = append(f.order, id)
	return entry
}

// --- authentication -------------------------------------------------------
//
// Browser tests use two fixed players when they need to verify another
// viewer's read model; all product authentication middleware still runs.

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

func (*fixture) Authenticate(_ context.Context, session string) (user.User, error) {
	if session == "browser-test-other" {
		return user.User{ID: otherViewerID, GameNick: "Grace Player", Role: user.RoleUser}, nil
	}
	return user.User{ID: viewerID, GameNick: "Ada Player", Role: user.RoleUser}, nil
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
	createdAt := time.Now()
	f.requests[id] = &fixtureRequest{
		request: requestquery.Request{
			ID: id, Game: game.GamePresentation{Name: gameName(gameID)}, CreatorID: viewerID, CreatorGameNick: "Ada Player",
			MaxPlayers: maxPlayers, StartsAt: startsAt, Description: description,
			Status: gamerequest.StatusOpen, MemberCount: 1, CreatedAt: createdAt,
			Members: []requestquery.Member{{ID: viewerID, GameNick: "Ada Player", JoinedAt: createdAt}},
		},
		members: map[string]bool{viewerID: true},
		activity: []requestquery.Activity{{
			Kind: gamerequest.ActivityCreated, ActorID: viewerID,
			ActorGameNick: "Ada Player", OccurredAt: createdAt,
		}},
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
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	if entry.members[userID] || entry.request.CreatorID == userID {
		return gamerequest.ErrAlreadyMember
	}
	if entry.request.MaxPlayers != nil && entry.request.MemberCount >= int64(*entry.request.MaxPlayers) {
		return gamerequest.ErrFull
	}
	f.joins++
	entry.members[userID] = true
	entry.request.MemberCount++
	entry.request.ViewerJoined = true
	entry.request.Members = append(entry.request.Members, requestquery.Member{ID: userID, GameNick: "Ada Player", JoinedAt: time.Now()})
	entry.activity = append(entry.activity, requestquery.Activity{Kind: gamerequest.ActivityJoined, ActorID: userID, ActorGameNick: "Ada Player", OccurredAt: time.Now()})
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
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	if entry.request.CreatorID == userID {
		return gamerequest.ErrCreatorCannotLeave
	}
	if !entry.members[userID] {
		return gamerequest.ErrNotMember
	}
	delete(entry.members, userID)
	entry.request.MemberCount--
	entry.request.ViewerJoined = false
	entry.activity = append(entry.activity, requestquery.Activity{Kind: gamerequest.ActivityLeft, ActorID: userID, ActorGameNick: "Ada Player", OccurredAt: time.Now()})
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventLeft, RequestID: requestID})
	return nil
}

func (f *fixture) Start(_ context.Context, requestID, userID string) error {
	return f.transition(requestID, userID, gamerequest.StatusOpen, gamerequest.StatusPlaying, gamerequest.ActivityStarted, gamerequest.EventStarted)
}

func (f *fixture) Close(_ context.Context, requestID, userID string) error {
	return f.transition(requestID, userID, gamerequest.StatusPlaying, gamerequest.StatusClosed, gamerequest.ActivityClosed, gamerequest.EventClosed)
}

func (f *fixture) Cancel(_ context.Context, requestID, userID string) error {
	return f.transition(requestID, userID, gamerequest.StatusOpen, gamerequest.StatusCancelled, gamerequest.ActivityCancelled, gamerequest.EventCancelled)
}

// UpdateDetails mirrors the store's Host and active-request rules. PostgreSQL
// row locking is not emulated; the integration tests own that.
func (f *fixture) UpdateDetails(_ context.Context, requestID, userID string, input gamerequest.UpdateDetailsInput) error {
	if err := f.beforeMutation("details"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.CreatorID != userID {
		return gamerequest.ErrNotCreator
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	previousMaxPlayers := entry.request.MaxPlayers
	previousStartsAt := entry.request.StartsAt
	capacityChanged := !sameCapacity(previousMaxPlayers, input.MaxPlayers)
	startsChanged := !previousStartsAt.Equal(input.StartsAt)
	descriptionChanged := entry.request.Description != input.Description
	if entry.request.Status == gamerequest.StatusPlaying && startsChanged {
		return gamerequest.ErrInvalidStartsAt
	}
	if capacityChanged && input.MaxPlayers != nil && entry.request.MemberCount > int64(*input.MaxPlayers) {
		return gamerequest.ErrCapacityBelowMembers
	}
	if !capacityChanged && !startsChanged && !descriptionChanged {
		return nil
	}
	entry.request.MaxPlayers = input.MaxPlayers
	entry.request.StartsAt = input.StartsAt
	entry.request.Description = input.Description
	activity := requestquery.Activity{
		Kind: gamerequest.ActivityUpdated, ActorID: userID, ActorGameNick: "Ada Player", OccurredAt: time.Now(),
		DescriptionChanged: descriptionChanged,
	}
	if capacityChanged {
		activity.PreviousMaxPlayers = previousMaxPlayers
		activity.NewMaxPlayers = input.MaxPlayers
	}
	if startsChanged {
		activity.PreviousStartsAt = &previousStartsAt
		newStartsAt := input.StartsAt
		activity.NewStartsAt = &newStartsAt
	}
	entry.activity = append(entry.activity, activity)
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventUpdated, RequestID: requestID})
	return nil
}

func sameCapacity(previous, next *int32) bool {
	return previous == nil && next == nil || previous != nil && next != nil && *previous == *next
}

// transition moves a request from one status to the next. Start and Cancel
// need `open`; Close needs `playing`.
func (f *fixture) transition(requestID, userID string, from, to gamerequest.Status, kind gamerequest.ActivityKind, event gamerequest.EventType) error {
	if err := f.beforeMutation("status"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.Status != from {
		if from == gamerequest.StatusPlaying {
			return gamerequest.ErrNotPlaying
		}
		return gamerequest.ErrNotOpen
	}
	now := time.Now()
	if to == gamerequest.StatusPlaying {
		entry.request.StartedAt = &now
	}
	entry.request.Status = to
	entry.activity = append(entry.activity, requestquery.Activity{Kind: kind, ActorID: userID, ActorGameNick: "Ada Player", OccurredAt: now})
	f.broker.Publish(gamerequest.Event{Type: event, RequestID: requestID})
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

func (f *fixture) ListActive(_ context.Context, viewerID string, filter requestquery.Filter) ([]requestquery.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]requestquery.Request, 0, len(f.order))
	for _, id := range f.order {
		entry := f.requests[id]
		// Active means `open` or `playing`, exactly as the read model lists it.
		if !isActiveStatus(entry.request.Status) {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(entry.request.Game.Name), strings.ToLower(filter.Search)) {
			continue
		}
		if filter.Scope == requestquery.ScopeMyGames && !entry.followed {
			continue
		}
		if filter.Scope == requestquery.ScopeJoined && !entry.members[viewerID] {
			continue
		}
		if filter.Scope == requestquery.ScopeMine && entry.request.CreatorID != viewerID {
			continue
		}
		if filter.OpenPlacesOnly && entry.request.MaxPlayers != nil && entry.request.MemberCount >= int64(*entry.request.MaxPlayers) {
			continue
		}
		request := entry.request
		request.ViewerJoined = entry.members[viewerID]
		result = append(result, request)
	}
	return result, nil
}

func (f *fixture) ByID(_ context.Context, id, viewerID string) (requestquery.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[id]
	if !ok {
		return requestquery.Request{}, gamerequest.ErrNotFound
	}
	request := entry.request
	request.ViewerJoined = entry.members[viewerID]
	return request, nil
}

func (f *fixture) LobbyAccess(_ context.Context, requestID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if !entry.members[userID] {
		return gamerequest.ErrNotMember
	}
	return nil
}

func (f *fixture) CanReadLobby(ctx context.Context, requestID, userID string) (bool, error) {
	err := f.LobbyAccess(ctx, requestID, userID)
	if errors.Is(err, gamerequest.ErrNotMember) {
		return false, nil
	}
	return err == nil, err
}

func (f *fixture) Lobby(_ context.Context, requestID, userID string) (requestquery.GameRequestLobby, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return requestquery.GameRequestLobby{}, gamerequest.ErrNotFound
	}
	if !entry.members[userID] {
		return requestquery.GameRequestLobby{}, gamerequest.ErrNotMember
	}
	type orderedEntry struct {
		entry  requestquery.LobbyEntry
		at     time.Time
		source requestquery.LobbySource
		order  int
	}
	rows := make([]orderedEntry, 0, len(entry.activity)+len(entry.messages))
	for index, activity := range entry.activity {
		rows = append(rows, orderedEntry{
			entry: requestquery.LobbyEntry{Source: requestquery.LobbySourceActivity, Activity: activity},
			at:    activity.OccurredAt, source: requestquery.LobbySourceActivity, order: index,
		})
	}
	for _, message := range entry.messages {
		order, _ := strconv.Atoi(message.ID)
		rows = append(rows, orderedEntry{
			entry: requestquery.LobbyEntry{Source: requestquery.LobbySourceChat, Message: message},
			at:    message.CreatedAt, source: requestquery.LobbySourceChat, order: order,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].at.Equal(rows[j].at) {
			return rows[i].at.Before(rows[j].at)
		}
		if rows[i].source != rows[j].source {
			return rows[i].source < rows[j].source
		}
		return rows[i].order < rows[j].order
	})
	if len(rows) > 200 {
		rows = rows[len(rows)-200:]
	}
	lobby := requestquery.GameRequestLobby{RequestID: requestID, HostID: entry.request.CreatorID, Entries: make([]requestquery.LobbyEntry, len(rows))}
	for i, row := range rows {
		lobby.Entries[i] = row.entry
	}
	return lobby, nil
}

// --- chat -----------------------------------------------------------------

func (f *fixture) PostMessage(_ context.Context, requestID, userID, body string) (gamerequest.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.Message{}, gamerequest.ErrNotFound
	}
	if !entry.members[userID] {
		return gamerequest.Message{}, gamerequest.ErrNotMember
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.Message{}, gamerequest.ErrChatClosed
	}
	message := gamerequest.Message{
		ID: strconv.Itoa(len(entry.messages) + 1), RequestID: requestID, AuthorID: userID,
		AuthorGameNick: "Ada Player", Body: strings.TrimSpace(body), CreatedAt: time.Now(),
	}
	entry.messages = append(entry.messages, message)
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventChatMessage, RequestID: requestID})
	return message, nil
}

// CountActive is the whole installation's active total (`open` or `playing`),
// never the filtered subset, which is what the navigation badge states.
func (f *fixture) CountActive(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var total int64
	for _, entry := range f.requests {
		if isActiveStatus(entry.request.Status) {
			total++
		}
	}
	return total, nil
}

func (f *fixture) HasCreated(_ context.Context, viewerID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.requests {
		if entry.request.CreatorID == viewerID {
			return true, nil
		}
	}
	return false, nil
}

// isActiveStatus mirrors the discovery read model's definition of an active
// Game Request. Join eligibility is deliberately not derived from it.
func isActiveStatus(status gamerequest.Status) bool {
	return status == gamerequest.StatusOpen || status == gamerequest.StatusPlaying
}

// RecordView and RecentlyViewed keep the sidebar's reading history in memory.
// Browser tests assert interaction behavior, not history ranking, so this is
// just enough to let the navigation render the same shape as production.
func (f *fixture) RecordView(_ context.Context, requestID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	f.viewed = append([]requestquery.RecentRequest{{
		ID: requestID, GameName: entry.request.Game.Name,
		Status: entry.request.Status, ViewedAt: time.Now(),
	}}, f.viewed...)
	return nil
}

func (f *fixture) RecentlyViewed(_ context.Context, _ string) ([]requestquery.RecentRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.viewed), nil
}

// CountCatalog and CountFollowed state the navigation's Games/My Games totals
// over the fixture's own seeded catalog.
func (f *fixture) CountCatalog(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.requests)), nil
}

func (f *fixture) CountFollowed(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var total int64
	for _, entry := range f.requests {
		if entry.followed {
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

// Select is the Game Request composer's read-only lookup. Browser
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

// BrowseFollowed backs the Games screen's "Followed only" filter. No fixture
// Game is followed, so it is always the empty state here -- the followed
// one, which needs the catalog itself to be reported as usable, exactly as
// the real Browse-backed call reports it.
func (*fixture) BrowseFollowed(context.Context, string, string) (game.DiscoveryPage, error) {
	return game.DiscoveryPage{ProviderStatus: game.ProviderStatus{State: game.ProviderCurrent, CatalogAvailable: true}}, nil
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
	mux.HandleFunc("/__test__/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		status := gamerequest.Status(r.URL.Query().Get("status"))
		startsAt, err := time.Parse(time.RFC3339, r.URL.Query().Get("starts_at"))
		if err != nil {
			http.Error(w, "invalid starts_at", http.StatusBadRequest)
			return
		}
		var startedAt *time.Time
		if value := r.URL.Query().Get("started_at"); value != "" {
			parsed, parseErr := time.Parse(time.RFC3339, value)
			if parseErr != nil {
				http.Error(w, "invalid started_at", http.StatusBadRequest)
				return
			}
			startedAt = &parsed
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		entry := f.requests["00000000-0000-0000-0000-000000000003"]
		entry.request.Status = status
		entry.request.StartsAt = startsAt
		if r.URL.Query().Get("joined") == "1" && !entry.members[viewerID] {
			entry.members[viewerID] = true
			entry.request.ViewerJoined = true
			entry.request.MemberCount++
			entry.request.Members = append(entry.request.Members, requestquery.Member{ID: viewerID, GameNick: "Ada Player", JoinedAt: time.Now()})
		}
		entry.request.StartedAt = startedAt
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

// ListByUser serves the account page; the fixture player has no linked
// identities worth showing.
func (*fixture) ListByUser(context.Context, string) ([]auth.Identity, error) { return nil, nil }
func (f *fixture) SessionInformation(_ context.Context, requestID, userID string) (gamerequest.SessionInformation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotFound
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotActive
	}
	if entry.request.CreatorID != userID && !entry.members[userID] {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotMember
	}
	return gamerequest.SessionInformation{Entries: slices.Clone(entry.sessionInformation), CanEdit: entry.request.CreatorID == userID}, nil
}

func (f *fixture) CanSessionInformation(ctx context.Context, requestID, userID string) (bool, error) {
	_, err := f.SessionInformation(ctx, requestID, userID)
	if errors.Is(err, gamerequest.ErrNotFound) || errors.Is(err, gamerequest.ErrNotActive) || errors.Is(err, gamerequest.ErrNotMember) {
		return false, nil
	}
	return err == nil, err
}

func (f *fixture) CreateSessionInformationEntry(_ context.Context, requestID, userID string, input gamerequest.SessionInformationInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.CreatorID != userID {
		return gamerequest.ErrNotCreator
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	input.Key, input.Value = strings.TrimSpace(input.Key), strings.TrimSpace(input.Value)
	if input.Key == "" || utf8.RuneCountInString(input.Key) > 40 || strings.ContainsAny(input.Key, "\r\n") {
		return gamerequest.ErrInvalidSessionInformationKey
	}
	if input.Value == "" || utf8.RuneCountInString(input.Value) > 300 || strings.ContainsAny(input.Value, "\r\n") {
		return gamerequest.ErrInvalidSessionInformationValue
	}
	if len(entry.sessionInformation) >= 12 {
		return gamerequest.ErrSessionInformationLimit
	}
	for _, current := range entry.sessionInformation {
		if strings.EqualFold(current.Key, input.Key) {
			return gamerequest.ErrSessionInformationKeyExists
		}
	}
	entry.nextSessionEntry++
	now := time.Now()
	entry.sessionInformation = append(entry.sessionInformation, gamerequest.SessionInformationEntry{ID: strconv.FormatInt(entry.nextSessionEntry, 10), RequestID: requestID, Key: input.Key, Value: input.Value, IsSecret: input.IsSecret, Sequence: entry.nextSessionEntry, CreatedAt: now, UpdatedAt: now})
	f.broker.Publish(gamerequest.Event{Type: gamerequest.EventSessionInformationChanged, RequestID: requestID})
	return nil
}

func (f *fixture) UpdateSessionInformationEntry(_ context.Context, requestID, entryID, userID string, input gamerequest.SessionInformationInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.CreatorID != userID {
		return gamerequest.ErrNotCreator
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	input.Key, input.Value = strings.TrimSpace(input.Key), strings.TrimSpace(input.Value)
	if input.Key == "" || utf8.RuneCountInString(input.Key) > 40 || strings.ContainsAny(input.Key, "\r\n") {
		return gamerequest.ErrInvalidSessionInformationKey
	}
	if input.Value == "" || utf8.RuneCountInString(input.Value) > 300 || strings.ContainsAny(input.Value, "\r\n") {
		return gamerequest.ErrInvalidSessionInformationValue
	}
	for i, current := range entry.sessionInformation {
		if current.ID != entryID {
			continue
		}
		if current.Key == input.Key && current.Value == input.Value && current.IsSecret == input.IsSecret {
			return nil
		}
		for _, other := range entry.sessionInformation {
			if other.ID != entryID && strings.EqualFold(other.Key, input.Key) {
				return gamerequest.ErrSessionInformationKeyExists
			}
		}
		current.Key, current.Value, current.IsSecret, current.UpdatedAt = input.Key, input.Value, input.IsSecret, time.Now()
		entry.sessionInformation[i] = current
		f.broker.Publish(gamerequest.Event{Type: gamerequest.EventSessionInformationChanged, RequestID: requestID})
		return nil
	}
	return gamerequest.ErrSessionInformationEntryNotFound
}

func (f *fixture) DeleteSessionInformationEntry(_ context.Context, requestID, entryID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.requests[requestID]
	if !ok {
		return gamerequest.ErrNotFound
	}
	if entry.request.CreatorID != userID {
		return gamerequest.ErrNotCreator
	}
	if !isActiveStatus(entry.request.Status) {
		return gamerequest.ErrNotActive
	}
	for i, current := range entry.sessionInformation {
		if current.ID == entryID {
			entry.sessionInformation = append(entry.sessionInformation[:i], entry.sessionInformation[i+1:]...)
			f.broker.Publish(gamerequest.Event{Type: gamerequest.EventSessionInformationChanged, RequestID: requestID})
			return nil
		}
	}
	return gamerequest.ErrSessionInformationEntryNotFound
}
