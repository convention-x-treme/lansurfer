package main

import (
	"io"
	"strings"
	"testing"
)

func TestControlValidation(t *testing.T) {
	valid := []control{{Mode: "normal"}, {Mode: "hold-refresh", DelayMS: 500}, {Mode: "fail-refresh", DurationMS: 1000}, {Mode: "cut-streams", DurationMS: 1000}, {Mode: "offline", DurationMS: 1000}, {Mode: "drop-sse", DropCount: 1, RequestID: "id", EventType: "game-request-chat"}, {Mode: "delay", DurationMS: 1000, DelayMS: 200}, {Mode: "loss", DurationMS: 1000}}
	for _, c := range valid {
		if err := validate(c); err != nil {
			t.Errorf("validate(%+v): %v", c, err)
		}
	}
	invalid := []control{{Mode: "normal", DelayMS: 1}, {Mode: "delay", DurationMS: 1, DelayMS: 201}, {Mode: "loss", DurationMS: 120001}, {Mode: "drop-sse", DropCount: 1, RequestID: "id", EventType: "untrusted"}, {Mode: "offline"}}
	for _, c := range invalid {
		if err := validate(c); err == nil {
			t.Errorf("validate(%+v) succeeded", c)
		}
	}
}

func TestDropParserPreservesCommentsAndDropsOnlyTargetFrame(t *testing.T) {
	input := ": connected\n\nevent: game-request\ndata: {\"requestId\":\"target\",\"type\":\"GameRequestJoined\"}\n\nevent: game-request\ndata: {\"requestId\":\"other\",\"type\":\"GameRequestJoined\"}\n\n"
	s := &state{}
	f := newFrameFilter(io.NopCloser(strings.NewReader(input)), control{Mode: "drop-sse", DropCount: 1, RequestID: "target", EventType: "GameRequestJoined"}, s)
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "target") || !strings.Contains(string(got), ": connected") || !strings.Contains(string(got), "other") {
		t.Fatalf("unexpected filtered frames: %q", got)
	}
	if s.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want 1", s.dropped.Load())
	}
}
