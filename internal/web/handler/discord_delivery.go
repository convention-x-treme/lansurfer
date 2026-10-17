package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

type DiscordDestinationSetup interface {
	Start(context.Context, string, string) (string, error)
	Consume(context.Context, string, string, string) (notification.DiscordSetupState, error)
}

type DiscordDestinationAuthorization interface {
	AuthorizationURL(string) string
}

type DiscordDestinationConnector interface {
	Connect(context.Context, string) error
}
type DiscordDestinationLifecycle interface {
	Disconnect(context.Context) error
}

type DiscordDestinationStatusReader interface {
	GetDiscordDestinationStatus(context.Context) (notification.DiscordDestination, error)
}

type DiscordDelivery struct {
	setup         DiscordDestinationSetup
	authorize     DiscordDestinationAuthorization
	connector     DiscordDestinationConnector
	lifecycle     DiscordDestinationLifecycle
	status        DiscordDestinationStatusReader
	navigation    *Navigation
	brand         branding.Brand
	secureCookies bool
}

func NewDiscordDelivery(
	setup DiscordDestinationSetup,
	authorize DiscordDestinationAuthorization,
	connector DiscordDestinationConnector,
	lifecycle DiscordDestinationLifecycle,
	status DiscordDestinationStatusReader,
	navigation *Navigation,
	brand branding.Brand,
	secureCookies bool,
) *DiscordDelivery {
	return &DiscordDelivery{
		setup: setup, authorize: authorize, connector: connector, lifecycle: lifecycle,
		status: status, navigation: navigation, brand: brand, secureCookies: secureCookies,
	}
}

func (h *DiscordDelivery) Show(w http.ResponseWriter, r *http.Request) {
	destination, err := h.status.GetDiscordDestinationStatus(r.Context())
	if err != nil && !errors.Is(err, notification.ErrDiscordDestinationNotFound) {
		webmiddleware.LogError(r, "load discord destination status", err)
		http.Error(w, "Discord delivery status could not be loaded.", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.AdminDiscordDelivery(
		h.brand, h.navigation.For(r, "administration-discord"), destination, discordDeliveryNotice(r),
	))
}

func discordDeliveryNotice(r *http.Request) components.Notice {
	switch r.URL.Query().Get("notice") {
	case "connected":
		return components.Notice{Text: "Discord destination connected.", Tone: components.NoticeSuccess}
	case "disconnected":
		return components.Notice{Text: "Discord destination disconnected.", Tone: components.NoticeInfo}
	case "cancelled":
		return components.Notice{Text: "Discord webhook setup was cancelled.", Tone: components.NoticeInfo}
	case "failed":
		return components.Notice{Text: "Discord webhook setup could not be completed. Please try again.", Tone: components.NoticeWarning}
	case "not-configured":
		return components.Notice{Text: "Discord destination is not configured.", Tone: components.NoticeWarning}
	default:
		return components.Notice{}
	}
}
func (h *DiscordDelivery) Start(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	binding, err := ensureBrowserBinding(w, r, h.secureCookies)
	if err != nil {
		webmiddleware.LogError(r, "issue browser-binding cookie for discord setup", err)
		http.Error(w, "Could not start Discord setup. Please try again.", http.StatusInternalServerError)
		return
	}
	state, err := h.setup.Start(r.Context(), account.ID, binding)
	if err != nil {
		webmiddleware.LogError(r, "start discord destination setup", err)
		http.Error(w, "Could not start Discord setup. Please try again.", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pages.Redirecting(h.brand, h.authorize.AuthorizationURL(state)))
}

func (h *DiscordDelivery) Callback(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	binding, ok := browserBindingCookie(r)
	if !ok {
		http.Error(w, "Discord setup state is invalid or expired.", http.StatusBadRequest)
		return
	}
	if _, err := h.setup.Consume(r.Context(), account.ID, binding, r.URL.Query().Get("state")); err != nil {
		if errors.Is(err, notification.ErrDiscordSetupStateInvalid) {
			http.Error(w, "Discord setup state is invalid or expired.", http.StatusBadRequest)
			return
		}
		webmiddleware.LogError(r, "consume discord destination setup state", err)
		http.Error(w, "Could not complete Discord setup. Please try again.", http.StatusInternalServerError)
		return
	}
	// ponytail: state was consumed above, so a cancel can't be replayed.
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		if providerError == "access_denied" {
			http.Redirect(w, r, "/admin/discord?notice=cancelled", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/discord?notice=failed", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/admin/discord?notice=failed", http.StatusSeeOther)
		return
	}
	if err := h.connector.Connect(r.Context(), code); err != nil {
		webmiddleware.LogError(r, "connect discord destination", err)
		http.Redirect(w, r, "/admin/discord?notice=failed", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/discord?notice=connected", http.StatusSeeOther)
}

func (h *DiscordDelivery) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.lifecycle.Disconnect(r.Context()); err != nil {
		if errors.Is(err, notification.ErrDiscordDestinationNotFound) {
			http.Redirect(w, r, "/admin/discord?notice=not-configured", http.StatusSeeOther)
			return
		}
		webmiddleware.LogError(r, "disconnect discord destination", err)
		http.Error(w, "Could not disconnect Discord delivery. Please try again.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/discord?notice=disconnected", http.StatusSeeOther)
}
