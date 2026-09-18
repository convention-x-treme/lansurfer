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
func (fakeGameRequestQueries) CreationOptions(context.Context) ([]requestquery.GameOption, error) {
	return []requestquery.GameOption{{ID: "game-1", Name: "Local Game"}}, nil
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

func TestCreateGameRequestResolvesProviderReference(t *testing.T) {
	commands := &fakeGameRequestCommands{created: gamerequest.GameRequest{ID: "request-1"}}
	resolver := &fakeGameResolver{game: game.Game{ID: "local-1", Name: "Provider Game", Slug: "provider-game"}}
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, branding.Default())

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, branding.Default())

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, branding.Default())

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
	handler := NewGameRequests(commands, fakeGameRequestQueries{}, resolver, branding.Default())

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
