package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
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
	if err := GameCatalogResults(branding.Brand{}, page, "", false, "", nil).Render(context.Background(), &rendered); err != nil {
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
	if err := GameCatalogResults(branding.Brand{}, page, "c", false, "", nil).Render(context.Background(), &rendered); err != nil {
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
		if err := GameCatalogResults(branding.Brand{}, page, "", false, "", nil).Render(context.Background(), &rendered); err != nil {
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
	if err := GameCatalogResults(branding.Brand{}, page, "", false, "", nil).Render(context.Background(), &rendered); err != nil {
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
	if err := GameCatalogResults(branding.Brand{}, page, "warcraft", false, "", nil).Render(context.Background(), &rendered); err != nil {
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
	if err := GameCatalogResults(branding.Brand{}, page, "", false, "", &components.Viewer{Role: "admin"}).Render(context.Background(), &adminRendered); err != nil {
		t.Fatalf("render GameCatalogResults for admin: %v", err)
	}
	if !strings.Contains(adminRendered.String(), `href="/admin/igdb"`) {
		t.Errorf("admin viewer does not get an administration link: %s", adminRendered.String())
	}

	var playerRendered bytes.Buffer
	if err := GameCatalogResults(branding.Brand{}, page, "", false, "", &components.Viewer{Role: "user"}).Render(context.Background(), &playerRendered); err != nil {
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
		Summary:          "Azeroth braces for war.",
		GenreNames:       []string{"Real Time Strategy (RTS)", "Strategy"},
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
		if err := GameCard(item, false).Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameCard for %s: %v", name, err)
		}
		markup := rendered.String()
		if strings.Contains(markup, "images.igdb.com") {
			t.Errorf("%s card references images.igdb.com: %s", name, markup)
		}
		for _, want := range []string{
			"/media/igdb/big/co1abc",
			"Warcraft III: Reign of Chaos",
			"2002 · Azeroth braces for war.",
			"game-card-genre",
			">RTS<",
			"Find Players",
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s card does not contain %q: %s", name, want, markup)
			}
		}
		// The card spends its secondary line on the summary, so game type
		// and platforms no longer appear there, and only the first genre
		// earns the cover badge.
		for _, unwanted := range []string{"Main Game", "PC (Microsoft Windows)", ">Strategy<", "Real Time Strategy (RTS)"} {
			if strings.Contains(markup, unwanted) {
				t.Errorf("%s card unexpectedly still contains %q: %s", name, unwanted, markup)
			}
		}
		if !strings.Contains(markup, `aria-label="Follow Warcraft III`) && !strings.Contains(markup, "Following") {
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
	if err := GameCard(item, false).Render(context.Background(), &rendered); err != nil {
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

// The card's secondary line degrades one field at a time: either half alone
// still reads, and a Game with neither shows nothing (never the slug).
func TestGameCardMetaCombinesYearAndSummary(t *testing.T) {
	releaseDate := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	long := strings.Repeat("a", gameCardSummaryLimit+10)
	for name, testCase := range map[string]struct {
		presentation game.GamePresentation
		want         string
	}{
		"year and summary": {
			presentation: game.GamePresentation{FirstReleaseDate: &releaseDate, Summary: "Azeroth braces for war."},
			want:         "2002 · Azeroth braces for war.",
		},
		"year only":    {presentation: game.GamePresentation{FirstReleaseDate: &releaseDate}, want: "2002"},
		"summary only": {presentation: game.GamePresentation{Summary: "Azeroth braces for war."}, want: "Azeroth braces for war."},
		"neither shows nothing": {
			presentation: game.GamePresentation{Slug: "warcraft-3", GameType: "Main Game"},
			want:         "",
		},
		"collapses provider whitespace": {
			presentation: game.GamePresentation{Summary: "Azeroth\n\nbraces  for war."},
			want:         "Azeroth braces for war.",
		},
		"over-long summary is cut with an ellipsis": {
			presentation: game.GamePresentation{Summary: long},
			want:         strings.Repeat("a", gameCardSummaryLimit) + "…",
		},
	} {
		if got := gameCardMeta(testCase.presentation); got != testCase.want {
			t.Errorf("%s: gameCardMeta() = %q, want %q", name, got, testCase.want)
		}
	}
}

// The cover badge is narrow, so IGDB's own trailing acronym is preferred
// over its long form wherever the provider supplies one. The genre name
// IGDB stores is never rewritten; only this label is shortened.
func TestShortGenrePrefersProviderAcronym(t *testing.T) {
	for name, want := range map[string]string{
		"Real Time Strategy (RTS)":   "RTS",
		"Role-playing (RPG)":         "RPG",
		"Turn-based strategy (TBS)":  "TBS",
		"Hack and slash/Beat 'em up": "Hack & Slash",
		"Strategy":                   "Strategy",
		"Point-and-click":            "Point-and-click",
		"Card & Board Game":          "Card & Board Game",
		// Nothing before the parenthesis means there is no long form to
		// drop, so the name survives whole rather than becoming empty.
		"(RTS)":     "(RTS)",
		"Puzzle ()": "Puzzle ()",
		"":          "",
	} {
		if got := shortGenre(name); got != want {
			t.Errorf("shortGenre(%q) = %q, want %q", name, got, want)
		}
	}
}

// A Game with no IGDB genres has no badge to show, and must render the
// cover without an empty chip floating on it.
func TestGameCardOmitsGenreBadgeWithoutGenres(t *testing.T) {
	item := game.DiscoveryItem{
		LocalGameID:      "game-1",
		GamePresentation: game.GamePresentation{Name: "Trackmania", Slug: "trackmania"},
		Materialized:     true,
	}
	var rendered bytes.Buffer
	if err := GameCard(item, false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCard: %v", err)
	}
	if strings.Contains(rendered.String(), "game-card-genre") {
		t.Errorf("genre-less card unexpectedly renders a genre badge: %s", rendered.String())
	}
}

// Typing filters the catalog without pressing the button, once the player
// pauses rather than once per keystroke; the button stays as the
// no-scripting submit path (spec: "3.2 Live search").
func TestGameSearchFormAppliesWhileTyping(t *testing.T) {
	var rendered bytes.Buffer
	if err := GameSearchForm("", false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameSearchForm: %v", err)
	}
	markup := rendered.String()
	for _, want := range []string{
		"input changed delay:300ms",
		"search from:(#game-search)",
		`hx-sync="this:replace"`,
		`hx-replace-url="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("search form is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "hx-push-url") {
		t.Error("live search must replace the URL, not push a history entry per keystroke")
	}
	// The filters apply themselves, so neither a Search nor a Clear button is
	// rendered for a player who has scripting. The submit control exists only
	// inside noscript, where it is the one thing that can apply the checkbox.
	if submit := strings.Index(markup, `type="submit"`); submit != -1 {
		if noscript := strings.Index(markup, "<noscript>"); noscript == -1 || submit < noscript {
			t.Errorf("a submit button is rendered outside noscript: %s", markup)
		}
	}
	if strings.Contains(markup, ">Clear<") {
		t.Errorf("search form still renders a Clear button: %s", markup)
	}
}

// Following is idempotent in the database, but an unguarded control still lets
// rapid clicks stack overlapping swaps against the same target.
func TestGameFollowControlGuardsAgainstDoubleSubmission(t *testing.T) {
	local := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}
	provider := game.DiscoveryItem{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "42"}, GamePresentation: game.GamePresentation{Name: "Warcraft III"}}
	followed := local
	followed.Subscribed = true
	tests := map[string]game.DiscoveryItem{
		"follow local":    local,
		"follow provider": provider,
		"unfollow":        followed,
	}
	for name, item := range tests {
		var rendered bytes.Buffer
		if err := GameCardFollowControl(item, false).Render(context.Background(), &rendered); err != nil {
			t.Fatalf("render GameCardFollowControl for %s: %v", name, err)
		}
		markup := rendered.String()
		for _, guard := range []string{`hx-sync="this:drop"`, `hx-disabled-elt="find button"`} {
			if !strings.Contains(markup, guard) {
				t.Errorf("%s control is missing %s: %s", name, guard, markup)
			}
		}
		// Following changes the cover tag and the card border as well as the
		// button, so the whole card is the only fragment that stays correct
		// after the swap.
		if !strings.Contains(markup, `hx-target="closest article"`) {
			t.Errorf("%s control does not swap the whole card: %s", name, markup)
		}
	}
}

// "Followed only" is a filter on the one Games screen, not a second page: it
// rides in the same live-search form and survives a no-JavaScript follow via
// the hidden field, so the redirect lands back on the filtered list.
func TestGameSearchFormCarriesFollowedOnlyFilter(t *testing.T) {
	var rendered bytes.Buffer
	if err := GameSearchForm("", true).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameSearchForm: %v", err)
	}
	markup := rendered.String()
	for _, want := range []string{`name="followed"`, `value="1"`, "checked", "change from:(.filter-toggle input)"} {
		if !strings.Contains(markup, want) {
			t.Errorf("followed filter is missing %q: %s", want, markup)
		}
	}

	item := game.DiscoveryItem{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Materialized: true}
	var card bytes.Buffer
	if err := GameCard(item, true).Render(context.Background(), &card); err != nil {
		t.Fatalf("render GameCard: %v", err)
	}
	if !strings.Contains(card.String(), `<input type="hidden" name="followed" value="1">`) {
		t.Errorf("follow control does not carry the filter through a no-JavaScript submit: %s", card.String())
	}
}

// The followed state is named in words on the cover tag rather than by the
// card border alone, and the button says only what pressing it does.
func TestGameCardNamesTheFollowedState(t *testing.T) {
	item := game.DiscoveryItem{
		LocalGameID:      "game-1",
		GamePresentation: game.GamePresentation{Name: "Warcraft III", Slug: "warcraft-3"},
		Subscribed:       true,
		Materialized:     true,
	}
	var rendered bytes.Buffer
	if err := GameCard(item, false).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render GameCard: %v", err)
	}
	markup := rendered.String()
	for _, want := range []string{"game-card-tag", "Following", "game-card-followed", "Unfollow"} {
		if !strings.Contains(markup, want) {
			t.Errorf("followed card is missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "btn-error") {
		t.Errorf("unfollow must not read as a destructive action: %s", markup)
	}
}

// The merged screen states what it is showing in the terms the filter uses,
// so a player never has to infer whether a short list is the whole catalog.
func TestGameCatalogCountLabelStatesTheCurrentView(t *testing.T) {
	items := []game.DiscoveryItem{
		{LocalGameID: "game-1", GamePresentation: game.GamePresentation{Name: "Warcraft III"}, Subscribed: true},
		{LocalGameID: "game-2", GamePresentation: game.GamePresentation{Name: "Trackmania"}},
	}
	if got, want := catalogCountLabel(items, false), "2 games · 1 followed"; got != want {
		t.Errorf("catalogCountLabel(unfiltered) = %q, want %q", got, want)
	}
	if got, want := catalogCountLabel(items[:1], true), "1 game followed"; got != want {
		t.Errorf("catalogCountLabel(followed) = %q, want %q", got, want)
	}
}

// Following nothing yet is a different situation from a search that missed,
// and the way out of each differs, so the two never share copy.
func TestGameCatalogEmptyDistinguishesFollowedFromSearchMiss(t *testing.T) {
	var followed bytes.Buffer
	if err := GameCatalogEmpty(branding.Brand{}, "", true).Render(context.Background(), &followed); err != nil {
		t.Fatalf("render GameCatalogEmpty: %v", err)
	}
	if !strings.Contains(followed.String(), "You follow nothing yet.") {
		t.Errorf("followed-only empty state does not name the situation: %s", followed.String())
	}

	var missed bytes.Buffer
	if err := GameCatalogEmpty(branding.Brand{}, "zzz", false).Render(context.Background(), &missed); err != nil {
		t.Fatalf("render GameCatalogEmpty: %v", err)
	}
	if !strings.Contains(missed.String(), "No game matches that.") {
		t.Errorf("search-miss empty state does not name the situation: %s", missed.String())
	}
}
