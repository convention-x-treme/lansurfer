package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// Real Web Push subscription material is small (an endpoint URL, a
// base64-encoded P-256 public key, and a base64-encoded 16-byte auth
// secret); these caps are generous but real, so a malformed or hostile
// submission is rejected long before it reaches encryption or storage.
// maxLabelLength matches the repo's existing convention for a human-entered
// display string (see internal/game's maxNameLength/maxAliasLength).
const (
	maxEndpointBytes = 2048
	maxP256dhBytes   = 256
	maxAuthBytes     = 256
	maxLabelLength   = 120
)

// SubscriptionManager is the narrow interface this handler needs from
// *notification.SubscriptionService. deviceID is the raw, opaque device
// cookie value: SubscriptionService is the only place that hashes it.
type SubscriptionManager interface {
	Enable(ctx context.Context, userID, deviceID, label, endpoint, p256dh, auth string) error
	Disable(ctx context.Context, userID string) error
	SetAudience(ctx context.Context, userID, audience string) error
	RemoveDevice(ctx context.Context, userID string, subscriptionID int64) error
	ListDevices(ctx context.Context, userID string) ([]notification.PushSubscriptionRecord, error)
	RemoveDeviceByHash(ctx context.Context, userID, deviceID string) error
	CurrentDeviceID(ctx context.Context, userID, deviceID string) (int64, error)
	Preference(ctx context.Context, userID string) (notification.PushPreference, error)
}

type NotificationSettingsHandler struct {
	subscriptions  SubscriptionManager
	vapidPublicKey string
	webPushEnabled bool // whether Web Push is configured at all (VAPID present)
	brand          branding.Brand
	secureCookies  bool
}

func NewNotificationSettings(subscriptions SubscriptionManager, vapidPublicKey string, webPushConfigured bool, brand branding.Brand, secureCookies bool) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{
		subscriptions: subscriptions, vapidPublicKey: vapidPublicKey,
		webPushEnabled: webPushConfigured, brand: brand, secureCookies: secureCookies,
	}
}

func (h *NotificationSettingsHandler) Show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

