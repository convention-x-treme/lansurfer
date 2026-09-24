package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
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

type DiscordDelivery struct {
	setup         DiscordDestinationSetup
	authorize     DiscordDestinationAuthorization
	connector     DiscordDestinationConnector
	lifecycle     DiscordDestinationLifecycle
	secureCookies bool
}

func NewDiscordDelivery(setup DiscordDestinationSetup, authorize DiscordDestinationAuthorization, connector DiscordDestinationConnector, lifecycle DiscordDestinationLifecycle, secureCookies bool) *DiscordDelivery {
	return &DiscordDelivery{setup: setup, authorize: authorize, connector: connector, lifecycle: lifecycle, secureCookies: secureCookies}
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
	http.Redirect(w, r, h.authorize.AuthorizationURL(state), http.StatusSeeOther)
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
	if code := r.URL.Query().Get("code"); code == "" {
		http.Error(w, "Discord authorization was not completed.", http.StatusBadRequest)
		return
	} else if err := h.connector.Connect(r.Context(), code); err != nil {
		http.Error(w, "Could not complete Discord setup. Please try again.", http.StatusBadGateway)
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
