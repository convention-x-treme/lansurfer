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
			{ID: "host-1", GameNick: "Otter", JoinedAt: now.Add(-10 * time.Minute)},
			{ID: "user-2", GameNick: "Alex", JoinedAt: now.Add(-2 * time.Minute)},
		},
		Activity: []requestquery.Activity{
			{Kind: gamerequest.ActivityJoined, ActorID: "user-2", ActorGameNick: "Alex", OccurredAt: now.Add(-2 * time.Minute)},
			{Kind: gamerequest.ActivityCreated, ActorID: "host-1", ActorGameNick: "Otter", OccurredAt: now.Add(-10 * time.Minute)},
		},
	}

	var refreshed bytes.Buffer
	if err := GameRequestDetailContent(request, "user-9", true, GameRequestDetailMessage{}).Render(context.Background(), &refreshed); err != nil {
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
	if err := GameRequestDetailContent(request, "user-9", false, GameRequestDetailMessage{}).Render(context.Background(), &initial); err != nil {
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
		Members: []requestquery.Member{{ID: "host-1", GameNick: "Otter", JoinedAt: now.Add(-30 * time.Minute)}},
		Activity: []requestquery.Activity{
			{Kind: gamerequest.ActivityLeft, ActorID: "user-2", ActorGameNick: "Alex", OccurredAt: now.Add(-time.Minute)},
			{Kind: gamerequest.ActivityJoined, ActorID: "user-2", ActorGameNick: "Alex", OccurredAt: now.Add(-5 * time.Minute)},
			{Kind: gamerequest.ActivityCreated, ActorID: "host-1", ActorGameNick: "Otter", OccurredAt: now.Add(-30 * time.Minute)},
		},
	}

	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "host-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
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
	filter := requestquery.Filter{Search: "wc3", Scope: requestquery.ScopeJoined, OpenPlacesOnly: true}

	var rendered bytes.Buffer
	if err := GameRequestListContent(branding.Default(), nil, filter, nil, "", false, nil, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render list content: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, "scope=joined") || !strings.Contains(body, "open_places=1") || !strings.Contains(body, "q=wc3") {
		t.Fatalf("list does not carry its filters: %s", body)
	}
	if !strings.Contains(body, "No Game Request matches that.") {
		t.Errorf("filtered empty list does not say the filters are the reason: %s", body)
	}
}

// The list label counts active requests (`open` or `playing`), so a round that
// has started still reads as part of the set the list is showing.
func TestActiveRequestsLabelDistinguishesFilteredLists(t *testing.T) {
	if got := activeRequestsLabel(2, requestquery.Filter{}); got != "2 active requests" {
		t.Errorf("unfiltered label = %q", got)
	}
	if got := activeRequestsLabel(1, requestquery.Filter{Search: "wc3"}); got != "1 matching active request" {
		t.Errorf("filtered label = %q", got)
	}
}

// Typing filters the list without a visible submit button, and it does so once
// the player pauses rather than once per keystroke. A noscript submit path
// remains for progressive enhancement.
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
		// Filters apply live; a submit button exists only for no-script browsers.
		`<noscript><button class="btn btn-quiet btn-sm" type="submit">`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("filter form is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `hx-push-url`) {
		t.Error("live filtering must replace the URL, not push a history entry per keystroke")
	}
}

// The list row leads with the Game's cover at the size the cover row shows
// it, so a Game materialized via IGDB shows its art rather than the
// abbreviation tile.
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
	if !strings.Contains(body, "/media/igdb/big/co1abc") {
		t.Fatalf("row does not render the cover: %s", body)
	}
	if !strings.Contains(body, `class="cover-art-fallback" aria-hidden="true" hidden>`) {
		t.Errorf("row must render a hidden abbreviation fallback for the cover to fall back to on load failure: %s", body)
	}
}

// A Game never materialized via IGDB has no cover, so the row must keep
// falling back to the abbreviation in the cover tile's place.
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
	if !strings.Contains(body, `class="cover-art-fallback"`) || !strings.Contains(body, ">W3<") {
		t.Fatalf("row does not fall back to the abbreviation tile: %s", body)
	}
	if strings.Contains(body, "images.igdb.com") {
		t.Errorf("row rendered a cover without a CoverImageID: %s", body)
	}
}

// PRODUCT requires the list itself to carry Game, capacity, start time, short
// description, and the Join action, so a player never has to open the detail
// page to decide. The row states who is looking for what in one reading line.
func TestGameRequestRowCarriesTheWholeDecision(t *testing.T) {
	maxPlayers := int32(4)
	request := requestquery.Request{
		ID:              "request-1",
		Game:            game.GamePresentation{Name: "Warcraft III"},
		CreatorID:       "host-1",
		CreatorGameNick: "Otter#1337",
		Description:     "2v2 melee on Twisted Meadows, no mic needed.",
		MaxPlayers:      &maxPlayers,
		MemberCount:     2,
		Status:          gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`>Otter#1337 <span class="request-row-connector">is looking for</span> Warcraft III</span>`,
		"2v2 melee on Twisted Meadows, no mic needed.",
		">2/4<",
		">Join</button>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("row does not contain %q: %s", want, body)
		}
	}
}