// Subscribe is the complete enable action: persist the browser-created
// PushSubscription and set web_push_enabled=true.
func (h *NotificationSettingsHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	if !h.webPushEnabled {
		http.Error(w, "Web Push is not configured", http.StatusNotFound)
		return
	}
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "The subscription request could not be read.")
		return
	}
	endpoint, p256dh, auth := r.FormValue("endpoint"), r.FormValue("p256dh"), r.FormValue("auth")
	if endpoint == "" || p256dh == "" || auth == "" {
		h.render(w, r, http.StatusUnprocessableEntity, "The browser subscription was incomplete.")
		return
	}
	if len(endpoint) > maxEndpointBytes || len(p256dh) > maxP256dhBytes || len(auth) > maxAuthBytes {
		h.render(w, r, http.StatusUnprocessableEntity, "The browser subscription was incomplete.")
		return
	}
	normalizedEndpoint, ok := validPushEndpoint(endpoint)
	if !ok {
		h.render(w, r, http.StatusUnprocessableEntity, "The browser subscription was incomplete.")
		return
	}
	label := r.FormValue("label")
	if label == "" {
		label = "This device"
	} else if utf8.RuneCountInString(label) > maxLabelLength {
		h.render(w, r, http.StatusUnprocessableEntity, "The browser subscription was incomplete.")
		return
	}
	deviceID, err := webmiddleware.CurrentOrNewDeviceID(r)
	if err != nil {
		webmiddleware.LogError(r, "generate device ID", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	if err := h.subscriptions.Enable(r.Context(), account.ID, deviceID, label, normalizedEndpoint, p256dh, auth); err != nil {
		webmiddleware.LogError(r, "enable Web Push", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, webmiddleware.DeviceCookie(deviceID, h.secureCookies))
	h.redirect(w, r, "/account/notifications?notice=push-enabled")
}

// validPushEndpoint reports whether endpoint is a parseable absolute HTTPS
// URL with a host and no userinfo component, mirroring the shape check
// config.Load applies to LANSURFER_BASE_URL. It returns the endpoint
// normalized by a Parse/String round-trip (which normalizes things like
// scheme/host case), so the value used for fingerprinting and later
// delivery is never the raw, unnormalized form the browser submitted -
// serving the "one endpoint belongs to at most one user" uniqueness
// invariant and guarding against the delivery worker later being pointed
// at an arbitrary non-HTTPS or private-network URL (SSRF).
func validPushEndpoint(endpoint string) (string, bool) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	return parsed.String(), true
}

func (h *NotificationSettingsHandler) Disable(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := h.subscriptions.Disable(r.Context(), account.ID); err != nil {
		webmiddleware.LogError(r, "disable Web Push", err)
		http.Error(w, "disable Web Push", http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/account/notifications?notice=push-disabled")
}

func (h *NotificationSettingsHandler) SetAudience(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "The form could not be read.")
		return
	}
	if err := h.subscriptions.SetAudience(r.Context(), account.ID, r.FormValue("audience")); err != nil {
		if errors.Is(err, notification.ErrInvalidAudience) {
			h.render(w, r, http.StatusUnprocessableEntity, "Choose a valid audience.")
			return
		}
		webmiddleware.LogError(r, "set notification audience", err)
		http.Error(w, "set notification audience", http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/account/notifications?notice=audience-updated")
}

func (h *NotificationSettingsHandler) RemoveDevice(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	subscriptionID, err := strconv.ParseInt(chi.URLParam(r, "subscriptionID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid device ID", http.StatusBadRequest)
		return
	}
	if err := h.subscriptions.RemoveDevice(r.Context(), account.ID, subscriptionID); err != nil {
		webmiddleware.LogError(r, "remove push device", err)
		http.Error(w, "remove device", http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/account/notifications?notice=device-removed")
}

// ReconcileCurrentDevice removes only the device identified by the HttpOnly
// cookie. Missing or stale cookies are idempotent; preferences stay intact.
func (h *NotificationSettingsHandler) ReconcileCurrentDevice(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if cookie, err := r.Cookie(webmiddleware.DeviceCookieName); err == nil && cookie.Value != "" {
		if err := h.subscriptions.RemoveDeviceByHash(r.Context(), account.ID, cookie.Value); err != nil {
			webmiddleware.LogError(r, "reconcile push device", err)
			http.Error(w, "reconcile device", http.StatusInternalServerError)
			return
		}
	}
	http.SetCookie(w, webmiddleware.ExpiredDeviceCookie(h.secureCookies))
	h.redirect(w, r, "/account/notifications")
}

func (h *NotificationSettingsHandler) render(w http.ResponseWriter, r *http.Request, status int, formError string) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var devices []notification.PushSubscriptionRecord
	preference := notification.PushPreference{NewRequestAudience: notification.AudienceFollowed}
	var currentDeviceID int64
	if h.subscriptions != nil {
		var err error
		devices, err = h.subscriptions.ListDevices(r.Context(), account.ID)
		if err != nil {
			webmiddleware.LogError(r, "list push devices", err)
			http.Error(w, "load notification settings", http.StatusInternalServerError)
			return
		}
		preference, err = h.subscriptions.Preference(r.Context(), account.ID)
		if err != nil {
			webmiddleware.LogError(r, "load push preference", err)
			http.Error(w, "load notification settings", http.StatusInternalServerError)
			return
		}
		if cookie, err := r.Cookie(webmiddleware.DeviceCookieName); err == nil && cookie.Value != "" {
			currentDeviceID, err = h.subscriptions.CurrentDeviceID(r.Context(), account.ID, cookie.Value)
			if err != nil {
				webmiddleware.LogError(r, "find current push device", err)
				http.Error(w, "load notification settings", http.StatusInternalServerError)
				return
			}
		}
	}
	navigation := components.NavigationState{Viewer: &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)}, Section: "account"}
	render(w, r, status, pages.NotificationSettings(h.brand, navigation, pages.NotificationSettingsView{
		WebPushAvailable: h.webPushEnabled,
		VAPIDPublicKey:   h.vapidPublicKey,
		Preference:       preference,
		Devices:          devices,
		CurrentDeviceID:  currentDeviceID,
		Notice:           notificationSettingsNotice(r),
		FormError:        formError,
	}))
}

func (h *NotificationSettingsHandler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func notificationSettingsNotice(r *http.Request) string {
	switch r.URL.Query().Get("notice") {
	case "push-enabled":
		return "Push notifications enabled on this device."
	case "push-disabled":
		return "Push notifications disabled. Your devices remain registered."
	case "audience-updated":
		return "Notification audience updated."
	case "device-removed":
		return "Device removed."
	default:
		return ""
	}
}
