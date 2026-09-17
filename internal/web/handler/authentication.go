// Package handler contains HTTP transport adapters for the web application.
package handler

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

const (
	loginFlowLifetime = 10 * time.Minute
	stateCookieName   = "lansurfer_oidc_state"
	nonceCookieName   = "lansurfer_oidc_nonce"
	pkceCookieName    = "lansurfer_oidc_pkce"
	callbackPath      = "/auth/callback"
)

type Authentication interface {
	Available() bool
	BeginLogin() (auth.LoginFlow, error)
	CompleteLogin(context.Context, string, string, string) (user.User, auth.BrowserSession, error)
	Logout(context.Context, string) error
}

type AuthenticationHandler struct {
	authentication Authentication
	brand          branding.Brand
	secureCookies  bool
}

func NewAuthentication(authentication Authentication, brand branding.Brand, secureCookies bool) *AuthenticationHandler {
	return &AuthenticationHandler{authentication: authentication, brand: brand, secureCookies: secureCookies}
}

func (h *AuthenticationHandler) Login(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Login(h.brand, h.authentication != nil && h.authentication.Available()))
}

func (h *AuthenticationHandler) BeginLogin(w http.ResponseWriter, r *http.Request) {
	if h.authentication == nil || !h.authentication.Available() {
		h.renderError(w, r, http.StatusServiceUnavailable, "Login is not configured for this LANsurfer instance.")
		return
	}
	flow, err := h.authentication.BeginLogin()
	if err != nil {
		webmiddleware.LogError(r, "start OIDC login", err)
		h.renderError(w, r, http.StatusInternalServerError, "Login could not be started. Please try again.")
		return
	}
	h.setFlowCookie(w, stateCookieName, flow.State)
	h.setFlowCookie(w, nonceCookieName, flow.Nonce)
	h.setFlowCookie(w, pkceCookieName, flow.PKCEVerifier)
	http.Redirect(w, r, flow.AuthorizationURL, http.StatusFound)
}

func (h *AuthenticationHandler) Callback(w http.ResponseWriter, r *http.Request) {
	state, stateErr := r.Cookie(stateCookieName)
	nonce, nonceErr := r.Cookie(nonceCookieName)
	verifier, verifierErr := r.Cookie(pkceCookieName)
	h.clearFlowCookies(w)
	if h.authentication == nil || !h.authentication.Available() {
		h.renderError(w, r, http.StatusServiceUnavailable, "Login is not configured for this LANsurfer instance.")
		return
	}

	if r.URL.Query().Get("error") != "" {
		h.renderError(w, r, http.StatusUnauthorized, "The identity provider did not complete login.")
		return
	}
	returnedState := r.URL.Query().Get("state")
	if stateErr != nil || nonceErr != nil || verifierErr != nil || returnedState == "" ||
		subtle.ConstantTimeCompare([]byte(state.Value), []byte(returnedState)) != 1 {
		h.renderError(w, r, http.StatusBadRequest, "The login attempt expired or could not be verified. Please start again.")
		return
	}

	_, session, err := h.authentication.CompleteLogin(
		r.Context(),
		r.URL.Query().Get("code"),
		verifier.Value,
		nonce.Value,
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCallback) || errors.Is(err, auth.ErrInvalidProfile) {
			h.renderError(w, r, http.StatusUnauthorized, "Login could not be completed. Please start again.")
			return
		}
		webmiddleware.LogError(r, "complete OIDC login", err)
		h.renderError(w, r, http.StatusInternalServerError, "Login could not be completed. Please try again.")
		return
	}
	http.SetCookie(w, webmiddleware.SessionCookie(session.Token, session.ExpiresAt, h.secureCookies))
	http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
}

func (h *AuthenticationHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(webmiddleware.SessionCookieName); err == nil && h.authentication != nil {
		if err := h.authentication.Logout(r.Context(), cookie.Value); err != nil {
			webmiddleware.LogError(r, "revoke authentication session", err)
			h.renderError(w, r, http.StatusInternalServerError, "Logout could not be completed. Please try again.")
			return
		}
	}
	http.SetCookie(w, webmiddleware.ExpiredSessionCookie(h.secureCookies))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthenticationHandler) setFlowCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     callbackPath,
		Expires:  time.Now().Add(loginFlowLifetime),
		MaxAge:   int(loginFlowLifetime.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthenticationHandler) clearFlowCookies(w http.ResponseWriter) {
	for _, name := range []string{stateCookieName, nonceCookieName, pkceCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Path:     callbackPath,
			Expires:  time.Unix(1, 0),
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   h.secureCookies,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func (h *AuthenticationHandler) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	render(w, r, status, pages.AuthenticationError(h.brand, message, h.authentication != nil && h.authentication.Available()))
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
