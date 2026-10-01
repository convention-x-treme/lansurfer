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
	URL             string       `json:"url"`
	Instance        string       `json:"instance"`
	ActorGameNick   string       `json:"actor_game_nick,omitempty"`
}

func buildWebPushPayload(activity Activity, summary GameRequestSummary, actorGameNick, baseURL, instanceName string) ([]byte, error) {
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	payload := webPushPayload{
		Version:         3,
		Kind:            activity.Kind,
		Game:            summary.GameName,
		MemberCount:     summary.MemberCount,
		MaxPlayers:      summary.MaxPlayers,
		CreatorGameNick: summary.CreatorGameNick,
		URL:             strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(activity.GameRequestID),
		Instance:        instanceName,
	}
	if !summary.StartsAt.IsZero() {
		payload.StartsAt = summary.StartsAt.Format(time.RFC3339)
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
