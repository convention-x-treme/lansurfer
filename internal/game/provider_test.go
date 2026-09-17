package game

import (
	"context"
	"errors"
	"testing"
	"time"
)

type finalProviderIndexRepositoryContract struct{}

func (finalProviderIndexRepositoryContract) BeginGeneration(context.Context) (string, error) {
	return "", nil
}
func (finalProviderIndexRepositoryContract) StoreProviderGames(context.Context, string, []ProviderGame) error {
	return nil
}
func (finalProviderIndexRepositoryContract) ActivateGeneration(context.Context, string, int64) error {
	return nil
}
func (finalProviderIndexRepositoryContract) FailGeneration(context.Context, string, string) error {
	return nil
}
func (finalProviderIndexRepositoryContract) Status(context.Context, time.Time) (ProviderStatus, error) {
	return ProviderStatus{}, nil
}

var _ ProviderIndexRepository = finalProviderIndexRepositoryContract{}

type finalDiscoveryRepositoryContract struct{}

func (finalDiscoveryRepositoryContract) Search(context.Context, string, string, int) ([]DiscoveryItem, error) {
	return nil, nil
}

type finalMaterializationRepositoryContract struct{}

func (finalMaterializationRepositoryContract) Materialize(context.Context, ProviderReference) (Game, error) {
	return Game{}, nil
}

var _ DiscoveryRepository = finalDiscoveryRepositoryContract{}
var _ MaterializationRepository = finalMaterializationRepositoryContract{}

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
	ref, err := NewProviderReference(" IGDB ", " 123 ")
	if err != nil {
		t.Fatalf("valid reference: %v", err)
	}
	if ref != (ProviderReference{Provider: "igdb", ExternalID: "123"}) {
		t.Fatalf("reference = %#v, want normalized IGDB reference", ref)
	}
	for _, tc := range [][2]string{{"", "123"}, {"igdb", ""}, {"steam", "123"}} {
		if _, err := NewProviderReference(tc[0], tc[1]); !errors.Is(err, ErrInvalidProviderReference) {
			t.Fatalf("reference %q/%q error = %v, want ErrInvalidProviderReference", tc[0], tc[1], err)
		}
	}
	for _, id := range []string{"0", "00", "12.3", "-1", "abc", "１２３"} {
		if _, err := NewProviderReference("igdb", id); !errors.Is(err, ErrInvalidProviderReference) {
			t.Fatalf("external ID %q error = %v, want ErrInvalidProviderReference", id, err)
		}
	}
}
