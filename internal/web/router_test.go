package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/event"
	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	assertResponse(t, response.Result(), http.StatusOK, "ok\n")
}

func TestReady(t *testing.T) {
	tests := []struct {
		name       string
		ready      ReadyCheck
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database ready",
			ready:      func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "ready\n",
		},
		{
			name:       "database unavailable",
			ready:      func(context.Context) error { return errors.New("unavailable") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready\n",
		},
		{
			name: "database check times out",
			ready: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: test.ready}).ServeHTTP(response, request)

			assertResponse(t, response.Result(), test.wantStatus, test.wantBody)
		})
	}
}

func TestHealthEndpointsRejectOtherMethods(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestRootRendersLoginLandingPage(t *testing.T) {
	authentication := &routerAuthentication{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
	}).ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if got := result.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, want := range []string{
		"LANsurfer",
		"Find your crew. Catch the next game.",
		"Continue with your CXT account",
		"CXT IdP",
		"Continue with Discord",
		`href="https://www.convention-x-treme.de"`,
		`href="/assets/app.css"`,
		`src="/assets/app.js"`,
		`data-theme-toggle`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	if remoteAssetPattern.Match(body) {
		t.Error("login page unexpectedly loads a remote runtime asset")
	}
}

// remoteAssetPattern matches elements that fetch remote content at runtime.
// Outbound anchors to the operating community are not runtime dependencies.
var remoteAssetPattern = regexp.MustCompile(`(?i)<(?:script|link|img|iframe|source|video|audio|embed)\b[^>]*\b(?:src|href)\s*=\s*["']https?://`)

func TestHomeRendersAuthenticatedViewer(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{DisplayName: "Ada Player", Role: user.RoleAdmin}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: authentication,
	}).ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/game-requests" {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPatternFragments(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/ui/patterns/empty", want: "Quiet out there. Start something?"},
		{path: "/ui/patterns/error", want: "Something went wrong"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			result := response.Result()
			defer result.Body.Close()
			if result.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
			}
			body, err := io.ReadAll(result.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), test.want) {
				t.Errorf("body does not contain %q", test.want)
			}
		})
	}
}

