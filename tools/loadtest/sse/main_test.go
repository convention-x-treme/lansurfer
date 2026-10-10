package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	input := "{\"session_token\":\"abc\"}\n{\"session_token\":\"xyz\"}\n"
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	tokens, err := readSessions(path)
	if err != nil || len(tokens) != 2 || tokens[0] != "abc" || tokens[1] != "xyz" {
		t.Fatalf("got %q, error %v", tokens, err)
	}
}

func TestReadSessionsRejectsMissingToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(path, []byte("{\"user_id\":\"test\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessions(path); err == nil {
		t.Fatal("expected error on missing session token")
	}
}

func TestConsumeEventsCountsGameRequestOnly(t *testing.T) {
	var stats counters
	input := ": connected\n\nevent: game-request\ndata: {}\n\nevent: other\ndata: foo\n\n"
	_ = consumeEvents(strings.NewReader(input), &stats)
	if stats.events.Load() != 1 {
		t.Fatalf("got %d, want 1 game-request event", stats.events.Load())
	}
}
