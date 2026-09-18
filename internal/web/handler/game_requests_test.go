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
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

type fakeGameRequestCommands struct {
	createCalls  int
	createGameID string
	created      gamerequest.GameRequest
	createErr    error
}

func (f *fakeGameRequestCommands) Create(_ context.Context, _, gameID string, _ *int32, _ time.Time, _ string) (gamerequest.GameRequest, error) {
	f.createCalls++
	f.createGameID = gameID
	if f.createErr != nil {
		return gamerequest.GameRequest{}, f.createErr
	}
	return f.created, nil
}
func (f *fakeGameRequestCommands) Join(context.Context, string, string) error   { return nil }
func (f *fakeGameRequestCommands) Leave(context.Context, string, string) error  { return nil }
func (f *fakeGameRequestCommands) Close(context.Context, string, string) error  { return nil }
func (f *fakeGameRequestCommands) Cancel(context.Context, string, string) error { return nil }

type fakeGameRequestQueries struct{}

func (fakeGameRequestQueries) ListActive(context.Context, string, requestquery.Filter) ([]requestquery.Request, error) {
	return nil, nil
}
func (fakeGameRequestQueries) ByID(context.Context, string, string) (requestquery.Request, error) {
	return requestquery.Request{}, nil
}
func (fakeGameRequestQueries) Trending(context.Context) ([]requestquery.TrendingGame, error) {
	return nil, nil
}
func (fakeGameRequestQueries) CountOpen(context.Context) (int64, error) { return 0, nil }

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, branding.Default())

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

func TestCreateGameRequestSkipsResolutionOnValidationFailure(t *testing.T) {
	commands := &fakeGameRequestCommands{}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, branding.Default())

	form := url.Values{
		"provider":    {"igdb"},
		"external_id": {"100"},
		"max_players": {"0"},
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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, branding.Default())

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, &fakeGameRequestDiscovery{}, branding.Default())

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
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, resolver, discovery, branding.Default())

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

// TestGameRequestDiscoveryFragmentUsesSameSearchModel proves the "Change
// game" live-search fragment route passes its exact query straight through
// to discovery.Browse and renders the shared discovery fragment.
func TestGameRequestDiscoveryFragmentUsesSameSearchModel(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, branding.Default())

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

// TestGameRequestSelectionIsReadOnly proves the Selection fragment calls only
// the read-only discovery.Select for a provider reference, never
// GameResolver.Resolve -- reselecting in the composer must never materialize
// a provider Game.
func TestGameRequestSelectionIsReadOnly(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{selectItem: game.DiscoveryItem{
		Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "100"},
		GamePresentation: game.GamePresentation{Name: "Provider Game"},
	}}
	resolver := &fakeGameResolver{}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, resolver, discovery, branding.Default())

	request := httptest.NewRequest(http.MethodGet, "/game-requests/new/selection?provider=igdb&external_id=100", nil)
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()

	handler.Selection(response, request)

	if discovery.selectCalls != 1 {
		t.Fatalf("selectCalls = %d, want 1", discovery.selectCalls)
	}
	want := game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"}}
	if discovery.selectSelection != want {
		t.Fatalf("selection = %#v, want %#v", discovery.selectSelection, want)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver.calls = %d, want 0", resolver.calls)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Provider Game") {
		t.Fatalf("body does not render the selected game: %s", response.Body.String())
	}
}

// TestGameRequestSelectionAcceptsLocalGameID proves a game_id query
// parameter becomes GameSelection{LocalGameID: ...}, exactly like New.
func TestGameRequestSelectionAcceptsLocalGameID(t *testing.T) {
	discovery := &fakeGameRequestDiscovery{selectItem: game.DiscoveryItem{
		LocalGameID: "local-1", GamePresentation: game.GamePresentation{Name: "Local Game"}, Materialized: true,
	}}
	handler := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, discovery, branding.Default())

	request := httptest.NewRequest(http.MethodGet, "/game-requests/new/selection?game_id=local-1", nil)
	request.Header.Set("HX-Request", "true")
	response := httptest.NewRecorder()

	handler.Selection(response, request)

	want := game.GameSelection{LocalGameID: "local-1"}
	if discovery.selectSelection != want {
		t.Fatalf("selection = %#v, want %#v", discovery.selectSelection, want)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, discovery, branding.Default())

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, discovery, branding.Default())

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
		"game_id":         {"game-1"},
		"max_players":     {"4"},
		"starts_at":       {"2027-05-01T18:30"},
		"timezone_offset": {"-120"},
		"description":     {" Casual players "},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsedForm, maximum, startsAt, err := handler.parseCreateForm(request)
	if err != nil {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
	if parsedForm.TimezoneOffset != "-120" || maximum == nil || *maximum != 4 {
		t.Fatalf("form/maximum = %#v, %#v", parsedForm, maximum)
	}
	want := time.Date(2027, 5, 1, 16, 30, 0, 0, time.UTC)
	if !startsAt.Equal(want) {
		t.Errorf("startsAt = %s, want %s", startsAt, want)
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
