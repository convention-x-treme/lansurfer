// Package handler contains HTTP transport adapters for the web application.
package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

const (
	browserBindingCookieName = "lansurfer_auth_binding"
	browserBindingLifetime   = 30 * time.Minute
	browserBindingBytes      = 32
	resolveQueryParam        = "resolve"
)

// Authentication starts and completes provider-agnostic OAuth flows. It is
// satisfied by *auth.OAuthService.
type Authentication interface {
	Available() bool
	Providers() []auth.ProviderAvailability
	Begin(context.Context, auth.OAuthStartInput) (auth.OAuthStart, error)
	Complete(context.Context, auth.OAuthCompletion) (auth.OAuthResult, error)
}

// Sessions revokes the browser session on logout. It is satisfied by
// *auth.SessionService; session resolution for request authentication is a
// separate narrow interface consumed directly by web/middleware.
type Sessions interface {
	Logout(context.Context, string) error
}

type AuthenticationHandler struct {
	authentication Authentication
	sessions       Sessions
	brand          branding.Brand
	secureCookies  bool
}

func NewAuthentication(authentication Authentication, sessions Sessions, brand branding.Brand, secureCookies bool) *AuthenticationHandler {
	return &AuthenticationHandler{authentication: authentication, sessions: sessions, brand: brand, secureCookies: secureCookies}
}

func (h *AuthenticationHandler) Login(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Login(h.brand, h.providers()))
}

// BeginFor starts a normal login (or, with a resolve query parameter, an
// existing-account proof) against one fixed, explicitly configured provider.
func (h *AuthenticationHandler) BeginFor(providerKind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authentication == nil || !h.authentication.Available() {
			h.renderError(w, r, http.StatusServiceUnavailable, "Login is not configured for this LANsurfer instance.")
			return
		}
		if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		binding, err := h.ensureBrowserBinding(w, r)
		if err != nil {
			webmiddleware.LogError(r, "issue browser-binding cookie", err)
			h.renderError(w, r, http.StatusInternalServerError, "Login could not be started. Please try again.")
			return
		}
		input := auth.OAuthStartInput{
			ProviderKind:   providerKind,
			Operation:      auth.FlowLogin,
			BrowserBinding: binding,
		}
		if resolve := r.URL.Query().Get(resolveQueryParam); resolve != "" {
			input.Operation = auth.FlowResolution
			input.ParentFlowID = resolve
		}
		start, err := h.authentication.Begin(r.Context(), input)
		if err != nil {
			webmiddleware.LogError(r, "start OAuth flow", err)
			h.renderError(w, r, http.StatusInternalServerError, "Login could not be started. Please try again.")
			return
		}
		http.Redirect(w, r, start.AuthorizationURL, http.StatusFound)
	}
}

// LinkFor starts an explicit account-linking flow for the current signed-in
// user against one fixed provider. The route this is mounted on requires
// authentication and CSRF protection (both applied by the composition root).
func (h *AuthenticationHandler) LinkFor(providerKind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := webmiddleware.CurrentUser(r.Context())
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if h.authentication == nil || !h.authentication.Available() {
			h.renderError(w, r, http.StatusServiceUnavailable, "Login is not configured for this LANsurfer instance.")
			return
		}
		binding, err := h.ensureBrowserBinding(w, r)
		if err != nil {
			webmiddleware.LogError(r, "issue browser-binding cookie", err)
			http.Redirect(w, r, "/account?link_error=1", http.StatusSeeOther)
			return
		}
		start, err := h.authentication.Begin(r.Context(), auth.OAuthStartInput{
			ProviderKind: providerKind, Operation: auth.FlowLink,
			BrowserBinding: binding, InitiatingUserID: account.ID,
		})
		if err != nil {
			webmiddleware.LogError(r, "start account link flow", err)
			http.Redirect(w, r, "/account?link_error=1", http.StatusSeeOther)
			return
		}
		// Not http.Redirect: this request is a form submission (CSRF requires
		// POST), and the form-action CSP directive is enforced against every
		// redirect hop chained from a form submission, so a raw 3xx redirect
		// straight to the provider's external authorization URL is silently
		// blocked by the browser. Completing the POST with a same-origin 200
		// page that then navigates onward itself sidesteps that restriction.
		render(w, r, http.StatusOK, pages.Redirecting(h.brand, start.AuthorizationURL))
	}
}

