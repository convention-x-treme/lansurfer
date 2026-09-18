package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