// A creator whose public Game Nick is not known falls back to the Game alone
// rather than to a dangling sentence or an empty name.
func TestGameRequestRowTitleFallsBackToTheGameWithoutANick(t *testing.T) {
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
	if !strings.Contains(body, `class="request-row-title-text">Warcraft III<`) {
		t.Errorf("row title does not fall back to the Game name: %s", body)
	}
	if strings.Contains(body, "is looking for") {
		t.Errorf("row title rendered a sentence without a creator name: %s", body)
	}
}

// A request with no description simply has no description line: an empty
// element would reserve the row height of a line that says nothing.
func TestGameRequestRowOmitsAnEmptyDescription(t *testing.T) {
	request := requestquery.Request{
		ID:     "request-1",
		Game:   game.GamePresentation{Name: "Warcraft III"},
		Status: gamerequest.StatusOpen,
	}
	var rendered bytes.Buffer
	if err := GameRequestRow(request, "viewer-1", "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestRow: %v", err)
	}
	if body := rendered.String(); strings.Contains(body, "request-row-description") {
		t.Errorf("row rendered an empty description line: %s", body)
	}
}

// The Trending rail is a column of cover art, ranked by order. A cover-only
// link has no text to name it, so each tile carries the game and its activity
// counts as its accessible name, and the rail keeps stating its window.
func TestTrendingRailRendersRankedCoversWithAccessibleNames(t *testing.T) {
	trending := []requestquery.TrendingGame{
		{GameID: "game-1", GameName: "Warcraft III", CoverImageID: "co1abc", ActivityCount: 4, RequestCount: 2},
		{GameID: "game-2", GameName: "Quake III Arena", ActivityCount: 1, RequestCount: 1},
	}
	var rendered bytes.Buffer
	if err := TrendingSection(trending, false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render TrendingSection: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		"/media/igdb/big/co1abc",
		`aria-label="Warcraft III, 4 activity entries across 2 Game Requests"`,
		`aria-label="Quake III Arena, 1 activity entry across 1 Game Request"`,
		"last 6 hours",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Trending rail does not contain %q: %s", want, body)
		}
	}
	// The second entry has no cover, so it must fall back rather than request
	// a cover URL built from an empty image ID.
	if strings.Contains(body, `/media/igdb/big/"`) {
		t.Errorf("Trending rail requested a cover for a Game without one: %s", body)
	}
	if !strings.Contains(body, ">Q3A<") {
		t.Errorf("Trending rail does not fall back to the abbreviation: %s", body)
	}
}

// The detail page leads with the Game's portrait cover and names it as the
// screen title. The cover is shown once: the rail's Game panel states the
// Game in words -- release year and the whole summary -- rather than
// repeating the art the hero already carries.
func TestGameRequestDetailContentRendersCoverOnceAndDescribesTheGame(t *testing.T) {
	release := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	request := requestquery.Request{
		ID: "request-1", GameID: "game-1",
		Game: game.GamePresentation{
			Name: "Warcraft III", CoverImageID: "co1abc", GameType: "Main Game",
			PlatformNames:    []string{"PC (Microsoft Windows)"},
			FirstReleaseDate: &release,
			Summary:          "Reign of Chaos is the third game in the Warcraft series.",
		},
		CreatorID: "host-1", Status: gamerequest.StatusOpen, MemberCount: 1,
	}
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "host-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`<h1 class="detail-title">Warcraft III</h1>`,
		"2002",
		"Reign of Chaos is the third game in the Warcraft series.",
		"More rounds of this game",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail does not contain %q: %s", want, body)
		}
	}
	if count := strings.Count(body, "co1abc"); count != 1 {
		t.Errorf("the cover should be rendered once, got %d: %s", count, body)
	}
}

// Following is offered where a player decides they want more of this Game,
// and only as an offer: unfollowing belongs to the catalog, not to a round.
func TestGameRequestDetailOffersFollowOnlyWhileNotFollowing(t *testing.T) {
	request := requestquery.Request{
		ID: "request-1", GameID: "game-1",
		Game:      game.GamePresentation{Name: "Warcraft III"},
		CreatorID: "host-1", Status: gamerequest.StatusOpen, MemberCount: 1,
	}
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "viewer-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `action="/games/game-1/follow"`) || !strings.Contains(body, `name="request_id" value="request-1"`) {
		t.Errorf("an unfollowed Game should offer a follow that returns to the round: %s", body)
	}

	request.ViewerSubscribed = true
	rendered.Reset()
	if err := GameRequestDetailContent(request, "viewer-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body = rendered.String()
	if strings.Contains(body, "/follow") || strings.Contains(body, "/unfollow") {
		t.Errorf("a followed Game states the fact and offers no action: %s", body)
	}
}

