package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
)

// A fresh usable provider instance (or an entirely local-only catalog) must
// never fall back to a bare-count/zero-games state; the search field itself
// is the primary experience (spec: "3.1 Empty search state").
func TestGameCatalogEmptySearchIsDiscoveryFirst(t *testing.T) {
	page := game.DiscoveryPage{
		ProviderStatus: game.ProviderStatus{State: game.ProviderCurrent, CatalogAvailable: true},
	}
	var rendered bytes.Buffer
	if err := GameCatalogResults(page, "", "", nil).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCatalogResults: %v", err)
	}
	markup := rendered.String()
	if !strings.Contains(markup, "Find your game") {
		t.Errorf("empty search does not render the discovery-first heading: %s", markup)
	}
	for _, unwanted := range []string{"0 games", "No games found"} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("empty search unexpectedly renders %q: %s", unwanted, markup)
		}
	}
}

// A one-character search is service-level SearchTooShort: Browse returns the
// full unfiltered local list alongside it (a known, intentional quirk), so
// the prompt above that list is what keeps the state legible.
func TestGameCatalogOneCharacterPromptsForTwoCharacters(t *testing.T) {
	page := game.DiscoveryPage{
		Items: []game.DiscoveryItem{{
			LocalGameID:      "game-1",
			GamePresentation: game.GamePresentation{Name: "Warcraft III", Slug: "warcraft-3"},
			Materialized:     true,
		}},
		SearchTooShort: true,
		ProviderStatus: game.ProviderStatus{State: game.ProviderCurrent, CatalogAvailable: true},
	}
	var rendered bytes.Buffer
	if err := GameCatalogResults(page, "c", "", nil).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCatalogResults: %v", err)
	}
	if !strings.Contains(rendered.String(), "Type at least 2 characters to search.") {
		t.Errorf("one-character search does not prompt for two characters: %s", rendered.String())
	}
}

// No local Games and a provider that has never completed a baseline (spec:
// "8.3 Initial provider unavailable and no local Games") must say so
// explicitly rather than silently rendering an empty catalog.
func TestGameCatalogNoUsableCatalogShowsAvailabilityState(t *testing.T) {
	tests := map[string]game.ProviderStatus{
		"disabled": {State: game.ProviderDisabled, CatalogAvailable: false},
		"failed":   {State: game.ProviderFailed, CatalogAvailable: false},
	}
	for name, status := range tests {
		page := game.DiscoveryPage{ProviderStatus: status}
		var rendered bytes.Buffer
		if err := GameCatalogResults(page, "", "", nil).Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameCatalogResults for %s: %v", name, err)
		}
		if !strings.Contains(rendered.String(), "No games are available yet.") {
			t.Errorf("%s provider status does not show the availability state: %s", name, rendered.String())
		}
	}
}

// Section 8.2: an initial synchronization in progress (no baseline yet) gets
// its own, less alarming copy than a genuinely unusable catalog.
func TestGameCatalogInitialSyncShowsGettingReady(t *testing.T) {
	page := game.DiscoveryPage{ProviderStatus: game.ProviderStatus{State: game.ProviderSyncing, CatalogAvailable: false}}
	var rendered bytes.Buffer
	if err := GameCatalogResults(page, "", "", nil).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCatalogResults: %v", err)
	}
	if !strings.Contains(rendered.String(), "Game discovery is getting ready.") {
		t.Errorf("initial sync does not render the getting-ready state: %s", rendered.String())
	}
}

// Section 3.5: discovery is bounded to 50 rows; HasMore signals truncation
// explicitly rather than the player being told a false total.
func TestGameCatalogBroadSearchShowsRefineHint(t *testing.T) {
	page := game.DiscoveryPage{
		Items: []game.DiscoveryItem{{
			LocalGameID:      "game-1",
			GamePresentation: game.GamePresentation{Name: "Warcraft III", Slug: "warcraft-3"},
			Materialized:     true,
		}},
		HasMore: true,
	}
	var rendered bytes.Buffer
	if err := GameCatalogResults(page, "warcraft", "", nil).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCatalogResults: %v", err)
	}
	markup := rendered.String()
	if !strings.Contains(markup, "Too many matches") || !strings.Contains(markup, "refine your search") {
		t.Errorf("broad search does not render the refine hint: %s", markup)
	}
}

// Section 8.3: technical provider state remains an administration concern;
// only an administrator gets a route into the relevant Game/IGDB
// administration surface from the no-catalog state.
func TestGameCatalogNoUsableCatalogShowsAdministrationLinkOnlyForAdmins(t *testing.T) {
	page := game.DiscoveryPage{ProviderStatus: game.ProviderStatus{State: game.ProviderFailed, CatalogAvailable: false}}

	var adminRendered bytes.Buffer
	if err := GameCatalogResults(page, "", "", &components.Viewer{Role: "admin"}).Render(context.Background(), &adminRendered); err != nil {
		t.Fatalf("render GameCatalogResults for admin: %v", err)
	}
	if !strings.Contains(adminRendered.String(), `href="/admin/igdb"`) {
		t.Errorf("admin viewer does not get an administration link: %s", adminRendered.String())
	}

	var playerRendered bytes.Buffer
	if err := GameCatalogResults(page, "", "", &components.Viewer{Role: "user"}).Render(context.Background(), &playerRendered); err != nil {
		t.Fatalf("render GameCatalogResults for player: %v", err)
	}
	if strings.Contains(playerRendered.String(), `href="/admin/igdb"`) {
		t.Errorf("non-admin viewer unexpectedly gets an administration link: %s", playerRendered.String())
	}
}

