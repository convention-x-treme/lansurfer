//go:build integration

package postgres

import (
	"net/http"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
)

func TestDiscordSendDoesNotReviveNewerTerminalWork(t *testing.T) {
	for _, status := range []string{"permanent", "suppressed"} {
		t.Run(status, func(t *testing.T) {
			pool, store, fixture, key := discordSendSetup(t)
			destination := discordSendConnect(t, store, key, "private-token")
			calls := make(chan discordSendHTTPCall, 1)
			client := discordSendTestClient(t, func(w http.ResponseWriter, request *http.Request) {
				discordSendReply(w, request, calls, "created-message")
			})
			transport := notification.NewDiscordSendTransport(store, key, "https://lan.example", "", client)
			request, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "First")
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.requests.Join(t.Context(), request.ID, fixture.guest.ID); err != nil {
				t.Fatal(err)
			}
			discordSendMaterialize(t, store, transport)
			claims := discordSendClaim(t, store)
			if len(claims) != 2 {
				t.Fatalf("claims = %+v", claims)
			}
			var old, newer notification.ClaimedDelivery
			for _, claim := range claims {
				if claim.Activity.Kind == notification.ActivityCreated {
					old = claim
				} else {
					newer = claim
				}
			}
			if status == "permanent" {
				err = store.RecordPermanent(t.Context(), newer.DeliveryID, "provider rejected")
			} else {
				err = store.RecordSuppressed(t.Context(), newer.DeliveryID, "no longer eligible")
			}
			if err != nil {
				t.Fatal(err)
			}
			if result := transport.SendClaimed(t.Context(), old); result.Outcome != notification.OutcomeSucceeded {
				t.Fatalf("older current-state create outcome = %v", result.Outcome)
			}
			call := discordSendNextCall(t, calls)
			discordSendAssertCall(t, call, http.MethodPost, "private-token", "", request.ID)
			if call.field("Players") != "2 players (unlimited)" || discordSendDeliveryStatus(t, pool, newer.DeliveryID) != status {
				t.Fatal("terminal newer work was revived or original create used stale state")
			}
			syncState, err := store.GetDiscordMessageSync(t.Context(), destination.ID, request.ID)
			if err != nil || syncState.AppliedVersion != old.Activity.Sequence || syncState.MessageID != "created-message" {
				t.Fatalf("sync marker = %+v, %v", syncState, err)
			}
		})
	}
}
