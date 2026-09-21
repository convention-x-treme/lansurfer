package notification

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// GameRequestSummary is the minimal, viewer-independent Game Request context
// WebPushTransport needs: which Game (for follower resolution and payload
// copy) and when it starts (for the payload). Resolved fresh at both Targets
// and Send time, never cached across a delayed retry.
type GameRequestSummary struct {
	GameID   string
	GameName string
	StartsAt time.Time
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

// WebPushTransport implements notification.Transport for personal Web Push.
// It never persists anything itself: recipient/eligibility state lives in
// SubscriptionRepository/PreferenceRepository/DeliveryReadRepository, all
// owned by Track E2 Foundation.
type WebPushTransport struct {
	subscriptions SubscriptionRepository
	preferences   PreferenceRepository
	delivery      DeliveryReadRepository
	followers     GameFollowers
	participants  RequestParticipants
	summaries     GameRequestSummaryReader
	baseURL       string
	sender        webPushSender // set in Task 5; nil is valid here, Send is not implemented yet
}

func NewWebPushTransport(
	subscriptions SubscriptionRepository, preferences PreferenceRepository, delivery DeliveryReadRepository,
	followers GameFollowers, participants RequestParticipants, summaries GameRequestSummaryReader,
	baseURL string, sender webPushSender,
) *WebPushTransport {
	return &WebPushTransport{
		subscriptions: subscriptions, preferences: preferences, delivery: delivery,
		followers: followers, participants: participants, summaries: summaries,
		baseURL: baseURL, sender: sender,
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
			targets = append(targets, Target{Transport: "webpush", Key: strconv.FormatInt(sub.ID, 10)})
		}
	}
	return targets, nil
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

// webPushSender is implemented in webpush_send.go (Task 5). Defined here so
// WebPushTransport can hold one before Send exists.
type webPushSender interface {
	send(ctx context.Context, endpoint, p256dh, auth string, payload []byte, ttlSeconds int) sendOutcome
}

type sendOutcome struct {
	Outcome    Outcome
	RetryAfter *time.Duration
	Err        error
	Expired    bool
}
