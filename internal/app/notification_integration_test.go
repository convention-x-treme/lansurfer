//go:build integration

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This test calls the exact helper shared by app startup and standalone retry.
func TestNotificationAppConstructionDeliversDiscordAndDisablesIndependently(t *testing.T) {
	databaseURL := os.Getenv("LANSURFER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LANSURFER_TEST_DATABASE_URL is not set")
	}
	admin, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "app_notification_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	cfg := config.Config{DatabaseURL: parsed.String(), BaseURL: "https://lan.example", DataEncryptionKey: []byte(fakeDataEncryptionKey), DiscordDelivery: &config.DiscordDelivery{ClientID: "client", ClientSecret: "secret"}}
	pool, err := pgxpool.New(t.Context(), cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := platformpostgres.Migrate(t.Context(), pool, slog.Default()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Runtime Host")
	if err != nil {
		t.Fatal(err)
	}
	games := game.NewService(gamepostgres.NewStore(pool))
	createdGame, err := games.Create(t.Context(), "Runtime Game", "runtime-game")
	if err != nil {
		t.Fatal(err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), games, nil)
	key := cfg.DataEncryptionKey
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "webhook-id", WebhookToken: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}, ciphertext); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = appDiscordRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/webhook-id/private-token") {
			t.Errorf("unexpected provider operation: %s", request.Method)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"runtime-message"}`))}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	makeTransports := func(settings config.Config) []notification.Transport {
		transports, err := notificationTransports(settings, store, game.NewDiscoveryService(gamepostgres.NewStore(pool), gamepostgres.NewStore(pool), gamepostgres.NewStore(pool), nil), gamerequestquery.NewStore(pool), users)
		if err != nil {
			t.Fatal(err)
		}
		return transports
	}
	disabled := cfg
	disabled.DiscordDelivery = nil
	if transports := makeTransports(disabled); len(transports) != 0 {
		t.Fatalf("disabled transports = %d", len(transports))
	}
	if _, err := requests.Create(t.Context(), host.ID, createdGame.ID, nil, time.Now().Add(time.Hour), "disabled request"); err != nil {
		t.Fatal(err)
	}
	newNotificationWorker(store, makeTransports(disabled), slog.Default()).Tick(t.Context())
	if calls.Load() != 0 {
		t.Fatal("disabled app called provider")
	}
	if _, err := requests.Create(t.Context(), host.ID, createdGame.ID, nil, time.Now().Add(2*time.Hour), "enabled request"); err != nil {
		t.Fatal(err)
	}
	transports := makeTransports(cfg)
	if len(transports) != 1 || transports[0].Name() != "discord" {
		t.Fatalf("configured transports = %v", transports)
	}
	worker := newNotificationWorker(store, transports, slog.Default())
	worker.Tick(t.Context())
	if calls.Load() != 1 {
		t.Fatalf("configured provider calls = %d", calls.Load())
	}
	var status, messageID string
	if err := pool.QueryRow(t.Context(), `SELECT d.status, s.message_id FROM notification_delivery d JOIN notification_work w ON w.id=d.work_id JOIN notification_discord_message_sync s ON s.game_request_id=w.game_request_id WHERE d.transport='discord'`).Scan(&status, &messageID); err != nil || status != "succeeded" || messageID != "runtime-message" {
		t.Fatalf("runtime send = %s, %s, %v", status, messageID, err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE notification_delivery SET status='exhausted' WHERE transport='discord'`); err != nil {
		t.Fatal(err)
	}
	problems, err := ListNotificationProblems(t.Context(), cfg, slog.Default())
	if err != nil || len(problems) != 1 || calls.Load() != 1 {
		t.Fatalf("CLI diagnostics = %d, error = %v, HTTP calls = %d", len(problems), err, calls.Load())
	}
}

type appDiscordRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip appDiscordRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
