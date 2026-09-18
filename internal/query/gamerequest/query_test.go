package gamerequest

import (
	"strings"
	"testing"
)

// The browser's maxlength counts characters and the game catalog counts
// characters, so counting bytes here rejected a search the player was allowed
// to type: 40 three-byte characters are well inside the limit but over it in
// bytes.
func TestSearchLimitCountsCharactersNotBytes(t *testing.T) {
	multibyte := strings.Repeat("ä", maxSearchLength)
	if len(multibyte) <= maxSearchLength {
		t.Fatalf("fixture is not multibyte: %d bytes for %d characters", len(multibyte), maxSearchLength)
	}
	if _, err := (&Store{}).ListActive(t.Context(), "", Filter{Search: multibyte}); err == ErrInvalidSearch {
		t.Error("a search at the character limit was rejected for its byte length")
	}
	if _, err := (&Store{}).ListActive(t.Context(), "", Filter{Search: multibyte + "ä"}); err != ErrInvalidSearch {
		t.Errorf("a search past the character limit was accepted: %v", err)
	}
}
