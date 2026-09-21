package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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
	handler := NewNotificationSettings(&stubSubscriptionManager{}, "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Show(response, httptest.NewRequest(http.MethodGet, "/account/notifications", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsSubscribeEnablesAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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
	if subscriptions.enableDeviceID == "" {
		t.Fatal("expected a non-empty device ID")
	}
	result := response.Result()
	defer result.Body.Close()
	deviceCookie := responseCookie(t, result, "lansurfer_device")
	if deviceCookie.Value == "" || !deviceCookie.HttpOnly || !deviceCookie.Secure {
		t.Fatalf("device cookie = %#v", deviceCookie)
	}
}

func TestNotificationSettingsSubscribeNormalizesEndpointBeforeEnable(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
	// The submitted scheme is uppercase; url.Parse+String lowercases it on
	// the round trip, so asserting the exact normalized string proves
	// Enable receives the Parse/String output, not the raw form value.
	rawEndpoint := "HTTPS://push.example/endpoint?x=1"
	form := strings.NewReader("endpoint=" + url.QueryEscape(rawEndpoint) + "&p256dh=p256dh-value&auth=auth-value")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Subscribe(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	const wantNormalized = "https://push.example/endpoint?x=1"
	if subscriptions.enableEndpoint != wantNormalized {
		t.Fatalf("enable endpoint = %q, want the normalized form %q (not the raw submitted value %q)", subscriptions.enableEndpoint, wantNormalized, rawEndpoint)
	}
}

func TestNotificationSettingsSubscribeRejectsNonHTTPSEndpoint(t *testing.T) {
	tests := []string{
		"http://192.168.1.1/endpoint",             // wrong scheme (and a private-network host)
		"http://push.example/endpoint",            // wrong scheme
		"https://user:pass@push.example/endpoint", // userinfo component
		"https:///endpoint",                       // empty host
		"not a url at all",
		"",
	}
	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			subscriptions := &stubSubscriptionManager{}
			handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
			form := strings.NewReader("endpoint=" + url.QueryEscape(endpoint) + "&p256dh=p256dh-value&auth=auth-value")
			request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", form), user.User{ID: "user-1"})
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()

			handler.Subscribe(response, request)

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
			}
			if subscriptions.enableCalls != 0 {
				t.Fatalf("enable calls = %d, want 0 (Enable must never be called for a rejected endpoint)", subscriptions.enableCalls)
			}
		})
	}
}

func TestNotificationSettingsSubscribeRejectsOversizedFields(t *testing.T) {
	tests := map[string]string{
		"endpoint": "endpoint=" + url.QueryEscape("https://push.example/"+strings.Repeat("a", maxEndpointBytes)) + "&p256dh=p256dh-value&auth=auth-value",
		"p256dh":   "endpoint=https://push.example/endpoint&p256dh=" + strings.Repeat("a", maxP256dhBytes+1) + "&auth=auth-value",
		"auth":     "endpoint=https://push.example/endpoint&p256dh=p256dh-value&auth=" + strings.Repeat("a", maxAuthBytes+1),
		"label":    "endpoint=https://push.example/endpoint&p256dh=p256dh-value&auth=auth-value&label=" + strings.Repeat("a", maxLabelLength+1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			subscriptions := &stubSubscriptionManager{}
			handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
			request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", strings.NewReader(body)), user.User{ID: "user-1"})
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()

			handler.Subscribe(response, request)

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
			}
			if subscriptions.enableCalls != 0 {
				t.Fatalf("enable calls = %d, want 0", subscriptions.enableCalls)
			}
		})
	}
}

func TestNotificationSettingsSubscribeRejectsIncompleteForm(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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
	handler := NewNotificationSettings(&stubSubscriptionManager{}, "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Subscribe(response, httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsSubscribeReturnsNotFoundWhenWebPushNotConfigured(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", false, branding.Default(), true)
	form := strings.NewReader("endpoint=https://push.example/endpoint&p256dh=p256dh-value&auth=auth-value")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/subscribe", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Subscribe(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if subscriptions.enableCalls != 0 {
		t.Fatalf("enable calls = %d, want 0", subscriptions.enableCalls)
	}
}

func TestNotificationSettingsDisableCallsThroughAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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
	handler := NewNotificationSettings(&stubSubscriptionManager{}, "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.Disable(response, httptest.NewRequest(http.MethodPost, "/account/notifications/disable", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsSetAudienceCallsThroughAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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

func TestNotificationSettingsSetAudienceRejectsInvalidAudienceWith422(t *testing.T) {
	subscriptions := &stubSubscriptionManager{audienceErr: notification.ErrInvalidAudience}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
	form := strings.NewReader("audience=everyone")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/audience", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.SetAudience(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
}

func TestNotificationSettingsSetAudienceReturns500OnStoreErrorDistinctFromInvalidAudience(t *testing.T) {
	subscriptions := &stubSubscriptionManager{audienceErr: errors.New("database is unreachable")}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
	form := strings.NewReader("audience=all")
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/notifications/audience", form), user.User{ID: "user-1"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.SetAudience(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (a genuine infrastructure failure must not look like a 422 user error)", response.Code, http.StatusInternalServerError)
	}
}

func TestNotificationSettingsSetAudienceRejectsAnonymousRequest(t *testing.T) {
	handler := NewNotificationSettings(&stubSubscriptionManager{}, "vapid-public-key", true, branding.Default(), true)
	response := httptest.NewRecorder()

	handler.SetAudience(response, httptest.NewRequest(http.MethodPost, "/account/notifications/audience", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNotificationSettingsRemoveDeviceParsesIDAndRedirects(t *testing.T) {
	subscriptions := &stubSubscriptionManager{}
	handler := NewNotificationSettings(subscriptions, "vapid-public-key", true, branding.Default(), true)
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
	handler := NewNotificationSettings(&stubSubscriptionManager{}, "vapid-public-key", true, branding.Default(), true)
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

	enableCalls    int
	enableUserID   string
	enableDeviceID string
	enableLabel    string
	enableEndpoint string
	enableP256dh   string
	enableAuth     string

	disableUserID string

	audienceUserID string
	audienceValue  string
	audienceErr    error

	removeDeviceUserID string
	removeDeviceID     int64
}

func (s *stubSubscriptionManager) Enable(_ context.Context, userID, deviceID, label, endpoint, p256dh, auth string) error {
	s.enableCalls++
	s.enableUserID, s.enableDeviceID, s.enableLabel = userID, deviceID, label
	s.enableEndpoint, s.enableP256dh, s.enableAuth = endpoint, p256dh, auth
	return nil
}

func (s *stubSubscriptionManager) Disable(_ context.Context, userID string) error {
	s.disableUserID = userID
	return nil
}

func (s *stubSubscriptionManager) SetAudience(_ context.Context, userID, audience string) error {
	s.audienceUserID, s.audienceValue = userID, audience
	return s.audienceErr
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
