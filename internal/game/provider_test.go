package game

import (
	"errors"
	"testing"
)

func TestVisibleProviderGameType(t *testing.T) {
	visible := []string{"Main Game", "Standalone Expansion", "Remake", "Remaster", "Port", "Expanded Game"}
	hidden := []string{"DLC Addon", "Bundle", "Mod", "Episode", "Season", "Pack", "Update", "future-new-type", ""}

	for _, value := range visible {
		if !VisibleProviderGameType(value) {
			t.Errorf("VisibleProviderGameType(%q) = false", value)
		}
	}
	for _, value := range hidden {
		if VisibleProviderGameType(value) {
			t.Errorf("VisibleProviderGameType(%q) = true", value)
		}
	}
}

func TestProviderReferenceValidation(t *testing.T) {
	ref, err := NewProviderReference(" igdb ", " 123 ")
	if err != nil {
		t.Fatalf("valid reference: %v", err)
	}
	if ref != (ProviderReference{Provider: "igdb", ExternalID: "123"}) {
		t.Fatalf("reference = %#v, want normalized IGDB reference", ref)
	}
	for _, tc := range [][2]string{{"", "123"}, {"igdb", ""}, {"steam", "123"}} {
		if _, err := NewProviderReference(tc[0], tc[1]); err == nil {
			t.Fatalf("reference %q/%q unexpectedly valid", tc[0], tc[1])
		}
	}
	for _, id := range []string{"12.3", "-1", "abc", "１２３"} {
		if _, err := NewProviderReference("igdb", id); !errors.Is(err, ErrInvalidProviderReference) {
			t.Fatalf("external ID %q error = %v, want ErrInvalidProviderReference", id, err)
		}
	}
}