// Materialized and unmaterialized discovery results share one player-facing
// card (spec: "4.1 One player-facing Game card"): same presentation fields
// render the same cover/name/metadata, and neither exposes materialization
// origin.
func TestGameCardRendersUnifiedPresentationForMaterializedAndProviderItems(t *testing.T) {
	releaseDate := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	presentation := game.GamePresentation{
		Name:             "Warcraft III: Reign of Chaos",
		GameType:         "Main Game",
		PlatformNames:    []string{"PC (Microsoft Windows)"},
		FirstReleaseDate: &releaseDate,
		CoverImageID:     "co1abc",
	}
	items := map[string]game.DiscoveryItem{
		"materialized": {
			LocalGameID: "game-1", GamePresentation: presentation, Materialized: true,
		},
		"unmaterialized": {
			Reference: game.ProviderReference{Provider: "igdb", ExternalID: "42"}, GamePresentation: presentation, Materialized: false,
		},
	}
	for name, item := range items {
		var rendered bytes.Buffer
		if err := GameCard(item).Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameCard for %s: %v", name, err)
		}
		markup := rendered.String()
		for _, want := range []string{
			"https://images.igdb.com/igdb/image/upload/t_cover_small/co1abc.jpg",
			"Warcraft III: Reign of Chaos",
			"Main Game",
			"PC (Microsoft Windows)",
			"2002",
			"Find Players",
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s card does not contain %q: %s", name, want, markup)
			}
		}
		if !strings.Contains(markup, "Follow Game") && !strings.Contains(markup, "Following") {
			t.Errorf("%s card does not offer a follow control: %s", name, markup)
		}
		// Materialization origin, sync-generation terminology, and Local/IGDB
		// origin badges are never player-visible (spec: "4.1"). The hidden
		// provider follow action legitimately contains the lowercase
		// substring "igdb" in its URL, so this only rejects the capitalized
		// player-facing label.
		for _, unwanted := range []string{"Not yet in LANsurfer catalog", "materialized", "Local", "IGDB"} {
			if strings.Contains(markup, unwanted) {
				t.Errorf("%s card unexpectedly contains %q: %s", name, unwanted, markup)
			}
		}
	}
}

// A manually-created local Game with no IGDB enrichment has no cover to
// show, and must still render the abbreviation tile rather than a broken
// image or blank space (spec: "4.2 Same-name Games").
func TestGameCardRendersAbbreviationFallbackForLocalGameWithoutCover(t *testing.T) {
	item := game.DiscoveryItem{
		LocalGameID:      "game-1",
		GamePresentation: game.GamePresentation{Name: "Trackmania", Slug: "trackmania"},
		Materialized:     true,
	}
	var rendered bytes.Buffer
	if err := GameCard(item).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCard: %v", err)
	}
	markup := rendered.String()
	if !strings.Contains(markup, `aria-hidden="true">TRA<`) {
		t.Errorf("no-cover local game does not render the abbreviation fallback: %s", markup)
	}
	if strings.Contains(markup, "<img") {
		t.Errorf("no-cover local game unexpectedly renders a cover image: %s", markup)
	}
}

// Typing filters the catalog without pressing the button, once the player
// pauses rather than once per keystroke; the button stays as the
// no-scripting submit path (spec: "3.2 Live search").
func TestGameSearchFormAppliesWhileTyping(t *testing.T) {
	var rendered bytes.Buffer
	if err := GameSearchForm("").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameSearchForm: %v", err)
	}
	markup := rendered.String()
	for _, want := range []string{
		"input changed delay:300ms",
		"search from:(#game-search)",
		`hx-sync="this:replace"`,
		`hx-replace-url="true"`,
		`type="submit"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("search form is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "hx-push-url") {
		t.Error("live search must replace the URL, not push a history entry per keystroke")
	}
}

// Following is idempotent in the database, but an unguarded control still lets
// rapid clicks stack overlapping swaps against the same target.
func TestGameFollowControlGuardsAgainstDoubleSubmission(t *testing.T) {
	tests := map[string]struct {
		subscribed bool
		myGames    bool
	}{
		"follow":            {subscribed: false},
		"unfollow":          {subscribed: true},
		"unfollow my games": {subscribed: true, myGames: true},
	}
	for name, test := range tests {
		var rendered bytes.Buffer
		if err := GameFollowControl("game-1", test.subscribed, test.myGames).Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameFollowControl for %s: %v", name, err)
		}
		markup := rendered.String()
		for _, guard := range []string{`hx-sync="this:drop"`, `hx-disabled-elt="find button"`} {
			if !strings.Contains(markup, guard) {
				t.Errorf("%s control is missing %s: %s", name, guard, markup)
			}
		}
	}
}
