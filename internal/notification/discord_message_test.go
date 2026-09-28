package notification

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDiscordMessageCurrentState(t *testing.T) {
	four := int32(4)
	three := int32(3)
	cases := []struct {
		name, status, description, places string
		members                           int64
		capacity                          *int32
	}{
		{"open", "open", "Bring snacks", "2/4 players (2 free)", 2, &four},
		{"full", "open", "", "3/3 players (full)", 3, &three},
		{"unlimited", "open", "ok", "7 players (unlimited)", 7, nil},
		{"closed", "closed", "", "2/4 players (2 free)", 2, &four},
		{"cancelled", "cancelled", "", "2/4 players (2 free)", 2, &four},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := buildDiscordMessage(GameRequestSummary{
				RequestID: "request/one", GameName: "Chess", StartsAt: time.Date(2026, 9, 25, 18, 30, 0, 0, time.UTC),
				Status: tc.status, MemberCount: tc.members, MaxPlayers: tc.capacity, CreatorGameNick: "Ada", Description: tc.description,
			}, "https://play.example/", "Friday Club")
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			if err := json.Unmarshal(encoded, &data); err != nil {
				t.Fatal(err)
			}
			if mentions, ok := data["allowed_mentions"].(map[string]any); !ok || len(mentions) != 0 {
				t.Fatalf("allowed_mentions = %v", data["allowed_mentions"])
			}
			embeds, ok := data["embeds"].([]any)
			if !ok || len(embeds) != 1 {
				t.Fatalf("embeds = %v", data["embeds"])
			}
			embed := embeds[0].(map[string]any)
			if embed["url"] != "https://play.example/game-requests/request%2Fone" || embed["title"] != "Game Request" {
				t.Fatalf("link/title = %v", embed)
			}
			fields := map[string]string{}
			for _, raw := range embed["fields"].([]any) {
				field := raw.(map[string]any)
				fields[field["name"].(string)] = field["value"].(string)
			}
			for key, want := range map[string]string{"Game": "Chess", "Planned start": "2026-09-25T18:30:00Z", "Status": tc.status, "Players": tc.places, "Creator": "Ada"} {
				if fields[key] != want {
					t.Errorf("%s = %q, want %q", key, fields[key], want)
				}
			}
			if tc.description == "" && embed["description"] != nil {
				t.Errorf("empty description = %v", embed["description"])
			}
			if tc.description != "" && embed["description"] != tc.description {
				t.Errorf("description = %v", embed["description"])
			}
			if embed["footer"].(map[string]any)["text"] != "Friday Club" {
				t.Errorf("footer = %v", embed["footer"])
			}
			for _, forbidden := range []string{"roster", "activity", "actor", "webhook", "token", "email", "guild", "channel"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Errorf("payload contains %s: %s", forbidden, encoded)
				}
			}
		})
	}
}

func TestDiscordMessagePlainDescription(t *testing.T) {
	payload := buildDiscordMessage(GameRequestSummary{
		RequestID: "one", GameName: "Chess", Status: "open", CreatorGameNick: "Ada",
		Description: "**@everyone** <@123> [click](https://evil.example)\n\x00\u202e__secret__",
	}, "https://play.example", "")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		AllowedMentions map[string]any `json:"allowed_mentions"`
		Embeds          []struct {
			Description string `json:"description"`
			URL         string `json:"url"`
			Footer      struct {
				Text string `json:"text"`
			} `json:"footer"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	if data.AllowedMentions == nil || len(data.AllowedMentions) != 0 {
		t.Fatalf("allowed mentions = %v", data.AllowedMentions)
	}
	got := data.Embeds[0].Description
	for _, unsafe := range []string{"@everyone", "<@123>", "**", "__", "[click]", "\x00", "\u202e", "\n"} {
		if strings.Contains(got, unsafe) {
			t.Errorf("unsafe %q in %q", unsafe, got)
		}
	}
	if !strings.Contains(got, "click") || !strings.Contains(got, "secret") || data.Embeds[0].URL != "https://play.example/game-requests/one" || data.Embeds[0].Footer.Text != "LANsurfer" {
		t.Errorf("payload = %s", encoded)
	}
}

func TestDiscordMessageDescriptionShortened(t *testing.T) {
	payload := buildDiscordMessage(GameRequestSummary{Description: strings.Repeat("界", 1000)}, "https://play.example", "LANsurfer")
	if length := len([]rune(payload.Embeds[0].Description)); length > 300 || length < 1 {
		t.Fatalf("description length = %d", length)
	}
}
