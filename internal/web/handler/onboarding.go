package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// PendingIdentities looks up a verified, not-yet-onboarded provider identity
// by its browser-bound pending resolution ID, without consuming it.
type PendingIdentities interface {
	Pending(context.Context, string, string) (auth.PendingIdentity, error)
}

// AccountRegistration atomically creates a new local User and Identity from
// a pending resolution once a valid Game Nick is chosen. It is satisfied by
// *app.AccountRegistration.
type AccountRegistration interface {
	Register(context.Context, string, string, string) (user.User, auth.BrowserSession, error)
}

// GameNickClaims claims or renames the caller's public Game Nick.
type GameNickClaims interface {
	ClaimGameNick(context.Context, string, string) (user.User, error)
}

// IdentityLookup lists a user's linked provider identities.
type IdentityLookup interface {
	ListByUser(context.Context, string) ([]auth.Identity, error)
}

type OnboardingHandler struct {
	pending        PendingIdentities
	registration   AccountRegistration
	claims         GameNickClaims
	identities     IdentityLookup
	authentication Authentication
	brand          branding.Brand
	secureCookies  bool
}

func NewOnboarding(pending PendingIdentities, registration AccountRegistration, claims GameNickClaims, identities IdentityLookup, authentication Authentication, brand branding.Brand, secureCookies bool) *OnboardingHandler {
	return &OnboardingHandler{
		pending: pending, registration: registration, claims: claims, identities: identities,
		authentication: authentication, brand: brand, secureCookies: secureCookies,
	}
}

// ShowPending renders the Game Nick form for a brand-new, not-yet-linked
// identity. No local session exists yet at this point. An already signed-in
// caller (e.g. a stale onboarding tab left open on a shared/kiosk browser
// after signing in as someone else) is sent to their own account instead of
// a form that would otherwise create and switch them into a second one.
func (h *OnboardingHandler) ShowPending(w http.ResponseWriter, r *http.Request) {
	if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	pendingID := chi.URLParam(r, "pendingID")
	binding, ok := browserBindingCookie(r)
	if !ok {
		h.redirectExpired(w, r)
		return
	}
	pending, err := h.pending.Pending(r.Context(), pendingID, binding)
	if err != nil {
		h.redirectExpired(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.Onboarding(h.brand, "/onboarding/"+pendingID, pending.GameNickSuggestion, "", h.resolveOptions(pending)))
}

// SubmitPending atomically creates the account once a valid, unclaimed Game
// Nick is submitted. An invalid or taken Game Nick re-renders the form
// without consuming the pending identity. Guarded the same way ShowPending
// is: an already signed-in caller is never allowed to complete this and have
// it silently overwrite their session with a freshly created account.
func (h *OnboardingHandler) SubmitPending(w http.ResponseWriter, r *http.Request) {
	if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	pendingID := chi.URLParam(r, "pendingID")
	binding, ok := browserBindingCookie(r)
	if !ok {
		h.redirectExpired(w, r)
		return
	}
	gameNick := r.FormValue("game_nick")
	_, session, err := h.registration.Register(r.Context(), pendingID, binding, gameNick)
	if err != nil {
		if isGameNickInputError(err) {
			pending, pendingErr := h.pending.Pending(r.Context(), pendingID, binding)
			if pendingErr != nil {
				h.redirectExpired(w, r)
				return
			}
			render(w, r, http.StatusUnprocessableEntity, pages.Onboarding(h.brand, "/onboarding/"+pendingID, gameNick, gameNickErrorMessage(err), h.resolveOptions(pending)))
			return
		}
		webmiddleware.LogError(r, "complete Game Nick onboarding", err)
		h.redirectExpired(w, r)
		return
	}
	http.SetCookie(w, webmiddleware.SessionCookie(session.Token, session.ExpiresAt, h.secureCookies))
	http.Redirect(w, r, consumeReturnPath(w, r, h.secureCookies), http.StatusSeeOther)
}

// ShowExisting renders the Game Nick form for an already-signed-in, upgraded
// user who has not chosen one yet.
func (h *OnboardingHandler) ShowExisting(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	if account.GameNick != "" {
		http.Redirect(w, r, "/game-requests", http.StatusSeeOther)
		return
	}
	render(w, r, http.StatusOK, pages.Onboarding(h.brand, "/onboarding", h.suggestGameNick(r.Context(), account.ID), "", nil))
}

// SubmitExisting claims the Game Nick for an already-signed-in, upgraded
// user. No new identity or session is involved.
func (h *OnboardingHandler) SubmitExisting(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	gameNick := r.FormValue("game_nick")
	if _, err := h.claims.ClaimGameNick(r.Context(), account.ID, gameNick); err != nil {
		if isGameNickInputError(err) {
			render(w, r, http.StatusUnprocessableEntity, pages.Onboarding(h.brand, "/onboarding", gameNick, gameNickErrorMessage(err), nil))
			return
		}
		webmiddleware.LogError(r, "claim Game Nick", err)
		http.Error(w, "claim Game Nick", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, consumeReturnPath(w, r, h.secureCookies), http.StatusSeeOther)
}

// resolveOptions offers proving an existing account through any configured
// provider other than the one that produced this pending identity. When
// only one provider is configured, resolution is never offered.
func (h *OnboardingHandler) resolveOptions(pending auth.PendingIdentity) []pages.OnboardingResolveOption {
	if h.authentication == nil {
		return nil
	}
	var options []pages.OnboardingResolveOption
	for _, provider := range h.authentication.Providers() {
		if provider.Kind == pending.ProviderKind {
			continue
		}
		options = append(options, pages.OnboardingResolveOption{
			Kind:  provider.Kind,
			Label: "Continue with " + provider.DisplayName,
			Href:  "/auth/" + provider.Kind + "?" + resolveQueryParam + "=" + pending.ID,
		})
	}
	return options
}

// suggestGameNick reuses a private stored identity display name as an
// upgraded user's Game Nick suggestion when it already passes validation.
// It never triggers a provider round-trip just for this.
func (h *OnboardingHandler) suggestGameNick(ctx context.Context, userID string) string {
	if h.identities == nil {
		return ""
	}
	identities, err := h.identities.ListByUser(ctx, userID)
	if err != nil {
		return ""
	}
	for _, identity := range identities {
		if validated, err := user.ValidateGameNick(identity.DisplayName); err == nil {
			return validated
		}
	}
	return ""
}

func (h *OnboardingHandler) redirectExpired(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func isGameNickInputError(err error) bool {
	return errors.Is(err, user.ErrInvalidGameNick) || errors.Is(err, user.ErrGameNickTaken)
}

func gameNickErrorMessage(err error) string {
	if errors.Is(err, user.ErrGameNickTaken) {
		return "That Game Nick is already taken. Try another."
	}
	return "Enter a Game Nick between 1 and 32 characters."
}

// RequireGameNick redirects an authenticated user who has not chosen a
// Game Nick yet away from normal product/admin surfaces and onto
// onboarding. It must run after RequireAuthentication/RequireAdmin so a
// signed-out request has already been redirected to login by then.
func RequireGameNick(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if account, ok := webmiddleware.CurrentUser(r.Context()); ok && account.GameNick == "" {
			http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
