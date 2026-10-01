package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/go-chi/chi/v5"
)

type fakeGameRequestCommands struct {
	createCalls       int
	createGameID      string
	createdMaxPlayers *int32
	created           gamerequest.GameRequest
	createErr         error

	viewed        []string
	recordViewErr error

	started string

	capacityCalls int
	capacityArgs  string
	capacityValue *int32
	capacityErr   error
}

func (f *fakeGameRequestCommands) Create(_ context.Context, _, gameID string, maxPlayers *int32, _ time.Time, _ string) (gamerequest.GameRequest, error) {
	f.createCalls++
	f.createGameID = gameID
	f.createdMaxPlayers = maxPlayers
	if f.createErr != nil {
		return gamerequest.GameRequest{}, f.createErr
	}
	return f.created, nil
}
func (f *fakeGameRequestCommands) Join(context.Context, string, string) error   { return nil }
func (f *fakeGameRequestCommands) Leave(context.Context, string, string) error  { return nil }
func (f *fakeGameRequestCommands) Close(context.Context, string, string) error  { return nil }
func (f *fakeGameRequestCommands) Cancel(context.Context, string, string) error { return nil }
func (f *fakeGameRequestCommands) Start(_ context.Context, requestID, userID string) error {
	f.started = requestID + "/" + userID
	return nil
}
func (f *fakeGameRequestCommands) UpdateCapacity(_ context.Context, requestID, userID string, maxPlayers *int32) error {
	f.capacityCalls++
	f.capacityArgs = requestID + "/" + userID
	f.capacityValue = maxPlayers
	return f.capacityErr
}
func (f *fakeGameRequestCommands) RecordView(_ context.Context, requestID, userID string) error {
	f.viewed = append(f.viewed, requestID+"/"+userID)
	return f.recordViewErr
}

type fakeGameRequestQueries struct {
	request requestquery.Request
}

func (fakeGameRequestQueries) ListActive(context.Context, string, requestquery.Filter) ([]requestquery.Request, error) {
	return nil, nil
}
func (f fakeGameRequestQueries) ByID(context.Context, string, string) (requestquery.Request, error) {
	return f.request, nil
}
func (fakeGameRequestQueries) Trending(context.Context) ([]requestquery.TrendingGame, error) {
	return nil, nil
}
func (fakeGameRequestQueries) CountActive(context.Context) (int64, error) { return 0, nil }
func (fakeGameRequestQueries) RecentlyViewed(context.Context, string) ([]requestquery.RecentRequest, error) {
	return nil, nil
}

type fakeGameResolver struct {
	calls int
	game  game.Game
	err   error
}

func (f *fakeGameResolver) Resolve(context.Context, game.ProviderReference) (game.Game, error) {
	f.calls++
	if f.err != nil {
		return game.Game{}, f.err
	}
	return f.game, nil
}

// fakeGameRequestDiscovery records Browse and Select calls separately, so
// tests can assert that a GET/search/selection path never materializes
// anything (that is GameResolver.Resolve's job alone).
type fakeGameRequestDiscovery struct {
	browseCalls  int
	browseUserID string
	browseSearch string
	browseItem   game.DiscoveryPage
	browseErr    error

	browseFollowedCalls int

	selectCalls     int
	selectUserID    string
	selectSelection game.GameSelection
	selectItem      game.DiscoveryItem
	selectErr       error
}

func (f *fakeGameRequestDiscovery) Browse(_ context.Context, userID, search string) (game.DiscoveryPage, error) {
	f.browseCalls++
	f.browseUserID = userID
	f.browseSearch = search
	if f.browseErr != nil {
		return game.DiscoveryPage{}, f.browseErr
	}
	return f.browseItem, nil
}

// BrowseFollowed is the composer's pre-search list. The fake records it as
// a Browse call with the same fields, so existing assertions about
// "the composer browsed, it never materialized" keep holding, and
// browseFollowedCalls alone distinguishes the followed list from a search.
func (f *fakeGameRequestDiscovery) BrowseFollowed(ctx context.Context, userID, search string) (game.DiscoveryPage, error) {
	f.browseFollowedCalls++
	return f.Browse(ctx, userID, search)
}

