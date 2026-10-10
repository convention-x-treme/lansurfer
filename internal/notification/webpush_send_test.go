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
	ttl                    int
	result                 WebPushSendOutcome
	beforeSend             func()
}

func (s *recordingPushSender) Send(_ context.Context, endpoint, p256dh, auth string, payload []byte, ttl int) WebPushSendOutcome {
	if s.beforeSend != nil {
		s.beforeSend()
	}
	s.calls++
	s.endpoint, s.p256dh, s.auth, s.payload, s.ttl = endpoint, p256dh, auth, payload, ttl
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
			status := "open"
			if tc.kind == ActivityClosed {
				status = "closed"
			} else if tc.kind == ActivityCancelled {
				status = "cancelled"
			}
			summary.summaries["request-1"] = GameRequestSummary{GameID: "game-1", GameName: "Fresh Game", Status: status}
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

func TestWebPushSendPlannedTimeChangeUsesMinimalPayloadAndSuppressesStaleTime(t *testing.T) {
	key := make([]byte, platformcrypto.KeySize)
	seal := func(value string) []byte {
		ciphertext, err := platformcrypto.Encrypt(key, value)
		if err != nil {
			t.Fatal(err)
		}
		return ciphertext
	}
	sub := PushSubscriptionRecord{ID: 10, UserID: "guest", EndpointCiphertext: seal("https://push.example/secret"), P256dhCiphertext: seal("p256dh"), AuthCiphertext: seal("auth")}
	source := &sendSubscriptionSource{record: sub, fakeWebPushSubscriptionSource: fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"guest": {sub}},
		preferences: map[string]PushPreference{"guest": {WebPushEnabled: true}},
	}}
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"guest"}}}
	previous := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	next := previous.Add(time.Hour)
	summary := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{"req-1": {
		Status: "open", StartsAt: next, GameName: "Chess", MemberCount: 4, Description: "private",
	}}}
	sender := &recordingPushSender{result: WebPushSendOutcome{Outcome: OutcomeSucceeded}}
	transport := &WebPushTransport{
		subscriptions: source, preferences: source, delivery: source, participants: participants, summaries: summary,
		baseURL: "https://play.example", instanceName: "Club", encryptionKey: key, sender: sender,
	}
	activity := Activity{GameRequestID: "req-1", ActorID: "host", Kind: ActivityUpdated, PreviousStartsAt: &previous, NewStartsAt: &next}
	target := Target{Transport: "webpush", Key: encodeWebPushTarget(sub.ID, sub.UserID)}
	result := transport.Send(t.Context(), activity, target)
	if result.Outcome != OutcomeSucceeded || sender.calls != 1 {
		t.Fatalf("Send() = %#v, calls %d; want success and one provider call", result, sender.calls)
	}
	var got map[string]any
	if err := json.Unmarshal(sender.payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || got["kind"] != "planned_start_changed" || got["starts_at"] != next.Format(time.RFC3339) {
		t.Fatalf("payload = %#v; want six-field planned_start_changed payload", got)
	}
	sender.result = WebPushSendOutcome{Outcome: OutcomeRetryable, Err: errors.New("provider unavailable")}
	result = transport.Send(t.Context(), activity, target)
	if result.Outcome != OutcomeRetryable || sender.calls != 2 {
		t.Fatalf("failed Send() = %#v, calls %d; want retryable and two provider calls", result, sender.calls)
	}

	summary.summaries["req-1"] = GameRequestSummary{Status: "open", StartsAt: next.Add(time.Minute)}
	result = transport.Send(t.Context(), activity, target)
	if result.Outcome != OutcomeNoLongerEligible || sender.calls != 2 {
		t.Fatalf("stale Send() = %#v, calls %d; want no-longer-eligible without another provider call", result, sender.calls)
	}
}
func TestWebPushReminderEligibilityTTLAndProviderAcceptance(t *testing.T) {
	key := make([]byte, platformcrypto.KeySize)
	seal := func(value string) []byte {
		ciphertext, err := platformcrypto.Encrypt(key, value)
		if err != nil {
			t.Fatal(err)
		}
		return ciphertext
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	startsAt := now.Add(14*time.Minute + 59*time.Second)
	sub := PushSubscriptionRecord{ID: 10, UserID: "host", EndpointCiphertext: seal("https://push.example/endpoint"), P256dhCiphertext: seal("p256dh"), AuthCiphertext: seal("auth")}
	source := &sendSubscriptionSource{record: sub, fakeWebPushSubscriptionSource: fakeWebPushSubscriptionSource{
		subsByUser:  map[string][]PushSubscriptionRecord{"host": {sub}},
		preferences: map[string]PushPreference{"host": {WebPushEnabled: true}},
	}}
	participants := &fakeRequestParticipants{participants: map[string][]string{"req-1": {"host", "guest", "guest"}}}
	summary := &fakeGameRequestSummaryReader{summaries: map[string]GameRequestSummary{
		"req-1": {Status: "open", StartsAt: startsAt, GameName: "Chess"},
	}}
	sender := &recordingPushSender{result: WebPushSendOutcome{Outcome: OutcomeRetryable, AcceptanceUnknown: true}}
	reservations := 0
	sender.beforeSend = func() {
		if reservations == 0 {
			t.Fatal("provider send started before durable reservation")
		}
	}
	reserve := func(context.Context) (bool, error) {
		reservations++
		return true, nil
	}
	transport := NewWebPushTransport(source, source, source, nil, participants, summary, nil, "https://play.example", "Club", key, sender, func() time.Time { return now })
	reminder := StartReminder{GameRequestID: "req-1", StartsAt: startsAt}
	targets, err := transport.ReminderTargets(t.Context(), reminder)
	if err != nil || len(targets) != 1 || targets[0].Key != encodeWebPushTarget(sub.ID, "host") {
		t.Fatalf("ReminderTargets() = %#v, %v; want Host subscription", targets, err)
	}
	result := transport.SendReminder(t.Context(), reminder, targets[0], reserve)
	if result.Outcome != OutcomeUncertain || sender.calls != 1 || sender.ttl != 300 || reservations != 1 {
		t.Fatalf("SendReminder() = %+v, calls %d TTL %d reservations %d; want uncertain, one send/reservation, TTL 300", result, sender.calls, sender.ttl, reservations)
	}
	var payload map[string]any
	if err := json.Unmarshal(sender.payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["kind"] != "starting_soon" || payload["starts_at"] != startsAt.Format(time.RFC3339) {
		t.Fatalf("reminder payload = %v", payload)
	}
	for _, tc := range []struct {
		name       string
		status     string
		reminderAt time.Time
		summaryAt  time.Time
	}{
		{"at start", "open", now, now},
		{"past start", "open", now.Add(-time.Second), now.Add(-time.Second)},
		{"outside window", "open", now.Add(StartReminderLead + time.Second), now.Add(StartReminderLead + time.Second)},
		{"playing", "playing", startsAt, startsAt},
		{"rescheduled", "open", startsAt, startsAt.Add(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := StartReminder{GameRequestID: "req-1", StartsAt: tc.reminderAt}
			summary.summaries["req-1"] = GameRequestSummary{Status: tc.status, StartsAt: tc.summaryAt}
			before := sender.calls
			got := transport.SendReminder(t.Context(), candidate, targets[0], reserve)
			gotTargets, targetErr := transport.ReminderTargets(t.Context(), candidate)
			if got.Outcome != OutcomeNoLongerEligible || sender.calls != before || targetErr != nil || len(gotTargets) != 0 {
				t.Fatalf("SendReminder() = %+v, targets %#v, error %v, calls %d; want suppressed without provider call", got, gotTargets, targetErr, sender.calls)
			}
		})
	}
	capped := StartReminder{GameRequestID: "req-1", StartsAt: now.Add(14 * time.Minute)}
	summary.summaries["req-1"] = GameRequestSummary{Status: "open", StartsAt: capped.StartsAt}
	sender.result = WebPushSendOutcome{Outcome: OutcomeRetryable}
	result = transport.SendReminder(t.Context(), capped, targets[0], reserve)
	if result.Outcome != OutcomeRetryable || sender.calls != 2 || sender.ttl != 300 {
		t.Fatalf("known retry = %+v, calls %d TTL %d; want retryable, one additional call, TTL 300", result, sender.calls, sender.ttl)
	}
}

type failingReminderSummaryReader struct{ calls int }

func (r *failingReminderSummaryReader) GetGameRequestSummary(context.Context, string) (GameRequestSummary, error) {
	r.calls++
	return GameRequestSummary{}, errors.New("summary should not be read")
}

func TestWebPushReminderRejectsMismatchedTransportBeforeReservation(t *testing.T) {
	summary := &failingReminderSummaryReader{}
	sender := &recordingPushSender{}
	transport := &WebPushTransport{summaries: summary, sender: sender}
	reservations := 0
	result := transport.SendReminder(
		t.Context(),
		StartReminder{GameRequestID: "request-1", StartsAt: time.Now().Add(time.Minute)},
		Target{Transport: "discord", Key: encodeWebPushTarget(1, "user-1")},
		func(context.Context) (bool, error) {
			reservations++
			return true, nil
		},
	)
	if result.Outcome != OutcomeNoLongerEligible || !errors.Is(result.Err, ErrNoLongerEligible) {
		t.Fatalf("SendReminder() = %+v; want suppressed mismatched target", result)
	}
	if summary.calls != 0 || reservations != 0 || sender.calls != 0 {
		t.Fatalf("summary reads=%d reservations=%d provider calls=%d; want all zero", summary.calls, reservations, sender.calls)
	}
}
