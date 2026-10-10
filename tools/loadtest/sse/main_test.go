package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	_ = consumeEvents(strings.NewReader(input), &stats, nil)
	if stats.events.Load() != 1 {
		t.Fatalf("got %d, want 1 game-request event", stats.events.Load())
	}
}

func TestPercentile(t *testing.T) {
	ms := func(values ...int) []time.Duration {
		out := make([]time.Duration, len(values))
		for i, v := range values {
			out[i] = time.Duration(v) * time.Millisecond
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		values []time.Duration
		p      float64
		want   time.Duration
	}{
		{"empty", nil, 95, 0},
		{"single", ms(7), 99, 7 * time.Millisecond},
		{"unsorted input", ms(30, 10, 20, 40), 50, 20 * time.Millisecond},
		{"p95 of 100", ms(seq(100)...), 95, 95 * time.Millisecond},
		{"max", ms(5, 1, 9), 100, 9 * time.Millisecond},
	} {
		if got := percentile(tc.values, tc.p); got != tc.want {
			t.Errorf("%s: percentile(p%v) = %v, want %v", tc.name, tc.p, got, tc.want)
		}
	}
}

func seq(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func TestConsumeEventsReportsGameRequestData(t *testing.T) {
	var stats counters
	var got []eventData
	input := "event: game-request\ndata: {\"type\":\"GameRequestJoined\",\"requestId\":\"r1\"}\n\n" +
		"event: game-request-chat\ndata: {\"type\":\"GameRequestChatMessage\",\"requestId\":\"r1\"}\n\n" +
		"event: game-request\ndata: not-json\n\n"
	_ = consumeEvents(strings.NewReader(input), &stats, func(e eventData) { got = append(got, e) })
	if len(got) != 1 || got[0].Type != "GameRequestJoined" || got[0].RequestID != "r1" {
		t.Fatalf("observed %+v, want exactly the one valid game-request event", got)
	}
	if stats.events.Load() != 2 {
		t.Fatalf("events = %d, want 2 game-request events counted including the malformed one", stats.events.Load())
	}
}
