package notification

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// discordMessage is shared by webhook execute and edit; neither path may omit
// the explicit empty mentions policy. No user data is used as an embed URL.
type discordMessage struct {
	Embeds          []discordEmbed `json:"embeds"`
	AllowedMentions struct{}       `json:"allowed_mentions"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Description string         `json:"description,omitempty"`
	Fields      []discordField `json:"fields"`
	Footer      discordFooter  `json:"footer"`
}

type discordField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type discordFooter struct {
	Text string `json:"text"`
}

// buildDiscordMessage renders the latest viewer-independent request snapshot.
// LANsurfer accepts at most 1000 description runes; Discord mirrors only the
// first 240 plain-content runes to keep this one embed compact.
func buildDiscordMessage(summary GameRequestSummary, baseURL, instanceName string) discordMessage {
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	players := strconv.FormatInt(summary.MemberCount, 10) + " players (unlimited)"
	if summary.MaxPlayers != nil {
		count := strconv.FormatInt(summary.MemberCount, 10)
		max := strconv.FormatInt(int64(*summary.MaxPlayers), 10)
		free := int64(*summary.MaxPlayers) - summary.MemberCount
		places := "full"
		if free > 0 {
			places = strconv.FormatInt(free, 10) + " free"
		}
		players = count + "/" + max + " players (" + places + ")"
	}
	startsAt := "Not set"
	if !summary.StartsAt.IsZero() {
		startsAt = summary.StartsAt.Format(time.RFC3339)
	}
	return discordMessage{Embeds: []discordEmbed{{
		Title:       "Game Request",
		URL:         strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(summary.RequestID),
		Description: discordPlainText(summary.Description, 240),
		Fields: []discordField{
			{Name: "Game", Value: discordPlainText(summary.GameName, 1024)},
			{Name: "Planned start", Value: startsAt},
			{Name: "Status", Value: discordPlainText(summary.Status, 1024)},
			{Name: "Players", Value: players},
			{Name: "Creator", Value: discordPlainText(summary.CreatorGameNick, 1024)},
		},
		Footer: discordFooter{Text: discordPlainText(instanceName, 2048)},
	}}}
}

// discordPlainText removes control/format characters, collapses whitespace,
// and escapes Discord markdown. A zero-width separator also breaks mention
// syntax; allowed_mentions remains empty even if Discord changes parsing.
func discordPlainText(text string, limit int) string {
	var result strings.Builder
	count := 0
	space := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			space = result.Len() > 0
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if count == limit {
			break
		}
		if space {
			result.WriteByte(' ')
			space = false
		}
		switch r {
		case '@':
			result.WriteString("@\u200b")
		case '*', '_', '~', '`', '|', '>', '<', '[', ']', '(', ')', '#', '\\':
			result.WriteByte('\\')
			result.WriteRune(r)
		default:
			result.WriteRune(r)
		}
		count++
	}
	return result.String()
}
