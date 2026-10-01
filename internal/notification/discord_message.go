package notification

import (
	"net/url"
	"strconv"
	"strings"
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
	Thumbnail   *discordImage  `json:"thumbnail,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields"`
	Footer      discordFooter  `json:"footer"`
}

type discordImage struct {
	URL string `json:"url"`
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
		startsAt = "<t:" + strconv.FormatInt(summary.StartsAt.Unix(), 10) + ":F>"
	}
	title := discordPlainText(summary.GameName, 256)
	if creator := discordPlainText(summary.CreatorGameNick, 256); creator != "" {
		title = creator + " is looking for " + title
	}
	if len(title) > 256 {
		units := 0
		for offset, r := range title {
			units++
			if r > 0xffff {
				units++
			}
			if units > 256 {
				title = strings.TrimSuffix(title[:offset], "\\")
				break
			}
		}
	}
	requestURL := strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(summary.RequestID)
	embed := discordEmbed{
		Title:       title,
		URL:         requestURL,
		Description: discordPlainText(summary.Description, 240),
		Fields: []discordField{
			{Name: "Start", Value: startsAt},
			{Name: "Status", Value: discordPlainText(summary.Status, 1024)},
			{Name: "Players", Value: players},
			{Name: "Link", Value: "[Open in browser ↗](" + requestURL + ")"},
		},
		Footer: discordFooter{Text: discordPlainText(instanceName, 2048)},
	}
	switch summary.Status {
	case "open":
		embed.Color = 0x00B4D8
	case "playing":
		embed.Color = 0xFF6B35
	case "closed":
		embed.Color = 0xBED3DF
	case "cancelled":
		embed.Color = 0xFF3F73
	}
	if summary.CoverImageID != "" {
		embed.Thumbnail = &discordImage{URL: "https://images.igdb.com/igdb/image/upload/t_cover_big/" + url.PathEscape(summary.CoverImageID) + ".jpg"}
	}
	return discordMessage{Embeds: []discordEmbed{embed}}
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
