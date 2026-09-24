package app

import (
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

func TestNotificationTransportsOptionalAndConfigured(t *testing.T) {
	disabled, err := notificationTransports(config.Config{}, nil, nil, nil, nil)
	if err != nil || len(disabled) != 0 {
		t.Fatalf("disabled transports = %v, err = %v; want none", disabled, err)
	}

	cfg := configuredPushForTest()
	transports, err := notificationTransports(cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("configured transports: %v", err)
	}
	if len(transports) != 1 || transports[0].Name() != "webpush" {
		t.Fatalf("configured transports = %v; want one webpush", transports)
	}
	if worker := newNotificationWorker(nil, transports, nil); worker == nil {
		t.Fatal("configured worker missing")
	}

	cfg.WebPush.Subject = ""
	if _, err := notificationTransports(cfg, nil, nil, nil, nil); err == nil {
		t.Fatal("invalid VAPID subject accepted")
	}
	cfg.WebPush.Subject = "mailto:ops@example.test"
	cfg.DataEncryptionKey = nil
	if _, err := notificationTransports(cfg, nil, nil, nil, nil); err == nil {
		t.Fatal("missing encryption key accepted")
	}
}

func configuredPushForTest() config.Config {
	return config.Config{BaseURL: "https://lan.example", InstanceName: "Camp Nova", DataEncryptionKey: make([]byte, 32), WebPush: &config.WebPush{
		PublicKey:  "BAPXjly0gRlUqDTh973k-L9pQbfuajNHt-75lr18ZrG1R9cW6wU9xZ62F7Ndf_nQW7FSAEP2-LXZldrFzngmFhQ",
		PrivateKey: "QUs8zCpDK_x4zKsunw_iz-nahUva_gFm-jekct86r70", Subject: "mailto:ops@example.test",
	}}
}

func TestCLIRetryTransportConstruction(t *testing.T) {
	disabled, err := cliNotificationTransports(config.Config{}, nil)
	if err != nil || len(disabled) != 0 {
		t.Fatalf("disabled CLI transports = %v, err = %v", disabled, err)
	}
	configured, err := cliNotificationTransports(configuredPushForTest(), nil)
	if err != nil {
		t.Fatalf("configured CLI transport: %v", err)
	}
	if len(configured) != 1 || configured[0].Name() != "webpush" {
		t.Fatalf("configured CLI transports = %v; want one webpush", configured)
	}
}
