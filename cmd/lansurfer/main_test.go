package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

func TestPromoteUserID(t *testing.T) {
	userID, err := promoteUserID([]string{"--user-id", "2c359aee-7690-4975-a2f8-1730cf37f42b"})
	if err != nil || userID != "2c359aee-7690-4975-a2f8-1730cf37f42b" {
		t.Fatalf("promoteUserID() = %q, %v", userID, err)
	}

	for _, arguments := range [][]string{nil, {"--user-id", ""}, {"unexpected"}} {
		if _, err := promoteUserID(arguments); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("promoteUserID(%q) error = %v, want usage error", arguments, err)
		}
	}
}

func TestNotificationDeliveryID(t *testing.T) {
	deliveryID, err := notificationDeliveryID([]string{"--delivery-id", "42"})
	if err != nil || deliveryID != "42" {
		t.Fatalf("notificationDeliveryID() = %q, %v", deliveryID, err)
	}

	for _, arguments := range [][]string{nil, {"--delivery-id", ""}, {"unexpected"}} {
		if _, err := notificationDeliveryID(arguments); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("notificationDeliveryID(%q) error = %v, want usage error", arguments, err)
		}
	}
}

func TestGenerateSecretsAcceptedByConfig(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), []string{"admin", "notifications", "generate-secrets"}, slog.New(slog.DiscardHandler), &out); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		t.Setenv(key, value)
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("config.Load() rejected generated secrets: %v", err)
	}
}
