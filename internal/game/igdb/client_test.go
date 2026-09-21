package igdb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
)

func TestGamesGetsOneTokenAndPagesByLastID(t *testing.T) {
	var tokenRequests atomic.Int32
	var mu sync.Mutex
	var gameRequests []recordedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("token method = %s, want POST", r.Method)
			}
			if got := r.URL.Query(); got.Get("client_id") != "client-id" || got.Get("client_secret") != "client-secret" || got.Get("grant_type") != "client_credentials" {
				t.Errorf("token query = %v, want client credentials", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"token-1","expires_in":3600,"token_type":"bearer"}`)
		case "/games":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read games body: %v", err)
				return
			}
			mu.Lock()
			gameRequests = append(gameRequests, recordedRequest{
				method:        r.Method,
				authorization: r.Header.Get("Authorization"),
				clientID:      r.Header.Get("Client-ID"),
				contentType:   r.Header.Get("Content-Type"),
				body:          string(body),
			})
			requestNumber := len(gameRequests)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			switch requestNumber {
			case 1:
				_, _ = io.WriteString(w, `[
					{"id":1,"name":"Alpha","slug":"alpha","game_type":{"type":"Main Game"},"first_release_date":100,"parent_game":9,"version_parent":8,"platforms":[{"id":6,"name":"PC"},{"id":49,"name":"Linux"}],"alternative_names":[{"name":"A"}],"cover":{"image_id":"cover-a"},"updated_at":200,"checksum":"checksum-a","summary":"Alpha summary","genres":[{"name":"Strategy"}],"game_modes":[{"name":"Single player"},{"name":"Multiplayer"}],"multiplayer_modes":[{"platform":6,"campaigncoop":true,"lancoop":true,"onlinecoopmax":4,"onlinemax":0},{"platform":49,"splitscreen":true,"onlinecoop":true}]},
					{"id":2,"name":"Beta","slug":"beta","game_type":{"type":"DLC / Add-on"},"platforms":[],"alternative_names":[]}
				]`)
			case 2:
				_, _ = io.WriteString(w, `[{"id":3,"name":"Gamma","slug":"gamma","game_type":{"type":"Remake"}}]`)
			default:
				t.Errorf("unexpected games request %d", requestNumber)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient("client-id", "client-secret",
		WithTokenURL(server.URL+"/token"),
		WithGamesURL(server.URL+"/games"),
		WithPageSize(2),
		withRequestInterval(0),
	)
	var got []game.ProviderGame
	err := client.Games(context.Background(), func(batch []game.ProviderGame) error {
		got = append(got, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("Games() error = %v", err)
	}

	firstRelease := time.Unix(100, 0).UTC()
	providerUpdated := time.Unix(200, 0).UTC()
	// onlinemax:0 and the absent offline limits must map to nil ("not
	// reported"), never to a zero-player cap (issue #46 decision).
	alphaOnlineCoopMax := 4
	want := []game.ProviderGame{
		{
			Reference:         game.ProviderReference{Provider: "igdb", ExternalID: "1"},
			Name:              "Alpha",
			Slug:              "alpha",
			GameType:          "Main Game",
			FirstReleaseDate:  &firstRelease,
			ParentExternalID:  "9",
			VersionParentID:   "8",
			PlatformNames:     []string{"PC", "Linux"},
			AlternativeNames:  []string{"A"},
			CoverImageID:      "cover-a",
			ProviderUpdatedAt: &providerUpdated,
			Checksum:          "checksum-a",
			Summary:           "Alpha summary",
			GenreNames:        []string{"Strategy"},
			GameModeNames:     []string{"Single player", "Multiplayer"},
			MultiplayerModes: []game.MultiplayerMode{
				{
					PlatformIGDBID: 6, PlatformName: "PC",
					CampaignCoop: true, LanCoop: true,
					OnlineCoopMax: new(alphaOnlineCoopMax),
				},
				{
					PlatformIGDBID: 49, PlatformName: "Linux",
					OnlineCoop: true, SplitScreen: true,
				},
			},
		},
		{
			Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "2"},
			Name:             "Beta",
			Slug:             "beta",
			GameType:         "DLC / Add-on",
			PlatformNames:    []string{},
			AlternativeNames: []string{},
		},
		{
			Reference: game.ProviderReference{Provider: "igdb", ExternalID: "3"},
			Name:      "Gamma",
			Slug:      "gamma",
			GameType:  "Remake",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Games() records = %#v, want %#v", got, want)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}

	mu.Lock()
	requests := append([]recordedRequest(nil), gameRequests...)
	mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("games requests = %d, want 2", len(requests))
	}
	wantBodies := []string{
		"fields id,name,slug,game_type.type,first_release_date,parent_game,version_parent,platforms.id,platforms.name,alternative_names.name,cover.image_id,updated_at,checksum,summary,genres.name,game_modes.name,multiplayer_modes.*;\nwhere id > 0;\nsort id asc;\nlimit 2;",
		"fields id,name,slug,game_type.type,first_release_date,parent_game,version_parent,platforms.id,platforms.name,alternative_names.name,cover.image_id,updated_at,checksum,summary,genres.name,game_modes.name,multiplayer_modes.*;\nwhere id > 2;\nsort id asc;\nlimit 2;",
	}
	for i, request := range requests {
		if request.method != http.MethodPost {
			t.Errorf("games request %d method = %q, want POST", i+1, request.method)
		}
		if request.authorization != "Bearer token-1" {
			t.Errorf("games request %d authorization = %q, want Bearer token-1", i+1, request.authorization)
		}
		if request.clientID != "client-id" {
			t.Errorf("games request %d Client-ID = %q, want client-id", i+1, request.clientID)
		}
		if request.contentType != "text/plain" {
			t.Errorf("games request %d Content-Type = %q, want text/plain", i+1, request.contentType)
		}
		if request.body != wantBodies[i] {
			t.Errorf("games request %d body = %q, want %q", i+1, request.body, wantBodies[i])
		}
	}
}

func TestGamesRefreshesTokenOnceAfter401(t *testing.T) {
	var tokenRequests atomic.Int32
	var mu sync.Mutex
	var authorizations []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			n := tokenRequests.Add(1)
			_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3600,"token_type":"bearer"}`, n)
		case "/games":
			mu.Lock()
			authorizations = append(authorizations, r.Header.Get("Authorization"))
			requestNumber := len(authorizations)
			mu.Unlock()
			if requestNumber == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient("client-id", "client-secret",
		WithTokenURL(server.URL+"/token"),
		WithGamesURL(server.URL+"/games"),
		withRequestInterval(0),
	)
	if err := client.Games(context.Background(), func([]game.ProviderGame) error { return nil }); err != nil {
		t.Fatalf("Games() error = %v", err)
	}

	if got := tokenRequests.Load(); got != 2 {
		t.Fatalf("token requests = %d, want 2", got)
	}
	mu.Lock()
	gotAuthorizations := append([]string(nil), authorizations...)
	mu.Unlock()
	if want := []string{"Bearer token-1", "Bearer token-2"}; !reflect.DeepEqual(gotAuthorizations, want) {
		t.Fatalf("games authorization headers = %v, want %v", gotAuthorizations, want)
	}
}

func TestGamesRejectsOversizedOrMalformedResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "non-array JSON", body: `{}`},
		{name: "malformed JSON", body: `[`},
		{name: "oversized JSON", body: `[` + strings.Repeat(" ", 64) + `]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600,"token_type":"bearer"}`)
				case "/games":
					_, _ = io.WriteString(w, tt.body)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			client := NewClient("client-id", "client-secret",
				WithTokenURL(server.URL+"/token"),
				WithGamesURL(server.URL+"/games"),
				withRequestInterval(0),
				WithMaxResponseBytes(32),
			)
			err := client.Games(context.Background(), func([]game.ProviderGame) error { return nil })
			if err == nil {
				t.Fatal("Games() error = nil, want response error")
			}
		})
	}
}

func TestGamesRejectsOversizedTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600}`+strings.Repeat(" ", 64))
		case "/games":
			t.Fatal("Games request made after oversized token response")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient("client-id", "client-secret",
		WithTokenURL(server.URL+"/token"),
		WithGamesURL(server.URL+"/games"),
		WithMaxResponseBytes(32),
	)
	if err := client.Games(context.Background(), func([]game.ProviderGame) error { return nil }); err == nil {
		t.Fatal("Games() error = nil, want oversized token response error")
	}
}

func TestGamesPacesRequestStarts(t *testing.T) {
	clock := newFakeClock(time.Unix(1_700_000_000, 0))
	var mu sync.Mutex
	var starts []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"token","expires_in":3600,"token_type":"bearer"}`)
		case "/games":
			mu.Lock()
			starts = append(starts, clock.Now())
			mu.Unlock()
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient("client-id", "client-secret",
		WithTokenURL(server.URL+"/token"),
		WithGamesURL(server.URL+"/games"),
		withRequestInterval(10*time.Millisecond),
		withNow(clock.Now),
		withSleep(clock.Sleep),
	)
	start := make(chan struct{})
	errors := make(chan error, 3)
	for range 3 {
		go func() {
			<-start
			errors <- client.Games(context.Background(), func([]game.ProviderGame) error { return nil })
		}()
	}
	close(start)
	for range 3 {
		if err := <-errors; err != nil {
			t.Fatalf("Games() error = %v", err)
		}
	}

	mu.Lock()
	gotStarts := append([]time.Time(nil), starts...)
	mu.Unlock()
	if len(gotStarts) != 3 {
		t.Fatalf("games request starts = %d, want 3", len(gotStarts))
	}
	for i := 1; i < len(gotStarts); i++ {
		if elapsed := gotStarts[i].Sub(gotStarts[i-1]); elapsed < 10*time.Millisecond {
			t.Errorf("games request start %d followed previous by %v, want at least 10ms", i+1, elapsed)
		}
	}
}

type recordedRequest struct {
	method        string
	authorization string
	clientID      string
	contentType   string
	body          string
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
	return nil
}
