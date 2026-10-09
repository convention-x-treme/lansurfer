package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type webPushPayload struct {
	Version         int          `json:"version"`
	Kind            ActivityKind `json:"kind"`
	Game            string       `json:"game"`
	MemberCount     int64        `json:"member_count"`
	MaxPlayers      *int32       `json:"max_players,omitempty"`
	CreatorGameNick string       `json:"creator_game_nick,omitempty"`
	StartsAt        string       `json:"starts_at,omitempty"`
	StartedAt       string       `json:"started_at,omitempty"`
	Status          string       `json:"status"`
	URL             string       `json:"url"`
	Instance        string       `json:"instance"`
	ActorGameNick   string       `json:"actor_game_nick,omitempty"`
}

type webPushTimePayload struct {
	Version  int    `json:"version"`
	Kind     string `json:"kind"`
	Game     string `json:"game"`
	StartsAt string `json:"starts_at"`
	URL      string `json:"url"`
	Instance string `json:"instance"`
}

func buildWebPushTimePayload(kind, requestID string, startsAt time.Time, summary GameRequestSummary, baseURL, instanceName string) ([]byte, error) {
	if (kind != "planned_start_changed" && kind != "starting_soon") || requestID == "" || startsAt.IsZero() {
		return nil, fmt.Errorf("invalid planned start payload")
	}
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	return json.Marshal(webPushTimePayload{
		Version: 4, Kind: kind, Game: summary.GameName, StartsAt: startsAt.Format(time.RFC3339),
		URL:      strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(requestID),
		Instance: instanceName,
	})
}

func buildWebPushPayload(activity Activity, summary GameRequestSummary, actorGameNick, baseURL, instanceName string) ([]byte, error) {
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	switch summary.Status {
	case "open", "playing", "closed", "cancelled":
	default:
		return nil, fmt.Errorf("unsupported Game Request status: %s", summary.Status)
	}
	payload := webPushPayload{
		Version: 4, Kind: activity.Kind, Game: summary.GameName,
		MemberCount: summary.MemberCount, MaxPlayers: summary.MaxPlayers,
		CreatorGameNick: summary.CreatorGameNick, Status: summary.Status,
		URL:      strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(activity.GameRequestID),
		Instance: instanceName,
	}
	if !summary.StartsAt.IsZero() {
		payload.StartsAt = summary.StartsAt.Format(time.RFC3339)
	}
	if summary.StartedAt != nil && !summary.StartedAt.IsZero() {
		payload.StartedAt = summary.StartedAt.Format(time.RFC3339)
	}
	switch activity.Kind {
	case ActivityCreated:
	case ActivityJoined, ActivityLeft:
		if actorGameNick == "" {
			return nil, fmt.Errorf("actor Game Nick required for %s", activity.Kind)
		}
		payload.ActorGameNick = actorGameNick
	case ActivityStarted, ActivityClosed, ActivityCancelled:
	default:
		return nil, fmt.Errorf("unsupported Web Push activity: %s", activity.Kind)
	}
	return json.Marshal(payload)
}
