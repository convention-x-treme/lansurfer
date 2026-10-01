package components

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
		if got := GameAbbreviation(input); got != want {
			t.Errorf("GameAbbreviation(%q) = %q, want %q", input, got, want)
		}
	}
}

// A round that has already started must read as "now" on both the request list
// and the sidebar, because one shared helper states it for both.
func TestStartLabelStatesUrgencyThenCountsDown(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		startsAt time.Time
		want     string
	}{
		{name: "already started", startsAt: now.Add(-time.Minute), want: "now"},
		{name: "inside the now window", startsAt: now.Add(90 * time.Second), want: "now"},
		{name: "minutes away", startsAt: now.Add(15 * time.Minute), want: "in 15 min"},
		{name: "hours away", startsAt: now.Add(3 * time.Hour), want: "in 3 h"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StartLabel(test.startsAt); got != test.want {
				t.Errorf("StartLabel = %q, want %q", got, test.want)
			}
		})
	}
}

func TestViewedAgoLabel(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		viewedAt time.Time
		want     string
	}{
		{name: "seconds", viewedAt: now.Add(-20 * time.Second), want: "now"},
		{name: "minutes", viewedAt: now.Add(-6 * time.Minute), want: "6m"},
		{name: "hours", viewedAt: now.Add(-2 * time.Hour), want: "2h"},
		{name: "days", viewedAt: now.Add(-50 * time.Hour), want: "2d"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ViewedAgoLabel(test.viewedAt); got != test.want {
				t.Errorf("ViewedAgoLabel = %q, want %q", got, test.want)
			}
		})
	}
}

// The sidebar is the same on every screen, so its counts have to render
// wherever the viewer is, not only on the screen that happens to own the
// number. The badge counts active requests (`open` or `playing`), and only the
// current section's count is the warm one.
func TestNavigationRendersEveryCountAndMarksOnlyTheCurrentSection(t *testing.T) {
	state := NavigationState{
		Viewer:         &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"},
		Section:        "games",
		ActiveRequests: 14,
		Games:          62,
		FollowedGames:  7,
	}
	body := renderNavigation(t, state)

	for _, want := range []string{">14<", ">62<", ">7<"} {
		if !strings.Contains(body, want) {
			t.Errorf("navigation is missing count %s: %s", want, body)
		}
	}
	if strings.Count(body, "count-hot") != 1 {
		t.Errorf("want exactly one warm count on the current section, got %d: %s", strings.Count(body, "count-hot"), body)
	}
	if !strings.Contains(body, "app-navigation-link-nested") {
		t.Errorf("My Games should render as a sub-item of Games: %s", body)
	}
}

// A count of zero is absent rather than rendered: "Games 0" states a fact the
// empty screen already states better.
// The two thresholds every start time in the product colours by. The
// boundaries themselves belong to the warmer tier, and a round that already
// started stays at the loudest one.
func TestStartUrgencyCrossesAtFifteenAndFiveMinutes(t *testing.T) {
	for _, test := range []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{"an hour out is quiet", time.Hour, UrgencyNone},
		{"just over a quarter hour is quiet", 15*time.Minute + time.Second, UrgencyNone},
		{"a quarter hour warms", 15 * time.Minute, UrgencySoon},
		{"just over five minutes stays warm", 5*time.Minute + time.Second, UrgencySoon},
		{"five minutes is starting", 5 * time.Minute, UrgencyStarting},
		{"already started is starting", -time.Hour, UrgencyStarting},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := StartUrgency(time.Now().Add(test.remaining)); got != test.want {
				t.Errorf("StartUrgency(%s) = %q, want %q", test.remaining, got, test.want)
			}
		})
	}
}

func TestNavigationOmitsZeroCounts(t *testing.T) {
	body := renderNavigation(t, NavigationState{
		Viewer:  &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"},
		Section: "games",
	})
	if strings.Contains(body, ">0<") {
		t.Errorf("zero counts should not render: %s", body)
	}
}

func TestUpcomingGamesMarksAStartedRoundUrgent(t *testing.T) {
	state := NavigationState{
		Viewer: &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"},
		Upcoming: []requestquery.Request{
			{ID: "request-1", Game: game.GamePresentation{Name: "Age of Empires II"}, StartsAt: time.Now().Add(-time.Minute)},
			{ID: "request-2", Game: game.GamePresentation{Name: "Trackmania"}, StartsAt: time.Now().Add(15 * time.Minute)},
		},
	}
	body := renderNavigation(t, state)

	if strings.Count(body, "nav-upcoming-item-now") != 1 {
		t.Errorf("only the started round should be urgent: %s", body)
	}
	if !strings.Contains(body, "/game-requests/request-1") || !strings.Contains(body, "in 15 min") {
		t.Errorf("upcoming rounds should link and state their start: %s", body)
	}
}

// A relative label alone hides the fact a player needs when planning, so every
// time in the product states its absolute local time on hover.
func TestUpcomingGamesStateTheAbsoluteStartOnHover(t *testing.T) {
	startsAt := time.Now().Add(3 * time.Hour)
	body := renderNavigation(t, NavigationState{
		Viewer:   &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"},
		Upcoming: []requestquery.Request{{ID: "request-1", Game: game.GamePresentation{Name: "Trackmania"}, StartsAt: startsAt}},
	})

	if want := `title="` + startsAt.Local().Format("2006-01-02 15:04") + `"`; !strings.Contains(body, want) {
		t.Errorf("want the absolute start %s in the title: %s", want, body)
	}
}

// Reading history is history: a request that closed after the viewer read it
// stays listed and says so, rather than vanishing from their own trail.
func TestRecentlyViewedKeepsClosedRequestsAndStatesTheirStatus(t *testing.T) {
	state := NavigationState{
		Viewer: &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"},
		RecentlyViewed: []requestquery.RecentRequest{
			{ID: "request-1", GameName: "Warcraft III", Status: gamerequest.StatusOpen, ViewedAt: time.Now().Add(-2 * time.Minute)},
			{ID: "request-2", GameName: "Counter-Strike 2", Status: gamerequest.StatusClosed, ViewedAt: time.Now().Add(-6 * time.Minute)},
		},
	}
	body := renderNavigation(t, state)

	for _, want := range []string{"Warcraft III", "2m", "Counter-Strike 2", "closed", "6m"} {
		if !strings.Contains(body, want) {
			t.Errorf("recently viewed is missing %q: %s", want, body)
		}
	}
}

// An empty list renders nothing at all: a heading over no rows is furniture,
// not information.
func TestSidebarSectionsAreAbsentWhenEmpty(t *testing.T) {
	body := renderNavigation(t, NavigationState{Viewer: &Viewer{ID: "user-1", GameNick: "Ada", Role: "player"}})
	for _, unwanted := range []string{"Your next games", "Recently viewed"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("empty section %q should not render: %s", unwanted, body)
		}
	}
}

func renderNavigation(t *testing.T, state NavigationState) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := DesktopNavigation(branding.Default(), state).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render DesktopNavigation: %v", err)
	}
	return rendered.String()
}
