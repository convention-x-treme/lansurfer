package main

import (
	"strings"
	"testing"
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
