package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
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
		GameName:     "Warcraft III",
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
		GameName:    "Warcraft III",
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
			ID: "request-1", GameName: "Warcraft III", MemberCount: 1, Status: gamerequest.StatusOpen,
		},
		"leave": {
			ID: "request-1", GameName: "Warcraft III", MemberCount: 2, ViewerJoined: true, Status: gamerequest.StatusOpen,
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
		ID: "request-1", GameName: "Warcraft III", CreatorID: "host-1",
		Status: gamerequest.StatusOpen, MemberCount: 2, CreatedAt: now.Add(-10 * time.Minute),
		Members: []requestquery.Member{
			{ID: "host-1", DisplayName: "Otter", JoinedAt: now.Add(-10 * time.Minute)},
			{ID: "user-2", DisplayName: "Alex", JoinedAt: now.Add(-2 * time.Minute)},
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
	if strings.Index(body, "Alex joined") > strings.Index(body, "Otter created the request") {
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
