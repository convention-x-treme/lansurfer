package components

import "testing"

func TestViewerInitial(t *testing.T) {
	tests := map[string]string{
		"[cXt] Stedofan": "S",
		"[cXt]Stedofan":  "S",
		"[Team]Player":   "P",
		"  -Alice":       "A",
		"123Player":      "1",
		"Änne":           "Ä",
		"[Solo]":         "S",
		"(clan) {x} Bob": "B",
		"[unclosed Zed":  "U",
		"!!!":            "?",
		"[]":             "?",
		"":               "?",
		"   ":            "?",
	}
	for nick, want := range tests {
		if got := ViewerInitial(nick); got != want {
			t.Errorf("ViewerInitial(%q) = %q, want %q", nick, got, want)
		}
	}
}
