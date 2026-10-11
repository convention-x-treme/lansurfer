package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidScenario(t *testing.T) {
	for _, scenario := range []string{"http-read", "browser120", "sse2"} {
		if !validScenario(scenario) {
			t.Errorf("validScenario(%q) = false", scenario)
		}
	}
	for _, scenario := range []string{"", "Upper", "../outside", "x y", "-bad"} {
		if validScenario(scenario) {
			t.Errorf("validScenario(%q) = true", scenario)
		}
	}
}

func TestSafeParametersExcludeSecrets(t *testing.T) {
	t.Setenv("USERS", "120")
	t.Setenv("FILTER_GAME", "Warcraft III")
	t.Setenv("LANSURFER_LOADTEST_DATABASE_URL", "postgres://secret")
	t.Setenv("LANSURFER_LOADTEST_METRICS_KEY", "secret")
	got := safeParameters()
	if got["USERS"] != "120" || got["FILTER_GAME"] != "Warcraft III" {
		t.Fatalf("safe parameters missing allowed values: %#v", got)
	}
	if _, ok := got["LANSURFER_LOADTEST_DATABASE_URL"]; ok {
		t.Fatal("database URL entered manifest")
	}
	if _, ok := got["LANSURFER_LOADTEST_METRICS_KEY"]; ok {
		t.Fatal("metrics key entered manifest")
	}
}

func TestFinishRunUpdatesPrivateManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "20261010T120000Z-http-read")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Second)
	manifest := runManifest{SchemaVersion: 1, Scenario: "http-read", StartedAt: started}
	path := filepath.Join(dir, "manifest.json")
	if err := writeManifest(path, manifest, os.O_EXCL); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANSURFER_LOADTEST_REPORT_ROOT", root)
	if err := finishRun(dir, 1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("manifest permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got runManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.FinishedAt == nil || got.ExitCode == nil || *got.ExitCode != 1 || got.ActualDuration == "" {
		t.Fatalf("manifest completion fields missing: %#v", got)
	}
	if err := finishRun(dir, 1); err == nil {
		t.Fatal("second finish succeeded")
	}
}
func TestScanSecretsCountsWithoutReturningValues(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions.jsonl")
	key := filepath.Join(dir, "metrics-key")
	report := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(sessions, []byte("{\"session_token\":\"test-session-token\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("test-metrics-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte("test-session-token test-metrics-key"), 0600); err != nil {
		t.Fatal(err)
	}
	matches, err := scanSecrets([]string{report}, sessions, key, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matches != 2 {
		t.Fatalf("secret matches = %d, want 2", matches)
	}
}
