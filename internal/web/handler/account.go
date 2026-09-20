package handler

import (
	"context"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// IdentityUnlinker removes one of the caller's linked provider identities.
// The persistence layer, not this handler, protects the invariant that at
// least one identity remains linked.
type IdentityUnlinker interface {
	Unlink(context.Context, string, string) error
}

type AccountHandler struct {
	identities     IdentityLookup
	unlinker       IdentityUnlinker
	claims         GameNickClaims
	authentication Authentication
	navigation     *Navigation
	brand          branding.Brand
}

func NewAccount(identities IdentityLookup, unlinker IdentityUnlinker, claims GameNickClaims, authentication Authentication, navigation *Navigation, brand branding.Brand) *AccountHandler {
	return &AccountHandler{
		identities: identities, unlinker: unlinker, claims: claims,
		authentication: authentication, navigation: navigation, brand: brand,
	}
}

func (h *AccountHandler) Show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "", "")
}

// RenameGameNick claims a new Game Nick under the same validation and
// uniqueness invariant as initial onboarding.
func (h *AccountHandler) RenameGameNick(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	gameNick := r.FormValue("game_nick")
	if _, err := h.claims.ClaimGameNick(r.Context(), account.ID, gameNick); err != nil {
		if isGameNickInputError(err) {
			h.render(w, r, http.StatusUnprocessableEntity, gameNick, gameNickErrorMessage(err))
			return
		}
		webmiddleware.LogError(r, "rename Game Nick", err)
		http.Error(w, "rename Game Nick", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// Unlink removes one linked identity. The UI never offers this action for a
// player's last identity, so an ErrIdentityNotFound here (identity missing
// or already the last one) is a race or tampering, not a normal path.
func (h *AccountHandler) Unlink(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := h.unlinker.Unlink(r.Context(), account.ID, chi.URLParam(r, "identityID")); err != nil {
		webmiddleware.LogError(r, "unlink provider identity", err)
		http.Redirect(w, r, "/account?unlink_error=1", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *AccountHandler) render(w http.ResponseWriter, r *http.Request, status int, gameNickInput, gameNickError string) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	identities, err := h.identities.ListByUser(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "list linked identities", err)
		http.Error(w, "load account", http.StatusInternalServerError)
		return
	}
	rows, linkable := accountIdentityRows(identities, h.providers())
	linkError := r.URL.Query().Get("link_error") != ""
	navigation := h.navigation.For(r, "account")
	render(w, r, status, pages.Account(h.brand, navigation, account.GameNick, rows, linkable, linkError, gameNickInput, gameNickError))
}

func (h *AccountHandler) providers() []auth.ProviderAvailability {
	if h.authentication == nil {
		return nil
	}
	return h.authentication.Providers()
}

func accountIdentityRows(identities []auth.Identity, providers []auth.ProviderAvailability) ([]pages.AccountIdentityRow, []pages.AccountProviderOption) {
	labels := make(map[string]string, len(providers))
	for _, provider := range providers {
		labels[provider.Kind] = provider.DisplayName
	}
	linked := make(map[string]bool, len(identities))
	canUnlink := len(identities) > 1
	rows := make([]pages.AccountIdentityRow, 0, len(identities))
	for _, identity := range identities {
		linked[identity.ProviderKind] = true
		label := labels[identity.ProviderKind]
		if label == "" {
			label = identity.ProviderKind
		}
		rows = append(rows, pages.AccountIdentityRow{
			Label: label, PrivateName: identity.DisplayName, PrivateEmail: identity.Email,
			CanUnlink: canUnlink, UnlinkPath: "/account/identities/" + identity.ID + "/unlink",
		})
	}
	var linkable []pages.AccountProviderOption
	for _, provider := range providers {
		if linked[provider.Kind] {
			continue
		}
		linkable = append(linkable, pages.AccountProviderOption{Label: provider.DisplayName, Href: "/account/link/" + provider.Kind})
	}
	return rows, linkable
}
