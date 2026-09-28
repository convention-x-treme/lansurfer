package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	discordSendDestinationID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	discordSendRequestID     = "11111111-1111-4111-8111-111111111111"
	discordSendSecret        = "webhook-token-secret"
)

type discordSendStore struct {
	destination DiscordDestination
	ciphertext  DiscordCredentialCiphertext
	summary     GameRequestSummary
	sync        DiscordMessageSync
	hasSync     bool
	lockErr     error
	advanceErr  error
	clearErr    error
	commitErr   error
	readErr     error
	advanceMiss bool
	newer       int64
	locked      atomic.Bool
	lockCalls   int
	superseded  bool
	// reserved commits immediately, like the pooled reservation insert;
	// releases apply only when the sync transaction commits.
	reserved         bool
	reserveErr       error
	reservationOwner int64
	releasePending   bool
	// E3-20 broken-destination/suppression tracking.
	markBrokenErr       error
	markBrokenMismatch  bool
	brokenReason        string
	suppressCalls       int
	suppressedTargetKey string
	suppressedExcludeID int64
	suppressedReason    string
	suppressErr         error
}

func (s *discordSendStore) MarkDiscordDestinationBroken(_ context.Context, destinationID, reason string) (bool, error) {
	if !s.locked.Load() {
		return false, errors.New("mark broken outside lock")
	}
	if s.markBrokenErr != nil {
		return false, s.markBrokenErr
	}
	if s.markBrokenMismatch || destinationID != s.destination.ID || s.destination.State != DiscordDestinationConnected {
		return false, nil
	}
	s.destination.State, s.ciphertext = DiscordDestinationBroken, nil
	s.brokenReason = reason
	return true, nil
}

func (s *discordSendStore) RecordPermanent(_ context.Context, _ int64, _ string) error {
	if !s.locked.Load() {
		return errors.New("record permanent outside lock")
	}
	return nil
}

func (s *discordSendStore) SuppressOtherDiscordDeliveries(_ context.Context, targetKey string, excludeDeliveryID int64, reason string) (int64, error) {
	if !s.locked.Load() {
		return 0, errors.New("suppress outside lock")
	}
	if s.suppressErr != nil {
		return 0, s.suppressErr
	}
	s.suppressCalls++
	s.suppressedTargetKey, s.suppressedExcludeID, s.suppressedReason = targetKey, excludeDeliveryID, reason
	return 1, nil
}

func (s *discordSendStore) ReserveDiscordCreate(_ context.Context, destinationID, requestID string, deliveryID int64) (bool, error) {
	if !s.locked.Load() || destinationID != discordSendDestinationID || requestID != discordSendRequestID {
		return false, errors.New("wrong reservation key or unlocked reservation")
	}
	if s.reserveErr != nil {
		return false, s.reserveErr
	}
	if s.reserved {
		return false, nil
	}
	s.reserved = true
	s.reservationOwner = deliveryID
	return true, nil
}

func (s *discordSendStore) ReleaseDiscordCreate(_ context.Context, destinationID, requestID string, deliveryID int64) (bool, error) {
	if !s.locked.Load() || destinationID != discordSendDestinationID || requestID != discordSendRequestID || !s.reserved || s.reservationOwner != deliveryID {
		return false, nil
	}
	s.releasePending = true
	return true, nil
}

func (s *discordSendStore) GetDiscordDestination(context.Context) (DiscordDestination, DiscordCredentialCiphertext, error) {
	return s.destination, s.ciphertext, nil
}

func (s *discordSendStore) GetGameRequestSummary(_ context.Context, requestID string) (GameRequestSummary, error) {
	if requestID != s.summary.RequestID {
		return GameRequestSummary{}, errors.New("request not found")
	}
	return s.summary, nil
}

