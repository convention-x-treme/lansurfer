//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
)

func TestDiscordTargetsOnlyMaterializeNewLiveActivity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	fixture := newNotificationFixture(t, pool)
	store := notificationpostgres.NewStore(pool)
	if err := store.InitializeBaseline(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport := notification.NewDiscordTransport(store)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Before Discord")
	if err != nil {
		t.Fatal(err)
	}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 {
		t.Fatalf("preconnect activities = %v, %v", activities, err)
	}
	before, err := transport.Targets(t.Context(), activities[0])
	if err != nil || len(before) != 0 {
		t.Fatalf("preconnect targets = %v, %v", before, err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], before); err != nil {
		t.Fatal(err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-old", ChannelID: "channel-old"}, notification.DiscordCredentialCiphertext("encrypted-old")); err != nil {
		t.Fatal(err)
	}
	old, err := transport.Targets(t.Context(), activities[0])
	if err != nil || len(old) != 1 {
		t.Fatalf("connected targets = %v, %v", old, err)
	}
	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 0 {
		t.Fatalf("historical replay = %v, %v", activities, err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	activities, err = store.DiscoverActivity(t.Context(), 10)
	if err != nil || len(activities) != 1 || activities[0].GameRequestID != request.ID {
		t.Fatalf("live activity = %v, %v", activities, err)
	}
	live, err := transport.Targets(t.Context(), activities[0])
	if err != nil || len(live) != 1 || live[0] != old[0] {
		t.Fatalf("live targets = %v, %v", live, err)
	}
	if err := store.MaterializeWork(t.Context(), activities[0], live); err != nil {
		t.Fatal(err)
	}
	var total, oldWork int
	if err := pool.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER (WHERE w.activity_sequence = $1) FROM notification_delivery d JOIN notification_work w ON w.id = d.work_id WHERE d.transport='discord'`, activities[0].Sequence).Scan(&total, &oldWork); err != nil {
		t.Fatal(err)
	}
	if total != 1 || oldWork != 1 {
		t.Fatalf("delivery count = %d, live = %d", total, oldWork)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-new", ChannelID: "channel-new"}, notification.DiscordCredentialCiphertext("encrypted-new")); err != nil {
		t.Fatal(err)
	}
	current, err := transport.Targets(t.Context(), activities[0])
	if err != nil || len(current) != 1 || current[0].Key == old[0].Key {
		t.Fatalf("replacement = %v, %v", current, err)
	}
	if _, _, err := transport.ResolveTarget(t.Context(), old[0]); !errors.Is(err, notification.ErrNoLongerEligible) {
		t.Fatalf("old target resolved new destination: %v", err)
	}
}
