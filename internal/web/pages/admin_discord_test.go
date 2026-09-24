package pages

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
)

func renderAdminDiscord(t *testing.T, destination notification.DiscordDestination) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := AdminDiscordDelivery(branding.Default(), components.NavigationState{}, destination, "").Render(context.Background(), &rendered); err != nil {
		t.Fatalf("render admin Discord delivery: %v", err)
	}
	return rendered.String()
}

func TestAdminDiscordDeliveryNotConfiguredOffersConnect(t *testing.T) {
	rendered := renderAdminDiscord(t, notification.DiscordDestination{})
	if !strings.Contains(rendered, "Not configured") || !strings.Contains(rendered, `action="/admin/discord/connect"`) || !strings.Contains(rendered, "Connect") {
		t.Fatalf("not-configured page lacks Connect action: %s", rendered)
	}
}

func TestAdminDiscordDeliveryConnectedShowsOnlySafeMetadata(t *testing.T) {
	connectedAt := time.Date(2026, 9, 25, 12, 30, 0, 0, time.UTC)
	destination := notification.DiscordDestination{
		State: notification.DiscordDestinationConnected, GuildID: "safe-guild-1", ChannelID: "safe-channel-2",
		WebhookName: "LANsurfer updates", ConnectedAt: connectedAt,
	}
	rendered := renderAdminDiscord(t, destination)
	for _, value := range []string{"Connected", destination.GuildID, destination.ChannelID, destination.WebhookName, connectedAt.Format(time.RFC3339), "/admin/discord/reconnect", "/admin/discord/disconnect"} {
		if !strings.Contains(rendered, value) {
			t.Errorf("connected page missing %q", value)
		}
	}
	assertDiscordDestinationHasNoCredentialFields(t)
}

func TestAdminDiscordDeliveryBrokenRequiresReconnect(t *testing.T) {
	brokenAt := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	destination := notification.DiscordDestination{
		State: notification.DiscordDestinationBroken, GuildID: "safe-guild-3", ChannelID: "safe-channel-4",
		WebhookName: "LANsurfer updates", BrokenAt: brokenAt, BrokenReason: "Discord webhook unavailable",
	}
	rendered := renderAdminDiscord(t, destination)
	for _, value := range []string{"Broken", destination.GuildID, destination.ChannelID, destination.WebhookName, brokenAt.Format(time.RFC3339), destination.BrokenReason, "Reconnect is required.", "/admin/discord/reconnect", "/admin/discord/disconnect"} {
		if !strings.Contains(rendered, value) {
			t.Errorf("broken page missing %q", value)
		}
	}
}

func assertDiscordDestinationHasNoCredentialFields(t *testing.T) {
	t.Helper()
	typeOfDestination := reflect.TypeOf(notification.DiscordDestination{})
	allowed := []string{"State", "GuildID", "ChannelID", "WebhookName", "ConnectedAt", "UpdatedAt", "BrokenAt", "BrokenReason"}
	if typeOfDestination.NumField() != len(allowed) {
		t.Fatalf("safe destination has %d fields, want exactly %d", typeOfDestination.NumField(), len(allowed))
	}
	for index, name := range allowed {
		if field := typeOfDestination.Field(index); field.Name != name {
			t.Errorf("safe destination field %d = %q, want %q", index, field.Name, name)
		}
	}
}