func (s *discordSendStore) WithDiscordMessageSyncLock(_ context.Context, destinationID, requestID string, apply func(DiscordMessageSyncRepository) error) error {
	s.lockCalls++
	if destinationID != discordSendDestinationID || requestID != discordSendRequestID {
		return errors.New("wrong sync lock key")
	}
	if s.lockErr != nil {
		return s.lockErr
	}
	before, hadSync := s.sync, s.hasSync
	beforeDestination, beforeCiphertext := s.destination, s.ciphertext
	s.releasePending = false
	s.locked.Store(true)
	defer s.locked.Store(false)
	if err := apply(s); err != nil {
		s.sync, s.hasSync = before, hadSync
		s.destination, s.ciphertext = beforeDestination, beforeCiphertext
		return err
	}
	if s.commitErr != nil {
		s.sync, s.hasSync = before, hadSync
		s.destination, s.ciphertext = beforeDestination, beforeCiphertext
		return s.commitErr
	}
	if s.releasePending {
		s.reserved = false
	}
	return nil
}

func (s *discordSendStore) GetDiscordMessageSync(_ context.Context, destinationID, requestID string) (DiscordMessageSync, error) {
	if !s.locked.Load() {
		return DiscordMessageSync{}, errors.New("sync read outside lock")
	}
	if destinationID != discordSendDestinationID || requestID != discordSendRequestID {
		return DiscordMessageSync{}, errors.New("wrong sync key")
	}
	if s.readErr != nil {
		return DiscordMessageSync{}, s.readErr
	}
	if !s.hasSync {
		return DiscordMessageSync{}, ErrDiscordMessageSyncNotFound
	}
	return s.sync, nil
}

func (s *discordSendStore) AdvanceDiscordMessageSync(_ context.Context, sync DiscordMessageSync) (bool, error) {
	if !s.locked.Load() {
		return false, errors.New("sync write outside lock")
	}
	if s.advanceErr != nil {
		return false, s.advanceErr
	}
	if s.advanceMiss {
		return false, nil
	}
	if s.hasSync && (sync.AppliedVersion < s.sync.AppliedVersion || (sync.AppliedVersion == s.sync.AppliedVersion && s.sync.MessageID != "")) {
		return false, nil
	}
	s.sync, s.hasSync = sync, true
	return true, nil
}

func (s *discordSendStore) ClearDiscordMessageSync(_ context.Context, destinationID, requestID, messageID string, appliedVersion int64) (bool, error) {
	if !s.locked.Load() {
		return false, errors.New("clear outside lock")
	}
	if destinationID != discordSendDestinationID || requestID != discordSendRequestID {
		return false, errors.New("wrong clear key")
	}
	if s.clearErr != nil {
		return false, s.clearErr
	}
	if !s.hasSync || s.sync.MessageID != messageID || s.sync.AppliedVersion != appliedVersion {
		return false, nil
	}
	s.sync.MessageID = ""
	return true, nil
}

func (s *discordSendStore) ClaimedDiscordDelivery(_ context.Context, id, sequence int64, requestID string, target Target) (int64, error) {
	if !s.locked.Load() || id != 27 || sequence < 1 || requestID != discordSendRequestID || target.Key != "d1:"+discordSendDestinationID {
		return 0, errors.New("wrong claimed delivery or unlocked query")
	}
	return id, nil
}

func (s *discordSendStore) NewerDiscordDelivery(context.Context, int64, string, Target) (int64, error) {
	if !s.locked.Load() {
		return 0, errors.New("newer delivery read outside lock")
	}
	return s.newer, nil
}

func (s *discordSendStore) RecordSuperseded(_ context.Context, deliveryID, version int64) (bool, error) {
	if !s.locked.Load() {
		return false, errors.New("supersession outside lock")
	}
	if deliveryID != 27 || version != 43 {
		return false, errors.New("wrong superseded delivery or version")
	}
	s.superseded = true
	return true, nil
}

