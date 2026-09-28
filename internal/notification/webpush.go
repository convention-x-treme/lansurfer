package notification

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GameRequestSummary is the viewer-independent current request snapshot
// notification transports use for recipient resolution and message content.
// Resolved fresh at delivery time, never cached across a delayed retry.
type GameRequestSummary struct {
	RequestID       string
	GameID          string
	GameName        string
	CoverImageID    string
	StartsAt        time.Time
	Status          string
	MemberCount     int64
	MaxPlayers      *int32
	CreatorGameNick string
	Description     string
}

// GameFollowers resolves which users follow one Game, for `created`
// recipient materialization.
type GameFollowers interface {
	ListFollowers(ctx context.Context, gameID string) ([]string, error)
}

// RequestParticipants resolves current Game Request membership, for
// lifecycle recipient materialization. It is always called fresh, so a
// delayed/retried send only ever loses recipients who have since left, never
// gains ones who joined afterward.
type RequestParticipants interface {
	ListParticipants(ctx context.Context, gameRequestID string) ([]string, error)
}

// GameRequestSummaryReader resolves GameRequestSummary for one Game Request.
type GameRequestSummaryReader interface {
	GetGameRequestSummary(ctx context.Context, gameRequestID string) (GameRequestSummary, error)
}

// GameNickReader resolves one user's public LANsurfer Game Nick.
type GameNickReader interface {
	GetGameNick(ctx context.Context, userID string) (string, error)
}

// WebPushTransport implements notification.Transport for personal Web Push.
// It never persists anything itself: recipient/eligibility state lives in
// SubscriptionRepository/PreferenceRepository/DeliveryReadRepository, all
// owned by the notification capability.
type WebPushTransport struct {
	subscriptions SubscriptionRepository
	preferences   PreferenceRepository
	delivery      DeliveryReadRepository
	followers     GameFollowers
	participants  RequestParticipants
	summaries     GameRequestSummaryReader
	gameNicks     GameNickReader
	baseURL       string
	instanceName  string
	encryptionKey []byte
	sender        WebPushSender
}

func NewWebPushTransport(
	subscriptions SubscriptionRepository, preferences PreferenceRepository, delivery DeliveryReadRepository,
	followers GameFollowers, participants RequestParticipants, summaries GameRequestSummaryReader,
	gameNicks GameNickReader, baseURL, instanceName string, encryptionKey []byte, sender WebPushSender,
) *WebPushTransport {
	return &WebPushTransport{
		subscriptions: subscriptions, preferences: preferences, delivery: delivery,
		followers: followers, participants: participants, summaries: summaries,
		gameNicks: gameNicks, baseURL: baseURL, instanceName: instanceName, encryptionKey: encryptionKey, sender: sender,
	}
}

func (t *WebPushTransport) Name() string { return "webpush" }

func (t *WebPushTransport) Targets(ctx context.Context, activity Activity) ([]Target, error) {
	var candidates []string
	// trusted marks candidates ListUsersWithAudience already returned:
	// DeliveryReadRepository.ListUsersWithAudience only returns users whose
	// stored preference already has WebPushEnabled=true for AudienceAll, so
	// re-fetching their preference below would be redundant (and, for a
	// follower who is separately opted into "all", would just repeat the
	// same lookup). Only candidates reached solely through followers/
	// participants need their preference fetched to check WebPushEnabled.
	trusted := map[string]bool{}
	switch activity.Kind {
	case ActivityCreated:
		summary, err := t.summaries.GetGameRequestSummary(ctx, activity.GameRequestID)
		if err != nil {
			return nil, fmt.Errorf("get game request summary: %w", err)
		}
		followers, err := t.followers.ListFollowers(ctx, summary.GameID)
		if err != nil {
			return nil, fmt.Errorf("list game followers: %w", err)
		}
		allAudience, err := t.delivery.ListUsersWithAudience(ctx, AudienceAll)
		if err != nil {
			return nil, fmt.Errorf("list all-audience users: %w", err)
		}
		for _, userID := range allAudience {
			trusted[userID] = true
		}
		candidates = dedupeExcept(append(followers, allAudience...), activity.ActorID)
	case ActivityJoined, ActivityLeft, ActivityClosed, ActivityCancelled:
		participants, err := t.participants.ListParticipants(ctx, activity.GameRequestID)
		if err != nil {
			return nil, fmt.Errorf("list game request participants: %w", err)
		}
		candidates = dedupeExcept(participants, activity.ActorID)
	default:
		return nil, nil
	}

	var targets []Target
	for _, userID := range candidates {
		if !trusted[userID] {
			preference, err := t.preferences.GetPreference(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("get push preference for %s: %w", userID, err)
			}
			if !preference.WebPushEnabled {
				continue
			}
		}
		subs, err := t.subscriptions.ListSubscriptions(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list subscriptions for %s: %w", userID, err)
		}
		for _, sub := range subs {
			key := encodeWebPushTarget(sub.ID, userID)
			targets = append(targets, Target{Transport: "webpush", Key: key})
		}
	}
	return targets, nil
}

func (t *WebPushTransport) actorGameNick(ctx context.Context, activity Activity) (string, error) {
	if activity.Kind != ActivityJoined && activity.Kind != ActivityLeft {
		return "", nil
	}
	return t.gameNicks.GetGameNick(ctx, activity.ActorID)
}

func dedupeExcept(userIDs []string, exclude string) []string {
	seen := make(map[string]bool, len(userIDs))
	result := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id == exclude || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func encodeWebPushTarget(subscriptionID int64, userID string) string {
	return strconv.FormatInt(subscriptionID, 10) + ":" + base64.RawURLEncoding.EncodeToString([]byte(userID))
}

func decodeWebPushTarget(key string) (int64, string, error) {
	subscriptionText, encodedUserID, ok := strings.Cut(key, ":")
	if !ok {
		return 0, "", fmt.Errorf("invalid web push target key")
	}
	subscriptionID, err := strconv.ParseInt(subscriptionText, 10, 64)
	if err != nil || subscriptionID <= 0 || strconv.FormatInt(subscriptionID, 10) != subscriptionText {
		return 0, "", fmt.Errorf("invalid web push target subscription ID")
	}
	userIDBytes, err := base64.RawURLEncoding.DecodeString(encodedUserID)
	if err != nil || len(userIDBytes) == 0 || base64.RawURLEncoding.EncodeToString(userIDBytes) != encodedUserID {
		return 0, "", fmt.Errorf("invalid web push target user ID")
	}
	return subscriptionID, string(userIDBytes), nil
}

// WebPushSender isolates provider I/O for Web Push delivery.
type WebPushSender interface {
	Send(ctx context.Context, endpoint, p256dh, auth string, payload []byte, ttlSeconds int) WebPushSendOutcome
}

type WebPushSendOutcome struct {
	Outcome    Outcome
	RetryAfter *time.Duration
	Err        error
	Expired    bool
}
