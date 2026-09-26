//go:build integration

package postgres

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

// A later uncertain delivery did not send a CREATE and cannot override A's reservation.
func TestDiscordManualRetryCannotClearAnotherDeliveryReservation(t *testing.T) {
	pool, store, fixture, key := discordSendSetup(t)
	destination := discordSendConnect(t, store, key, "private-token")
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := discordSendTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`)) // A received a 2xx but no durable message ID.
	})
	transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
	discordSendMaterialize(t, store, transport)
	first := discordSendClaim(t, store)
	if len(first) != 1 || transport.SendClaimed(t.Context(), first[0]).Outcome != notification.OutcomeUncertain {
		t.Fatal("first create not uncertain")
	}
	if err := store.RecordUncertain(t.Context(), first[0].DeliveryID, "uncertain create"); err != nil {
		t.Fatal(err)
	}
	if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
		t.Fatal(err)
	}
	discordSendMaterialize(t, store, transport)
	second := discordSendClaim(t, store)
	if len(second) != 1 || transport.SendClaimed(t.Context(), second[0]).Outcome != notification.OutcomeUncertain {
		t.Fatal("competing delivery did not encounter reservation")
	}
	if err := store.RecordUncertain(t.Context(), second[0].DeliveryID, "unresolved create"); err != nil {
		t.Fatal(err)
	}
	service := notification.NewService(store, []notification.Transport{transport})
	if err := service.ManualRetryUncertain(t.Context(), second[0].DeliveryID); !errors.Is(err, notification.ErrDiscordReservationNotOwned) {
		t.Fatalf("competing retry = %v, want reservation ownership error", err)
	}
	if got := discordSendReservationCount(t, pool, destination.ID, request.ID); got != 1 {
		t.Fatalf("reservation count = %d", got)
	}
	if status := discordSendDeliveryStatus(t, pool, second[0].DeliveryID); status != "uncertain" {
		t.Fatalf("competitor status = %s", status)
	}
	if calls != 1 {
		t.Fatalf("provider create calls = %d, want 1", calls)
	}
	if err := service.ManualRetryUncertain(t.Context(), first[0].DeliveryID); err != nil {
		t.Fatalf("owner retry = %v", err)
	}
	if got := discordSendReservationCount(t, pool, destination.ID, request.ID); got != 0 {
		t.Fatalf("owner retry retained reservation: %d", got)
	}
	reopened := discordSendClaim(t, store)
	if len(reopened) != 1 || reopened[0].DeliveryID != first[0].DeliveryID {
		t.Fatalf("reopened owner claim = %+v", reopened)
	}
	if outcome := transport.SendClaimed(t.Context(), reopened[0]).Outcome; outcome != notification.OutcomeUncertain {
		t.Fatalf("owner retry outcome = %v", outcome)
	}
	if err := store.RecordUncertain(t.Context(), reopened[0].DeliveryID, "another ambiguous create"); err != nil {
		t.Fatal(err)
	}
	if err := service.ManualRetryUncertain(t.Context(), second[0].DeliveryID); !errors.Is(err, notification.ErrDiscordReservationNotOwned) {
		t.Fatalf("competitor cleared owner's new reservation: %v", err)
	}
	if got := discordSendReservationCount(t, pool, destination.ID, request.ID); got != 1 {
		t.Fatalf("new owner reservation count = %d", got)
	}
}

func TestDiscordReservationMigrationKeepsLegacyCreateUnowned(t *testing.T) {
	pool := newTestPool(t)
	migrateToVersion(t, pool, productionMigrationFS(t), 21)
	fixture := newNotificationFixture(t, pool)
	request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Legacy")
	if err != nil {
		t.Fatal(err)
	}
	oldGeneration := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_discord_create_reservation (destination_id, game_request_id) VALUES ($1, $2)`, oldGeneration, request.ID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	var owner *int64
	if err := pool.QueryRow(t.Context(), `SELECT owner_delivery_id FROM notification_discord_create_reservation WHERE destination_id = $1 AND game_request_id = $2`, oldGeneration, request.ID).Scan(&owner); err != nil || owner != nil {
		t.Fatalf("legacy reservation owner = %v, error = %v; want unknown", owner, err)
	}
}

type discordRetryAfterEligibility struct {
	*notification.DiscordTransport
	change func() error
}

func (t discordRetryAfterEligibility) Targets(ctx context.Context, activity notification.Activity) ([]notification.Target, error) {
	targets, err := t.DiscordTransport.Targets(ctx, activity)
	if err == nil && t.change != nil {
		err = t.change()
	}
	return targets, err
}

func TestDiscordManualRetryRechecksGenerationAndLegacyOwnership(t *testing.T) {
	for _, scenario := range []struct {
		name, transition string
		reserved         bool
	}{
		{"replacement with reservation", "replace", true},
		{"disconnect with reservation", "disconnect", true},
		{"broken with reservation", "break", true},
		{"replacement without reservation", "replace", false},
		{"current without reservation", "", false},
		{"legacy unowned reservation", "", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pool, store, fixture, key := discordSendSetup(t)
			old := discordSendConnect(t, store, key, "old-token")
			request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Retry")
			if err != nil {
				t.Fatal(err)
			}
			transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", nil)
			discordSendMaterialize(t, store, transport)
			claims := discordSendClaim(t, store)
			if len(claims) != 1 {
				t.Fatalf("claims = %+v", claims)
			}
			selected := claims[0]
			if err := store.RecordUncertain(t.Context(), selected.DeliveryID, "create uncertain"); err != nil {
				t.Fatal(err)
			}
			if scenario.reserved {
				if _, err := pool.Exec(t.Context(), `INSERT INTO notification_discord_create_reservation (destination_id, game_request_id) VALUES ($1, $2)`, old.ID, request.ID); err != nil {
					t.Fatal(err)
				}
			}
			wrapped := discordRetryAfterEligibility{DiscordTransport: transport}
			wrapped.change = func() error {
				switch scenario.transition {
				case "replace":
					discordSendConnect(t, store, key, "new-token")
				case "disconnect":
					return store.DeleteDiscordDestination(t.Context())
				case "break":
					_, err := store.MarkDiscordDestinationBroken(t.Context(), old.ID, "unknown webhook")
					return err
				}
				return nil
			}
			service := notification.NewService(store, []notification.Transport{wrapped})
			err = service.ManualRetryUncertain(t.Context(), selected.DeliveryID)
			switch {
			case scenario.transition != "" && !errors.Is(err, notification.ErrNoLongerEligible):
				t.Fatalf("generation changed: retry = %v", err)
			case scenario.transition == "" && scenario.reserved && !errors.Is(err, notification.ErrDiscordReservationNotOwned):
				t.Fatalf("legacy reservation: retry = %v", err)
			case scenario.transition == "" && !scenario.reserved && err != nil:
				t.Fatalf("no-reservation retry = %v", err)
			}
			want := "uncertain"
			if scenario.transition == "" && !scenario.reserved {
				want = "pending"
			}
			if got := discordSendDeliveryStatus(t, pool, selected.DeliveryID); got != want {
				t.Fatalf("status = %s, want %s", got, want)
			}
			if scenario.reserved && discordSendReservationCount(t, pool, old.ID, request.ID) != 1 {
				t.Fatal("reservation cleared across generation/without owner")
			}
		})
	}
}
