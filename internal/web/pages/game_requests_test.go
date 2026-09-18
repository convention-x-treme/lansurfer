package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
)

func TestGameAbbreviation(t *testing.T) {
	tests := map[string]string{
		"Counter-Strike 2": "C2",
		"Warcraft III":     "W3",
		"Trackmania":       "TRA",
		"Age of Empires":   "AOE",
		"":                 "?",
	}
	for input, want := range tests {
		if got := gameAbbreviation(input); got != want {
			t.Errorf("gameAbbreviation(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGameRequestRowLetsJoinedViewerLeaveFullRequest(t *testing.T) {
	maxPlayers := int32(4)
	request := requestquery.Request{
		ID:           "request-1",
		Game:         game.GamePresentation{Name: "Warcraft III"},
		MaxPlayers:   &maxPlayers,
		MemberCount:  4,
		ViewerJoined: true,
		Status:       gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, ">Leave</button>") || strings.Contains(body, ">Full</span>") {
		t.Fatalf("row actions = %q", body)
	}
}

func TestGameRequestRowUsesPositiveJoinAction(t *testing.T) {
	request := requestquery.Request{
		ID:          "request-1",
		Game:        game.GamePresentation{Name: "Warcraft III"},
		MemberCount: 1,
		Status:      gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, `class="btn btn-primary btn-sm"`) {
		t.Fatalf("row action = %q", body)
	}
}

// Rapid clicking must not be able to queue a second mutation for the same row,
// so both membership actions carry htmx's in-flight guards.
func TestGameRequestRowGuardsAgainstDoubleSubmission(t *testing.T) {
	tests := map[string]requestquery.Request{
		"join": {
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, MemberCount: 1, Status: gamerequest.StatusOpen,
		},
		"leave": {
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, MemberCount: 2, ViewerJoined: true, Status: gamerequest.StatusOpen,
		},
	}
	for action, request := range tests {
		var rendered bytes.Buffer
		if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameRequestRow for %s: %v", action, err)
		}
		markup := rendered.String()
		if !strings.Contains(markup, `hx-post="/game-requests/request-1/`+action+`"`) {
			t.Fatalf("%s row does not post the %s action: %s", action, action, markup)
		}
		for _, guard := range []string{`hx-sync="closest article:drop"`, `hx-disabled-elt="find button"`} {
			if !strings.Contains(markup, guard) {
				t.Errorf("%s row is missing %s", action, guard)
			}
		}
	}
}

// The detail refresh replaces the request card in place, so the "updated" stamp
// in the top bar has to travel with it out of band. Activity is newest first.
func TestGameRequestDetailRefreshCarriesStampAndNewestActivityFirst(t *testing.T) {
	now := time.Now()
	request := requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusOpen, MemberCount: 2, CreatedAt: now.Add(-10 * time.Minute),
		Members: []requestquery.Member{
			{ID: "host-1", DisplayName: "Otter", JoinedAt: now.Add(-10 * time.Minute)},
			{ID: "user-2", DisplayName: "Alex", JoinedAt: now.Add(-2 * time.Minute)},
		},
		Activity: []requestquery.Activity{
			{Kind: gamerequest.ActivityJoined, ActorID: "user-2", ActorName: "Alex", OccurredAt: now.Add(-2 * time.Minute)},
			{Kind: gamerequest.ActivityCreated, ActorID: "host-1", ActorName: "Otter", OccurredAt: now.Add(-10 * time.Minute)},
		},
	}

	var refreshed bytes.Buffer
	if err := GameRequestDetailContent(request, "user-9", true).Render(context.Background(), &refreshed); err != nil {
		t.Fatalf("render refreshed detail: %v", err)
	}
	body := refreshed.String()
	for _, want := range []string{`hx-swap-oob="true"`, `id="detail-stamp"`, "updated "} {
		if !strings.Contains(body, want) {
			t.Errorf("refreshed detail does not contain %q", want)
		}
	}
	newest, oldest := strings.Index(body, "Alex joined"), strings.Index(body, "Otter created the request")
	if newest < 0 || oldest < 0 {
		t.Fatalf("detail does not render the durable activity record: %s", body)
	}
	if newest > oldest {
		t.Error("activity does not list the newest entry first")
	}

	var initial bytes.Buffer
	if err := GameRequestDetailContent(request, "user-9", false).Render(context.Background(), &initial); err != nil {
		t.Fatalf("render initial detail: %v", err)
	}
	if strings.Contains(initial.String(), `hx-swap-oob`) {
		t.Error("initial detail render must not carry an out-of-band swap")
	}
}

// Leaving removes current membership but not the history of the round, so the
// activity record keeps an entry the player list no longer shows.
func TestDetailActivityKeepsHistoryAfterLeave(t *testing.T) {
	now := time.Now()
	request := requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusOpen, MemberCount: 1, CreatedAt: now.Add(-30 * time.Minute),
		Members: []requestquery.Member{{ID: "host-1", DisplayName: "Otter", JoinedAt: now.Add(-30 * time.Minute)}},
		Activity: []requestquery.Activity{
			{Kind: gamerequest.ActivityLeft, ActorID: "user-2", ActorName: "Alex", OccurredAt: now.Add(-time.Minute)},
			{Kind: gamerequest.ActivityJoined, ActorID: "user-2", ActorName: "Alex", OccurredAt: now.Add(-5 * time.Minute)},
			{Kind: gamerequest.ActivityCreated, ActorID: "host-1", ActorName: "Otter", OccurredAt: now.Add(-30 * time.Minute)},
		},
	}

	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "host-1", false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{"Alex left", "Alex joined", "Otter created the request"} {
		if !strings.Contains(body, want) {
			t.Errorf("activity does not contain %q", want)
		}
	}
}

