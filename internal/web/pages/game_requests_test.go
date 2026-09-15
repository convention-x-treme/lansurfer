package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
