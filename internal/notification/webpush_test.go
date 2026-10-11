package notification

import (
	"context"
	"testing"
	"time"
)

type fakeGameNickReader struct {
	calls []string
	nick  string
	err   error
}

func (f *fakeGameNickReader) GetGameNick(ctx context.Context, userID string) (string, error) {
	f.calls = append(f.calls, userID)
	return f.nick, f.err
}

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
// satisfies DeliveryReadRepository in full. The Send tests exercise it
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
		audienceAll: []string{"follower-1", "all-audience-1"},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, nil, "https://lansurfer.example", "", nil, nil, time.Now)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 1, GameRequestID: "req-1", ActorID: "actor-1", Kind: ActivityCreated})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	got := map[string]bool{}
	for _, target := range targets {
		if target.Transport != "webpush" {
			t.Fatalf("target.Transport = %q, want %q", target.Transport, "webpush")
		}
		subscriptionID, userID, err := decodeWebPushTarget(target.Key)
		if err != nil {
			t.Fatalf("decodeWebPushTarget(%q) error = %v", target.Key, err)
		}
		got[encodeWebPushTarget(subscriptionID, userID)] = true
	}
	if len(got) != 2 || !got[encodeWebPushTarget(10, "follower-1")] || !got[encodeWebPushTarget(20, "all-audience-1")] {
		t.Fatalf("Targets() = %#v, want follower and all-audience targets with actor excluded", targets)
	}
}

func TestWebPushTargetsForLifecycleUsesCurrentParticipantsRegardlessOfAudience(t *testing.T) {
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"participant-1", "leaving-actor"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"participant-1": {{ID: 30}}},
		preferences: map[string]PushPreference{"participant-1": {WebPushEnabled: true, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, &fakeGameFollowers{}, participants, &fakeGameRequestSummaryReader{}, nil, "https://lansurfer.example", "", nil, nil, time.Now)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 2, GameRequestID: "req-1", ActorID: "leaving-actor", Kind: ActivityLeft})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	wantKey := encodeWebPushTarget(30, "participant-1")
	if len(targets) != 1 || targets[0].Key != wantKey {
		t.Fatalf("Targets() for left = %#v, want exactly subscription 30 for participant-1 (leaving actor excluded)", targets)
	}
}

func TestWebPushTargetsForStartedUsesCurrentParticipantsExcludingHostAndHonorsPreference(t *testing.T) {
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"participant-1", "disabled-1", "host"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"participant-1": {{ID: 30}}, "disabled-1": {{ID: 31}}, "host": {{ID: 32}}},
		preferences: map[string]PushPreference{"participant-1": {WebPushEnabled: true}, "disabled-1": {WebPushEnabled: false}, "host": {WebPushEnabled: true}},
	}
	nicks := &fakeGameNickReader{nick: "unused"}
	transport := NewWebPushTransport(subs, subs, subs, &fakeGameFollowers{}, participants, &fakeGameRequestSummaryReader{}, nicks, "https://lansurfer.example", "", nil, nil, time.Now)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 4, GameRequestID: "req-1", ActorID: "host", Kind: ActivityStarted})
	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	wantKey := encodeWebPushTarget(30, "participant-1")
	if len(targets) != 1 || targets[0].Key != wantKey {
		t.Fatalf("Targets() for started = %#v, want only subscription 30 (host and disabled user excluded)", targets)
	}
	if len(nicks.calls) != 0 {
		t.Fatalf("Game Nick lookups = %#v, want none", nicks.calls)
	}
}

func TestWebPushTargetKeyRoundTripsAndSeparatesOwners(t *testing.T) {
	key := encodeWebPushTarget(42, "owner/α")
	subscriptionID, userID, err := decodeWebPushTarget(key)
	if err != nil {
		t.Fatalf("decodeWebPushTarget() error = %v", err)
	}
	if subscriptionID != 42 || userID != "owner/α" {
		t.Fatalf("decoded target = (%d, %q), want (42, %q)", subscriptionID, userID, "owner/α")
	}
	if key == encodeWebPushTarget(42, "another-owner") {
		t.Fatal("same subscription with different owner produced identical keys")
	}
}

func TestWebPushTargetDecoderRejectsMalformedKeys(t *testing.T) {
	for _, key := range []string{"", "1", "0:dXNlcg", "-1:dXNlcg", "01:dXNlcg", "1:", "1:!!!!", "1:dXNlcg==", "1:dXNlcg:extra"} {
		if _, _, err := decodeWebPushTarget(key); err == nil {
			t.Errorf("decodeWebPushTarget(%q) succeeded, want error", key)
		}
	}
}

