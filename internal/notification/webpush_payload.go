package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type webPushPayload struct {
	Version       int          `json:"version"`
	Kind          ActivityKind `json:"kind"`
	Game          string       `json:"game"`
	StartsAt      string       `json:"starts_at,omitempty"`
	URL           string       `json:"url"`
	Instance      string       `json:"instance"`
	ActorGameNick string       `json:"actor_game_nick,omitempty"`
	Title         string       `json:"title"`
	Body          string       `json:"body"`
}

func buildWebPushPayload(activity Activity, summary GameRequestSummary, actorGameNick, baseURL, instanceName string) ([]byte, error) {
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	payload := webPushPayload{
		Version:  1,
		Kind:     activity.Kind,
		Game:     summary.GameName,
		URL:      strings.TrimRight(baseURL, "/") + "/game-requests/" + url.PathEscape(activity.GameRequestID),
		Instance: instanceName,
		Body:     instanceName,
	}
	if !summary.StartsAt.IsZero() {
		payload.StartsAt = summary.StartsAt.Format(time.RFC3339)
		payload.Body += " · Starts " + payload.StartsAt
	}
	switch activity.Kind {
	case ActivityCreated:
		payload.Title = "New " + summary.GameName + " Game Request"
	case ActivityJoined, ActivityLeft:
		if actorGameNick == "" {
			return nil, fmt.Errorf("actor Game Nick required for %s", activity.Kind)
		}
		payload.ActorGameNick = actorGameNick
		payload.Title = actorGameNick + " " + string(activity.Kind) + " " + summary.GameName
	case ActivityClosed, ActivityCancelled:
		payload.Title = summary.GameName + " Game Request " + string(activity.Kind)
	default:
		return nil, fmt.Errorf("unsupported Web Push activity: %s", activity.Kind)
	}
	return json.Marshal(payload)
}