func (f *fakeGameRequestDiscovery) Select(_ context.Context, userID string, selection game.GameSelection) (game.DiscoveryItem, error) {
	f.selectCalls++
	f.selectUserID = userID
	f.selectSelection = selection
	if f.selectErr != nil {
		return game.DiscoveryItem{}, f.selectErr
	}
	return f.selectItem, nil
}

func TestCreateGameRequestResolvesProviderReference(t *testing.T) {
	commands := &fakeGameRequestCommands{created: gamerequest.GameRequest{ID: "request-1"}}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1", Name: "Provider Game", Slug: "provider-game"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, nil, branding.Default())

	form := url.Values{
		"provider":    {"igdb"},
		"external_id": {"100"},
		"max_players": {"4"},
		"description": {"LAN night"},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if resolver.calls != 1 {
		t.Fatalf("resolver.calls = %d, want 1", resolver.calls)
	}
	if commands.createCalls != 1 || commands.createGameID != "local-1" {
		t.Fatalf("commands.createCalls/createGameID = %d, %q", commands.createCalls, commands.createGameID)
	}
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

// 0 and an empty field are the composer's two ways of saying "open round",
// and both must reach the service as no maximum at all rather than as a
// zero the domain would reject.
func TestCreateGameRequestTreatsZeroMaxPlayersAsNoLimit(t *testing.T) {
	for _, value := range []string{"0", ""} {
		commands := &fakeGameRequestCommands{created: gamerequest.GameRequest{ID: "request-1"}}
		handler := NewGameRequests(commands, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default())

		form := url.Values{"game_id": {"game-1"}, "max_players": {value}}
		request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		handler.Create(httptest.NewRecorder(), request)

		if commands.createCalls != 1 {
			t.Fatalf("max_players=%q: createCalls = %d, want 1", value, commands.createCalls)
		}
		if commands.createdMaxPlayers != nil {
			t.Errorf("max_players=%q: maximum = %d, want no limit", value, *commands.createdMaxPlayers)
		}
	}
}

// A rejected max players must come back named as a max players problem,
// not as an anonymous banner: the field association in the composer is
// built from this.
func TestCreateGameRequestNamesTheRejectedField(t *testing.T) {
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default())

	form := url.Values{"game_id": {"game-1"}, "max_players": {"1000"}}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	body := response.Body.String()
	if !strings.Contains(body, `id="max-players-error"`) {
		t.Errorf("rejection is not rendered with the max players field: %s", body)
	}
	if !strings.Contains(body, `aria-invalid="true"`) {
		t.Errorf("rejected field is not marked invalid: %s", body)
	}
}

func TestCreateGameRequestSkipsResolutionOnValidationFailure(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, nil, branding.Default())

	form := url.Values{
		"provider":    {"igdb"},
		"external_id": {"100"},
		"max_players": {"1000"},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if resolver.calls != 0 {
		t.Fatalf("resolver.calls = %d, want 0", resolver.calls)
	}
	if commands.createCalls != 0 {
		t.Fatalf("commands.createCalls = %d, want 0", commands.createCalls)
	}
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestCreateGameRequestSkipsResolutionOnDescriptionTooLong(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, nil, branding.Default())

	form := url.Values{
		"provider":    {"igdb"},
		"external_id": {"100"},
		"max_players": {"4"},
		"description": {strings.Repeat("x", 1001)},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if resolver.calls != 0 {
		t.Fatalf("resolver.calls = %d, want 0", resolver.calls)
	}
	if commands.createCalls != 0 {
		t.Fatalf("commands.createCalls = %d, want 0", commands.createCalls)
	}
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestCreateGameRequestWithLocalGameIDDoesNotResolve(t *testing.T) {
	commands := &fakeGameRequestCommands{created: gamerequest.GameRequest{ID: "request-2"}}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, nil, branding.Default())

	form := url.Values{"game_id": {"game-1"}, "max_players": {"4"}}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if resolver.calls != 0 {
		t.Fatalf("resolver.calls = %d, want 0", resolver.calls)
	}
	if commands.createCalls != 1 || commands.createGameID != "game-1" {
		t.Fatalf("commands.createCalls/createGameID = %d, %q", commands.createCalls, commands.createGameID)
	}
}

// TestNewGameRequestUsesDiscoveryWithoutMaterializing proves the composer's
// GET path renders from the read-only discovery.Browse alone: a search never
// materializes a provider Game (that is GameResolver.Resolve's job, only at
// the final validated Create POST).
func TestNewGameRequestUsesDiscoveryWithoutMaterializing(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{}
	resolver := &fakeGameResolver{}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, resolver, discovery, nil, branding.Default())

	request := httptest.NewRequest(http.MethodGet, "/game-requests/new?q=war", nil)
	response := httptest.NewRecorder()

	handler.New(response, request)

	if discovery.browseCalls != 1 || discovery.browseSearch != "war" {
		t.Fatalf("browseCalls=%d browseSearch=%q, want 1/%q", discovery.browseCalls, discovery.browseSearch, "war")
	}
	if discovery.selectCalls != 0 {
		t.Fatalf("selectCalls = %d, want 0", discovery.selectCalls)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver.calls = %d, want 0", resolver.calls)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

// The composer opens on the games this player follows, and only a real
// search of two characters or more reaches the whole catalog. A
// one-character search never reaches the repository anyway, so it keeps
// showing the followed list rather than flashing the full catalog between
// keystrokes.
func TestNewGameRequestListsFollowedGamesBeforeASearch(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		query          string
		wantFollowed   int
		wantBrowseOnly bool
	}{
		{name: "no search", query: "", wantFollowed: 1},
		{name: "one character", query: "?q=w", wantFollowed: 1},
		{name: "real search", query: "?q=war", wantFollowed: 0, wantBrowseOnly: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			discovery := &fakeGameRequestDiscovery{}
			handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, nil, branding.Default())

			handler.New(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/game-requests/new"+testCase.query, nil))

			if discovery.browseFollowedCalls != testCase.wantFollowed {
				t.Fatalf("browseFollowedCalls = %d, want %d", discovery.browseFollowedCalls, testCase.wantFollowed)
			}
			if testCase.wantBrowseOnly && discovery.browseCalls != 1 {
				t.Fatalf("browseCalls = %d, want 1", discovery.browseCalls)
			}
		})
	}
}

// A normal GET search/selection is the no-JavaScript composer path. Its
// request-detail fields are transport state, not trusted game metadata, and
// must survive both searching and selecting without calling Resolve.
func TestNewGameRequestNoJSSelectionPreservesRequestDetails(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{selectItem: game.DiscoveryItem{
		LocalGameID: "local-2", GamePresentation: game.GamePresentation{Name: "Age of Empires"}, Materialized: true,
	}}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, nil, branding.Default())

	request := httptest.NewRequest(http.MethodGet, "/game-requests/new?game_id=local-2&max_players=4&start_preset=custom&start_date=2027-05-01&start_time=18%3A00&timezone=Europe%2FBerlin&description=LAN+night", nil)
	response := httptest.NewRecorder()
	handler.New(response, request)

	if response.Code != http.StatusOK || discovery.selectCalls != 1 {
		t.Fatalf("status/selectCalls = %d/%d; body = %s", response.Code, discovery.selectCalls, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, `value="4"`) || !strings.Contains(body, `value="2027-05-01"`) || !strings.Contains(body, `value="18:00"`) || !strings.Contains(body, `value="Europe/Berlin"`) || !strings.Contains(body, "LAN night") {
		t.Fatalf("selected no-JS composer lost request details: %s", body)
	}
}

// The live-search fragment renders the composer's own pick list: plain
// full-page links whose href must still carry the request details the
// player already typed.
func TestGameRequestDiscoveryFragmentKeepsComposerDetailsInCardLinks(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{browseItem: game.DiscoveryPage{Items: []game.DiscoveryItem{{
		LocalGameID: "local-2", GamePresentation: game.GamePresentation{Name: "Age of Empires"}, Materialized: true,
	}}}}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, nil, branding.Default())

	response := httptest.NewRecorder()
	handler.Discovery(response, httptest.NewRequest(http.MethodGet, "/game-requests/new/discovery?q=age&max_players=4", nil))

	body := response.Body.String()
	if !strings.Contains(body, "max_players=4") {
		t.Errorf("fragment dropped the composer request details: %s", body)
	}
	if strings.Contains(body, "hx-get=") {
		t.Errorf("fragment cards are no longer plain links: %s", body)
	}
}