// A last free place borders the hero and names itself as a chip, the same
// scarcity signal the list row the player came from carries.
func TestGameRequestDetailContentMarksLastPlace(t *testing.T) {
	limit := int32(4)
	request := requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"},
		CreatorID: "host-1", Status: gamerequest.StatusOpen, MemberCount: 3, MaxPlayers: &limit,
	}
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, "host-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{"detail-hero detail-hero-last", `<span class="status-chip status-chip-last">Last place</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("last-place detail does not contain %q: %s", want, body)
		}
	}

	request.MemberCount = 1
	rendered.Reset()
	if err := GameRequestDetailContent(request, "host-1", false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	if body := rendered.String(); strings.Contains(body, "detail-hero-last") || strings.Contains(body, "Last place") {
		t.Errorf("request with two places left is marked as the last place: %s", body)
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
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, page, nil, false, "", form, ComposerMessage{}).Render(context.Background(), &rendered); err != nil {
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
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, &selected, true, "", form, ComposerMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render NewGameRequest: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		"Warcraft III: Reign of Chaos",
		"/media/igdb/big/co1abc",
		`loading="eager"`,
		`fetchpriority="high"`,
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
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, nil, false, "", form, ComposerMessage{}).Render(context.Background(), &unselected); err != nil {
		t.Fatalf("render unselected composer: %v", err)
	}
	if strings.Contains(unselected.String(), `name="max_players"`) {
		t.Errorf("unselected composer unexpectedly renders request detail fields: %s", unselected.String())
	}

	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}
	var afterSelection bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, &selected, true, "", form, ComposerMessage{}).Render(context.Background(), &afterSelection); err != nil {
		t.Fatalf("render selected composer: %v", err)
	}
	if !strings.Contains(afterSelection.String(), `name="max_players"`) {
		t.Errorf("selected composer does not render request detail fields: %s", afterSelection.String())
	}
}

// "Change game" is a link back to the pick list, not an inline second
// search: one list picks games, and the link carries every request detail
// already typed so coming back does not cost the player their answers.
func TestGameRequestSelectedGameChangesGameThroughThePickList(t *testing.T) {
	selected := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}
	form := GameRequestForm{MaxPlayers: "4", StartPreset: "custom", StartDate: "2027-05-01", StartTime: "18:00", Description: "LAN night"}

	var rendered bytes.Buffer
	if err := GameRequestSelectedGame(selected, form).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectedGame: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `href="/game-requests/new?description=LAN+night&amp;max_players=4&amp;start_date=2027-05-01&amp;start_preset=custom&amp;start_time=18%3A00"`) {
		t.Errorf("change-game link does not return to the pick list with the typed details: %s", body)
	}
	for _, unwanted := range []string{"hx-get=", `id="game-request-discovery"`, "<details"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("change-game still carries the inline search panel (%s): %s", unwanted, body)
		}
	}
}

// The details step names the Game and nothing else about it: the player
// picked it one screen ago, so its type/platform/release line and its
// follow state are both noise where they are answering a different
// question.
func TestGameRequestSelectedGameShowsNameWithoutCatalogMetadata(t *testing.T) {
	release := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	selected := game.DiscoveryItem{
		LocalGameID: "game-1",
		GamePresentation: game.GamePresentation{
			Name: "Warcraft III", Slug: "warcraft-3", GameType: "Main Game",
			PlatformNames: []string{"PC (Microsoft Windows)"}, FirstReleaseDate: &release,
		},
		Subscribed: true, Materialized: true,
	}

	var rendered bytes.Buffer
	if err := GameRequestSelectedGame(selected, GameRequestForm{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameRequestSelectedGame: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, "Warcraft III") {
		t.Errorf("selected game does not name the Game: %s", body)
	}
	for _, unwanted := range []string{"Main Game", "PC (Microsoft Windows)", "2002", "warcraft-3", "followed"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("selected game still renders catalog metadata (%s): %s", unwanted, body)
		}
	}
}

