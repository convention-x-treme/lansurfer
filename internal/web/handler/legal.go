package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/platform/legal"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// LegalDocuments reads and replaces the operator's legal texts.
type LegalDocuments interface {
	Document(context.Context, legal.Kind) (legal.Document, error)
	Save(context.Context, legal.Kind, string) error
}

// adminLegalKinds is the editor's order. The imprint is editable even while
// LANSURFER_IMPRINT_ENABLED is off, so it can be prepared before publishing.
var adminLegalKinds = []legal.Kind{legal.Terms, legal.Privacy, legal.Imprint}

// LegalHandler renders the public legal documents, their administrator
// editor, and the "How it works" help page. The documents are public so the
// Discord Developer Portal can link them; a signed-in player still gets the
// application shell.
type LegalHandler struct {
	documents  LegalDocuments
	navigation *Navigation
	brand      branding.Brand
}

func NewLegal(documents LegalDocuments, navigation *Navigation, brand branding.Brand) *LegalHandler {
	return &LegalHandler{documents: documents, navigation: navigation, brand: brand}
}

// Document serves one legal text at its stable public path.
func (h *LegalHandler) Document(kind legal.Kind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		document, err := h.documents.Document(r.Context(), kind)
		if err != nil {
			webmiddleware.LogError(r, "read legal document", err)
			http.Error(w, "document unavailable", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusOK, pages.LegalDocument(h.brand, h.state(r, ""), document))
	}
}

func (h *LegalHandler) Help(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Help(h.brand, h.state(r, "help")))
}

func (h *LegalHandler) Admin(w http.ResponseWriter, r *http.Request) {
	forms, err := h.adminForms(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "read legal documents", err)
		http.Error(w, "legal documents unavailable", http.StatusInternalServerError)
		return
	}
	var saved components.Notice
	if r.URL.Query().Get("notice") == "legal-saved" {
		saved = components.Notice{Text: "Legal text saved and published.", Tone: components.NoticeSuccess}
	}
	render(w, r, http.StatusOK, pages.AdminLegal(h.brand, h.navigation.For(r, "administration-legal"), forms, saved))
}

func (h *LegalHandler) Save(w http.ResponseWriter, r *http.Request) {
	kind := legal.Kind(chi.URLParam(r, "kind"))
	if kind != legal.Terms && kind != legal.Privacy && kind != legal.Imprint {
		http.NotFound(w, r)
		return
	}
	body := r.PostFormValue("body")
	err := h.documents.Save(r.Context(), kind, body)
	if err == nil {
		http.Redirect(w, r, "/admin/legal?notice=legal-saved#"+string(kind), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, legal.ErrEmpty) && !errors.Is(err, legal.ErrTooLong) {
		webmiddleware.LogError(r, "save legal document", err)
		http.Error(w, "legal document could not be saved; try again", http.StatusInternalServerError)
		return
	}
	forms, readErr := h.adminForms(r.Context())
	if readErr != nil {
		webmiddleware.LogError(r, "read legal documents", readErr)
		http.Error(w, "legal documents unavailable", http.StatusInternalServerError)
		return
	}
	for i := range forms {
		if forms[i].Document.Kind == kind {
			forms[i].Document.Body = body
			forms[i].Error = err.Error()
		}
	}
	render(w, r, http.StatusUnprocessableEntity, pages.AdminLegal(h.brand, h.navigation.For(r, "administration-legal"), forms, components.Notice{}))
}

func (h *LegalHandler) adminForms(ctx context.Context) ([]pages.AdminLegalForm, error) {
	forms := make([]pages.AdminLegalForm, 0, len(adminLegalKinds))
	for _, kind := range adminLegalKinds {
		document, err := h.documents.Document(ctx, kind)
		if err != nil {
			return nil, err
		}
		forms = append(forms, pages.AdminLegalForm{Document: document})
	}
	return forms, nil
}

func (h *LegalHandler) state(r *http.Request, section string) components.NavigationState {
	// Onboarding links here too. A player without a Game Nick gets the public
	// shell: every application-shell link would bounce back to onboarding.
	if account, ok := webmiddleware.CurrentUser(r.Context()); ok && account.GameNick == "" {
		return components.NavigationState{}
	}
	state := h.navigation.For(r, section)
	state.LoginAvailable = state.Viewer == nil
	return state
}