// The list wrapper hands the active filters to the live refresh, so an SSE
// update reloads the same selection instead of silently widening it.
func TestListContentCarriesActiveFilters(t *testing.T) {
	filter := requestquery.Filter{Search: "wc3", FollowedOnly: true, OpenPlacesOnly: true}

	var rendered bytes.Buffer
	if err := GameRequestListContent(branding.Default(), nil, filter, nil, "", false, nil, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render list content: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, "followed=1") || !strings.Contains(body, "open_places=1") || !strings.Contains(body, "q=wc3") {
		t.Fatalf("list does not carry its filters: %s", body)
	}
	if !strings.Contains(body, "No Game Request matches that.") {
		t.Errorf("filtered empty list does not say the filters are the reason: %s", body)
	}
}

func TestOpenRequestsLabelDistinguishesFilteredLists(t *testing.T) {
	if got := openRequestsLabel(2, requestquery.Filter{}); got != "2 open requests" {
		t.Errorf("unfiltered label = %q", got)
	}
	if got := openRequestsLabel(1, requestquery.Filter{Search: "wc3"}); got != "1 matching open request" {
		t.Errorf("filtered label = %q", got)
	}
}

// Typing filters the list without pressing the button, and it does so once the
// player pauses rather than once per keystroke. The button stays as the
// no-scripting submit path.
func TestRequestFiltersApplyWhileTyping(t *testing.T) {
	var rendered bytes.Buffer
	if err := RequestFilters(requestquery.Filter{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render filters: %v", err)
	}
	markup := rendered.String()
	for _, want := range []string{
		"input changed delay:300ms from:(#request-search)",
		"search from:(#request-search)",
		`hx-sync="this:replace"`,
		// A debounced keystroke must not push one history entry per pause.
		`hx-replace-url="true"`,
		`type="submit"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("filter form is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `hx-push-url`) {
		t.Error("live filtering must replace the URL, not push a history entry per keystroke")
	}
}

// The list row shares GameIdentity's cover-or-abbreviation rendering with
// /games (Task 6), so a Game materialized via IGDB shows its cover in the
// row exactly as it does everywhere else, not the abbreviation tile.
func TestGameRequestRowUsesCoverWhenAvailable(t *testing.T) {
	request := requestquery.Request{
		ID:     "request-1",
		Game:   game.GamePresentation{Name: "Warcraft III", CoverImageID: "co1abc"},
		Status: gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, "https://images.igdb.com/igdb/image/upload/t_cover_small/co1abc.jpg") {
		t.Fatalf("row does not render the cover: %s", body)
	}
	if strings.Contains(body, `class="game-abbreviation"`) {
		t.Errorf("row rendered the abbreviation tile even though a cover was available: %s", body)
	}
}

// A Game never materialized via IGDB has no cover, so the row must keep
// falling back to the abbreviation tile exactly as before Task 6.
func TestGameRequestRowFallsBackToAbbreviationWithoutCover(t *testing.T) {
	request := requestquery.Request{
		ID:     "request-1",
		Game:   game.GamePresentation{Name: "Warcraft III"},
		Status: gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `class="game-abbreviation"`) || !strings.Contains(body, ">W3<") {
		t.Fatalf("row does not fall back to the abbreviation tile: %s", body)
	}
	if strings.Contains(body, "images.igdb.com") {
		t.Errorf("row rendered a cover without a CoverImageID: %s", body)
	}
}

// Trending is a focused text-only read model over Game Request activity, not
// a card grid; it must never grow a cover image, even though the Request
// rows on the same page can now show one.
func TestTrendingRemainsTextOnly(t *testing.T) {
	trending := []requestquery.TrendingGame{{GameID: "game-1", GameName: "Warcraft III", ActivityCount: 4, RequestCount: 2}}
	var rendered bytes.Buffer
	if err := TrendingSection(trending, false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render TrendingSection: %v", err)
	}
	if body := rendered.String(); strings.Contains(body, "<img") {
		t.Errorf("Trending must stay text-only: %s", body)
	}
}

// The detail hero renders the same shared GameIdentity presentation as
// /games and the list rows, so a materialized Game's cover and
// type/platform/release metadata show up at the top of its detail page too.
func TestGameRequestDetailContentRendersGameIdentityWithCoverAndMetadata(t *testing.T) {
	release := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	request := requestquery.Request{
		ID: "request-1",
		Game: game.GamePresentation{
			Name: "Warcraft III", CoverImageID: "co1abc", GameType: "Main Game",
			PlatformNames: []string{"PC (Microsoft Windows)"}, FirstReleaseDate: &release,
		},
		CreatorID: "host-1", Status: gamerequest.StatusOpen, MemberCount: 1,
	}
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "host-1", false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		"https://images.igdb.com/igdb/image/upload/t_cover_small/co1abc.jpg",
		"Main Game",
		"PC (Microsoft Windows)",
		"2002",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail does not contain %q: %s", want, body)
		}
	}
}

// The composer's initial unselected state is discovery-first, exactly like
// /games (Task 4): it shows the shared "Find your game" heading and
// discovery list/LAN list rather than a dropdown of pre-existing Games.
func TestNewGameRequestShowsDiscoveryBeforeSelection(t *testing.T) {
	page := game.DiscoveryPage{Items: []game.DiscoveryItem{{
		LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true,
	}}}
	form := GameRequestForm{StartPreset: "now"}

	var rendered bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, page, nil, false, "", form, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render NewGameRequest: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{"Find your game", "Warcraft III", `id="game-request-discovery"`} {
		if !strings.Contains(body, want) {
			t.Errorf("unselected composer missing %q: %s", want, body)
		}
	}
}

// Once a Game is selected, the composer renders it through the shared
// GameIdentity presentation (real name and cover), a "Change game" action,
// and hidden identity fields -- never the old static "Selected from IGDB
// search results." placeholder (spec: "4.1 One player-facing Game card").
func TestNewGameRequestSelectedStateRendersIdentityAndHiddenFields(t *testing.T) {
	release := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	selected := game.DiscoveryItem{
		Reference: game.ProviderReference{Provider: "igdb", ExternalID: "42"},
		GamePresentation: game.GamePresentation{
			Name: "Warcraft III: Reign of Chaos", CoverImageID: "co1abc", GameType: "Main Game", FirstReleaseDate: &release,
		},
	}
	form := GameRequestForm{StartPreset: "now", Provider: "igdb", ExternalID: "42"}

	var rendered bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, &selected, true, "", form, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render NewGameRequest: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		"Warcraft III: Reign of Chaos",
		"https://images.igdb.com/igdb/image/upload/t_cover_small/co1abc.jpg",
		"Change game",
		`id="game-request-game-fields"`,
		`name="provider" value="igdb"`,
		`name="external_id" value="42"`,
		`id="game-request-details"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("selected composer missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Selected from IGDB search results.") {
		t.Errorf("selected composer still renders the stale placeholder copy: %s", body)
	}
}

// Request-local fields (max players/start/description) do not exist at all
// before a Game is selected, and appear once one is (Task 7 Step 8).
func TestNewGameRequestDetailFieldsAppearOnlyAfterSelection(t *testing.T) {
	form := GameRequestForm{StartPreset: "now"}

	var unselected bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, nil, false, "", form, "").Render(context.Background(), &unselected); err != nil {
		t.Fatalf("render unselected composer: %v", err)
	}
	if strings.Contains(unselected.String(), `name="max_players"`) {
		t.Errorf("unselected composer unexpectedly renders request detail fields: %s", unselected.String())
	}

	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}
	var afterSelection bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, &selected, true, "", form, "").Render(context.Background(), &afterSelection); err != nil {
		t.Fatalf("render selected composer: %v", err)
	}
	if !strings.Contains(afterSelection.String(), `name="max_players"`) {
		t.Errorf("selected composer does not render request detail fields: %s", afterSelection.String())
	}
}

