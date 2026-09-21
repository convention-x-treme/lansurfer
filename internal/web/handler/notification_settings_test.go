package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

func TestNotificationSettingsShowRendersForSignedInUser(t *testing.T) {
	subscriptions := &stubSubscriptionManager{
		devices:    []notification.PushSubscriptionRecord{{ID: 1, Label: "Laptop"}},
		preference: notification.PushPreference{WebPushEnabled: true, NewRequestAudience: notification.AudienceAll},
	}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/account/notifications", nil), user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	handler.Show(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Laptop") {
		t.Fatalf("status/body = %d, %q", response.Code, response.Body.String())
	}
	if subscriptions.listDevicesUserID != "user-1" || subscriptions.preferenceUserID != "user-1" {
		t.Fatalf("query calls = %#v", subscriptions)
	}
}

func TestNotificationSettingsShowRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Show(response, httptest.NewRequest(http.MethodGet, "/account/notifications", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsSubscribeEnablesAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	form := strings.NewReader("endpoint=https://push.example/endpoint&p256dh=p256dh-value&auth=auth-value&label=Laptop")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Subscribe(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account/notifications?notice=push-enabled" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if subscriptions.enableUserID != "user-1" || subscriptions.enableLabel != "Laptop" ||
		subscriptions.enableEndpoint != "https://push.example/endpoint" || subscriptions.enableP256dh != "p256dh-value" || subscriptions.enableAuth != "auth-value" {
		t.Fatalf("enable call = %#v", subscriptions)
	}
	if subscriptions.enableDeviceIDHash == "" {
		t.Fatal("expected a non-empty device ID hash")
	}
	result := response.Result()
	defer result.Body.Close()
	deviceCookie := responseCookie(t, result, "lansurfer_device")
	if deviceCookie.Value == "" || !deviceCookie.HttpOnly || !deviceCookie.Secure {
		t.Fatalf("device cookie = %#v", deviceCookie)
	}
}

func TestNotificationSettingsSubscribeRejectsIncompleteForm(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	form := strings.NewReader("endpoint=https://push.example/endpoint")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Subscribe(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if subscriptions.enableCalls != 0 {
		t.Fatalf("enable calls = %d, want 0", subscriptions.enableCalls)
	}
}

func TestNotificationSettingsSubscribeRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Subscribe(response, httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsDisableCallsThroughAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/disable", nil), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.Disable(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account/notifications?notice=push-disabled" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if subscriptions.disableUserID != "user-1" {
		t.Fatalf("disable userID = %q", subscriptions.disableUserID)
	}
}

func TestNotificationSettingsDisableRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Disable(response, httptest.NewRequest(http.MethodPost, "/account/notifications/disable", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsSetAudienceCallsThroughAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	form := strings.NewReader("audience=all")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/audience", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.SetAudience(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account/notifications?notice=audience-updated" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if subscriptions.audienceUserID != "user-1" || subscriptions.audienceValue != "all" {
		t.Fatalf("set audience call = %#v", subscriptions)
	}
}

func TestNotificationSettingsSetAudienceRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.SetAudience(response, httptest.NewRequest(http.MethodPost, "/account/notifications/audience", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsRemoveDeviceParsesIDAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	request := withCurrentUser(withURLParam(httptest.NewRequest(http.MethodPost, "/account/notifications/devices/42/remove", nil), "subscriptionID", "42"), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.RemoveDevice(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account/notifications?notice=device-removed" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if subscriptions.removeDeviceUserID != "user-1" || subscriptions.removeDeviceID != 42 {
		t.Fatalf("remove device call = %#v", subscriptions)
	}
}

func TestNotificationSettingsRemoveDeviceRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, []byte("key"), "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.RemoveDevice(response, httptest.NewRequest(http.MethodPost, "/account/notifications/devices/42/remove", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

type stubSubscriptionManager struct {
	devices    []notification.PushSubscriptionRecord
	preference notification.PushPreference

	listDevicesUserID string
	preferenceUserID  string

	enableCalls        int
	enableUserID       string
	enableDeviceIDHash string
	enableLabel        string
	enableEndpoint     string
	enableP256dh       string
	enableAuth         string

	disableUserID string

	audienceUserID string
	audienceValue  string

	removeDeviceUserID string
	removeDeviceID     int64
}

func (s *stubSubscriptionManager) Enable(_ context.Context, userID, deviceIDHash, label, endpoint, p256dh, auth string) error {
	s.enableCalls++
	s.enableUserID, s.enableDeviceIDHash, s.enableLabel = userID, deviceIDHash, label
	s.enableEndpoint, s.enableP256dh, s.enableAuth = endpoint, p256dh, auth
	return nil
}

func (s *stubSubscriptionManager) Disable(_ context.Context, userID string) error {
	s.disableUserID = userID
	return nil
}

func (s *stubSubscriptionManager) SetAudience(_ context.Context, userID, audience string) error {
	s.audienceUserID, s.audienceValue = userID, audience
	return nil
}

func (s *stubSubscriptionManager) RemoveDevice(_ context.Context, userID string, subscriptionID int64) error {
	s.removeDeviceUserID, s.removeDeviceID = userID, subscriptionID
	return nil
}

func (s *stubSubscriptionManager) ListDevices(_ context.Context, userID string) ([]notification.PushSubscriptionRecord, error) {
	s.listDevicesUserID = userID
	return s.devices, nil
}

func (s *stubSubscriptionManager) Preference(_ context.Context, userID string) (notification.PushPreference, error) {
	s.preferenceUserID = userID
	return s.preference, nil
}