// The recovery state after a provider disappears has no selected game but
// does retain the request fields. Plain GET search and Select links must carry
// those fields so a no-JavaScript player can choose a replacement without
// retyping them.
func TestComposerRecoveryDiscoveryPreservesDetailsInNoJSActions(t *testing.T) {
	page := game.DiscoveryPage{Items: []game.DiscoveryItem{{
		LocalGameID: "local-2", GamePresentation: game.GamePresentation{Name: "Age of Empires"}, Materialized: true,
	}}}
	form := GameRequestForm{
		MaxPlayers: "4", StartPreset: "custom", StartDate: "2027-05-01", StartTime: "18:00", Timezone: "Europe/Berlin", Description: "LAN night",
	}

	var rendered bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, page, nil, true, "age", form, ComposerMessage{Text: "Choose another game."}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render recovery composer: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`name="max_players" value="4"`,
		`name="start_preset" value="custom"`,
		`name="start_date" value="2027-05-01"`,
		`name="start_time" value="18:00"`,
		`name="timezone" value="Europe/Berlin"`,
		`name="description" value="LAN night"`,
		`href="/game-requests/new?description=LAN+night&amp;game_id=local-2&amp;max_players=4&amp;start_date=2027-05-01&amp;start_preset=custom&amp;start_time=18%3A00&amp;timezone=Europe%2FBerlin"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recovery discovery missing %q: %s", want, body)
		}
	}
}

// The pick search is live like Games' search: typing filters the list on
// the same debounce, and the only submit control left is the
// no-JavaScript fallback.
func TestComposerPickSearchIsLive(t *testing.T) {
	form := GameRequestForm{MaxPlayers: "4", Description: "LAN night"}

	var rendered bytes.Buffer
	if err := GameRequestDiscoverySearchForm("war", form).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render first-pick search: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`hx-get="/game-requests/new/discovery"`,
		`data-game-request-search`,
		`hx-target="#game-request-discovery"`,
		"input changed delay:300ms from:(#game-request-search)",
		"search from:(#game-request-search)",
		`action="/game-requests/new"`,
		`name="max_players" value="4"`,
		`<noscript><button`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pick search missing %q: %s", want, body)
		}
	}
	if before, _, _ := strings.Cut(body, "<noscript>"); strings.Contains(before, "<button") {
		t.Errorf("pick search still renders a visible search button: %s", body)
	}
}

// Max players is a plain numeric field: 0 (or empty) is an open round, a
// finite maximum starts at 2, and the field itself states both bounds and what 0 means, so a player
// never has to submit to find out.
func TestGameRequestMaxPlayersIsABoundedNumericField(t *testing.T) {
	var rendered bytes.Buffer
	if err := GameRequestDetailsFields(GameRequestForm{MaxPlayers: "4", StartPreset: "now"}, ComposerMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render details fields: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`for="max-players"`,
		`type="number" name="max_players" min="0" max="999"`,
		"0 for an open round, or 2 to 999 players.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("max players field missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "data-stepper") {
		t.Errorf("max players still renders the stepper buttons: %s", body)
	}
}

// A rejection that names one field belongs with that field: rendered
// inside its group, announced through aria-describedby, marked invalid,
// and focused on arrival, rather than as a banner the player has to match
// to a control by eye.
func TestComposerFieldErrorsAreAttachedToTheirField(t *testing.T) {
	form := GameRequestForm{MaxPlayers: "1000", StartPreset: "now"}
	message := ComposerMessage{Text: "Maximum players must be 0 for unlimited or a whole number from 2 to 999.", Field: "max_players"}

	var rendered bytes.Buffer
	if err := GameRequestDetailsFields(form, message).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render details fields: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`aria-describedby="max-players-error max-players-hint"`,
		`aria-invalid="true"`,
		`autofocus`,
		`<p id="max-players-error" class="field-error">Maximum players must be`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("max players error missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, `id="description-error"`) || strings.Contains(body, `id="start-error"`) {
		t.Errorf("a max players error leaked onto another field: %s", body)
	}
}

// With nothing rejected, the same fields carry their hint alone and no
// invalid state at all.
func TestComposerFieldsCarryNoErrorStateWhenValid(t *testing.T) {
	var rendered bytes.Buffer
	if err := GameRequestDetailsFields(GameRequestForm{StartPreset: "now"}, ComposerMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render details fields: %v", err)
	}
	body := rendered.String()
	if !strings.Contains(body, `aria-describedby="max-players-hint"`) {
		t.Errorf("max players lost its hint association: %s", body)
	}
	for _, unwanted := range []string{"aria-invalid", "autofocus", "field-error", `aria-describedby=""`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("valid composer renders %q: %s", unwanted, body)
		}
	}
}

// A message about the request as a whole -- a Game that left the catalog --
// has no field to sit beside and stays the page-level alert.
func TestComposerRequestLevelMessageStaysAPageAlert(t *testing.T) {
	var rendered bytes.Buffer
	if err := NewGameRequest(branding.Default(), components.NavigationState{}, game.DiscoveryPage{}, nil, false, "", GameRequestForm{}, ComposerMessage{Text: "This game is no longer available."}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render composer: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "This game is no longer available.") {
		t.Errorf("request-level message is not announced: %s", body)
	}
}

// The pick row itself is the select control, so there is no separate
// button to hunt for, and the cover is what makes a game recognisable in
// the list.
func TestGameRequestDiscoveryRowIsTheSelectControl(t *testing.T) {
	page := game.DiscoveryPage{Items: []game.DiscoveryItem{{
		LocalGameID:      "local-2",
		GamePresentation: game.GamePresentation{Name: "Age of Empires", CoverImageID: "cover-2"},
		Subscribed:       true,
		Materialized:     true,
	}}}

	var rendered bytes.Buffer
	if err := GameRequestDiscovery(page, "", nil, GameRequestForm{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render discovery: %v", err)
	}
	body := rendered.String()
	for _, want := range []string{
		`<a class="pick-card" href="/game-requests/new?game_id=local-2"`,
		`class="pick-cover"`,
		`/media/igdb/big/cover-2`,
		`Age of Empires`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pick row missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, ">Select<") {
		t.Errorf("pick row still renders a separate select button: %s", body)
	}
}

// Following nothing yet is not an empty catalog: the pre-search list says
// so and points at the search, while a search that matched nothing keeps
// its own message.
func TestGameRequestDiscoveryEmptyStates(t *testing.T) {
	available := game.ProviderStatus{CatalogAvailable: true}

	var followed bytes.Buffer
	if err := GameRequestDiscovery(game.DiscoveryPage{ProviderStatus: available}, "", nil, GameRequestForm{}).Render(context.Background(), &followed); err != nil {
		t.Fatalf("render pre-search discovery: %v", err)
	}
	if body := followed.String(); !strings.Contains(body, "You follow no games yet.") {
		t.Errorf("pre-search empty list lacks the follow explanation: %s", body)
	}

	var searched bytes.Buffer
	if err := GameRequestDiscovery(game.DiscoveryPage{ProviderStatus: available}, "warcraft", nil, GameRequestForm{}).Render(context.Background(), &searched); err != nil {
		t.Fatalf("render searched discovery: %v", err)
	}
	body := searched.String()
	if !strings.Contains(body, `No games found for &#34;warcraft&#34;`) {
		t.Errorf("empty search result lacks its own message: %s", body)
	}
	if strings.Contains(body, "You follow no games yet.") {
		t.Errorf("empty search result reused the followed-list message: %s", body)
	}
}

