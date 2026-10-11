package notification

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A known message edit that receives Discord 10008 Unknown Message must
// compare-and-clear the exact stale reference, keep the destination
// connected, and create exactly one replacement message from the latest
// current snapshot, persisting its ID through the normal monotonic sync
// semantics so later activity edits the replacement instead of creating a
// second one.
func TestDiscordSendClaimedRecoversDeletedMessageWithOneReplacementCreate(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
	store.hasSync = true
	delivery.Activity.Sequence = 42

	var methods []string
	var recovered atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPatch && !recovered.Load() {
			recovered.Store(true)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"message-replacement"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("recovery outcome = %v, want succeeded", result.Outcome)
	}
	if len(methods) != 2 || methods[0] != http.MethodPatch || methods[1] != http.MethodPost {
		t.Fatalf("provider calls = %v, want one failed edit then one create", methods)
	}
	if store.destination.State != DiscordDestinationConnected {
		t.Fatal("10008 recovery must keep the destination connected")
	}
	if store.sync.MessageID != "message-replacement" || store.sync.AppliedVersion != 42 {
		t.Fatalf("recovered sync = %+v, want replacement message at version 42", store.sync)
	}

	// Later activity edits the replacement message rather than creating
	// another one.
	delivery.Activity.Sequence = 43
	methods = nil
	if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeSucceeded {
		t.Fatalf("post-recovery edit outcome = %v, want succeeded", result.Outcome)
	}
	if len(methods) != 1 || methods[0] != http.MethodPatch {
		t.Fatalf("post-recovery provider calls = %v, want exactly one edit", methods)
	}
	if store.sync.MessageID != "message-replacement" {
		t.Fatal("later activity must edit the replacement message, not create a new one")
	}
}

// The replacement create must render the latest current snapshot, not the
// snapshot captured before the failed edit, and must never replay
// intermediate historical activity content.
func TestDiscordSendClaimedRecoveryUsesLatestSnapshotNotHistoricalActivity(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
	store.hasSync = true
	delivery.Activity.Sequence, delivery.Activity.Kind = 42, ActivityJoined

	var createBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			// Mutate the current snapshot only after the failed edit reads
			// it, so a success here proves the replacement re-resolved the
			// latest state instead of reusing the pre-failure summary.
			store.summary.Status, store.summary.GameName = "cancelled", "Renamed Game"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		createBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"message-replacement"}`)
	}))
	defer server.Close()

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeSucceeded {
		t.Fatalf("recovery outcome = %v, want succeeded", result.Outcome)
	}
	assertDiscordSendPayload(t, createBody, "cancelled", "Renamed Game", "Fresh snapshot")
}

// The compare-and-clear must be conditional on the exact stale message ID
// and expected applied version. PostgreSQL's advisory lock already
// serializes concurrent access to one destination + Game Request, so a
// mismatch can only mean this defensive invariant guard fired; the send
// must fail safely without ever attempting a replacement create in that
// case, and the whole synchronization transaction (including the failed
// clear) must roll back rather than leave a partial state.
func TestDiscordSendClaimedRecoveryNeverCreatesAfterFailedCompareAndClear(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
	store.hasSync = true
	store.clearErr = nil // exercised via a forced mismatch below, not an error
	delivery.Activity.Sequence = 42

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPatch {
			t.Fatal("recovery must not create a replacement after a failed compare-and-clear")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
	}))
	defer server.Close()
	// Force the compare-and-clear to observe a mismatch, as a real
	// invariant guard would if the read and the clear somehow disagreed.
	store.clearErr = errors.New("discord message reference already changed")

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome == OutcomeSucceeded || calls.Load() != 1 {
		t.Fatalf("outcome = %v, calls = %d; want a failure with no replacement create", result.Outcome, calls.Load())
	}
	if store.sync.MessageID != "message-deleted" || store.sync.AppliedVersion != 41 {
		t.Fatalf("failed clear must leave the original reference untouched: %+v", store.sync)
	}
}

// A stored sync row whose message slot was already cleared by a prior
// recovery, at exactly the version now being processed, is not "already
// applied": the delivery must still attempt the replacement create rather
// than being marked superseded merely because the version already matches.
func TestDiscordSendClaimedEqualVersionEmptySlotRecoveryIsNotSuperseded(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "", AppliedVersion: 42}
	store.hasSync = true
	delivery.Activity.Sequence = 42

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"message-filled"}`)
	}))
	defer server.Close()

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeSucceeded || store.superseded {
		t.Fatalf("equal-version empty-slot outcome = %v, superseded = %v; want succeeded create, never superseded", result.Outcome, store.superseded)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want exactly one create", calls.Load())
	}
	if store.sync.MessageID != "message-filled" || store.sync.AppliedVersion != 42 {
		t.Fatalf("filled sync = %+v, want message-filled at version 42", store.sync)
	}
}

