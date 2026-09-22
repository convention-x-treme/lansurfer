package handler

import (
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

// LegalHandler renders the public Terms of Service and Privacy Policy. Both
// are public so the Discord Developer Portal can link them; a signed-in
// player still gets the application shell.
type LegalHandler struct {
	navigation *Navigation
	brand      branding.Brand
}

func NewLegal(navigation *Navigation, brand branding.Brand) *LegalHandler {
	return &LegalHandler{navigation: navigation, brand: brand}
}

func (h *LegalHandler) Terms(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Terms(h.brand, h.state(r)))
}

func (h *LegalHandler) Privacy(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Privacy(h.brand, h.state(r)))
}

func (h *LegalHandler) state(r *http.Request) components.NavigationState {
	// Onboarding links here too. A player without a Game Nick gets the public
	// shell: every application-shell link would bounce back to onboarding.
	if account, ok := webmiddleware.CurrentUser(r.Context()); ok && account.GameNick == "" {
		return components.NavigationState{}
	}
	state := h.navigation.For(r, "")
	state.LoginAvailable = state.Viewer == nil
	return state
}