// CallbackFor completes a login, existing-account-proof, or account-link
// flow against one fixed provider, whichever operation the persisted flow
// row was created for.
func (h *AuthenticationHandler) CallbackFor(providerKind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authentication == nil || !h.authentication.Available() {
			h.renderError(w, r, http.StatusServiceUnavailable, "Login is not configured for this LANsurfer instance.")
			return
		}
		if r.URL.Query().Get("error") != "" {
			h.renderError(w, r, http.StatusUnauthorized, "The identity provider did not complete login.")
			return
		}
		binding, ok := h.browserBinding(r)
		currentUser, signedIn := webmiddleware.CurrentUser(r.Context())
		if !ok {
			h.renderError(w, r, http.StatusBadRequest, "The login attempt expired or could not be verified. Please start again.")
			return
		}
		completion := auth.OAuthCompletion{
			ProviderKind: providerKind, State: r.URL.Query().Get("state"), BrowserBinding: binding, Code: r.URL.Query().Get("code"),
		}
		if signedIn {
			completion.CurrentUserID = currentUser.ID
		}
		result, err := h.authentication.Complete(r.Context(), completion)
		if err != nil {
			h.handleCallbackError(w, r, err, signedIn)
			return
		}

		switch {
		case result.Pending.ID != "":
			http.Redirect(w, r, "/onboarding/"+result.Pending.ID, http.StatusSeeOther)
		case result.Operation == auth.FlowLink:
			http.Redirect(w, r, "/account", http.StatusSeeOther)
		default: // FlowLogin or FlowResolution: a normal session was created.
			http.SetCookie(w, webmiddleware.SessionCookie(result.Session.Token, result.Session.ExpiresAt, h.secureCookies))
			http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
		}
	}
}

func (h *AuthenticationHandler) handleCallbackError(w http.ResponseWriter, r *http.Request, err error, signedIn bool) {
	if (errors.Is(err, auth.ErrIdentityConflict) || errors.Is(err, auth.ErrProviderAlreadyLinked)) && signedIn {
		http.Redirect(w, r, "/account?link_error=1", http.StatusSeeOther)
		return
	}
	if errors.Is(err, auth.ErrResolutionIdentityUnknown) {
		h.renderError(w, r, http.StatusUnauthorized, "We could not verify that you already have a LANsurfer account. Please start again.")
		return
	}
	if errors.Is(err, auth.ErrInvalidCallback) || errors.Is(err, auth.ErrInvalidProfile) {
		h.renderError(w, r, http.StatusUnauthorized, "Login could not be completed. Please start again.")
		return
	}
	if isFlowValidationError(err) {
		h.renderError(w, r, http.StatusBadRequest, "The login attempt expired or could not be verified. Please start again.")
		return
	}
	webmiddleware.LogError(r, "complete OAuth flow", err)
	h.renderError(w, r, http.StatusInternalServerError, "Login could not be completed. Please try again.")
}

func isFlowValidationError(err error) bool {
	return errors.Is(err, auth.ErrFlowNotFound) || errors.Is(err, auth.ErrFlowExpired) || errors.Is(err, auth.ErrFlowConsumed) ||
		errors.Is(err, auth.ErrFlowBrowser) || errors.Is(err, auth.ErrFlowProvider) || errors.Is(err, auth.ErrFlowOperation) ||
		errors.Is(err, auth.ErrFlowInitiatingUser)
}

func (h *AuthenticationHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(webmiddleware.SessionCookieName); err == nil && h.sessions != nil {
		if err := h.sessions.Logout(r.Context(), cookie.Value); err != nil {
			webmiddleware.LogError(r, "revoke authentication session", err)
			h.renderError(w, r, http.StatusInternalServerError, "Logout could not be completed. Please try again.")
			return
		}
	}
	http.SetCookie(w, webmiddleware.ExpiredSessionCookie(h.secureCookies))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthenticationHandler) providers() []auth.ProviderAvailability {
	if h.authentication == nil {
		return nil
	}
	return h.authentication.Providers()
}

// ensureBrowserBinding returns the browser-binding secret already on the
// request, or issues a new one. The same value is reused across concurrent
// tabs/flows from one browser so a resolution flow started from the
// onboarding page can still reach the original pending identity.
func (h *AuthenticationHandler) ensureBrowserBinding(w http.ResponseWriter, r *http.Request) (string, error) {
	if value, ok := h.browserBinding(r); ok {
		return value, nil
	}
	value, err := randomBrowserBinding()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: browserBindingCookieName, Value: value, Path: "/",
		MaxAge: int(browserBindingLifetime.Seconds()), HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	return value, nil
}

func (h *AuthenticationHandler) browserBinding(r *http.Request) (string, bool) {
	return browserBindingCookie(r)
}

// browserBindingCookie reads the browser-binding secret already on the
// request, if any, without issuing a new one. It is shared by
// AuthenticationHandler and OnboardingHandler, since a resolution flow
// started from the onboarding page must present the same binding the
// original pending identity was created under.
func browserBindingCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(browserBindingCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

func randomBrowserBinding() (string, error) {
	value := make([]byte, browserBindingBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (h *AuthenticationHandler) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	render(w, r, status, pages.AuthenticationError(h.brand, message, h.providers()))
}

func render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	var rendered bytes.Buffer
	if err := component.Render(r.Context(), &rendered); err != nil {
		webmiddleware.LogError(r, "render HTML response", err)
		http.Error(w, "render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = rendered.WriteTo(w)
}
