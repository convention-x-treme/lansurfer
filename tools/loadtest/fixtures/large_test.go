package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScaledGameIdentityIsStable(t *testing.T) {
	name, slug := scaledGameIdentity(21)
	if name != "Loadtest Game 0021" || slug != "loadtest-large-v1-game-0021" {
		t.Fatalf("identity = %q, %q", name, slug)
	}
	lastName, lastSlug := scaledGameIdentity(1000)
	if lastName == name || lastSlug == slug {
		t.Fatalf("identities collide: %q/%q and %q/%q", name, slug, lastName, lastSlug)
	}
}

func TestFixtureCheckpointIsPrivateAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.json")
	want := fixtureFile{
		Games:    []string{"game-id"},
		Requests: map[string][]string{"open": {"request-id"}},
		Scale:    &scaleProgress{TargetGames: 1000, TargetRequests: 5000, RequestsComplete: 60},
	}
	if err := writeFixtureFile(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("manifest mode = %04o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got fixtureFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Scale == nil || got.Scale.TargetGames != want.Scale.TargetGames || got.Scale.RequestsComplete != want.Scale.RequestsComplete || got.Requests["open"][0] != "request-id" {
		t.Fatalf("checkpoint round trip = %+v", got)
	}
}
