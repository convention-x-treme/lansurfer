//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestDiscordSyncIsScopedToConnectionAndRequest(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Sync state")
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, crypto.KeySize)
	credential, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "private-webhook", WebhookToken: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-one", ChannelID: "channel-one"}, credential); err != nil {
		t.Fatal(err)
	}
	first, _, err := store.GetDiscordDestination(t.Context())
	if err != nil || first.ID == "" || first.ID == "private-webhook" || first.ID == "private-token" {
		t.Fatalf("first destination = %+v, error = %v", first, err)
	}
	var parsed pgtype.UUID
	if err := parsed.Scan(first.ID); err != nil {
		t.Fatalf("destination ID is not UUID: %v", err)
	}
	original := notification.DiscordMessageSync{DestinationID: first.ID, GameRequestID: request.ID, MessageID: "message-old", AppliedVersion: 12}
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), original); err != nil || !changed {
		t.Fatalf("initial sync changed = %v, error = %v", changed, err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_discord_message_sync (destination_id, game_request_id, message_id, applied_version) VALUES ($1, $2, 'duplicate', 13)`, first.ID, request.ID); err == nil {
		t.Fatal("database accepted duplicate destination/request synchronization record")
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-two", ChannelID: "channel-two"}, credential); err != nil {
		t.Fatal(err)
	}
	second, _, err := store.GetDiscordDestination(t.Context())
	if err != nil || second.ID == first.ID || second.ID == "" {
		t.Fatalf("replacement identity = %q, previous = %q, error = %v", second.ID, first.ID, err)
	}
	if _, err := store.GetDiscordMessageSync(t.Context(), second.ID, request.ID); !errors.Is(err, notification.ErrDiscordMessageSyncNotFound) {
		t.Fatalf("new destination inherited old sync: %v", err)
	}
	newRecord := notification.DiscordMessageSync{DestinationID: second.ID, GameRequestID: request.ID, MessageID: "message-new", AppliedVersion: 13}
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), newRecord); err != nil || !changed {
		t.Fatalf("new destination sync changed = %v, error = %v", changed, err)
	}
	old, err := store.GetDiscordMessageSync(t.Context(), first.ID, request.ID)
	if err != nil || old.MessageID != "message-old" || old.AppliedVersion != 12 {
		t.Fatalf("old destination sync = %+v, error = %v", old, err)
	}
	latest, err := store.GetDiscordMessageSync(t.Context(), second.ID, request.ID)
	if err != nil || latest.MessageID != "message-new" || latest.AppliedVersion != 13 {
		t.Fatalf("new destination sync = %+v, error = %v", latest, err)
	}
}

func TestDiscordSyncRejectsStaleUpdateAndRecoversDeletedMessage(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Recovery")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}, notification.DiscordCredentialCiphertext("encrypted")); err != nil {
		t.Fatal(err)
	}
	destination, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	record := notification.DiscordMessageSync{DestinationID: destination.ID, GameRequestID: request.ID, MessageID: "message-v20", AppliedVersion: 20}
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), record); err != nil || !changed {
		t.Fatalf("initial sync changed = %v, error = %v", changed, err)
	}
	record.MessageID, record.AppliedVersion = "stale-message", 19
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), record); err != nil || changed {
		t.Fatalf("stale sync changed = %v, error = %v", changed, err)
	}
	if cleared, err := store.ClearDiscordMessageSync(t.Context(), destination.ID, request.ID, "stale-message", 20); err != nil || cleared {
		t.Fatalf("wrong reference cleared = %v, error = %v", cleared, err)
	}
	if cleared, err := store.ClearDiscordMessageSync(t.Context(), destination.ID, request.ID, "message-v20", 20); err != nil || !cleared {
		t.Fatalf("expected reference cleared = %v, error = %v", cleared, err)
	}
	current, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || current.MessageID != "" || current.AppliedVersion != 20 {
		t.Fatalf("cleared sync = %+v, error = %v", current, err)
	}
	record.MessageID, record.AppliedVersion = "replacement", 20
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), record); err != nil || !changed {
		t.Fatalf("replacement changed = %v, error = %v", changed, err)
	}
	record.MessageID, record.AppliedVersion = "stale-after-recovery", 19
	if changed, err := store.AdvanceDiscordMessageSync(t.Context(), record); err != nil || changed {
		t.Fatalf("post-recovery stale sync changed = %v, error = %v", changed, err)
	}
	current, err = store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || current.MessageID != "replacement" || current.AppliedVersion != 20 {
		t.Fatalf("recovered sync = %+v, error = %v", current, err)
	}
}

func TestDiscordSyncConcurrentAdvancesRetainNewestVersion(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Concurrent sync")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}, notification.DiscordCredentialCiphertext("encrypted")); err != nil {
		t.Fatal(err)
	}
	destination, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for _, version := range []int64{21, 22} {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.AdvanceDiscordMessageSync(t.Context(), notification.DiscordMessageSync{
				DestinationID: destination.ID, GameRequestID: request.ID,
				MessageID: "message-" + string(rune('a'+version-21)), AppliedVersion: version,
			})
			if err != nil {
				failures <- err
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	current, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
	if err != nil || current.AppliedVersion != 22 || current.MessageID != "message-b" {
		t.Fatalf("concurrent sync = %+v, error = %v", current, err)
	}
}

func TestDiscordDestinationReconnectAfterBreakAndDisconnectGetsNewIdentity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	metadata := notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}
	ciphertext := notification.DiscordCredentialCiphertext("encrypted")
	if err := store.ConnectDiscordDestination(t.Context(), metadata, ciphertext); err != nil {
		t.Fatal(err)
	}
	first, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDiscordDestinationBroken(t.Context(), "webhook deleted"); err != nil {
		t.Fatal(err)
	}
	broken, _, err := store.GetDiscordDestination(t.Context())
	if err != nil || broken.ID != first.ID {
		t.Fatalf("broken identity = %q, first = %q, error = %v", broken.ID, first.ID, err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), metadata, ciphertext); err != nil {
		t.Fatal(err)
	}
	reconnected, _, err := store.GetDiscordDestination(t.Context())
	if err != nil || reconnected.ID == first.ID {
		t.Fatalf("reconnected identity = %q, first = %q, error = %v", reconnected.ID, first.ID, err)
	}
	if err := store.DeleteDiscordDestination(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), metadata, ciphertext); err != nil {
		t.Fatal(err)
	}
	afterDisconnect, _, err := store.GetDiscordDestination(t.Context())
	if err != nil || afterDisconnect.ID == reconnected.ID || afterDisconnect.ID == first.ID {
		t.Fatalf("identity after disconnect = %q, previous = %q/%q, error = %v", afterDisconnect.ID, first.ID, reconnected.ID, err)
	}
}

func TestDiscordSyncLockSerializesOneDestinationAndRequest(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	store := notificationpostgres.NewStore(pool)
	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Serialized sync")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild", ChannelID: "channel"}, notification.DiscordCredentialCiphertext("encrypted")); err != nil {
		t.Fatal(err)
	}
	destination, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	firstHeld := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.WithDiscordMessageSyncLock(t.Context(), destination.ID, request.ID, func(repo notification.DiscordMessageSyncRepository) error {
			close(firstHeld)
			<-release
			_, err := repo.AdvanceDiscordMessageSync(t.Context(), notification.DiscordMessageSync{
				DestinationID: destination.ID, GameRequestID: request.ID, MessageID: "first", AppliedVersion: 1,
			})
			return err
		})
	}()
	<-firstHeld
	secondCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := store.WithDiscordMessageSyncLock(secondCtx, destination.ID, request.ID, func(notification.DiscordMessageSyncRepository) error {
		t.Error("second mutation ran before first transaction released its lock")
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second lock while first held = %v, want deadline exceeded", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := store.WithDiscordMessageSyncLock(t.Context(), destination.ID, request.ID, func(repo notification.DiscordMessageSyncRepository) error {
		current, err := repo.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
		if err != nil || current.MessageID != "first" {
			t.Errorf("committed sync = %+v, error = %v", current, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
