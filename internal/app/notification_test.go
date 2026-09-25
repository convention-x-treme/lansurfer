package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	"github.com/convention-x-treme/lansurfer/internal/web"
)

// validVAPIDPublicKeyForTests/validVAPIDPrivateKeyForTests mirror the fixed
// test key pair internal/platform/config uses: their exact values don't
// matter here, only that they parse as a well-formed VAPID key pair so a
// configured Web Push transport actually constructs.
const (
	validVAPIDPublicKeyForTests  = "BAPXjly0gRlUqDTh973k-L9pQbfuajNHt-75lr18ZrG1R9cW6wU9xZ62F7Ndf_nQW7FSAEP2-LXZldrFzngmFhQ"
	validVAPIDPrivateKeyForTests = "QUs8zCpDK_x4zKsunw_iz-nahUva_gFm-jekct86r70"
	fakeDataEncryptionKey        = "01234567890123456789012345678901"
)

func configuredPushForTest() config.Config {
	return config.Config{BaseURL: "https://lan.example", InstanceName: "Camp Nova", DataEncryptionKey: make([]byte, 32), WebPush: &config.WebPush{
		PublicKey:  validVAPIDPublicKeyForTests,
		PrivateKey: validVAPIDPrivateKeyForTests, Subject: "mailto:ops@example.test",
	}}
}

func transportNames(t *testing.T, cfg config.Config) []string {
	t.Helper()
	// notificationTransports never queries the database at construction
	// time (Targets()/Send() are the only methods that do), so a Store
	// wrapping a nil pool is safe here: this test only proves which
	// transports get registered for a given operator configuration, not
	// their runtime delivery behavior (already covered elsewhere).
	store := notificationpostgres.NewStore(nil)
	transports, err := notificationTransports(cfg, store, nil, nil, nil)
	if err != nil {
		t.Fatalf("notificationTransports() error = %v", err)
	}
	names := make([]string, len(transports))
	for i, transport := range transports {
		names[i] = transport.Name()
	}
	return names
}

func TestNotificationTransportsDisabledConfigRegistersNone(t *testing.T) {
	if names := transportNames(t, config.Config{}); len(names) != 0 {
		t.Fatalf("transports = %v, want none when neither Web Push nor Discord delivery is configured", names)
	}
}

func TestNotificationTransportsWebPushConfiguredRegistersWebPushOnly(t *testing.T) {
	cfg := configuredPushForTest()
	names := transportNames(t, cfg)
	if len(names) != 1 || names[0] != "webpush" {
		t.Fatalf("configured transports = %v, want exactly [webpush]", names)
	}
	transports, err := notificationTransports(cfg, notificationpostgres.NewStore(nil), nil, nil, nil)
	if err != nil {
		t.Fatalf("notificationTransports() error = %v", err)
	}
	if worker := newNotificationWorker(nil, transports, nil); worker == nil {
		t.Fatal("configured worker missing")
	}
}

func TestNotificationTransportsWebPushInvalidSubjectRejected(t *testing.T) {
	cfg := configuredPushForTest()
	cfg.WebPush.Subject = ""
	store := notificationpostgres.NewStore(nil)
	if _, err := notificationTransports(cfg, store, nil, nil, nil); err == nil {
		t.Fatal("invalid VAPID subject accepted")
	}
}

func TestNotificationTransportsWebPushMissingEncryptionKeyRejected(t *testing.T) {
	cfg := configuredPushForTest()
	cfg.DataEncryptionKey = nil
	store := notificationpostgres.NewStore(nil)
	if _, err := notificationTransports(cfg, store, nil, nil, nil); err == nil {
		t.Fatal("missing encryption key accepted")
	}
}

// Discord operator configuration being enabled must register the transport
// regardless of whether any destination currently exists in the database:
// Targets() (exercised in internal/notification) resolves the no-
// destination, Connected, and Broken states at delivery time, never here.
func TestNotificationTransportsDiscordEnabledRegistersDiscordTransport(t *testing.T) {
	cfg := config.Config{
		DiscordDelivery:   &config.DiscordDelivery{ClientID: "client", ClientSecret: "secret"},
		DataEncryptionKey: []byte(fakeDataEncryptionKey),
	}
	names := transportNames(t, cfg)
	if len(names) != 1 || names[0] != "discord" {
		t.Fatalf("transports = %v, want exactly [discord]", names)
	}
}

func TestNotificationTransportsBothConfiguredRegistersBoth(t *testing.T) {
	cfg := config.Config{
		WebPush: &config.WebPush{
			PublicKey: validVAPIDPublicKeyForTests, PrivateKey: validVAPIDPrivateKeyForTests, Subject: "mailto:ops@example.com",
		},
		DiscordDelivery:   &config.DiscordDelivery{ClientID: "client", ClientSecret: "secret"},
		DataEncryptionKey: []byte(fakeDataEncryptionKey),
	}
	names := transportNames(t, cfg)
	if len(names) != 2 {
		t.Fatalf("transports = %v, want both webpush and discord", names)
	}
}

func TestNotificationTransportsDiscordWithInvalidDataEncryptionKeyFails(t *testing.T) {
	cfg := config.Config{DiscordDelivery: &config.DiscordDelivery{ClientID: "client", ClientSecret: "secret"}}
	store := notificationpostgres.NewStore(nil)
	if _, err := notificationTransports(cfg, store, nil, nil, nil); err == nil {
		t.Fatal("notificationTransports() with Discord enabled and no data-encryption key = nil error, want error")
	}
}

// cliNotificationTransports must reuse notificationTransports rather than
// duplicating transport wiring: the same configuration produces the same
// registered transports through either entry point. A nil pool is safe:
// construction performs no I/O.
func TestCLINotificationTransportsReusesRuntimeConstruction(t *testing.T) {
	cfg := config.Config{
		DiscordDelivery:   &config.DiscordDelivery{ClientID: "client", ClientSecret: "secret"},
		DataEncryptionKey: []byte(fakeDataEncryptionKey),
	}
	transports, err := cliNotificationTransports(cfg, nil)
	if err != nil {
		t.Fatalf("cliNotificationTransports() error = %v", err)
	}
	if len(transports) != 1 || transports[0].Name() != "discord" {
		t.Fatalf("cliNotificationTransports() = %v, want exactly [discord] matching runtime construction", transports)
	}
}

func TestCLINotificationTransportsDisabledConfigSkipsConstructionEntirely(t *testing.T) {
	transports, err := cliNotificationTransports(config.Config{}, nil)
	if err != nil || len(transports) != 0 {
		t.Fatalf("cliNotificationTransports() with nothing configured = %v, %v, want none, nil", transports, err)
	}
}

func TestSubscriptionServiceRouterDependenciesAreOptional(t *testing.T) {
	var service *notification.SubscriptionService
	disabled := withSubscriptionService(web.RouterDependencies{}, service)
	if disabled.Subscriptions != nil || disabled.PushDevices != nil {
		t.Fatal("disabled Web Push exposed a subscription dependency")
	}

	router := web.NewRouter(disabled)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/account/notifications", nil))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("disabled notification route status = %d, want %d", response.Code, http.StatusSeeOther)
	}

	service = new(notification.SubscriptionService)
	configured := withSubscriptionService(web.RouterDependencies{}, service)
	if configured.Subscriptions == nil || configured.PushDevices == nil {
		t.Fatal("configured Web Push did not expose subscription dependencies")
	}
}
