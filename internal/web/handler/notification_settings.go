package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// SubscriptionManager is the narrow interface this handler needs from
// *notification.SubscriptionService.
type SubscriptionManager interface {
	Enable(ctx context.Context, userID, deviceIDHash, label, endpoint, p256dh, auth string) error
	Disable(ctx context.Context, userID string) error
	SetAudience(ctx context.Context, userID, audience string) error
	RemoveDevice(ctx context.Context, userID string, subscriptionID int64) error
	ListDevices(ctx context.Context, userID string) ([]notification.PushSubscriptionRecord, error)
	Preference(ctx context.Context, userID string) (notification.PushPreference, error)
}

type NotificationSettingsHandler struct {
	subscriptions  SubscriptionManager
	encryptionKey  []byte
	vapidPublicKey string
	webPushEnabled bool // whether Web Push is configured at all (VAPID present)
	brand          branding.Brand
	secureCookies  bool
}

func NewNotificationSettings(subscriptions SubscriptionManager, encryptionKey []byte, vapidPublicKey string, webPushConfigured bool, brand branding.Brand, secureCookies bool) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{
		subscriptions: subscriptions, encryptionKey: encryptionKey, vapidPublicKey: vapidPublicKey,
		webPushEnabled: webPushConfigured, brand: brand, secureCookies: secureCookies,
	}
}

func (h *NotificationSettingsHandler) Show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

// Subscribe is the complete enable action: persist the browser-created
// PushSubscription and set web_push_enabled=true.
func (h *NotificationSettingsHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
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
	label := r.FormValue("label")
	if label == "" {
		label = "This device"
	}
	deviceID, err := webmiddleware.CurrentOrNewDeviceID(r)
	if err != nil {
		webmiddleware.LogError(r, "generate device ID", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	deviceHash := platformcrypto.Fingerprint(h.encryptionKey, deviceID)
	if err := h.subscriptions.Enable(r.Context(), account.ID, deviceHash, label, endpoint, p256dh, auth); err != nil {
		webmiddleware.LogError(r, "enable Web Push", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, webmiddleware.DeviceCookie(deviceID, h.secureCookies))
	h.redirect(w, r, "/account/notifications?notice=push-enabled")
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
		h.render(w, r, http.StatusUnprocessableEntity, "Choose a valid audience.")
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

func (h *NotificationSettingsHandler) render(w http.ResponseWriter, r *http.Request, status int, formError string) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	devices, err := h.subscriptions.ListDevices(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "list push devices", err)
		http.Error(w, "load notification settings", http.StatusInternalServerError)
		return
	}
	preference, err := h.subscriptions.Preference(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "load push preference", err)
		http.Error(w, "load notification settings", http.StatusInternalServerError)
		return
	}
	navigation := components.NavigationState{Viewer: &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)}, Section: "account"}
	render(w, r, status, pages.NotificationSettings(h.brand, navigation, pages.NotificationSettingsView{
		WebPushAvailable: h.webPushEnabled,
		VAPIDPublicKey:   h.vapidPublicKey,
		Preference:       preference,
		Devices:          devices,
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
