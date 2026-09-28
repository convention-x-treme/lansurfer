package notification

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestWebPushPayloadKindsAndPrivacy(t *testing.T) {
	start := time.Date(2026, 9, 25, 18, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		kind  ActivityKind
		title string
		actor bool
	}{
		{ActivityCreated, "New Chess Game Request", false},
		{ActivityJoined, "Night Otter joined Chess", true},
		{ActivityLeft, "Night Otter left Chess", true},
		{ActivityClosed, "Chess Game Request closed", false},
		{ActivityCancelled, "Chess Game Request cancelled", false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			body, err := buildWebPushPayload(Activity{Kind: tc.kind, GameRequestID: "request-42", ActorID: "secret-user"}, GameRequestSummary{GameName: "Chess", StartsAt: start}, "Night Otter", "https://play.example/", "Friday Club")
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			wantKeys := []string{"body", "game", "instance", "kind", "starts_at", "title", "url", "version"}
			if tc.actor {
				wantKeys = append(wantKeys, "actor_game_nick")
			}
			sort.Strings(wantKeys)
			keys := make([]string, 0, len(got))
			for key := range got {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, wantKeys) {
				t.Fatalf("JSON keys = %v, want %v", keys, wantKeys)
			}
			if got["version"] != float64(1) || got["kind"] != string(tc.kind) || got["game"] != "Chess" || got["title"] != tc.title || got["instance"] != "Friday Club" || got["url"] != "https://play.example/game-requests/request-42" || got["starts_at"] != "2026-09-25T18:30:00Z" || got["body"] != "Friday Club · Starts 2026-09-25T18:30:00Z" {
				t.Fatalf("payload = %v", got)
			}
			if tc.actor && got["actor_game_nick"] != "Night Otter" {
				t.Fatalf("actor Game Nick = %v", got["actor_game_nick"])
			}
			for _, forbidden := range []string{"description", "roster", "provider_identity", "membership_list", "activity_history", "actor_id", "user_id", "subscription", "delivery_id"} {
				if _, exists := got[forbidden]; exists {
					t.Fatalf("forbidden field %q in payload", forbidden)
				}
			}
		})
	}
}

func TestWebPushPayloadOmitsZeroStartAndUsesInstanceFallback(t *testing.T) {
	body, err := buildWebPushPayload(Activity{Kind: ActivityCreated, GameRequestID: "req/with?reserved"}, GameRequestSummary{GameName: "Chess"}, "", "https://play.example", "")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["starts_at"]; present {
		t.Fatalf("zero start time must be omitted: %v", got)
	}
	if got["instance"] != "LANsurfer" || got["body"] != "LANsurfer" || got["url"] != "https://play.example/game-requests/req%2Fwith%3Freserved" {
		t.Fatalf("fallback payload = %v", got)
	}
}
