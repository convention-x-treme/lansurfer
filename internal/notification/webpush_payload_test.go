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
		actor bool
	}{
		{ActivityCreated, false},
		{ActivityJoined, true},
		{ActivityLeft, true},
		{ActivityStarted, false},
		{ActivityClosed, false},
		{ActivityCancelled, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			four := int32(4)
			body, err := buildWebPushPayload(Activity{Kind: tc.kind, GameRequestID: "request-42", ActorID: "secret-user"}, GameRequestSummary{GameName: "Chess", CreatorGameNick: "Ada", CoverImageID: "co1abc", Description: "Private", StartsAt: start, MemberCount: 1, MaxPlayers: &four}, "Night Otter", "https://play.example/", "Friday Club")
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			wantKeys := []string{"creator_game_nick", "game", "instance", "kind", "starts_at", "url", "version", "member_count", "max_players"}
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
			if got["version"] != float64(3) || got["kind"] != string(tc.kind) || got["game"] != "Chess" || got["creator_game_nick"] != "Ada" || got["instance"] != "Friday Club" || got["url"] != "https://play.example/game-requests/request-42" || got["starts_at"] != "2026-09-25T18:30:00Z" || got["member_count"] != float64(1) || got["max_players"] != float64(4) {
				t.Fatalf("payload = %v", got)
			}
			if tc.actor && got["actor_game_nick"] != "Night Otter" {
				t.Fatalf("actor Game Nick = %v", got["actor_game_nick"])
			}
			for _, forbidden := range []string{"title", "body", "cover_image_id", "description", "roster", "provider_identity", "membership_list", "activity_history", "actor_id", "user_id", "subscription", "delivery_id"} {
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
	if got["instance"] != "LANsurfer" || got["url"] != "https://play.example/game-requests/req%2Fwith%3Freserved" {
		t.Fatalf("fallback payload = %v", got)
	}
	if got["member_count"] != float64(0) {
		t.Fatalf("unlimited request member count = %v", got["member_count"])
	}
	if _, present := got["max_players"]; present {
		t.Fatalf("unlimited request max_players must be omitted: %v", got)
	}
	if _, present := got["creator_game_nick"]; present {
		t.Fatalf("missing creator Game Nick must be omitted: %v", got)
	}
}

func TestWebPushPayloadRequiresActorAndKnownKind(t *testing.T) {
	for _, kind := range []ActivityKind{ActivityJoined, ActivityLeft, "unknown"} {
		if _, err := buildWebPushPayload(Activity{Kind: kind}, GameRequestSummary{}, "", "", ""); err == nil {
			t.Fatalf("kind %q accepted without required actor or support", kind)
		}
	}
}