func TestWebPushTargetsExcludesDisabledUsers(t *testing.T) {
	followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {"disabled-follower"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {GameID: "game-1"}}}
	subs := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"disabled-follower": {{ID: 40}}},
		preferences: map[string]PushPreference{"disabled-follower": {WebPushEnabled: false, NewRequestAudience: AudienceFollowed}},
	}
	transport := NewWebPushTransport(subs, subs, subs, followers, &fakeRequestParticipants{}, summaries, nil, "https://lansurfer.example", "", nil, nil, time.Now)

	targets, err := transport.Targets(t.Context(), Activity{Sequence: 3, GameRequestID: "req-1", ActorID: "someone-else", Kind: ActivityCreated})

	if err != nil {
		t.Fatalf("Targets() error = %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("Targets() = %#v, want none (the only follower has web_push_enabled=false)", targets)
	}
}
func TestWebPushActorGameNickLookupOnlyForJoinedAndLeft(t *testing.T) {
	reader := &fakeGameNickReader{nick: "Night Otter"}
	transport := &WebPushTransport{gameNicks: reader}
	for _, kind := range []ActivityKind{ActivityCreated, ActivityStarted, ActivityClosed, ActivityCancelled} {
		nick, err := transport.actorGameNick(t.Context(), Activity{Kind: kind, ActorID: "actor-1"})
		if err != nil || nick != "" {
			t.Errorf("actorGameNick(%s) = (%q, %v), want empty nick and no error", kind, nick, err)
		}
	}
	if len(reader.calls) != 0 {
		t.Fatalf("Game Nick lookups = %#v, want none for non-join/leave activities", reader.calls)
	}
	for _, kind := range []ActivityKind{ActivityJoined, ActivityLeft} {
		nick, err := transport.actorGameNick(t.Context(), Activity{Kind: kind, ActorID: "actor-1"})
		if err != nil || nick != "Night Otter" {
			t.Errorf("actorGameNick(%s) = (%q, %v), want actor's public Game Nick", kind, nick, err)
		}
	}
	if len(reader.calls) != 2 || reader.calls[0] != "actor-1" || reader.calls[1] != "actor-1" {
		t.Fatalf("Game Nick lookups = %#v, want actor lookup for joined and left", reader.calls)
	}
}

func TestWebPushTargetsForRequestUpdatedRequiresCurrentChangedTimeAndExcludesActor(t *testing.T) {
	previous := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	next := previous.Add(time.Hour)
	source := &fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"guest": {{ID: 3}}, "host": {{ID: 4}}},
		preferences: map[string]PushPreference{"guest": {WebPushEnabled: true}, "host": {WebPushEnabled: true}},
	}
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"host", "guest"}}}
	summaries := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{
		"req-1": {Status: "open", StartsAt: next},
	}}
	transport := NewWebPushTransport(source, source, source, nil, participants, summaries, nil, "https://lansurfer.example", "", nil, nil, time.Now)
	activity := Activity{GameRequestID: "req-1", ActorID: "host", Kind: ActivityUpdated, PreviousStartsAt: &previous, NewStartsAt: &next}
	targets, err := transport.Targets(t.Context(), activity)
	if err != nil || len(targets) != 1 || targets[0].Key != encodeWebPushTarget(3, "guest") {
		t.Fatalf("Targets() = %#v, %v; want guest only", targets, err)
	}

	for _, tc := range []struct {
		name string
		edit func()
	}{
		{"capacity or description only", func() { activity.NewStartsAt = &previous }},
		{"stale time", func() { summaries.summaries["req-1"] = GameRequestSummary{Status: "open", StartsAt: previous} }},
		{"non-open request", func() { summaries.summaries["req-1"] = GameRequestSummary{Status: "playing", StartsAt: next} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit()
			got, err := transport.Targets(t.Context(), activity)
			if err != nil || len(got) != 0 {
				t.Fatalf("Targets() = %#v, %v; want no targets", got, err)
			}
		})
		activity.NewStartsAt = &next
		summaries.summaries["req-1"] = GameRequestSummary{Status: "open", StartsAt: next}
	}
}

func TestWebPushTargetsForNonTimeUpdateDoesNotReadRecipients(t *testing.T) {
	failing := &failOnReadWebPushSource{t: t}
	transport := NewWebPushTransport(failing, failing, failing, failing, failing, failing, nil, "https://lansurfer.example", "", nil, nil, time.Now)
	for _, activity := range []Activity{
		{GameRequestID: "req-1", Kind: ActivityUpdated},
		{GameRequestID: "req-1", Kind: ActivityUpdated, PreviousStartsAt: new(time.Time), NewStartsAt: new(time.Time)},
	} {
		targets, err := transport.Targets(t.Context(), activity)
		if err != nil || len(targets) != 0 {
			t.Fatalf("Targets() = %#v, %v; want no targets", targets, err)
		}
	}
}

// failOnReadWebPushSource satisfies every repository the transport reads and
// fails the test on any call.
type failOnReadWebPushSource struct {
	fakeWebPushSubscriptionSource
	t *testing.T
}

func (f *failOnReadWebPushSource) ListSubscriptions(context.Context, string) ([]PushSubscriptionRecord, error) {
	f.t.Error("subscriptions were read for request_updated")
	return nil, nil
}
func (f *failOnReadWebPushSource) GetPreference(context.Context, string) (PushPreference, error) {
	f.t.Error("preferences were read for request_updated")
	return PushPreference{}, nil
}
func (f *failOnReadWebPushSource) ListUsersWithAudience(context.Context, string) ([]string, error) {
	f.t.Error("audience was read for request_updated")
	return nil, nil
}
func (f *failOnReadWebPushSource) ListFollowers(context.Context, string) ([]string, error) {
	f.t.Error("followers were read for request_updated")
	return nil, nil
}
func (f *failOnReadWebPushSource) ListParticipants(context.Context, string) ([]string, error) {
	f.t.Error("participants were read for request_updated")
	return nil, nil
}
func (f *failOnReadWebPushSource) GetGameRequestSummary(context.Context, string) (GameRequestSummary, error) {
	f.t.Error("summary was read for request_updated")
	return GameRequestSummary{}, nil
}