func discordSendFixture(t *testing.T) (*discordSendStore, ClaimedDelivery, []byte) {
	t.Helper()
	key := testDiscordKey(41)
	ciphertext, err := EncryptDiscordCredential(key, DiscordWebhookCredential{WebhookID: "webhook-id", WebhookToken: discordSendSecret})
	if err != nil {
		t.Fatal(err)
	}
	store := &discordSendStore{
		destination: DiscordDestination{ID: discordSendDestinationID, State: DiscordDestinationConnected},
		ciphertext:  ciphertext,
		summary: GameRequestSummary{
			RequestID: discordSendRequestID, GameName: "Current Chess", Status: "closed", MemberCount: 3,
			CreatorGameNick: "Current Nick", Description: "Fresh snapshot",
		},
	}
	return store, ClaimedDelivery{
		DeliveryID: 27, Target: Target{Transport: DiscordTransportName, Key: "d1:" + discordSendDestinationID},
		Activity: Activity{Sequence: 42, GameRequestID: discordSendRequestID, Kind: ActivityCreated},
	}, key
}

// Redirect only the HTTP transport to the local server. The send still builds
// the real Discord webhook URL and the embed still uses the app's base URL.
func discordSendClient(server *httptest.Server) *http.Client {
	client := server.Client()
	upstream := client.Transport
	local, _ := url.Parse(server.URL)
	client.Transport = discordRoundTrip(func(request *http.Request) (*http.Response, error) {
		localRequest := request.Clone(request.Context())
		localRequest.URL.Scheme, localRequest.URL.Host, localRequest.Host = local.Scheme, local.Host, local.Host
		return upstream.RoundTrip(localRequest)
	})
	return client
}

type discordSendRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
	locked bool
}

func TestDiscordSendClaimedCreatesCurrentStateAndEditsSameMessage(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	requests := make(chan discordSendRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- discordSendRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body, locked: store.locked.Load()}
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"message-42"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	transport := NewDiscordSendTransport(store, key, "https://play.example/", "Friday Club", discordSendClient(server))

	if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeSucceeded {
		t.Fatalf("create outcome = %v, want succeeded", result.Outcome)
	}
	if len(requests) != 1 {
		t.Fatalf("create generated %d provider requests, want one", len(requests))
	}
	created := <-requests
	if created.method != http.MethodPost || created.query.Get("wait") != "true" || !created.locked || !strings.HasSuffix(created.path, "/webhook-id/"+discordSendSecret) {
		t.Fatalf("create request method = %s, wait = %q, locked = %v; expected POST with webhook path", created.method, created.query.Get("wait"), created.locked)
	}
	assertDiscordSendPayload(t, created.body, "closed", "Current Chess", "Fresh snapshot")
	if store.reserved || !store.hasSync || store.sync.MessageID != "message-42" || store.sync.AppliedVersion != 42 || store.sync.DestinationID != discordSendDestinationID || store.sync.GameRequestID != discordSendRequestID {
		t.Fatalf("create did not save reference/version: %+v", store.sync)
	}

	store.summary.GameName, store.summary.Status, store.summary.Description = "Updated Chess", "cancelled", "Newest snapshot"
	delivery.Activity.Sequence, delivery.Activity.Kind = 43, ActivityCancelled
	if result := transport.SendClaimed(t.Context(), delivery); result.Outcome != OutcomeSucceeded {
		t.Fatalf("edit outcome = %v, want succeeded", result.Outcome)
	}
	if len(requests) != 1 {
		t.Fatalf("edit generated %d provider requests, want one", len(requests))
	}
	edited := <-requests
	if edited.method != http.MethodPatch || !edited.locked || !strings.HasSuffix(edited.path, "/webhook-id/"+discordSendSecret+"/messages/message-42") {
		t.Fatalf("edit request method = %s, locked = %v; expected PATCH with saved message path", edited.method, edited.locked)
	}
	assertDiscordSendPayload(t, edited.body, "cancelled", "Updated Chess", "Newest snapshot")
	if store.sync.MessageID != "message-42" || store.sync.AppliedVersion != 43 {
		t.Fatalf("edit did not advance same message: %+v", store.sync)
	}
}

