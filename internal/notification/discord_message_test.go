package notification

import (
	"encoding/json"
	"strconv"
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
		color                             int
	}{
		{"open", "open", "Bring snacks", "2/4 players (2 free)", 2, &four, 0x00B4D8},
		{"full", "open", "", "3/3 players (full)", 3, &three, 0x00B4D8},
		{"unlimited", "open", "ok", "7 players (unlimited)", 7, nil, 0x00B4D8},
		{"closed", "closed", "", "2/4 players (2 free)", 2, &four, 0xBED3DF},
		{"cancelled", "cancelled", "", "2/4 players (2 free)", 2, &four, 0xFF3F73},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := buildDiscordMessage(GameRequestSummary{
				RequestID: "request/one", GameName: "Chess", CoverImageID: "co1abc", StartsAt: time.Date(2026, 9, 25, 18, 30, 0, 0, time.UTC),
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
			if embed["url"] != "https://play.example/game-requests/request%2Fone" || embed["title"] != "Ada is looking for Chess" {
				t.Fatalf("link/title = %v", embed)
			}
			if image := embed["thumbnail"].(map[string]any)["url"]; image != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1abc.jpg" {
				t.Fatalf("thumbnail = %v", image)
			}
			if embed["image"] != nil || embed["color"] != float64(tc.color) {
				t.Fatalf("unexpected image or status color = %v", embed)
			}
			fields := map[string]string{}
			for _, raw := range embed["fields"].([]any) {
				field := raw.(map[string]any)
				fields[field["name"].(string)] = field["value"].(string)
			}
			for key, want := range map[string]string{"Start": "<t:" + strconv.FormatInt(time.Date(2026, 9, 25, 18, 30, 0, 0, time.UTC).Unix(), 10) + ":F>", "Status": tc.status, "Players": tc.places} {
				if fields[key] != want {
					t.Errorf("%s = %q, want %q", key, fields[key], want)
				}
			}
			if len(fields) != 4 || fields["Link"] != "[Im Browser öffnen ↗](https://play.example/game-requests/request%2Fone)" {
				t.Fatalf("missing browser link = %v", fields)
			}
			last := embed["fields"].([]any)[3].(map[string]any)
			if last["name"] != "Link" {
				t.Errorf("browser link is not last: %v", embed["fields"])
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

func TestDiscordCoverUsesIGDBWhenInstanceIsLocal(t *testing.T) {
	embed := buildDiscordMessage(GameRequestSummary{
		RequestID: "one", GameName: "Chess", CoverImageID: "co1abc",
	}, "http://localhost:8080", "").Embeds[0]
	if embed.URL != "http://localhost:8080/game-requests/one" || embed.Thumbnail == nil ||
		embed.Thumbnail.URL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1abc.jpg" {
		t.Fatalf("local instance Discord embed = %#v", embed)
	}
}

func TestDiscordMessageTitleFallbackAndNoCover(t *testing.T) {
	embed := buildDiscordMessage(GameRequestSummary{GameName: "Chess"}, "https://play.example", "").Embeds[0]
	if embed.Title != "Chess" || embed.Thumbnail != nil || embed.Fields[0].Value != "Not set" {
		t.Fatalf("embed = %#v", embed)
	}
}

func TestDiscordMessageTitleSanitizesMentions(t *testing.T) {
	embed := buildDiscordMessage(GameRequestSummary{
		CreatorGameNick: "@everyone", GameName: "<@123> **Chess**",
	}, "https://play.example", "").Embeds[0]
	if strings.Contains(embed.Title, "@everyone") || strings.Contains(embed.Title, "<@123>") || strings.Contains(embed.Title, "**") {
		t.Fatalf("unsafe title = %q", embed.Title)
	}
	if !strings.Contains(embed.Title, "is looking for") || !strings.Contains(embed.Title, "Chess") {
		t.Fatalf("missing list title = %q", embed.Title)
	}
}

func TestDiscordMessageTitleFitsDiscordLimitAfterEscaping(t *testing.T) {
	embed := buildDiscordMessage(GameRequestSummary{
		CreatorGameNick: strings.Repeat("A", 32),
		GameName:        strings.Repeat("*", 120),
	}, "https://play.example", "").Embeds[0]
	if len([]rune(embed.Title)) > 256 || strings.HasSuffix(embed.Title, "\\") {
		t.Fatalf("Discord title too long or dangling escape: %q", embed.Title)
	}
	if !strings.Contains(embed.Title, "is looking for") {
		t.Fatalf("missing request heading: %q", embed.Title)
	}
}

func TestDiscordMessageDescriptionShortened(t *testing.T) {
	payload := buildDiscordMessage(GameRequestSummary{Description: strings.Repeat("界", 1000)}, "https://play.example", "LANsurfer")
	if length := len([]rune(payload.Embeds[0].Description)); length > 300 || length < 1 {
		t.Fatalf("description length = %d", length)
	}
}