func TestGameRequestDiscoveryUsesAdminForCatalogAvailability(t *testing.T) {
	page := game.DiscoveryPage{ProviderStatus: game.ProviderStatus{State: game.ProviderDisabled}}
	viewer := &components.Viewer{Role: "admin"}
	var rendered bytes.Buffer
	if err := GameRequestDiscovery(page, "", viewer, GameRequestForm{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render discovery: %v", err)
	}
	if body := rendered.String(); !strings.Contains(body, `href="/admin/igdb"`) {
		t.Errorf("admin composer discovery lacks IGDB administration link: %s", body)
	}
}

func renderLifecycleDetail(t *testing.T, status gamerequest.Status, viewerID string, activity ...requestquery.Activity) string {
	t.Helper()
	request := requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: status, MemberCount: 1, Activity: activity,
		Members: []requestquery.Member{{ID: "host-1", GameNick: "Otter"}},
	}
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, viewerID, false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	return rendered.String()
}

func TestDetailHostActionsFollowTheLifecycle(t *testing.T) {
	open := renderLifecycleDetail(t, gamerequest.StatusOpen, "host-1")
	for _, want := range []string{"Start game", "Edit", "Cancel Game Request", "Visible to the Host only.", `action="/game-requests/request-1/start"`, `data-confirm="Start this game?`} {
		if !strings.Contains(open, want) {
			t.Errorf("open Host detail lacks %q", want)
		}
	}
	openActions := open[strings.Index(open, `action="/game-requests/request-1/start"`):]
	if !(strings.Index(openActions, "Start game") < strings.Index(openActions, `href="/game-requests/request-1?edit=1"`) &&
		strings.Index(openActions, `href="/game-requests/request-1?edit=1"`) < strings.Index(openActions, "Cancel Game Request") &&
		strings.Index(openActions, "Cancel Game Request") < strings.Index(openActions, "Visible to the Host only.")) {
		t.Errorf("open Host action order is wrong: %s", openActions)
	}
	editLink := open[strings.Index(open, `href="/game-requests/request-1?edit=1"`):]
	if strings.Contains(editLink[:strings.Index(editLink, ">Edit</a>")], "data-confirm") {
		t.Error("Edit unexpectedly asks for confirmation")
	}

	playing := renderLifecycleDetail(t, gamerequest.StatusPlaying, "host-1")
	if !strings.Contains(playing, "Close game") || !strings.Contains(playing, `action="/game-requests/request-1/close"`) {
		t.Error("playing Host detail lacks Close game")
	}
	for _, unwanted := range []string{"Start game", "/start", "Cancel Game Request", "/cancel"} {
		if strings.Contains(playing, unwanted) {
			t.Errorf("playing Host detail contains %q", unwanted)
		}
	}
	playingActions := playing[strings.Index(playing, `action="/game-requests/request-1/close"`):]
	if !(strings.Index(playingActions, "Close game") < strings.Index(playingActions, `href="/game-requests/request-1?edit=1"`) &&
		strings.Index(playingActions, `href="/game-requests/request-1?edit=1"`) < strings.Index(playingActions, "Visible to the Host only.")) {
		t.Errorf("playing Host action order is wrong: %s", playingActions)
	}
	if !strings.Contains(playingActions, `data-confirm="Close this game?`) {
		t.Error("Close game confirmation changed")
	}
}

func renderRow(t *testing.T, request requestquery.Request, viewerID string) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := GameRequestRow(request, viewerID, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render row: %v", err)
	}
	return rendered.String()
}

func renderDetail(t *testing.T, request requestquery.Request, viewerID string) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, viewerID, false, GameRequestDetailMessage{}).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	return rendered.String()
}