// Conversely, an older version must never be allowed to replace/refill a
// newer one: a genuinely stale delivery for a lower sequence than the
// stored applied version still supersedes normally.
func TestDiscordSendClaimedOlderVersionCannotFillNewerEmptySlot(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "", AppliedVersion: 43}
	store.hasSync = true
	delivery.Activity.Sequence = 42

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeAlreadyRecorded || !store.superseded || calls.Load() != 0 {
		t.Fatalf("stale delivery outcome = %v, superseded = %v, calls = %d; want superseded without a provider call", result.Outcome, store.superseded, calls.Load())
	}
	if store.sync.MessageID != "" || store.sync.AppliedVersion != 43 {
		t.Fatalf("stale delivery changed durable sync: %+v", store.sync)
	}
}

// A replacement create is externally non-idempotent exactly like an
// initial create: an ambiguous outcome (no definitive HTTP response) must
// retain the durable reservation and never auto-replay.
func TestDiscordSendClaimedRecoveryAmbiguousCreateRetainsReservationAndNeverReplays(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
	store.hasSync = true
	delivery.Activity.Sequence = 42

	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
			return
		}
		posts.Add(1)
		// The replacement create's response is lost after transmission.
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeUncertain || !store.reserved {
		t.Fatalf("ambiguous replacement create outcome = %v, reserved = %v; want uncertain with a retained reservation", result.Outcome, store.reserved)
	}
	// The whole synchronization transaction (including the recovery clear)
	// rolls back with the ambiguous outcome, exactly like a real PostgreSQL
	// transaction that never commits: only the reservation, which commits on
	// its own connection before the provider call, survives.
	if store.sync.MessageID != "message-deleted" || store.sync.AppliedVersion != 41 {
		t.Fatalf("ambiguous replacement create must not durably record a new reference: %+v", store.sync)
	}
	if posts.Load() != 1 {
		t.Fatalf("replacement create calls = %d, want exactly one", posts.Load())
	}

	// A lease-expiry-style replay of the same delivery may re-discover the
	// still-deleted message and clear it again, but the retained reservation
	// must block a second create: no second POST ever reaches the provider.
	result = transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeUncertain || !errors.Is(result.Err, errDiscordCreateUnrecorded) {
		t.Fatalf("replay outcome = %v, err = %v; want uncertain unrecorded create", result.Outcome, result.Err)
	}
	if posts.Load() != 1 {
		t.Fatalf("replacement create calls after replay = %d, want still exactly one", posts.Load())
	}
}

// A replacement create that Discord accepted, followed by a lost sync
// commit, must also retain the reservation and never auto-replay: this is
// the same durability guarantee as an initial create, applied to recovery.
func TestDiscordSendClaimedRecoverySuccessfulCreateWithLostPersistenceNeverReplays(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
	store.hasSync = true
	delivery.Activity.Sequence = 42

	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"message-replacement"}`)
	}))
	defer server.Close()
	store.commitErr = errors.New("commit lost")

	transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
	result := transport.SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeUncertain || !store.reserved {
		t.Fatalf("lost-persistence outcome = %v, reserved = %v; want uncertain with a retained reservation", result.Outcome, store.reserved)
	}
	if len(methods) != 2 {
		t.Fatalf("provider calls = %v, want one failed edit then one create", methods)
	}
	if store.sync.MessageID != "message-deleted" {
		t.Fatal("lost-persistence create must not durably record the replacement reference")
	}

	store.commitErr = nil
	methods = nil
	if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeUncertain || !errors.Is(result.Err, errDiscordCreateUnrecorded) {
		t.Fatalf("replay outcome = %v, err = %v; want uncertain unrecorded create", result.Outcome, result.Err)
	}
	// The reverted reference still looks deleted, so replay re-discovers
	// 10008 and re-clears it, but the retained reservation must block a
	// second create: no second POST ever reaches the provider.
	for _, method := range methods {
		if method == http.MethodPost {
			t.Fatal("replay must never create a second replacement while the reservation is unresolved")
		}
	}
}

// An explicit 429/5xx to the replacement create is a definitive provider
// rejection: it releases the reservation and permits an immediate
// automatic retry, exactly like an initial create.
func TestDiscordSendClaimedRecoveryExplicitRejectionReleasesReservationAndRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"429", http.StatusTooManyRequests},
		{"explicit 5xx", http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-deleted", AppliedVersion: 41}
			store.hasSync = true
			delivery.Activity.Sequence = 42

			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPatch {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"code":10008,"message":"Unknown Message"}`)
					return
				}
				if attempts.Add(1) == 1 {
					w.WriteHeader(tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"message-replacement"}`)
			}))
			defer server.Close()

			transport := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server))
			result := transport.SendClaimed(t.Context(), delivery)
			if result.Outcome != OutcomeRetryable || store.reserved {
				t.Fatalf("rejected replacement create outcome = %v, reserved = %v; want retryable and released", result.Outcome, store.reserved)
			}

			// The clear already succeeded, so the immediate retry must go
			// straight to a fresh create attempt (empty message slot), not
			// another edit.
			result = transport.SendClaimed(t.Context(), delivery)
			if result.Outcome != OutcomeSucceeded || store.sync.MessageID != "message-replacement" {
				t.Fatalf("retried recovery outcome = %v, sync = %+v; want succeeded with replacement recorded", result.Outcome, store.sync)
			}
		})
	}
}