func assertDiscordSendPayload(t *testing.T, body []byte, status, game, description string) {
	t.Helper()
	var payload struct {
		AllowedMentions map[string]any `json:"allowed_mentions"`
		Embeds          []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Fields      []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode sent payload: %v", err)
	}
	if payload.AllowedMentions == nil || len(payload.AllowedMentions) != 0 || len(payload.Embeds) != 1 {
		t.Fatal("unsafe mention policy or embed count in sent payload")
	}
	embed := payload.Embeds[0]
	if embed.URL != "https://play.example/game-requests/"+discordSendRequestID || embed.Title != "Current Nick is looking for "+game || embed.Description != description {
		t.Fatal("sent stale URL, title, or description")
	}
	fields := make(map[string]string, len(embed.Fields))
	for _, field := range embed.Fields {
		fields[field.Name] = field.Value
	}
	if fields["Status"] != status || fields["Players"] != "3 players (unlimited)" ||
		fields["Link"] != "[Open in browser ↗](https://play.example/game-requests/"+discordSendRequestID+")" {
		t.Fatal("sent stale current state or missing browser link")
	}
}

func TestDiscordSendClaimedHonorsUncappedProviderRetryAfter(t *testing.T) {
	for _, edit := range []bool{false, true} {
		name := "create"
		if edit {
			name = "edit"
		}
		t.Run(name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			if edit {
				store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-old", AppliedVersion: 41}
				store.hasSync = true
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "21600")
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
			if result.Outcome != OutcomeRetryable || result.RetryAfter == nil || *result.RetryAfter != 6*time.Hour {
				t.Fatalf("provider 429 outcome = %v, retry after = %v; want retryable after 6h", result.Outcome, result.RetryAfter)
			}
			if store.reserved || edit && (store.sync.MessageID != "message-old" || store.sync.AppliedVersion != 41) || !edit && store.hasSync {
				t.Fatal("rate-limited send changed durable message reference")
			}
		})
	}
}

func TestDiscordSendClaimedAmbiguousNetworkFailureDiffersForCreateAndEdit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edit   bool
		want   Outcome
		method string
	}{
		{"create", false, OutcomeUncertain, http.MethodPost},
		{"edit", true, OutcomeRetryable, http.MethodPatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			if tc.edit {
				store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-old", AppliedVersion: 41}
				store.hasSync = true
			}
			methods := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				methods <- r.Method
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close() // provider received the write, but response is lost
				}
			}))
			defer server.Close()
			result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
			if result.Outcome != tc.want || result.Err == nil {
				t.Fatalf("lost %s response outcome = %v, has error = %v; want %v with safe error", tc.method, result.Outcome, result.Err != nil, tc.want)
			}
			if len(methods) != 1 {
				t.Fatalf("lost response generated %d provider requests, want one", len(methods))
			}
			if method := <-methods; method != tc.method {
				t.Fatalf("provider received method %q, want %q", method, tc.method)
			}
			if strings.Contains(result.Err.Error(), discordSendSecret) || strings.Contains(result.Err.Error(), "webhook-id") {
				t.Fatal("lost response leaked webhook credential")
			}
			if tc.edit == store.reserved || tc.edit && (store.sync.MessageID != "message-old" || store.sync.AppliedVersion != 41) || !tc.edit && store.hasSync {
				t.Fatalf("failed send changed durable sync: %+v", store.sync)
			}
		})
	}
}

