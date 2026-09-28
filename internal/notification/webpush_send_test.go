package notification

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

type sendSubscriptionSource struct {
	fakeWebPushSubscriptionSource
	record    PushSubscriptionRecord
	lookupErr error
	deletions []string
}

func (s *sendSubscriptionSource) GetSubscriptionByID(_ context.Context, id int64) (PushSubscriptionRecord, error) {
	if id != s.record.ID {
		return PushSubscriptionRecord{}, ErrDeliveryNotFound
	}
	return s.record, s.lookupErr
}

func (s *sendSubscriptionSource) DeleteSubscription(_ context.Context, id int64, owner string) (bool, error) {
	s.deletions = append(s.deletions, encodeWebPushTarget(id, owner))
	return true, nil
}

type recordingPushSender struct {
	calls                  int
	payload                []byte
	endpoint, p256dh, auth string
	result                 WebPushSendOutcome
}

func (s *recordingPushSender) Send(_ context.Context, endpoint, p256dh, auth string, payload []byte, ttl int) WebPushSendOutcome {
	s.calls++
	s.endpoint, s.p256dh, s.auth, s.payload = endpoint, p256dh, auth, payload
	if ttl != 300 {
		return WebPushSendOutcome{Outcome: OutcomePermanent, Err: errors.New("wrong TTL")}
	}
	return s.result
}