// Open and playing are both active: the viewer's membership action depends on
// membership and capacity, never on which of the two the request is in.
func TestActiveRequestsOfferJoinLeaveAndFullByMembershipAndCapacity(t *testing.T) {
	limit := int32(3)
	cases := []struct {
		name    string
		request requestquery.Request
		viewer  string
		want    []string
		unwant  []string
	}{
		{"joined viewer leaves", requestquery.Request{ViewerJoined: true, MemberCount: 2, MaxPlayers: &limit}, "user-2",
			[]string{"/game-requests/request-1/leave", "Leave"}, []string{"/join", "Join</button>"}},
		{"viewer with room joins", requestquery.Request{MemberCount: 1, MaxPlayers: &limit}, "user-2",
			[]string{"/game-requests/request-1/join", "Join</button>"}, []string{"/leave", "Leave</button>"}},
		{"viewer on a full request gets no action", requestquery.Request{MemberCount: 3, MaxPlayers: &limit}, "user-2",
			nil, []string{"/join", "/leave", "Join</button>", "Leave</button>"}},
		{"unlimited request is joinable", requestquery.Request{MemberCount: 12}, "user-2",
			[]string{"/game-requests/request-1/join"}, []string{"/leave"}},
		{"last finite place is called out", requestquery.Request{MemberCount: 2, MaxPlayers: &limit}, "user-2",
			[]string{"Last place", "/game-requests/request-1/join"}, []string{"/leave"}},
		{"Host never gets Join or Leave", requestquery.Request{ViewerJoined: true, MemberCount: 1, MaxPlayers: &limit}, "host-1",
			nil, []string{"/join", "/leave", "Join</button>", "Leave</button>"}},
	}
	for _, status := range []gamerequest.Status{gamerequest.StatusOpen, gamerequest.StatusPlaying} {
		for _, tc := range cases {
			request := tc.request
			request.ID, request.CreatorID, request.Status = "request-1", "host-1", status
			request.Game = game.GamePresentation{Name: "Warcraft III"}
			for surface, body := range map[string]string{
				"row":    renderRow(t, request, tc.viewer),
				"detail": renderDetail(t, request, tc.viewer),
			} {
				for _, want := range tc.want {
					if !strings.Contains(body, want) {
						t.Errorf("%s %s %s lacks %q", status, surface, tc.name, want)
					}
				}
				for _, unwanted := range tc.unwant {
					if strings.Contains(body, unwanted) {
						t.Errorf("%s %s %s contains %q", status, surface, tc.name, unwanted)
					}
				}
			}
		}
	}
}

func TestInactiveRequestsStayStatic(t *testing.T) {
	for status, label := range map[gamerequest.Status]string{gamerequest.StatusClosed: "Closed", gamerequest.StatusCancelled: "Cancelled"} {
		limit := int32(4)
		request := requestquery.Request{
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
			Status: status, MemberCount: 3, MaxPlayers: &limit,
		}
		row := renderRow(t, request, "user-2")
		if !strings.Contains(row, `<span class="action-static">`+label+`</span>`) {
			t.Errorf("%s row action is not static %q: %s", status, label, row)
		}
		for surface, body := range map[string]string{"row": row, "detail": renderDetail(t, request, "user-2")} {
			for _, unwanted := range []string{"/join", "/leave", "Last place"} {
				if strings.Contains(body, unwanted) {
					t.Errorf("%s %s contains %q", status, surface, unwanted)
				}
			}
		}
	}
}

func TestPlayingRowKeepsPlayingIdentity(t *testing.T) {
	limit := int32(4)
	body := renderRow(t, requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		CreatorGameNick: "Otter", Status: gamerequest.StatusPlaying, MaxPlayers: &limit, MemberCount: 2,
	}, "user-2")
	if !strings.Contains(body, "is playing") || strings.Contains(body, "is looking for") {
		t.Errorf("row connector wrong: %s", body)
	}
	if strings.Contains(body, "request-row-closed") || !strings.Contains(body, "request-row-playing") {
		t.Errorf("row class wrong: %s", body)
	}
	if strings.Contains(body, "count-muted") {
		t.Errorf("a playing request with free places has a muted counter: %s", body)
	}
}

