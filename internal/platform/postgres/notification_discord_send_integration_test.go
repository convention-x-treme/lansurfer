//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

type discordSendHTTPCall struct {
	method, path string
	payload      struct {
		AllowedMentions map[string]any `json:"allowed_mentions"`
		Embeds          []struct {
			Description string `json:"description"`
			URL         string `json:"url"`
			Fields      []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"embeds"`
	}
	err error
}

func (c discordSendHTTPCall) field(name string) string {
	if len(c.payload.Embeds) != 1 {
		return ""
	}
	for _, field := range c.payload.Embeds[0].Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}

func discordSendTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: discordSendRoundTrip(func(request *http.Request) (*http.Response, error) {
		forwarded := request.Clone(request.Context())
		forwarded.URL.Scheme, forwarded.URL.Host, forwarded.Host = endpoint.Scheme, endpoint.Host, endpoint.Host
		return server.Client().Transport.RoundTrip(forwarded)
	})}
}

type discordSendRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip discordSendRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func discordSendReply(w http.ResponseWriter, request *http.Request, calls chan<- discordSendHTTPCall, id string) {
	call := discordSendHTTPCall{method: request.Method, path: request.URL.RequestURI()}
	call.err = json.NewDecoder(request.Body).Decode(&call.payload)
	calls <- call
	if call.err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func discordSendNextCall(t *testing.T, calls <-chan discordSendHTTPCall) discordSendHTTPCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	default:
		t.Fatal("send completed without an HTTP request")
		return discordSendHTTPCall{}
	}
}

func discordSendSetup(t *testing.T) (*pgxpool.Pool, *notificationpostgres.Store, notificationFixture, []byte) {
	t.Helper()
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool, store, fixture, make([]byte, crypto.KeySize)
}

func discordSendConnect(t *testing.T, store *notificationpostgres.Store, key []byte, token string) notification.DiscordDestination {
	t.Helper()
	credential, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "webhook-id", WebhookToken: token})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}, credential); err != nil {
		t.Fatal(err)
	}
	destination, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func discordSendMaterialize(t *testing.T, store *notificationpostgres.Store, transport *notification.DiscordTransport) {
	t.Helper()
	activities, err := store.DiscoverActivity(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, activity := range activities {
		targets, err := transport.Targets(t.Context(), activity)
		if err != nil || len(targets) != 1 {
			t.Fatalf("Targets() = %v, %v", targets, err)
		}
		if err := store.MaterializeWork(t.Context(), activity, targets); err != nil {
			t.Fatal(err)
		}
	}
}

func discordSendClaim(t *testing.T, store *notificationpostgres.Store) []notification.ClaimedDelivery {
	t.Helper()
	claims, err := store.ClaimDueDeliveries(t.Context(), 20, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

func discordSendAssertCall(t *testing.T, call discordSendHTTPCall, method, token, messageID, requestID string) {
	t.Helper()
	if call.err != nil {
		t.Fatalf("decode Discord payload: %v", call.err)
	}
	if call.method != method || !strings.Contains(call.path, "/webhook-id/"+token) {
		t.Fatalf("unexpected Discord method/path: %s (path redacted)", call.method)
	}
	if method == http.MethodPost && !strings.Contains(call.path, "wait=true") {
		t.Fatal("create omitted wait=true")
	}
	if method == http.MethodPatch && !strings.HasSuffix(call.path, "/messages/"+messageID) {
		t.Fatal("edit used wrong message")
	}
	if len(call.payload.Embeds) != 1 || call.payload.Embeds[0].URL != "https://lan.example/game-requests/"+requestID || call.payload.AllowedMentions == nil || len(call.payload.AllowedMentions) != 0 {
		t.Fatalf("unsafe or wrong Discord embed: %+v", call.payload)
	}
}

func TestDiscordSendCreatesLatestSnapshotThenEditsSameMessage(t *testing.T) {
	_, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	calls := make(chan discordSendHTTPCall, 3)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		discordSendReply(w, request, calls, "message-42")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Current description")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claimed := discordSendClaim(t, store)
	if len(claimed) != 1 || claimed[0].Activity.GameRequestID != request.ID {
		t.Fatalf("original claim = %+v", claimed)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	if result := transport.SendClaimed(t.Context(), claimed[0]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("create outcome = %v", result.Outcome)
	}
	if err := store.RecordSuccess(t.Context(), claimed[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	created := discordSendNextCall(t, calls)
	discordSendAssertCall(t, created, http.MethodPost, "private-token", "", request.ID)
	if created.field("Players") != "2 players (unlimited)" || created.field("Status") != "open" || created.payload.Embeds[0].Description != "Current description" {
		t.Fatalf("delayed create did not render joined state: %+v", created.payload)
	}
	syncState, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || syncState.MessageID != "message-42" || syncState.AppliedVersion != claimed[0].Activity.Sequence {
		t.Fatalf("create sync = %+v, %v", syncState, err)
	}
	// The create renders current state but records only its own E1 activity
	// sequence. The later joined delivery must still advance that marker.
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	joined := discordSendNextCall(t, calls)
	discordSendAssertCall(t, joined, http.MethodPatch, "private-token", "message-42", request.ID)
	if joined.field("Players") != "2 players (unlimited)" {
		t.Fatalf("joined edit players = %q", joined.field("Players"))
	}
	joinedSync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || joinedSync.MessageID != "message-42" || joinedSync.AppliedVersion <= syncState.AppliedVersion {
		t.Fatalf("joined sync = %+v, previous = %+v, %v", joinedSync, syncState, err)
	}
	if err := fixture.requests.Cancel(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatal(err)
	}
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	edited := discordSendNextCall(t, calls)
	discordSendAssertCall(t, edited, http.MethodPatch, "private-token", "message-42", request.ID)
	if edited.field("Status") != "cancelled" || edited.field("Players") != "2 players (unlimited)" {
		t.Fatalf("edit did not render current state: %+v", edited.payload)
	}
	current, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || current.MessageID != "message-42" || current.AppliedVersion <= joinedSync.AppliedVersion {
		t.Fatalf("edit sync = %+v, previous = %+v, %v", current, joinedSync, err)
	}
}

func TestDiscordSendSupersedesWaitingAndAlreadyAppliedDeliveriesWithoutHTTP(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	calls := make(chan discordSendHTTPCall, 4)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		discordSendReply(w, request, calls, "message-42")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Supersession")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	created := discordSendClaim(t, store)
	if len(created) != 1 || transport.SendClaimed(t.Context(), created[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("initial create did not succeed")
	}
	discordSendNextCall(t, calls)
	if err := store.RecordSuccess(t.Context(), created[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Leave(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Cancel(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claimed := discordSendClaim(t, store)
	byKind := make(map[notification.ActivityKind]notification.ClaimedDelivery)
	for _, delivery := range claimed {
		byKind[delivery.Activity.Kind] = delivery
	}
	if len(claimed) != 3 || len(byKind) != 3 {
		t.Fatalf("expected joined, left, cancelled claims; got %+v", claimed)
	}
	transport.SendClaimed(t.Context(), byKind[notification.ActivityJoined])
	if got := discordSendDeliveryStatus(t, pool, byKind[notification.ActivityJoined].DeliveryID); got != "superseded" || len(calls) != 0 {
		t.Fatalf("obsolete waiting join: status %q, HTTP calls %d", got, len(calls))
	}
	if result := transport.SendClaimed(t.Context(), byKind[notification.ActivityCancelled]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("latest edit outcome = %v", result.Outcome)
	}
	last := discordSendNextCall(t, calls)
	discordSendAssertCall(t, last, http.MethodPatch, "private-token", "message-42", request.ID)
	if last.field("Status") != "cancelled" {
		t.Fatalf("latest edit status = %q", last.field("Status"))
	}
	transport.SendClaimed(t.Context(), byKind[notification.ActivityLeft])
	if got := discordSendDeliveryStatus(t, pool, byKind[notification.ActivityLeft].DeliveryID); got != "superseded" || len(calls) != 0 {
		t.Fatalf("already applied left: status %q, HTTP calls %d", got, len(calls))
	}
	syncState, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || syncState.MessageID != "message-42" || syncState.AppliedVersion != byKind[notification.ActivityCancelled].Activity.Sequence {
		t.Fatalf("latest sync = %+v, %v", syncState, err)
	}
}

func discordSendDeliveryStatus(t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(t.Context(), `SELECT status FROM notification_delivery WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestDiscordSendSerializesOneRequestButNotDifferentRequests(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	received := make(chan discordSendHTTPCall, 4)
	firstHeld := make(chan struct{})
	firstRelease := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(firstRelease) })
	var firstRequestID string
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		call := discordSendHTTPCall{method: request.Method, path: request.URL.RequestURI()}
		call.err = json.NewDecoder(request.Body).Decode(&call.payload)
		received <- call
		if len(call.payload.Embeds) == 1 && strings.Contains(call.payload.Embeds[0].URL, firstRequestID) {
			select {
			case <-firstHeld:
			default:
				close(firstHeld)
			}
			select {
			case <-firstRelease:
			case <-request.Context().Done():
				return
			}
		}
		if request.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"message-42"}`))
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	first, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatal(err)
	}
	firstRequestID = first.ID
	other, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Other")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	claims := discordSendClaim(t, store)
	byRequest := make(map[string]notification.ClaimedDelivery)
	for _, claim := range claims {
		byRequest[claim.Activity.GameRequestID] = claim
	}
	if len(byRequest) != 2 {
		t.Fatalf("claims = %+v", claims)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	firstDone := make(chan notification.SendResult, 1)
	laterDone := make(chan notification.SendResult, 1)
	otherDone := make(chan notification.SendResult, 1)
	go func() { firstDone <- transport.SendClaimed(ctx, byRequest[first.ID]) }()
	select {
	case <-firstHeld:
	case <-ctx.Done():
		t.Fatal("first Discord create did not reach HTTP barrier")
	}
	firstCall := <-received
	discordSendAssertCall(t, firstCall, http.MethodPost, "private-token", "", first.ID)
	if err := fixture.requests.Join(t.Context(), first.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	later := discordSendClaim(t, store)
	if len(later) != 1 || later[0].Activity.GameRequestID != first.ID || later[0].Activity.Kind != notification.ActivityJoined {
		t.Fatalf("later activity claim = %+v", later)
	}
	go func() { laterDone <- transport.SendClaimed(ctx, later[0]) }()
	// A second worker reached PostgreSQL's same-key advisory lock while the
	// first HTTP call is still held. No timed sleep or scheduler assumption.
	for {
		var waiters int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiters)
		if err != nil {
			t.Fatal(err)
		}
		if waiters > 0 {
			break
		}
		select {
		case <-received:
			t.Fatal("same-key HTTP request escaped lock")
		case result := <-laterDone:
			t.Fatalf("later same-key send returned before first released: %v", result.Outcome)
		case <-ctx.Done():
			t.Fatal("second send never reached PostgreSQL lock")
		default:
			runtime.Gosched()
		}
	}
	go func() { otherDone <- transport.SendClaimed(ctx, byRequest[other.ID]) }()
	select {
	case call := <-received:
		discordSendAssertCall(t, call, http.MethodPost, "private-token", "", other.ID)
	case <-ctx.Done():
		t.Fatal("different request blocked behind first request")
	}
	select {
	case result := <-otherDone:
		if result.Outcome != notification.OutcomeSucceeded {
			t.Fatalf("parallel request outcome = %v", result.Outcome)
		}
	case <-ctx.Done():
		t.Fatal("parallel request did not complete")
	}
	releaseOnce.Do(func() { close(firstRelease) })
	select {
	case result := <-firstDone:
		if result.Outcome != notification.OutcomeSucceeded {
			t.Fatalf("first request outcome = %v", result.Outcome)
		}
	case <-ctx.Done():
		t.Fatal("first send did not complete")
	}
	select {
	case result := <-laterDone:
		if result.Outcome != notification.OutcomeSucceeded {
			t.Fatalf("later request outcome = %v", result.Outcome)
		}
	case <-ctx.Done():
		t.Fatal("later same-key send did not leave lock")
	}
	select {
	case call := <-received:
		discordSendAssertCall(t, call, http.MethodPatch, "private-token", "message-42", first.ID)
		if call.field("Players") != "2 players (unlimited)" {
			t.Fatalf("later edit players = %q", call.field("Players"))
		}
	default:
		t.Fatal("later activity did not edit synchronized message")
	}
	if len(received) != 0 {
		t.Fatalf("extra same-key HTTP requests = %d", len(received))
	}
	for _, id := range []string{first.ID, other.ID} {
		current, err := store.GetDiscordMessageSync(t.Context(), destination.ID, id)
		if err != nil || current.MessageID != "message-42" || (id == first.ID && current.AppliedVersion != later[0].Activity.Sequence) {
			t.Fatalf("sync for %s = %+v, %v", id, current, err)
		}
	}
}

func TestDiscordSendReplacementNeverRoutesOldWorkToNewGeneration(t *testing.T) {
	_, store, fixture, key := discordSendSetup(t)
	old := discordSendConnect(t, store, key, "old-token")
	calls := make(chan discordSendHTTPCall, 4)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		id := "old-message"
		if strings.Contains(request.URL.Path, "/new-token") {
			id = "new-message"
		}
		discordSendReply(w, request, calls, id)
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Generation")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	original := discordSendClaim(t, store)
	if len(original) != 1 || transport.SendClaimed(t.Context(), original[0]).Outcome != notification.OutcomeSucceeded {
		t.Fatal("old generation create failed")
	}
	created := discordSendNextCall(t, calls)
	discordSendAssertCall(t, created, http.MethodPost, "old-token", "", request.ID)
	if err := store.RecordSuccess(t.Context(), original[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	stale := discordSendClaim(t, store)
	if len(stale) != 1 || stale[0].Target.Key != "d1:"+old.ID {
		t.Fatalf("old generation claim = %+v", stale)
	}
	newGeneration := discordSendConnect(t, store, key, "new-token")
	if newGeneration.ID == old.ID {
		t.Fatal("replacement reused destination identity")
	}
	result := transport.SendClaimed(t.Context(), stale[0])
	if result.Outcome == notification.OutcomeSucceeded || len(calls) != 0 {
		t.Fatalf("old target used replacement credential: outcome %v, HTTP calls %d", result.Outcome, len(calls))
	}
	if result.Err != nil && (strings.Contains(result.Err.Error(), "old-token") || strings.Contains(result.Err.Error(), "new-token")) {
		t.Fatal("old-generation failure disclosed credential")
	}
	if err := fixture.requests.Cancel(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	fresh := discordSendClaim(t, store)
	if len(fresh) != 1 || fresh[0].Target.Key != "d1:"+newGeneration.ID {
		t.Fatalf("replacement claim = %+v", fresh)
	}
	if result := transport.SendClaimed(t.Context(), fresh[0]); result.Outcome != notification.OutcomeSucceeded {
		t.Fatalf("new generation create outcome = %v", result.Outcome)
	}
	newCall := discordSendNextCall(t, calls)
	discordSendAssertCall(t, newCall, http.MethodPost, "new-token", "", request.ID)
	previous, err := store.GetDiscordMessageSync(t.Context(), old.ID, request.ID)
	if err != nil || previous.MessageID != "old-message" || previous.AppliedVersion != original[0].Activity.Sequence {
		t.Fatalf("old sync mutated after replacement: %+v, %v", previous, err)
	}
	current, err := store.GetDiscordMessageSync(t.Context(), newGeneration.ID, request.ID)
	if err != nil || current.MessageID != "new-message" || current.AppliedVersion != fresh[0].Activity.Sequence {
		t.Fatalf("new sync = %+v, %v", current, err)
	}
}

// A request update edits the synchronized Discord message from one current snapshot.
func TestDiscordSendEditsSameMessageWithRequestDetails(t *testing.T) {
	_, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	calls := make(chan discordSendHTTPCall, 4)
	client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		discordSendReply(w, request, calls, "message-42")
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "Friday Club", client)
	maximum := int32(4)
	startAt := time.Now().Add(time.Hour)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, &maximum, startAt, "Current description")
	if err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	created := discordSendNextCall(t, calls)
	discordSendAssertCall(t, created, http.MethodPost, "private-token", "", request.ID)
	if created.field("Players") != "1/4 players (3 free)" {
		t.Fatalf("created players = %q", created.field("Players"))
	}
	createdSync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || createdSync.MessageID != "message-42" {
		t.Fatalf("created sync = %+v, %v", createdSync, err)
	}

	newMaximum := int32(6)
	newStart := startAt.Add(30 * time.Minute)
	input := gamerequest.UpdateDetailsInput{
		MaxPlayers: &newMaximum, StartsAt: newStart, Description: "Updated description",
	}
	if err := fixture.requests.UpdateDetails(t.Context(), request.ID, fixture.host.ID, input); err != nil {
		t.Fatal(err)
	}
	newTestWorker(store, []notification.Transport{transport}).Tick(t.Context())
	edited := discordSendNextCall(t, calls)
	discordSendAssertCall(t, edited, http.MethodPatch, "private-token", "message-42", request.ID)
	if edited.field("Players") != "1/6 players (5 free)" ||
		edited.field("Planned start") != "<t:"+fmt.Sprint(newStart.Unix())+":F>" ||
		edited.payload.Embeds[0].Description != "Updated description" ||
		edited.field("Status") != "open" {
		t.Fatalf("request update rendered stale snapshot: players=%q planned start=%q description=%q",
			edited.field("Players"), edited.field("Planned start"), edited.payload.Embeds[0].Description)
	}

	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || sync.MessageID != "message-42" || sync.AppliedVersion <= createdSync.AppliedVersion {
		t.Fatalf("sync after request update = %+v, previous = %+v, %v", sync, createdSync, err)
	}
	select {
	case extra := <-calls:
		t.Fatalf("unexpected extra Discord call: %s", extra.method)
	default:
	}
}