func TestEmbeddedAssetsAreServed(t *testing.T) {
	for _, path := range []string{
		"/assets/app.css",
		"/assets/app.js",
		"/assets/fonts/montserrat-latin-wght-normal.woff2",
		"/assets/fonts/jetbrains-mono-latin-wght-normal.woff2",
		"/assets/images/otter.png",
		"/assets/licenses/htmx-BSD-2-Clause.txt",
		"/assets/licenses/montserrat-OFL-1.1.txt",
		"/assets/licenses/jetbrains-mono-OFL-1.1.txt",
		"/assets/licenses/tailwindcss-MIT.txt",
		"/assets/licenses/daisyui-MIT.txt",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if response.Body.Len() == 0 {
				t.Error("asset response is empty")
			}
		})
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	dependencies := RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{DisplayName: "Player", Role: user.RoleUser}},
		Events:         &routerEvents{}, Games: &routerGames{},
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/games", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	NewRouter(dependencies).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestAdminCanCreateEvent(t *testing.T) {
	events := &routerEvents{}
	router := adminTestRouter(events, &routerGames{})
	form := url.Values{
		"name": {"CXT 2027"}, "starts_at": {"2027-05-01T10:00"}, "ends_at": {"2027-05-03T10:00"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/events", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(request)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/admin/events?notice=event-created" {
		t.Fatalf("response = %d %q", response.Code, response.Header().Get("Location"))
	}
	if events.created.Name != "CXT 2027" || events.created.StartsAt.Location() != time.UTC {
		t.Fatalf("created event = %#v", events.created)
	}
}

func TestAdminGameSearchRendersAliasMatch(t *testing.T) {
	games := &routerGames{items: []game.Game{{ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Active: true, Aliases: []game.Alias{{ID: "alias-1", Value: "WC3"}}}}}
	request := httptest.NewRequest(http.MethodGet, "/admin/games?q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	adminTestRouter(&routerEvents{}, games).ServeHTTP(response, request)

	if response.Code != http.StatusOK || games.search != "wc3" {
		t.Fatalf("response status = %d, search = %q", response.Code, games.search)
	}
	for _, want := range []string{"Warcraft III", "WC3", "Deactivate game"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}

func TestGameCatalogRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/games", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:         func(context.Context) error { return nil },
		Subscriptions: &routerSubscriptions{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticatedUserCanBrowseAndFollowGames(t *testing.T) {
	subscriptions := &routerSubscriptions{items: []game.SubscriptionGame{{
		ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3",
	}}}
	router := gameTestRouter(subscriptions)

	request := httptest.NewRequest(http.MethodGet, "/games?q=wc3", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || subscriptions.search != "wc3" {
		t.Fatalf("browse response = %d, search = %q", response.Code, subscriptions.search)
	}
	for _, want := range []string{"Warcraft III", "Follow Game", `hx-post="/games/game-1/follow"`, "My Games"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q", want)
		}
	}

	follow := httptest.NewRequest(http.MethodPost, "/games/game-1/follow", nil)
	follow.Header.Set("HX-Request", "true")
	follow.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(follow)
	followResponse := httptest.NewRecorder()
	router.ServeHTTP(followResponse, follow)
	if followResponse.Code != http.StatusOK || subscriptions.followed != "user-1/game-1" {
		t.Fatalf("follow response = %d, write = %q", followResponse.Code, subscriptions.followed)
	}
	if !strings.Contains(followResponse.Body.String(), "Unfollow") {
		t.Errorf("follow fragment = %q", followResponse.Body.String())
	}
}

func TestMyGamesRendersOnlySubscriptions(t *testing.T) {
	subscriptions := &routerSubscriptions{mine: []game.SubscriptionGame{{
		ID: "game-1", Name: "Warcraft III", Slug: "warcraft-3", Subscribed: true,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/games/mine", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	gameTestRouter(subscriptions).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Warcraft III") {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestGameRequestsRequireAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:        func(context.Context) error { return nil },
		GameRequests: &routerGameRequests{}, RequestQueries: &routerGameRequestQueries{},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestRealtimeRequiresAuthentication(t *testing.T) {
	events := make(chan gamerequest.Event)
	close(events)
	request := httptest.NewRequest(http.MethodGet, "/events/game-requests", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{
		Ready:    func(context.Context) error { return nil },
		Realtime: &routerRealtime{events: events},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestRealtimeRefreshHighlightsFollowedGame(t *testing.T) {
	queries := &routerGameRequestQueries{items: []requestquery.Request{{
		ID: "request-1", EventName: "CXT 2027", GameName: "Warcraft III",
		CreatorID: "creator-1", CreatorName: "Grace", StartsAt: time.Now(),
		Status: gamerequest.StatusOpen, MemberCount: 1, ViewerSubscribed: true,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/game-requests/refresh?highlight=request-1", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()

	gameRequestTestRouter(&routerGameRequests{}, queries).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	for _, want := range []string{`id="game-request-list"`, "request-row-incoming", "New Game Request for Warcraft III", `hx-swap-oob="beforeend:#live-notices"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("body does not contain %q: %q", want, response.Body.String())
		}
	}
}

func TestGameRequestBrowserWorkflow(t *testing.T) {
	commands := &routerGameRequests{}
	queries := &routerGameRequestQueries{
		items: []requestquery.Request{{
			ID: "request-1", EventName: "CXT 2027", GameName: "Warcraft III",
			CreatorID: "creator-1", CreatorName: "Grace", StartsAt: time.Now(),
			Status: gamerequest.StatusOpen, MemberCount: 1,
		}},
		options: requestquery.CreationOptions{
			EventID: "event-1", EventName: "CXT 2027",
			Games: []requestquery.GameOption{{ID: "game-1", Name: "Warcraft III"}},
		},
	}
	router := gameRequestTestRouter(commands, queries)

	list := httptest.NewRequest(http.MethodGet, "/game-requests", nil)
	list.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	for _, want := range []string{"Game Requests", "Warcraft III", `class="request-row"`, `hx-post="/game-requests/request-1/join"`} {
		if !strings.Contains(listResponse.Body.String(), want) {
			t.Errorf("list body does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"places · start", "Live <span>· CXT 2027</span>"} {
		if strings.Contains(listResponse.Body.String(), unwanted) {
			t.Errorf("list body unexpectedly contains %q", unwanted)
		}
	}

	newRequest := httptest.NewRequest(http.MethodGet, "/game-requests/new?game_id=game-1", nil)
	newRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	newResponse := httptest.NewRecorder()
	router.ServeHTTP(newResponse, newRequest)
	if newResponse.Code != http.StatusOK || !strings.Contains(newResponse.Body.String(), `value="game-1" selected`) {
		t.Errorf("preselected creation form = %d %q", newResponse.Code, newResponse.Body.String())
	}

	form := url.Values{"game_id": {"game-1"}, "max_players": {"4"}, "description": {"Casual players"}}
	create := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	create.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	create.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(create)
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusSeeOther || createResponse.Header().Get("Location") != "/game-requests/request-created?notice=request-created" {
		t.Fatalf("create response = %d %q", createResponse.Code, createResponse.Header().Get("Location"))
	}
	if commands.created.CreatorID != "user-1" || commands.created.GameID != "game-1" || commands.created.MaxPlayers == nil || *commands.created.MaxPlayers != 4 {
		t.Fatalf("created request = %#v", commands.created)
	}

	join := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", nil)
	join.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(join)
	joinResponse := httptest.NewRecorder()
	router.ServeHTTP(joinResponse, join)
	if joinResponse.Code != http.StatusSeeOther || commands.joined != "request-1/user-1" {
		t.Fatalf("join response = %d, command = %q", joinResponse.Code, commands.joined)
	}

	queries.items[0].ViewerJoined = true
	htmxJoin := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", strings.NewReader(url.Values{"return_to": {"list"}}.Encode()))
	htmxJoin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	htmxJoin.Header.Set("HX-Request", "true")
	htmxJoin.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(htmxJoin)
	htmxResponse := httptest.NewRecorder()
	router.ServeHTTP(htmxResponse, htmxJoin)
	if htmxResponse.Code != http.StatusOK || !strings.Contains(htmxResponse.Body.String(), ">Leave</button>") {
		t.Fatalf("htmx join response = %d %q", htmxResponse.Code, htmxResponse.Body.String())
	}
}

func assertResponse(t *testing.T, response *http.Response, wantStatus int, wantBody string) {
	t.Helper()
	defer response.Body.Close()

	if response.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d", response.StatusCode, wantStatus)
	}
	if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if string(body) != wantBody {
		t.Errorf("body = %q, want %q", body, wantBody)
	}
}

func addCSRF(request *http.Request) {
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	request.AddCookie(&http.Cookie{Name: "lansurfer_csrf", Value: token})
	request.Header.Set("X-CSRF-Token", token)
}

type routerAuthentication struct {
	account user.User
}

func adminTestRouter(events *routerEvents, games *routerGames) http.Handler {
	return NewRouter(RouterDependencies{
		Ready:          func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{DisplayName: "Ada Admin", Role: user.RoleAdmin}},
		Events:         events,
		Games:          games,
	})
}

func gameTestRouter(subscriptions *routerSubscriptions) http.Handler {
	return NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{
			ID: "user-1", DisplayName: "Ada Player", Role: user.RoleUser,
		}},
		Subscriptions: subscriptions,
	})
}

func gameRequestTestRouter(commands *routerGameRequests, queries *routerGameRequestQueries) http.Handler {
	return NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil },
		Authentication: &routerAuthentication{account: user.User{
			ID: "user-1", DisplayName: "Ada Player", Role: user.RoleUser,
		}},
		GameRequests: commands, RequestQueries: queries,
	})
}

type routerEvents struct{ created event.Event }

func (*routerEvents) List(context.Context) ([]event.Event, error) { return nil, nil }
func (r *routerEvents) Create(_ context.Context, name string, startsAt, endsAt time.Time) (event.Event, error) {
	r.created = event.Event{Name: name, StartsAt: startsAt, EndsAt: endsAt}
	return r.created, nil
}
func (*routerEvents) Update(context.Context, string, string, time.Time, time.Time) (event.Event, error) {
	return event.Event{}, nil
}
func (*routerEvents) SetCurrent(context.Context, string) error { return nil }

type routerGames struct {
	items  []game.Game
	search string
}

func (r *routerGames) List(_ context.Context, search string) ([]game.Game, error) {
	r.search = search
	return r.items, nil
}
func (*routerGames) Create(context.Context, string, string) (game.Game, error) {
	return game.Game{}, nil
}
func (*routerGames) Update(context.Context, string, string, string) (game.Game, error) {
	return game.Game{}, nil
}
func (*routerGames) SetActive(context.Context, string, bool) error { return nil }
func (*routerGames) AddAlias(context.Context, string, string) (game.Alias, error) {
	return game.Alias{}, nil
}
func (*routerGames) RemoveAlias(context.Context, string, string) error { return nil }

type routerSubscriptions struct {
	items, mine []game.SubscriptionGame
	search      string
	followed    string
	unfollowed  string
}

type routerGameRequests struct {
	created   gamerequest.GameRequest
	joined    string
	left      string
	closed    string
	cancelled string
	joinError error
}

func (r *routerGameRequests) Create(_ context.Context, creatorID, gameID string, maximum *int32, startsAt time.Time, description string) (gamerequest.GameRequest, error) {
	r.created = gamerequest.GameRequest{
		ID: "request-created", CreatorID: creatorID, GameID: gameID, MaxPlayers: maximum,
		StartsAt: startsAt, Description: description, Status: gamerequest.StatusOpen,
	}
	return r.created, nil
}
func (r *routerGameRequests) Join(_ context.Context, requestID, userID string) error {
	if r.joinError != nil {
		return r.joinError
	}
	r.joined = requestID + "/" + userID
	return nil
}
func (r *routerGameRequests) Leave(_ context.Context, requestID, userID string) error {
	r.left = requestID + "/" + userID
	return nil
}
func (r *routerGameRequests) Close(_ context.Context, requestID, userID string) error {
	r.closed = requestID + "/" + userID
	return nil
}
func (r *routerGameRequests) Cancel(_ context.Context, requestID, userID string) error {
	r.cancelled = requestID + "/" + userID
	return nil
}

type routerGameRequestQueries struct {
	items   []requestquery.Request
	options requestquery.CreationOptions
}

type routerRealtime struct{ events <-chan gamerequest.Event }

func (r *routerRealtime) Subscribe() (<-chan gamerequest.Event, func()) {
	return r.events, func() {}
}

func (r *routerGameRequestQueries) ListActive(context.Context, string) ([]requestquery.Request, error) {
	return r.items, nil
}
func (r *routerGameRequestQueries) ByID(_ context.Context, id, _ string) (requestquery.Request, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return requestquery.Request{}, gamerequest.ErrNotFound
}
func (r *routerGameRequestQueries) CreationOptions(context.Context) (requestquery.CreationOptions, error) {
	return r.options, nil
}
func (r *routerGameRequestQueries) CurrentEvent(context.Context) (requestquery.EventContext, error) {
	return requestquery.EventContext{ID: r.options.EventID, Name: r.options.EventName}, nil
}

func (r *routerSubscriptions) Browse(_ context.Context, _, search string) ([]game.SubscriptionGame, error) {
	r.search = search
	return r.items, nil
}
func (r *routerSubscriptions) MyGames(context.Context, string) ([]game.SubscriptionGame, error) {
	return r.mine, nil
}
func (r *routerSubscriptions) Follow(_ context.Context, userID, gameID string) error {
	r.followed = userID + "/" + gameID
	return nil
}
func (r *routerSubscriptions) Unfollow(_ context.Context, userID, gameID string) error {
	r.unfollowed = userID + "/" + gameID
	return nil
}

func (*routerAuthentication) Available() bool { return true }

func (*routerAuthentication) BeginLogin() (auth.LoginFlow, error) { return auth.LoginFlow{}, nil }

func (*routerAuthentication) CompleteLogin(context.Context, string, string, string) (user.User, auth.BrowserSession, error) {
	return user.User{}, auth.BrowserSession{}, nil
}

func (*routerAuthentication) Logout(context.Context, string) error { return nil }

func (a *routerAuthentication) Authenticate(context.Context, string) (user.User, error) {
	return a.account, nil
}

// A second Join that lands after the first already succeeded must resolve into
// a dismissible notice, leaving the row and the rest of the page usable.
func TestDuplicateJoinReportsRecoverableConflict(t *testing.T) {
	commands := &routerGameRequests{joinError: gamerequest.ErrAlreadyMember}
	queries := &routerGameRequestQueries{items: []requestquery.Request{{
		ID: "request-1", GameName: "Warcraft III", CreatorID: "user-2",
		CreatorName: "Other", MemberCount: 2, ViewerJoined: true, Status: gamerequest.StatusOpen,
		StartsAt: time.Now().Add(time.Hour),
	}}}
	router := gameRequestTestRouter(commands, queries)

	duplicate := httptest.NewRequest(http.MethodPost, "/game-requests/request-1/join", strings.NewReader(url.Values{"return_to": {"list"}}.Encode()))
	duplicate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	duplicate.Header.Set("HX-Request", "true")
	duplicate.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	addCSRF(duplicate)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, duplicate)

	if response.Code != http.StatusOK {
		t.Fatalf("duplicate join status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("HX-Retarget"); got != "#live-notices" {
		t.Errorf("HX-Retarget = %q, want %q", got, "#live-notices")
	}
	if got := response.Header().Get("HX-Reswap"); got != "beforeend" {
		t.Errorf("HX-Reswap = %q, want %q", got, "beforeend")
	}
	body := response.Body.String()
	if !strings.Contains(body, "You have already joined this Game Request.") {
		t.Errorf("duplicate join body does not explain the conflict: %s", body)
	}
	if !strings.Contains(body, "data-dismiss-live-toast") {
		t.Errorf("duplicate join notice cannot be dismissed: %s", body)
	}
}