// The "Change game" panel's own search uses the identical 300ms debounce/
// replace-URL live-search interaction model as Games' GameSearchForm (Task
// 4), reused rather than reinvented.
func TestGameRequestChangeGameSearchUsesLiveSearchInteractionModel(t *testing.T) {
	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}

	var rendered bytes.Buffer
	if err := GameRequestSelectedGame(selected).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectedGame: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`hx-get="/game-requests/new/discovery"`,
		`hx-target="#game-request-discovery"`,
		"input changed delay:300ms",
		`hx-sync="this:replace"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("change-game search missing %q: %s", want, body)
		}
	}
}

// Review finding: the change-game search form's no-scripting fallback must
// land on the real composer page, never on the layout-less discovery
// fragment endpoint -- that fragment has no page chrome, no selection, and
// no create form. hx-replace-url must not be set either, since it would
// otherwise point the address bar at that same broken fragment URL on every
// live search.
func TestGameRequestChangeGameSearchFallsBackToTheComposerPage(t *testing.T) {
	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}

	var rendered bytes.Buffer
	if err := GameRequestSelectedGame(selected).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectedGame: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `action="/game-requests/new"`) {
		t.Errorf("change-game search does not fall back to the composer page: %s", body)
	}
	if strings.Contains(body, `action="/game-requests/new/discovery"`) {
		t.Errorf("change-game search still falls back to the layout-less fragment endpoint: %s", body)
	}
	if strings.Contains(body, "hx-replace-url") {
		t.Errorf("change-game search unexpectedly sets hx-replace-url: %s", body)
	}
}

// Review finding: the collapsed "Change game" panel must not render a false
// "No games are available yet." message on a perfectly healthy instance --
// that happened because it rendered GameRequestDiscovery with a fabricated
// zero-value DiscoveryPage (zero-value ProviderStatus) instead of a real
// Browse result or its own pre-search prompt.
func TestGameRequestSelectedGameChangePanelDoesNotFakeUnavailability(t *testing.T) {
	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}

	var rendered bytes.Buffer
	if err := GameRequestSelectedGame(selected).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectedGame: %v", err)
	}
	body := rendered.String()
	if strings.Contains(body, "No games are available yet.") {
		t.Errorf("change-game panel falsely reports the catalog unavailable: %s", body)
	}
	if !strings.Contains(body, `id="game-request-discovery"`) {
		t.Errorf("change-game panel is missing its live-search swap target: %s", body)
	}
}

// Selecting a new game via the "Change game" panel must swap only the
// selected-game display and the hidden identity fields, never
// #game-request-details -- that is what leaves the start/max-player/
// description values the player already typed exactly as they are in the
// browser (Task 7 Step 7).
func TestGameRequestSelectionSwapExcludesRequestDetails(t *testing.T) {
	item := game.DiscoveryItem{LocalGameID: "game-2", GamePresentation: game.GamePresentation{Name: "Age of Empires"}, Materialized: true}

	var rendered bytes.Buffer
	if err := GameRequestSelectionSwap(item).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectionSwap: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`id="game-request-selected-game"`,
		`id="game-request-game-fields"`,
		`hx-swap-oob="true"`,
		"Age of Empires",
		`name="game_id" value="game-2"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("selection swap missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `id="game-request-details"`) {
		t.Errorf("selection swap unexpectedly touches request details: %s", body)
	}
}
