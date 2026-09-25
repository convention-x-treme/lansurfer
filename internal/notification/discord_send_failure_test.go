package notification

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscordSendClaimedPersistenceCommitAndMonotonicFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edit   bool
		commit bool
		want   Outcome
	}{
		{"create commit", false, true, OutcomeUncertain},
		{"edit commit", true, true, OutcomeRetryable},
		{"create rejected advance", false, false, OutcomeUncertain},
		{"edit rejected advance", true, false, OutcomeRetryable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			if tc.edit {
				store.hasSync = true
				store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "existing-message", AppliedVersion: 41}
			}
			if tc.commit {
				store.commitErr = errors.New("commit lost")
			} else {
				store.advanceMiss = true
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method == http.MethodPost {
					_, _ = w.Write([]byte(`{"id":"created-message"}`))
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			result := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
			if result.Outcome != tc.want || calls.Load() != 1 {
				t.Fatalf("outcome = %v, calls = %d; want %v and one mutation", result.Outcome, calls.Load(), tc.want)
			}
			if tc.edit && store.sync.MessageID != "existing-message" || !tc.edit && store.hasSync {
				t.Fatal("failed persistence changed durable message reference")
			}
		})
	}
}

func TestDiscordSendClaimedReadFailureNeverCallsProvider(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.readErr = errors.New("unavailable")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeRetryable || calls.Load() != 0 {
		t.Fatalf("read failure outcome = %v, calls = %d", result.Outcome, calls.Load())
	}
}

func TestDiscordSendClaimedSupersedesNewerWaitingWork(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.newer = 43
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeAlreadyRecorded || !store.superseded || calls.Load() != 0 || store.hasSync {
		t.Fatalf("waiting work outcome = %v, superseded = %v, calls = %d", result.Outcome, store.superseded, calls.Load())
	}
}

// A committed reservation, not the claim lease, decides whether a create may
// run. Once Discord may have accepted a create whose message ID was not
// recorded, no automatic replay creates again for that destination/request.
// A definitive HTTP outcome such as a 5xx is not this case: it releases the
// reservation and is covered by TestDiscordSendClaimedReleasesRejectedCreate.
func TestDiscordSendClaimedNeverReplaysUnrecordedCreate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		commit bool
		want   Outcome
	}{
		{"accepted create with lost commit", http.StatusOK, true, OutcomeUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			if tc.commit {
				store.commitErr = errors.New("database unavailable")
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"id":"created-message"}`))
			}))
			defer server.Close()
			transport := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server))
			if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != tc.want || !store.reserved {
				t.Fatalf("first create outcome = %v, reserved = %v; want %v with retained reservation", result.Outcome, store.reserved, tc.want)
			}
			store.commitErr = nil
			// Lease-expiry replay of the same delivery, then newer work.
			for _, sequence := range []int64{42, 43} {
				delivery.Activity.Sequence = sequence
				result := transport.SendClaimed(t.Context(), delivery)
				if result.Outcome != OutcomeUncertain || !errors.Is(result.Err, errDiscordCreateUnrecorded) {
					t.Fatalf("replay %d outcome = %v, err = %v; want uncertain unrecorded create", sequence, result.Outcome, result.Err)
				}
			}
			if calls.Load() != 1 || store.hasSync {
				t.Fatalf("provider calls = %d, sync = %v; want exactly one create and no message reference", calls.Load(), store.hasSync)
			}
		})
	}
}

// A definitive Discord HTTP response to CREATE - a 4xx rejection or an
// explicit 5xx - is proof the create did not durably succeed. Both release
// the reservation and permit an immediate automatic retry, unlike an
// ambiguous outcome (TestDiscordSendClaimedNeverReplaysUnrecordedCreate).
func TestDiscordSendClaimedReleasesRejectedCreate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		retryAfter string
		want       Outcome
	}{
		{"4xx rejection", http.StatusBadRequest, "", OutcomePermanent},
		{"explicit 5xx", http.StatusBadGateway, "", OutcomeRetryable},
		{"explicit 5xx with retry-after", http.StatusServiceUnavailable, "30", OutcomeRetryable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					if tc.retryAfter != "" {
						w.Header().Set("Retry-After", tc.retryAfter)
					}
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(`{"id":"created-message"}`))
			}))
			defer server.Close()
			transport := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server))
			result := transport.SendClaimed(t.Context(), delivery)
			if result.Outcome != tc.want || store.reserved {
				t.Fatalf("rejected create outcome = %v, reserved = %v; want %v and released", result.Outcome, store.reserved, tc.want)
			}
			if tc.retryAfter != "" && (result.RetryAfter == nil || *result.RetryAfter != 30*time.Second) {
				t.Fatalf("rejected create retry-after = %v; want 30s", result.RetryAfter)
			}
			delivery.Activity.Sequence = 43
			if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeSucceeded || store.reserved || store.sync.MessageID != "created-message" {
				t.Fatalf("later create outcome = %v, reserved = %v, sync = %+v", result.Outcome, store.reserved, store.sync)
			}
		})
	}
}

func TestDiscordSendClaimedReservationFailureNeverCallsProvider(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.reserveErr = errors.New("database unavailable")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://lan.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeRetryable || calls.Load() != 0 || store.reserved {
		t.Fatalf("reservation failure outcome = %v, calls = %d, reserved = %v", result.Outcome, calls.Load(), store.reserved)
	}
}
