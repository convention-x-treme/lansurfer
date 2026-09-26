//go:build integration

package postgres

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

func TestDiscordOAuthConnectPersistsOnlyEncryptedWebhookCredential(t *testing.T) {
	pool, store, _, key := discordSendSetup(t)
	provider := notification.NewDiscordWebhookOAuthClient(notification.DiscordWebhookOAuthConfig{
		ClientID: "delivery-client", ClientSecret: "oauth-client-secret", RedirectURL: "https://lan.example/admin/discord/callback",
		HTTPClient: &http.Client{Transport: discordSendRoundTrip(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "discord.com" || request.URL.Path != "/api/oauth2/token" {
				t.Errorf("wrong OAuth endpoint: %s", request.URL.Host)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"oauth-access-secret","refresh_token":"oauth-refresh-secret","webhook":{"id":"private-webhook-id","token":"private-webhook-token","guild_id":"guild-1","channel_id":"channel-1","name":"Weekly Games"}}`))}, nil
		})},
	})
	connect := notification.NewDiscordConnectService(store, provider, key, nil)
	if err := connect.Connect(t.Context(), "oauth-code"); err != nil {
		t.Fatal(err)
	}
	destination, ciphertext, err := store.GetDiscordDestination(t.Context())
	if err != nil || destination.ID == "" || destination.GuildID != "guild-1" || destination.ChannelID != "channel-1" || destination.WebhookName != "Weekly Games" {
		t.Fatalf("stored safe destination = %+v, %v", destination, err)
	}
	credential, err := notification.DecryptDiscordCredential(key, ciphertext)
	if err != nil || credential.WebhookID != "private-webhook-id" || credential.WebhookToken != "private-webhook-token" {
		t.Fatal("credential was not stored encrypted and usable")
	}
	var persisted string
	if err := pool.QueryRow(t.Context(), `SELECT row_to_json(d)::text FROM notification_discord_destination d`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-webhook-id", "private-webhook-token", "oauth-access-secret", "oauth-refresh-secret"} {
		if strings.Contains(persisted, secret) {
			t.Fatal("provider secret persisted in destination row")
		}
	}
}

func TestDiscordWorkerHonorsProvider429ScheduleAndReleasesCreateReservation(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Retry-After")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "21600")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":0,"retry_after":21600}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"after-rate-limit"}`))
	})
	worker := newTestWorker(store, []notification.Transport{notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)})
	worker.Tick(t.Context())
	before := time.Now()
	worker.Tick(t.Context())
	var deliveryID int64
	var status string
	var next time.Time
	if err := pool.QueryRow(t.Context(), `SELECT d.id, d.status, d.next_attempt_at FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1 AND d.transport='discord'`, request.ID).Scan(&deliveryID, &status, &next); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || next.Before(before.Add(6*time.Hour-2*time.Second)) || next.After(before.Add(6*time.Hour+time.Minute)) {
		t.Fatalf("429 persisted status=%s next=%s", status, next)
	}
	if discordSendReservationCount(t, pool, destination.ID, request.ID) != 0 {
		t.Fatal("429 retained create reservation")
	}
	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, deliveryID); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	if calls != 2 || discordSendDeliveryStatus(t, pool, deliveryID) != "succeeded" {
		t.Fatalf("scheduled retry calls=%d status=%s", calls, discordSendDeliveryStatus(t, pool, deliveryID))
	}
}

func TestDiscordWorkerRetriesLostEditOnSameMessage(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Original")
	if err != nil {
		t.Fatal(err)
	}
	posts, patches := 0, 0
	server := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			_, _ = w.Write([]byte(`{"id":"stable-message"}`))
			return
		}
		patches++
		w.WriteHeader(http.StatusNoContent)
	})
	originalTransport := server.Transport
	server.Transport = discordSendRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPatch && patches == 0 {
			patches++
			return nil, errors.New("connection lost after edit")
		}
		return originalTransport.RoundTrip(r)
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", server)
	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	var id int64
	var status string
	if err := pool.QueryRow(t.Context(), `SELECT d.id, d.status FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1 AND w.kind='joined' AND d.transport='discord'`, request.ID).Scan(&id, &status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("network edit outcome = %s, want pending", status)
	}
	sync, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || sync.MessageID != "stable-message" || posts != 1 || discordSendReservationCount(t, pool, destination.ID, request.ID) != 0 {
		t.Fatal("failed edit changed message or created a reservation")
	}
	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	if patches != 2 || posts != 1 || discordSendDeliveryStatus(t, pool, id) != "succeeded" {
		t.Fatalf("edit retry patches=%d posts=%d status=%s", patches, posts, discordSendDeliveryStatus(t, pool, id))
	}
}

func TestDiscordDisconnectReconnectSkipsOldWorkThenCreatesOnNewActivity(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	old := discordSendConnect(t, store, key, "old-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Original")
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan discordSendHTTPCall, 4)
	client := discordSendTestClient(t, func(w http.ResponseWriter, r *http.Request) { discordSendReply(w, r, calls, "new-message") })
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	worker := newTestWorker(store, []notification.Transport{transport})
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	discordSendNextCall(t, calls)
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport) // Leave old work pending until disconnect.
	provider := &fakeDiscordWebhookOAuthProviderForConnect{}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transport.ResolveTarget(t.Context(), notification.Target{Transport: "discord", Key: "d1:" + old.ID}); !errors.Is(err, notification.ErrNoLongerEligible) {
		t.Fatalf("old target still resolves: %v", err)
	}
	fresh := discordSendConnect(t, store, key, "new-token")
	if fresh.ID == old.ID {
		t.Fatal("reconnect reused destination generation")
	}
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	if len(calls) != 0 {
		t.Fatal("reconnect replayed old work")
	}
	var oldStatus string
	if err := pool.QueryRow(t.Context(), `SELECT d.status FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id WHERE w.game_request_id=$1 AND d.target_key=$2 AND w.kind='joined'`, request.ID, "d1:"+old.ID).Scan(&oldStatus); err != nil || oldStatus != "suppressed" {
		t.Fatalf("old work = %s, %v", oldStatus, err)
	}
	if err := fixture.requests.Close(t.Context(), request.ID, fixture.host.ID); err != nil {
		t.Fatal(err)
	}
	var closeSequence int64
	if err := pool.QueryRow(t.Context(), `SELECT sequence FROM game_request_activity WHERE game_request_id=$1 AND kind='closed' AND origin='live'`, request.ID).Scan(&closeSequence); err != nil {
		t.Fatal(err)
	}
	worker.Tick(t.Context())
	worker.Tick(t.Context())
	call := discordSendNextCall(t, calls)
	discordSendAssertCall(t, call, http.MethodPost, "new-token", "", request.ID)
	if call.field("Status") != "closed" || call.field("Players") != "2 players (unlimited)" {
		t.Fatalf("reconnected create did not use current request snapshot: status=%q players=%q", call.field("Status"), call.field("Players"))
	}
	if len(calls) != 0 {
		t.Fatal("more than one new-generation create")
	}
	sync, err := store.GetDiscordMessageSync(t.Context(), fresh.ID, request.ID)
	if err != nil || sync.DestinationID != fresh.ID || sync.GameRequestID != request.ID || sync.MessageID != "new-message" || sync.AppliedVersion != closeSequence {
		t.Fatalf("reconnected sync = %+v, close activity sequence = %d, error = %v", sync, closeSequence, err)
	}
}