func TestPlayingDetailKeepsPlayingIdentityAndLastPlace(t *testing.T) {
	limit := int32(3)
	body := renderDetail(t, requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: gamerequest.StatusPlaying, MaxPlayers: &limit, MemberCount: 2,
	}, "user-2")
	for _, want := range []string{"status-chip-playing", "detail-hero-playing", "Last place open", `<span class="status-chip status-chip-last">Last place</span>`, "count-last"} {
		if !strings.Contains(body, want) {
			t.Errorf("playing detail lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "count-muted") {
		t.Errorf("a playing request with a free place has a muted counter: %s", body)
	}
}

func TestUpdatedActivityLabelsChangedDimensions(t *testing.T) {
	four, six := int32(4), int32(6)
	before, after := time.Date(2027, 5, 1, 17, 0, 0, 0, time.UTC), time.Date(2027, 5, 1, 18, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		entry requestquery.Activity
		want  string
	}{
		"capacity": {
			entry: requestquery.Activity{Kind: gamerequest.ActivityUpdated, ActorGameNick: "Alex", PreviousMaxPlayers: &four, NewMaxPlayers: &six},
			want:  "Alex changed capacity from 4 to 6",
		},
		"description": {
			entry: requestquery.Activity{Kind: gamerequest.ActivityUpdated, ActorGameNick: "Alex", DescriptionChanged: true},
			want:  "Alex updated the description",
		},
		"multiple dimensions": {
			entry: requestquery.Activity{Kind: gamerequest.ActivityUpdated, ActorGameNick: "Alex", PreviousMaxPlayers: &four, NewMaxPlayers: &six, PreviousStartsAt: &before, NewStartsAt: &after, DescriptionChanged: true},
			want:  "Alex updated capacity, planned start, and description",
		},
	} {
		if got := activityLabel(tc.entry); got != tc.want {
			t.Errorf("%s: activityLabel = %q, want %q", name, got, tc.want)
		}
	}
}

func TestUpdatedActivityRendersPlannedStartTimesAsLocalizableInstants(t *testing.T) {
	before, after := time.Date(2027, 5, 1, 17, 0, 0, 0, time.UTC), time.Date(2027, 5, 1, 18, 0, 0, 0, time.UTC)
	four, six := int32(4), int32(6)
	for _, tc := range []struct {
		name  string
		entry requestquery.Activity
		label string
	}{
		{"start only", requestquery.Activity{Kind: gamerequest.ActivityUpdated, ActorGameNick: "Alex", PreviousStartsAt: &before, NewStartsAt: &after}, "Alex changed the planned start"},
		{"capacity and start", requestquery.Activity{Kind: gamerequest.ActivityUpdated, ActorGameNick: "Alex", PreviousMaxPlayers: &four, NewMaxPlayers: &six, PreviousStartsAt: &before, NewStartsAt: &after}, "Alex updated capacity and planned start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := capacityRequest(gamerequest.StatusOpen, nil)
			tc.entry.OccurredAt = time.Date(2027, 5, 1, 16, 0, 0, 0, time.UTC)
			request.Activity = []requestquery.Activity{tc.entry}
			body := renderCapacityDetail(t, request, "user-2", GameRequestDetailMessage{})
			rowStart := strings.Index(body, `<li class="activity-row">`)
			if rowStart < 0 {
				t.Fatalf("activity row missing: %s", body)
			}
			rowEnd := strings.Index(body[rowStart:], `</li>`)
			if rowEnd < 0 {
				t.Fatalf("activity row is not closed: %s", body[rowStart:])
			}
			row := body[rowStart : rowStart+rowEnd]
			contentStart := strings.Index(row, `<div class="activity-content`)
			if contentStart < 0 {
				t.Fatalf("activity content missing: %s", row)
			}
			contentEnd := strings.Index(row[contentStart:], `</div>`)
			if contentEnd < 0 {
				t.Fatalf("activity content is not closed: %s", row[contentStart:])
			}
			content := row[contentStart : contentStart+contentEnd]
			for _, want := range []string{tc.label, `datetime="2027-05-01T17:00:00Z"`, `datetime="2027-05-01T18:00:00Z"`, `data-local-time="absolute"`} {
				if !strings.Contains(content, want) {
					t.Errorf("activity content lacks %q: %s", want, content)
				}
			}
			detailStart := strings.Index(content, `<p class="activity-detail`)
			if detailStart < 0 {
				t.Fatalf("activity detail paragraph missing: %s", content)
			}
			detailEnd := strings.Index(content[detailStart:], `</p>`)
			if detailEnd < 0 {
				t.Fatalf("activity detail paragraph is not closed: %s", content[detailStart:])
			}
			detail := content[detailStart : detailStart+detailEnd]
			if strings.Count(detail, `data-local-time="absolute"`) != 2 ||
				!strings.Contains(row[:contentStart], `datetime="2027-05-01T16:00:00Z"`) ||
				strings.Contains(row[:contentStart], `data-local-time="absolute"`) ||
				strings.Contains(row[contentStart+contentEnd:], `data-local-time="absolute"`) {
				t.Errorf("activity times do not match intended hierarchy: %s", row)
			}
		})
	}
}

func TestCreateAndOpenEditShareFieldCopy(t *testing.T) {
	create := renderComposerFields(t, GameRequestForm{MaxPlayers: "4", StartPreset: "now"}, ComposerMessage{})
	request := capacityRequest(gamerequest.StatusOpen, nil)
	editor := renderCapacityDetail(t, request, "host-1", GameRequestDetailMessage{Editing: true, Form: GameRequestForm{MaxPlayers: "4", StartPreset: "custom", Description: "draft"}})
	editorStart := strings.Index(editor, `action="/game-requests/request-1/details"`)
	editor = editor[editorStart:]
	editor = editor[:strings.Index(editor, "</form>")]
	for name, body := range map[string]string{"create": create, "edit": editor} {
		if !strings.Contains(body, "Max players") || !strings.Contains(body, "0 for an open round, or 2 to 999 players.") || !strings.Contains(body, "Description") || !strings.Contains(body, "(optional)") {
			t.Errorf("%s field copy differs: %s", name, body)
		}
		if strings.Contains(body, "Maximum players") || strings.Contains(body, "0 means unlimited; finite limits start at 2.") {
			t.Errorf("%s retained old field copy: %s", name, body)
		}
	}
}

func TestOpenEditorOffersStartChoicesAndPrefillsStoredCustomStart(t *testing.T) {
	request := capacityRequest(gamerequest.StatusOpen, nil)
	request.StartsAt = time.Date(2027, 5, 1, 18, 30, 0, 0, time.UTC)
	body := renderCapacityDetail(t, request, "host-1", GameRequestDetailMessage{Editing: true, InitializeStart: true, Form: GameRequestForm{StartPreset: "custom", StartDate: "2027-05-01", StartTime: "18:30", Timezone: "UTC"}})
	editorStart := strings.Index(body, `action="/game-requests/request-1/details"`)
	editor := body[editorStart:]
	editor = editor[:strings.Index(editor, "</form>")]
	for _, value := range []string{`value="now"`, `value="15"`, `value="30"`, `value="60"`, `value="custom"`, `name="start_date" value="2027-05-01"`, `name="start_time" value="18:30"`, `name="timezone" value="UTC"`, `data-start-options`, `data-custom-start`} {
		if !strings.Contains(editor, value) {
			t.Errorf("open editor lacks %q: %s", value, editor)
		}
	}
	if !strings.Contains(editor, `value="custom" checked`) {
		t.Errorf("custom start not selected: %s", editor)
	}
}

