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