func TestDiscordSendClaimedPersistenceFailureAfterProviderMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		edit   bool
		want   Outcome
		method string
	}{
		{"create", false, OutcomeUncertain, http.MethodPost},
		{"edit", true, OutcomeRetryable, http.MethodPatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			store.advanceErr = errors.New("database temporarily unavailable")
			if tc.edit {
				store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-old", AppliedVersion: 41}
				store.hasSync = true
			}
			methods := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods <- r.Method
				if r.Method == http.MethodPost {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"message-new"}`)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
			if result.Outcome != tc.want || result.Err == nil {
				t.Fatalf("%s persistence failure outcome = %v, has error = %v; want %v with error", tc.name, result.Outcome, result.Err != nil, tc.want)
			}
			if len(methods) != 1 {
				t.Fatalf("persistence failure generated %d provider requests, want one", len(methods))
			}
			if method := <-methods; method != tc.method {
				t.Fatalf("provider received method %q, want %q", method, tc.method)
			}
			if strings.Contains(result.Err.Error(), discordSendSecret) || strings.Contains(result.Err.Error(), "webhook-id") {
				t.Fatal("database failure leaked webhook credential")
			}
			if tc.edit && (store.sync.MessageID != "message-old" || store.sync.AppliedVersion != 41) || !tc.edit && store.hasSync {
				t.Fatalf("failed persistence changed durable sync: %+v", store.sync)
			}
		})
	}
}

func TestDiscordSendClaimedLockFailureNeverCallsProvider(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.lockErr = errors.New("database lock unavailable")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeRetryable || calls.Load() != 0 {
		t.Fatalf("lock failure outcome = %v; provider calls = %d, want retryable and zero", result.Outcome, calls.Load())
	}
}

func TestDiscordSendClaimedSuppressesReplacedDestination(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.destination.ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeNoLongerEligible || calls.Load() != 0 || store.lockCalls != 0 {
		t.Fatalf("old target outcome = %v; provider calls = %d, locks = %d, want suppression before provider", result.Outcome, calls.Load(), store.lockCalls)
	}
}

func TestDiscordSendClaimedSupersedesOlderVersionBeforeMutation(t *testing.T) {
	store, delivery, key := discordSendFixture(t)
	store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-newer", AppliedVersion: 43}
	store.hasSync = true
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
	if result.Outcome != OutcomeAlreadyRecorded || !store.superseded || calls.Load() != 0 || store.sync.MessageID != "message-newer" {
		t.Fatalf("obsolete delivery outcome = %v; superseded = %v, calls = %d, sync version = %d", result.Outcome, store.superseded, calls.Load(), store.sync.AppliedVersion)
	}
}

func TestDiscordSendClaimed10015BreaksDestinationOnCreateAndEdit(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		existing   bool
	}{
		{"missing webhook on edit", `{"code":10015,"message":"Unknown Webhook"}`, true},
		{"missing webhook on create", `{"code":10015,"message":"Unknown Webhook"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, delivery, key := discordSendFixture(t)
			if tc.existing {
				store.sync = DiscordMessageSync{DestinationID: discordSendDestinationID, GameRequestID: discordSendRequestID, MessageID: "message-old", AppliedVersion: 41}
				store.hasSync = true
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			result := NewDiscordSendTransport(store, key, "https://play.example", "", discordSendClient(server)).SendClaimed(t.Context(), delivery)
			if result.Outcome != OutcomePermanent || result.Err == nil || calls.Load() != 1 {
				t.Fatalf("provider %s outcome = %v, err = %v; calls = %d, want terminal permanent and one call", tc.name, result.Outcome, result.Err, calls.Load())
			}
			if strings.Contains(result.Err.Error(), discordSendSecret) || strings.Contains(result.Err.Error(), "webhook-id") {
				t.Fatal("provider error leaked webhook credential")
			}
			if store.destination.State != DiscordDestinationBroken || store.ciphertext != nil {
				t.Fatalf("destination state = %v, ciphertext = %v; want broken with cleared credential", store.destination.State, store.ciphertext)
			}
			if store.suppressCalls != 1 || store.suppressedTargetKey != "d1:"+discordSendDestinationID || store.suppressedExcludeID != 27 {
				t.Fatalf("suppression calls = %d, target = %q, excluded = %d; want one generation-scoped call excluding the current claim", store.suppressCalls, store.suppressedTargetKey, store.suppressedExcludeID)
			}
		})
	}
}
