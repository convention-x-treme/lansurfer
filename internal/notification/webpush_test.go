package notification

import (
	"context"
	"testing"
)

type fakeGameFollowers struct{ followers map[string][]string }

func (f *fakeGameFollowers) ListFollowers(ctx context.Context, gameID string) ([]string, error) {
	return f.followers[gameID], nil
}

type fakeRequestParticipants struct{ participants map[string][]string }

func (f *fakeRequestParticipants) ListParticipants(ctx context.Context, gameRequestID string) ([]string, error) {
	return f.participants[gameRequestID], nil
}

type fakeGameRequestSummaryReader struct{ summaries map[string]GameRequestSummary }

func (f *fakeGameRequestSummaryReader) GetGameRequestSummary(ctx context.Context, gameRequestID string) (GameRequestSummary, error) {
	return f.summaries[gameRequestID], nil
}

type fakeWebPushSubscriptionSource struct {
	subsByUser  map[string][]PushSubscriptionRecord
	preferences map[string]PushPreference
	audienceAll []string
}

func (f *fakeWebPushSubscriptionSource) ListSubscriptions(ctx context.Context, userID string) ([]PushSubscriptionRecord, error) {
	return f.subsByUser[userID], nil
}

// UpsertSubscription, DeleteSubscription, and DeleteSubscriptionByDeviceHash
// are unused by Targets; they exist only so this fake satisfies
// SubscriptionRepository in full.
func (f *fakeWebPushSubscriptionSource) UpsertSubscription(ctx context.Context, sub PushSubscriptionRecord) (PushSubscriptionRecord, error) {
	return sub, nil
}
func (f *fakeWebPushSubscriptionSource) DeleteSubscription(ctx context.Context, subscriptionID int64, userID string) (bool, error) {
	return false, nil
}
func (f *fakeWebPushSubscriptionSource) DeleteSubscriptionByDeviceHash(ctx context.Context, userID, deviceIDHash string) (bool, error) {
	return false, nil
}
func (f *fakeWebPushSubscriptionSource) GetPreference(ctx context.Context, userID string) (PushPreference, error) {
	if pref, ok := f.preferences[userID]; ok {
		return pref, nil
	}
	return PushPreference{NewRequestAudience: AudienceFollowed}, nil
}

// SetPreference is unused by Targets; it exists only so this fake satisfies
// PreferenceRepository in full.
func (f *fakeWebPushSubscriptionSource) SetPreference(ctx context.Context, userID string, preference PushPreference) error {
	return nil
}
func (f *fakeWebPushSubscriptionSource) ListUsersWithAudience(ctx context.Context, audience string) ([]string, error) {
	if audience == AudienceAll {
		return f.audienceAll, nil
	}
	return nil, nil
}

// GetSubscriptionByID is unused by Targets; it exists only so this fake
// satisfies DeliveryReadRepository in full. Task 5's Send tests exercise it
// for real.
func (f *fakeWebPushSubscriptionSource) GetSubscriptionByID(ctx context.Context, id int64) (PushSubscriptionRecord, error) {
	return PushSubscriptionRecord{}, nil
}

func TestWebPushTargetsForCreatedUnionsFollowersAndAllAudienceExcludingActor(t *testing.T) {
	followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {"follower-1", "actor-1"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {GameID: "game-1", GameName: "Warcraft III"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser: map[string][]PushSubscriptionRecord{
			"follower-1":     {{ID: 10}},
			"all-audience-1": {{ID: 20}},
		},
		preferences: map[string]PushPreference{
			"follower-1": {WebPushEnabled: true, NewRequestAudience: AudienceFollowed},
			"actor-1":    {WebPushEnabled: true, NewRequestAudience: AudienceFollowed},
		},
		audienceAll: []string{"all-audience-1"},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 1, GameRequestID: "req-1", ActorID: "actor-1", Kind: ActivityCreated})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	got := map[string]bool{}
	for _, target := range targets {
		if target.Transport != "webpush" {
			t.Fatalf("target.Transport = %q, want %q", target.Transport, "webpush")
		}
		got[target.Key] = true
	}
	if len(got) != 2 || !got["10"] || !got["20"] {
		t.Fatalf("Targets() = %#v, want exactly subscription IDs 10 and 20 (follower's device + all-audience user's device, actor excluded)", targets)
	}
}

func TestWebPushTargetsForLifecycleUsesCurrentParticipantsRegardlessOfAudience(t *testing.T) {
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"participant-1", "leaving-actor"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"participant-1": {{ID: 30}}},
		preferences: map[string]PushPreference{"participant-1": {WebPushEnabled: true, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, &fakeGameFollowers{}, participants, &fakeGameRequestSummaryReader{}, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 2, GameRequestID: "req-1", ActorID: "leaving-actor", Kind: ActivityLeft})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Key != "30" {
		t.Fatalf("Targets() for left = %#v, want exactly subscription 30 (leaving actor excluded)", targets)
	}
}

func TestWebPushTargetsExcludesDisabledUsers(t *testing.T) {
	followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {"disabled-follower"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {GameID: "game-1"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"disabled-follower": {{ID: 40}}},
		preferences: map[string]PushPreference{"disabled-follower": {WebPushEnabled: false, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, "https://lansurfer.example", nil)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 3, GameRequestID: "req-1", ActorID: "someone-else", Kind: ActivityCreated})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("Targets() = %#v, want none (the only follower has web_push_enabled=false)", targets)
	}
}