func TestWebPushSendOwnershipEligibilityAndPayload(t *testing.T) {
	key := make([]byte, platformcrypto.KeySize)
	seal := func(value string) []byte {
		ciphertext, err := platformcrypto.Encrypt(key, value)
		if err != nil {
			t.Fatal(err)
		}
		return ciphertext
	}
	record := PushSubscriptionRecord{ID: 10, UserID: "owner-1", EndpointCiphertext: seal("https://push.example/secret"), P256dhCiphertext: seal("browser-key"), AuthCiphertext: seal("browser-auth")}
	for _, tc := range []struct {
		name      string
		kind      ActivityKind
		target    string
		owner     string
		missing   bool
		corrupt   bool
		actorErr  bool
		leftGroup bool
		allowed   bool
		want      Outcome
		provider  WebPushSendOutcome
		deletions int
	}{
		{name: "success joined", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeSucceeded},
		{name: "success left", kind: ActivityLeft, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeSucceeded},
		{name: "success created", kind: ActivityCreated, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeSucceeded},
		{name: "success closed", kind: ActivityClosed, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeSucceeded},
		{name: "success cancelled", kind: ActivityCancelled, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeSucceeded},
		{name: "reassigned owner", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-2", allowed: true, want: OutcomePermanent},
		{name: "deleted", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", missing: true, allowed: true, want: OutcomePermanent},
		{name: "legacy numeric", kind: ActivityJoined, target: "10", owner: "owner-1", allowed: true, want: OutcomePermanent},
		{name: "malformed", kind: ActivityJoined, target: "bad-key", owner: "owner-1", allowed: true, want: OutcomePermanent},
		{name: "disabled", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", want: OutcomePermanent},
		{name: "left membership", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, leftGroup: true, want: OutcomePermanent},
		{name: "actor lookup fails", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, actorErr: true, want: OutcomeRetryable},
		{name: "decrypt failure", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", corrupt: true, allowed: true, want: OutcomeRetryable},
		{name: "expired", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomePermanent, provider: WebPushSendOutcome{Outcome: OutcomePermanent, Expired: true}, deletions: 1},
		{name: "non-expiry permanent", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomePermanent, provider: WebPushSendOutcome{Outcome: OutcomePermanent}},
		{name: "retry after", kind: ActivityJoined, target: encodeWebPushTarget(10, "owner-1"), owner: "owner-1", allowed: true, want: OutcomeRetryable, provider: WebPushSendOutcome{Outcome: OutcomeRetryable, RetryAfter: new(17 * time.Minute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := record
			sub.UserID = tc.owner
			if tc.corrupt {
				sub.AuthCiphertext = []byte("corrupt")
			}
			source := &sendSubscriptionSource{record: sub}
			if tc.missing {
				source.lookupErr = ErrDeliveryNotFound
			}
			if tc.allowed {
				source.subsByUser = map[string][]PushSubscriptionRecord{tc.owner: {sub}}
				source.preferences = map[string]PushPreference{tc.owner: {WebPushEnabled: true, NewRequestAudience: AudienceFollowed}}
			}
			summary := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"request-1": {GameID: "game-1", GameName: "Stale Game"}}}
			participants := &fakeRequestParticipants{participants: map[string][]string{"request-1": {tc.owner}}}
			followers := &fakeGameFollowers{followers: map[string][]string{"game-1": {tc.owner}}}
			nicks := &fakeGameNickReader{nick: "Night Otter"}
			if tc.actorErr {
				nicks.err = errors.New("user read failed")
			}
			sender := &recordingPushSender{result: tc.provider}
			transport := &WebPushTransport{subscriptions: source, preferences: source, delivery: source, followers: followers, participants: participants, summaries: summary, gameNicks: nicks, baseURL: "https://play.example", instanceName: "Friday Club", encryptionKey: key, sender: sender}
			activity := Activity{Kind: tc.kind, GameRequestID: "request-1", ActorID: "actor-2"}
			if tc.allowed && !tc.missing && tc.owner == "owner-1" && tc.target == encodeWebPushTarget(10, "owner-1") {
				if _, err := transport.Targets(t.Context(), activity); err != nil {
					t.Fatal(err)
				}
			}
			if tc.leftGroup {
				participants.participants["request-1"] = nil
			}
			summary.summaries["request-1"] = GameRequestSummary{GameID: "game-1", GameName: "Fresh Game"}
			result := transport.Send(t.Context(), activity, Target{Transport: "webpush", Key: tc.target})
			if result.Outcome != tc.want || (tc.want == OutcomePermanent && tc.name != "non-expiry permanent" && tc.name != "expired" && result.Err == nil) {
				t.Fatalf("result = %+v, want %v", result, tc.want)
			}
			wantCalls := 0
			if tc.allowed && !tc.missing && !tc.corrupt && !tc.actorErr && !tc.leftGroup && tc.owner == "owner-1" && tc.target == encodeWebPushTarget(10, "owner-1") {
				wantCalls = 1
			}
			if sender.calls != wantCalls || len(source.deletions) != tc.deletions {
				t.Fatalf("provider calls=%d deletions=%v, want calls=%d deletions=%d", sender.calls, source.deletions, wantCalls, tc.deletions)
			}
			if tc.deletions == 1 && source.deletions[0] != encodeWebPushTarget(10, "owner-1") {
				t.Fatalf("deleted wrong subscription: %v", source.deletions)
			}
			if tc.provider.RetryAfter != nil && (result.RetryAfter == nil || *result.RetryAfter != *tc.provider.RetryAfter) {
				t.Fatalf("Retry-After changed: %v", result.RetryAfter)
			}
			if wantCalls == 0 {
				return
			}
			if sender.endpoint != "https://push.example/secret" || sender.p256dh != "browser-key" || sender.auth != "browser-auth" {
				t.Fatal("sender did not receive decrypted subscription")
			}
			var payload map[string]any
			if err := json.Unmarshal(sender.payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["game"] != "Fresh Game" || payload["instance"] != "Friday Club" {
				t.Fatalf("not fresh payload: %v", payload)
			}
			if tc.kind == ActivityJoined || tc.kind == ActivityLeft {
				if payload["actor_game_nick"] != "Night Otter" || !reflect.DeepEqual(nicks.calls, []string{"actor-2"}) {
					t.Fatalf("missing actor Game Nick: %v, calls %v", payload, nicks.calls)
				}
			} else if _, present := payload["actor_game_nick"]; present || len(nicks.calls) > 0 {
				t.Fatalf("actor lookup for %s: %v, calls %v", tc.kind, payload, nicks.calls)
			}
		})
	}
}