func renderComposerFields(t *testing.T, form GameRequestForm, message ComposerMessage) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := GameRequestDetailsFields(form, message).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render composer fields: %v", err)
	}
	return rendered.String()
}

func TestHostEditActionAndEditorFollowOwnershipAndStatus(t *testing.T) {
	limit := int32(4)
	for name, tc := range map[string]struct {
		status gamerequest.Status
		viewer string
		show   bool
	}{
		"open Host":      {gamerequest.StatusOpen, "host-1", true},
		"playing Host":   {gamerequest.StatusPlaying, "host-1", true},
		"participant":    {gamerequest.StatusOpen, "user-2", false},
		"closed Host":    {gamerequest.StatusClosed, "host-1", false},
		"cancelled Host": {gamerequest.StatusCancelled, "host-1", false},
	} {
		body := renderCapacityDetail(t, capacityRequest(tc.status, &limit), tc.viewer, GameRequestDetailMessage{})
		if got := strings.Contains(body, `href="/game-requests/request-1?edit=1"`); got != tc.show {
			t.Errorf("%s edit action = %v, want %v", name, got, tc.show)
		}
		if strings.Contains(body, `action="/game-requests/request-1/details"`) {
			t.Errorf("%s sees an editor without choosing Edit", name)
		}
	}
}

func TestRequestEditorKeepsCapacityUXAndReadOnlyGame(t *testing.T) {
	limit := int32(4)
	request := capacityRequest(gamerequest.StatusOpen, &limit)
	message := GameRequestDetailMessage{
		Editing: true, InitializeStart: true,
		Form: GameRequestForm{MaxPlayers: "1", StartDate: "2027-05-01", StartTime: "18:30", Timezone: "UTC", Description: "keep raw"},
		Text: "Maximum players must be 0 for unlimited or a whole number from 2 to 999.", Field: "max_players", Value: "1",
	}
	body := renderCapacityDetail(t, request, "host-1", message)
	editorStart := strings.Index(body, `action="/game-requests/request-1/details"`)
	if editorStart < 0 {
		t.Fatalf("Host editor missing: %s", body)
	}
	editor := body[editorStart:]
	editor = editor[:strings.Index(editor, "</form>")]
	for _, want := range []string{
		`name="max_players"`, `min="0"`, `max="999"`, `step="1"`, `type="number"`,
		`value="1"`, "0 for an open round, or 2 to 999 players.", `id="max-players-error"`,
		`name="start_date"`, `name="start_time"`, `name="timezone"`, `data-start-options`, `data-local-start`,
		`name="description"`, "keep raw", `data-description-count`, ">Save</button>", ">Cancel</a>",
	} {
		if !strings.Contains(editor, want) {
			t.Errorf("editor lacks %q: %s", want, editor)
		}
	}
	for _, forbidden := range []string{`name="game_id"`, `name="provider"`, `name="external_id"`, `data-confirm`} {
		if strings.Contains(editor, forbidden) {
			t.Errorf("editor contains %q: %s", forbidden, editor)
		}
	}
}

func TestPlayingEditorCannotEditPlannedStart(t *testing.T) {
	limit := int32(4)
	request := capacityRequest(gamerequest.StatusPlaying, &limit)
	request.StartsAt = time.Date(2027, 5, 1, 18, 30, 0, 0, time.UTC)
	body := renderCapacityDetail(t, request, "host-1", GameRequestDetailMessage{Editing: true})
	start := strings.Index(body, `action="/game-requests/request-1/details"`)
	if start < 0 {
		t.Fatalf("Host editor missing: %s", body)
	}
	editor := body[start:]
	editor = editor[:strings.Index(editor, "</form>")]
	if strings.Contains(editor, `name="start_date"`) || strings.Contains(editor, `name="start_time"`) ||
		strings.Contains(editor, `name="timezone"`) || strings.Contains(editor, `name="start_preset"`) ||
		!strings.Contains(editor, "Start") || !strings.Contains(editor, `datetime="2027-05-01T18:30:00Z"`) {
		t.Errorf("playing start is not read-only: %s", editor)
	}
}

func TestFullActiveRowSaysFullInsteadOfJoin(t *testing.T) {
	limit := int32(3)
	for _, status := range []gamerequest.Status{gamerequest.StatusOpen, gamerequest.StatusPlaying} {
		body := renderRow(t, requestquery.Request{
			ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
			Status: status, MaxPlayers: &limit, MemberCount: 3,
		}, "user-2")
		if !strings.Contains(body, `<span class="action-static">Full</span>`) {
			t.Errorf("%s full row lacks the Full action: %s", status, body)
		}
	}
}

func renderCapacityDetail(t *testing.T, request requestquery.Request, viewerID string, message GameRequestDetailMessage) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := GameRequestDetailContent(request, viewerID, false, message).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render detail: %v", err)
	}
	return rendered.String()
}

func capacityRequest(status gamerequest.Status, limit *int32) requestquery.Request {
	return requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"}, CreatorID: "host-1",
		Status: status, MemberCount: 2, MaxPlayers: limit,
		Members: []requestquery.Member{{ID: "host-1", GameNick: "Otter"}, {ID: "user-2", GameNick: "Alex"}},
	}
}