// TestGameRequestDiscoveryFragmentUsesSameSearchModel proves the "Change
// game" live-search fragment route passes its exact query straight through
// to discovery.Browse and renders the shared discovery fragment.
func TestGameRequestDiscoveryFragmentUsesSameSearchModel(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, nil, branding.Default())

	request := httptest.NewRequest(http.MethodGet, "/game-requests/new/discovery?q=war", nil)
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()

	handler.Discovery(response, request)

	if discovery.browseCalls != 1 || discovery.browseSearch != "war" {
		t.Fatalf("browseCalls=%d browseSearch=%q, want 1/%q", discovery.browseCalls, discovery.browseSearch, "war")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `id="game-request-discovery"`) {
		t.Fatalf("body does not render the discovery fragment: %s", response.Body.String())
	}
}

// TestCreateGameRequestProviderMissingPreservesFormAndAsksForAnotherGame
// proves that when the final provider resolution at Create time reports the
// catalog no longer has that provider record, the composer re-renders with
// 422 (not a dead-end 404), keeps every posted request-detail value, and
// asks the player to choose another game.
func TestCreateGameRequestProviderMissingPreservesFormAndAsksForAnotherGame(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	resolver := &fakeGameResolver{err: game.ErrProviderRecordNotFound}
	discovery := &fakeGameRequestDiscovery{}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, discovery, nil, branding.Default())

	form := url.Values{
		"provider": {"igdb"}, "external_id": {"100"},
		"max_players": {"4"}, "start_preset": {"custom"}, "start_date": {"2027-05-01"}, "start_time": {"18:00"},
		"description": {"LAN night"},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
	if commands.createCalls != 0 {
		t.Fatalf("commands.createCalls = %d, want 0", commands.createCalls)
	}
	body := response.Body.String()
	for _, want := range []string{
		"This game is no longer available from the game catalog. Choose another game.",
		`value="4"`,
		`value="2027-05-01"`,
		`value="18:00"`,
		"LAN night",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
	if !strings.Contains(body, `href="/game-requests/new?game_id=`) && !strings.Contains(body, `name="max_players"`) {
		t.Fatalf("recovery composer does not offer a detail-preserving reselection: %s", body)
	}
}

// TestCreateGameRequestKeepsMaterializedGameWhenCommandFails proves that
// when Resolve succeeds (materializing a local Game) but commands.Create
// itself then fails, the composer keeps the resolved local Game selected on
// the re-render and never compensates by deleting/unmaterializing it.
func TestCreateGameRequestKeepsMaterializedGameWhenCommandFails(t *testing.T) {
	commands := &fakeGameRequestCommands{createErr: errors.New("create failed")}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1", Name: "Provider Game", Slug: "provider-game"}}
	discovery := &fakeGameRequestDiscovery{selectItem: game.DiscoveryItem{
		LocalGameID: "local-1", GamePresentation: game.GamePresentation{Name: "Provider Game"}, Materialized: true,
	}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, discovery, nil, branding.Default())

	form := url.Values{
		"provider": {"igdb"}, "external_id": {"100"},
		"max_players": {"4"}, "description": {"LAN night"},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if resolver.calls != 1 {
		t.Fatalf("resolver.calls = %d, want 1", resolver.calls)
	}
	if commands.createCalls != 1 || commands.createGameID != "local-1" {
		t.Fatalf("commands.createCalls/createGameID = %d, %q", commands.createCalls, commands.createGameID)
	}
	// No compensating delete/unmaterialize call exists on GameResolver or
	// GameRequestCommands at all -- there is nothing further for this test to
	// assert beyond the calls already checked above.
	want := game.GameSelection{LocalGameID: "local-1"}
	if discovery.selectSelection != want {
		t.Fatalf("re-render selection = %#v, want %#v", discovery.selectSelection, want)
	}
	body := response.Body.String()
	for _, want := range []string{"Provider Game", `value="4"`, "LAN night", `name="game_id" value="local-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("re-render does not preserve selection/form values: missing %q in %s", want, body)
		}
	}
}

func TestParseCreateFormUsesBrowserTimezone(t *testing.T) {
	handler := &GameRequests{now: func() time.Time { return time.Date(2027, 5, 1, 8, 0, 0, 0, time.UTC) }}
	form := url.Values{
		"game_id":     {"game-1"},
		"max_players": {"4"},
		"starts_at":   {"2027-05-01T18:30"},
		"timezone":    {"Europe/Berlin"},
		"description": {" Casual players "},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsedForm, maximum, startsAt, err := handler.parseCreateForm(request)
	if err != nil {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
	if parsedForm.Timezone != "Europe/Berlin" || maximum == nil || *maximum != 4 {
		t.Fatalf("form/maximum = %#v, %#v", parsedForm, maximum)
	}
	want := time.Date(2027, 5, 1, 16, 30, 0, 0, time.UTC)
	if !startsAt.Equal(want) {
		t.Errorf("startsAt = %s, want %s", startsAt, want)
	}
}

// The offset must be the one in force at the selected date, not the viewer's
// current one: Berlin is UTC+2 in summer and UTC+1 in winter.
func TestParseBrowserTimeAppliesDSTOfSelectedDate(t *testing.T) {
	for _, test := range []struct{ value, zone, want string }{
		{"2027-07-15T18:00", "Europe/Berlin", "2027-07-15T16:00:00Z"},
		{"2027-01-15T18:00", "Europe/Berlin", "2027-01-15T17:00:00Z"},
		{"2027-01-15T18:00", "", "2027-01-15T18:00:00Z"},
	} {
		got, err := parseBrowserTime(test.value, test.zone)
		if err != nil || got.Format(time.RFC3339) != test.want {
			t.Errorf("parseBrowserTime(%q, %q) = %s, %v; want %s", test.value, test.zone, got.Format(time.RFC3339), err, test.want)
		}
	}
	if _, err := parseBrowserTime("2027-01-15T18:00", "Not/AZone"); !errors.Is(err, gamerequest.ErrInvalidStartsAt) {
		t.Errorf("unknown zone error = %v, want ErrInvalidStartsAt", err)
	}
}

func TestParseCreateFormSupportsStartPresets(t *testing.T) {
	now := time.Date(2027, 5, 1, 8, 0, 0, 0, time.UTC)
	handler := &GameRequests{now: func() time.Time { return now }}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(url.Values{
		"game_id": {"game-1"}, "start_preset": {"30"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, startsAt, err := handler.parseCreateForm(request)
	if err != nil {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
	if want := now.Add(30 * time.Minute); !startsAt.Equal(want) {
		t.Errorf("startsAt = %s, want %s", startsAt, want)
	}
}

func TestParseCreateFormRequiresCompleteCustomStart(t *testing.T) {
	handler := &GameRequests{now: time.Now}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(url.Values{
		"game_id": {"game-1"}, "start_preset": {"custom"}, "start_date": {"2027-05-01"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if _, _, _, err := handler.parseCreateForm(request); !errors.Is(err, gamerequest.ErrInvalidStartsAt) {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
}

func TestGameRequestErrorStatus(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{gamerequest.ErrNotFound, http.StatusNotFound},
		{gamerequest.ErrNotCreator, http.StatusForbidden},
		{gamerequest.ErrFull, http.StatusConflict},
		{gamerequest.ErrCapacityBelowMembers, http.StatusConflict},
		{gamerequest.ErrNotActive, http.StatusConflict},
		{gamerequest.ErrInvalidMaxPlayers, http.StatusUnprocessableEntity},
		{gamerequest.ErrInvalidDescription, http.StatusUnprocessableEntity},
		{game.ErrInvalidProviderReference, http.StatusUnprocessableEntity},
		{game.ErrProviderRecordNotFound, http.StatusNotFound},
		{errors.New("database unavailable"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		if got := gameRequestErrorStatus(test.err); got != test.want {
			t.Errorf("gameRequestErrorStatus(%v) = %d, want %d", test.err, got, test.want)
		}
	}
}

// Opening a Game Request is what puts it in the viewer's own "Recently viewed"
// list, so the detail view has to record the visit. A failure to record must
// not cost the viewer the page they actually asked for.
func TestViewRecordsTheVisitAndStillRendersWhenRecordingFails(t *testing.T) {
	tests := map[string]error{"recorded": nil, "recording failed": errors.New("database unavailable")}
	for name, recordErr := range tests {
		t.Run(name, func(t *testing.T) {
			commands := &fakeGameRequestCommands{recordViewErr: recordErr}
			queries := fakeGameRequestQueries{request: requestquery.Request{
				ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, Status: gamerequest.StatusOpen,
			}}
			handler := NewGameRequests(commands, queries, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default())

			request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/game-requests/request-1", nil), user.User{ID: "user-1", GameNick: "Ada"})
			response := httptest.NewRecorder()
			handler.View(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if len(commands.viewed) != 1 || commands.viewed[0] != "request-1/user-1" {
				t.Fatalf("recorded views = %#v, want the viewed request and viewer", commands.viewed)
			}
		})
	}
}

// Refreshing the detail fragment over HTMX is the realtime stream redrawing
// the page the viewer is already on, not a new visit, so it must not reorder
// their history.
func TestRefreshViewDoesNotRecordAVisit(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	queries := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, Status: gamerequest.StatusOpen,
	}}
	handler := NewGameRequests(commands, queries, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default())

	request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/game-requests/request-1/refresh", nil), user.User{ID: "user-1", GameNick: "Ada"})
	handler.RefreshView(httptest.NewRecorder(), request)

	if len(commands.viewed) != 0 {
		t.Fatalf("recorded views = %#v, want none", commands.viewed)
	}
}

func TestStartCallsCommandAndRedirectsToDetail(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, nil, &fakeGameRequestDiscovery{}, nil, branding.Default())
	router := chi.NewRouter()
	router.Post("/{requestID}/start", handler.Start)

	request := httptest.NewRequest(http.MethodPost, "/request-1/start", nil)
	request = request.WithContext(webmiddleware.ContextWithUser(request.Context(), user.User{ID: "host-1"}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if commands.started != "request-1/host-1" {
		t.Errorf("Start called with %q", commands.started)
	}
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests/request-1" {
		t.Errorf("response = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestNotPlayingIsAConflictWithItsOwnMessage(t *testing.T) {
	if got := gameRequestErrorStatus(gamerequest.ErrNotPlaying); got != http.StatusConflict {
		t.Errorf("status = %d, want %d", got, http.StatusConflict)
	}
	for err, want := range map[error]string{
		gamerequest.ErrNotPlaying:         "This Game Request is not currently playing.",
		gamerequest.ErrNotCreator:         "Only the Host can start, close, or cancel this Game Request.",
		gamerequest.ErrNotOpen:            "This Game Request is no longer open.",
		gamerequest.ErrCreatorCannotLeave: "The Host cannot leave their own Game Request.",
	} {
		if got := gameRequestMessage(err); got != want {
			t.Errorf("message for %v = %q, want %q", err, got, want)
		}
	}
}

// The capacity form reuses the composer's meaning for the field -- blank and 0
// are "unlimited" -- but adds the domain's finite minimum of 2, so it is parsed
// on its own instead of through parseCreateForm.
func TestParseCapacity(t *testing.T) {
	for value, want := range map[string]*int32{"": nil, "0": nil, "  ": nil, "2": new(int32(2)), "999": new(int32(999))} {
		got, err := parseCapacity(value)
		if err != nil {
			t.Errorf("parseCapacity(%q) error = %v", value, err)
			continue
		}
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("parseCapacity(%q) = %v, want %v", value, got, want)
		}
	}
	for _, value := range []string{"1", "-1", "abc", "2.5", "1000", "99999999999"} {
		if got, err := parseCapacity(value); !errors.Is(err, gamerequest.ErrInvalidMaxPlayers) || got != nil {
			t.Errorf("parseCapacity(%q) = %v, %v, want ErrInvalidMaxPlayers", value, got, err)
		}
	}
}

func postCapacity(t *testing.T, commands *fakeGameRequestCommands, queries fakeGameRequestQueries, viewerID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewGameRequests(commands, queries, nil, &fakeGameRequestDiscovery{}, nil, branding.Default())
	router := chi.NewRouter()
	router.Post("/{requestID}/capacity", handler.UpdateCapacity)

	request := httptest.NewRequest(http.MethodPost, "/request-1/capacity", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request = request.WithContext(webmiddleware.ContextWithUser(request.Context(), user.User{ID: viewerID, GameNick: "Ada"}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestUpdateCapacityCallsCommandAndRedirectsToDetail(t *testing.T) {
	for value, want := range map[string]*int32{"6": new(int32(6)), "0": nil, "": nil} {
		commands := &fakeGameRequestCommands{}
		response := postCapacity(t, commands, fakeGameRequestQueries{}, "host-1", url.Values{"max_players": {value}})

		if commands.capacityCalls != 1 || commands.capacityArgs != "request-1/host-1" {
			t.Fatalf("max_players=%q: command calls = %d with %q", value, commands.capacityCalls, commands.capacityArgs)
		}
		if (commands.capacityValue == nil) != (want == nil) || (want != nil && *commands.capacityValue != *want) {
			t.Errorf("max_players=%q reached the command as %v, want %v", value, commands.capacityValue, want)
		}
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests/request-1" {
			t.Errorf("max_players=%q response = %d %q", value, response.Code, response.Header().Get("Location"))
		}
	}
}

func TestUpdateCapacityRejectsInvalidInputBeforeTheCommand(t *testing.T) {
	queries := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1", Status: gamerequest.StatusOpen,
	}}
	for _, value := range []string{"1", "abc", "1000", "-3"} {
		commands := &fakeGameRequestCommands{}
		response := postCapacity(t, commands, queries, "host-1", url.Values{"max_players": {value}})
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("max_players=%q status = %d, want 422", value, response.Code)
		}
		if commands.capacityCalls != 0 {
			t.Errorf("max_players=%q reached the command", value)
		}
	}
}

func TestUpdateCapacityMapsBusinessFailures(t *testing.T) {
	queries := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1", Status: gamerequest.StatusOpen,
	}}
	for cause, want := range map[error]int{
		gamerequest.ErrCapacityBelowMembers: http.StatusConflict,
		gamerequest.ErrNotActive:            http.StatusConflict,
		gamerequest.ErrNotCreator:           http.StatusForbidden,
		gamerequest.ErrNotFound:             http.StatusNotFound,
		errors.New("database unavailable"):  http.StatusInternalServerError,
	} {
		commands := &fakeGameRequestCommands{capacityErr: cause}
		if response := postCapacity(t, commands, queries, "host-1", url.Values{"max_players": {"3"}}); response.Code != want {
			t.Errorf("%v: status = %d, want %d", cause, response.Code, want)
		}
	}
}

// A rejected capacity comes back on the detail page beside the Host's editor,
// with what was typed, and the detail keeps the request's own status code.
func TestUpdateCapacityRendersFieldLocalErrorWithSubmittedValue(t *testing.T) {
	limit := int32(4)
	queries := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusPlaying, MemberCount: 3, MaxPlayers: &limit,
	}}
	for name, tc := range map[string]struct {
		value string
		cause error
		code  int
		text  string
	}{
		"invalid value":    {"1", nil, http.StatusUnprocessableEntity, "Maximum players must be 0 for unlimited or a whole number from 2 to 999."},
		"below membership": {"2", gamerequest.ErrCapacityBelowMembers, http.StatusConflict, "Maximum players cannot be lower than the current player count."},
	} {
		commands := &fakeGameRequestCommands{capacityErr: tc.cause}
		response := postCapacity(t, commands, queries, "host-1", url.Values{"max_players": {tc.value}})
		body := response.Body.String()

		if response.Code != tc.code {
			t.Errorf("%s: status = %d, want %d", name, response.Code, tc.code)
		}
		for _, want := range []string{`id="capacity-error"`, tc.text, `value="` + tc.value + `"`, `aria-invalid="true"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: body lacks %q", name, want)
			}
		}
	}
}

func TestUpdateCapacityKeepsRefusalsTheEditorCannotShowAsPageAlerts(t *testing.T) {
	limit := int32(4)
	closed := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusClosed, MemberCount: 2, MaxPlayers: &limit,
	}}
	response := postCapacity(t, &fakeGameRequestCommands{capacityErr: gamerequest.ErrNotActive}, closed, "host-1", url.Values{"max_players": {"6"}})
	body := response.Body.String()
	if response.Code != http.StatusConflict || !strings.Contains(body, `role="alert"`) ||
		!strings.Contains(body, "Capacity can only be changed while the Game Request is active.") {
		t.Errorf("inactive refusal: status = %d, body = %s", response.Code, body)
	}

	open := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusOpen, MemberCount: 2, MaxPlayers: &limit,
	}}
	response = postCapacity(t, &fakeGameRequestCommands{capacityErr: gamerequest.ErrNotCreator}, open, "user-2", url.Values{"max_players": {"6"}})
	body = response.Body.String()
	if response.Code != http.StatusForbidden || !strings.Contains(body, `role="alert"`) || strings.Contains(body, "capacity-error") {
		t.Errorf("non-Host refusal: status = %d, body = %s", response.Code, body)
	}
}
