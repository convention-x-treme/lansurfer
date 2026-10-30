package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// GuestInvites manages and validates the shared guest invite.
type GuestInvites interface {
	State(context.Context) (auth.GuestAccessState, error)
	SetEnabled(context.Context, bool) error
	SetShowOnLogin(context.Context, bool) error
	Rotate(context.Context) error
	Valid(context.Context, string) (bool, error)
}

// GuestRegistration creates a guest account and its session atomically.
type GuestRegistration interface {
	Register(ctx context.Context, invite, gameNick string) (user.User, auth.BrowserSession, error)
}

// GuestQueries lists accounts that still have a guest identity.
type GuestQueries interface {
	ListGuests(context.Context) ([]user.User, error)
}

// GuestDeletion completely deletes guest accounts.
type GuestDeletion interface {
	DeleteGuests(ctx context.Context, ids []string) ([]string, error)
	DeleteAllGuests(context.Context) ([]string, error)
}

// guestPageSize bounds one Guest Management page.
const guestPageSize = 20

type Guests struct {
	invites       GuestInvites
	registration  GuestRegistration
	queries       GuestQueries
	deletion      GuestDeletion
	navigation    *Navigation
	brand         branding.Brand
	baseURL       string
	secureCookies bool
}

func NewGuests(invites GuestInvites, registration GuestRegistration, queries GuestQueries, deletion GuestDeletion, navigation *Navigation, brand branding.Brand, baseURL string, secureCookies bool) *Guests {
	return &Guests{invites: invites, registration: registration, queries: queries, deletion: deletion, navigation: navigation, brand: brand, baseURL: baseURL, secureCookies: secureCookies}
}

func (h *Guests) Start(w http.ResponseWriter, r *http.Request) {
	if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	state, err := h.invites.State(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "start guest signup", err)
		http.Error(w, "guest access unavailable", http.StatusServiceUnavailable)
		return
	}
	if !state.Enabled || !state.ShowOnLogin || state.Invite == "" {
		h.invalid(w, r)
		return
	}
	http.Redirect(w, r, "/auth/guest?invite="+url.QueryEscape(state.Invite), http.StatusSeeOther)
}

// Show renders the signup form for a valid invite. It never creates an account.
func (h *Guests) Show(w http.ResponseWriter, r *http.Request) {
	if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	invite := r.URL.Query().Get("invite")
	valid, err := h.invites.Valid(r.Context(), invite)
	if err != nil {
		webmiddleware.LogError(r, "validate guest invite", err)
		http.Error(w, "guest access unavailable", http.StatusServiceUnavailable)
		return
	}
	if !valid {
		h.invalid(w, r)
		return
	}
	render(w, r, http.StatusOK, pages.GuestSignup(h.brand, invite, "", ""))
}

func (h *Guests) Submit(w http.ResponseWriter, r *http.Request) {
	if _, ok := webmiddleware.CurrentUser(r.Context()); ok {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	invite, gameNick := r.PostFormValue("invite"), r.PostFormValue("game_nick")
	_, session, err := h.registration.Register(r.Context(), invite, gameNick)
	switch {
	case errors.Is(err, auth.ErrGuestInviteInvalid):
		h.invalid(w, r)
		return
	case err != nil && isGameNickInputError(err):
		render(w, r, http.StatusUnprocessableEntity, pages.GuestSignup(h.brand, invite, gameNick, gameNickErrorMessage(err)))
		return
	case err != nil:
		webmiddleware.LogError(r, "register guest", err)
		http.Error(w, "guest access unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, webmiddleware.SessionCookie(session.Token, session.ExpiresAt, h.secureCookies))
	next := consumeReturnPath(w, r, h.secureCookies)
	if next == defaultReturnPath {
		next = "/welcome"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *Guests) invalid(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, pages.GuestInviteInvalid(h.brand))
}

// Manage renders Guest Management.
func (h *Guests) Manage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

func (h *Guests) render(w http.ResponseWriter, r *http.Request, status int, problem string) {
	w.Header().Set("Cache-Control", "no-store")
	state, err := h.invites.State(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "read guest access", err)
		http.Error(w, "guest access unavailable", http.StatusServiceUnavailable)
		return
	}
	guests, err := h.queries.ListGuests(r.Context())
	if err != nil {
		webmiddleware.LogError(r, "list guests", err)
		http.Error(w, "guest list unavailable", http.StatusServiceUnavailable)
		return
	}
	view := pages.AdminGuestsView{Enabled: state.Enabled, ShowOnLogin: state.ShowOnLogin, Error: problem}
	if state.Invite != "" {
		view.InviteURL = h.baseURL + "/auth/guest?invite=" + url.QueryEscape(state.Invite)
	}
	view.Total = len(guests)
	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 1 {
		page = n
	}
	if last := (view.Total + guestPageSize - 1) / guestPageSize; page > last && last > 0 {
		page = last
	}
	start := min((page-1)*guestPageSize, view.Total)
	end := min(start+guestPageSize, view.Total)
	view.From, view.To = start+1, end
	if page > 1 {
		view.PrevHref = "/admin/guests?page=" + strconv.Itoa(page-1)
	}
	if end < view.Total {
		view.NextHref = "/admin/guests?page=" + strconv.Itoa(page+1)
	}
	for _, guest := range guests[start:end] {
		view.Guests = append(view.Guests, pages.AdminGuestRow{
			ID: guest.ID, GameNick: guest.GameNick, Role: string(guest.Role),
			CreatedAt: guest.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), CreatedAtISO: guest.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	if r.URL.Query().Get("notice") == "demo-forbidden" {
		view.Notice = components.Notice{Text: "Guest Management is read-only in demo mode.", Tone: components.NoticeInfo}
	}
	render(w, r, status, pages.AdminGuests(h.brand, h.navigation.For(r, "administration-guests"), view))
}

// SetAccess enables or disables Guest Access.
func (h *Guests) SetAccess(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	switch r.PostFormValue("enabled") {
	case "true":
		enabled = true
	case "false":
	default:
		http.Error(w, "enabled must be true or false", http.StatusBadRequest)
		return
	}
	h.mutateInvite(w, r, "set guest access", func() error { return h.invites.SetEnabled(r.Context(), enabled) })
}

func (h *Guests) SetLoginOption(w http.ResponseWriter, r *http.Request) {
	var show bool
	switch r.PostFormValue("show_on_login") {
	case "true":
		show = true
	case "", "false":
	default:
		http.Error(w, "show_on_login must be true or false", http.StatusBadRequest)
		return
	}
	h.mutateInvite(w, r, "set guest login option", func() error { return h.invites.SetShowOnLogin(r.Context(), show) })
}

// RotateInvite replaces the shared invite.
func (h *Guests) RotateInvite(w http.ResponseWriter, r *http.Request) {
	h.mutateInvite(w, r, "rotate guest invite", func() error { return h.invites.Rotate(r.Context()) })
}

func (h *Guests) mutateInvite(w http.ResponseWriter, r *http.Request, operation string, mutate func() error) {
	err := mutate()
	if errors.Is(err, auth.ErrGuestAccessUnavailable) {
		h.render(w, r, http.StatusServiceUnavailable, "Guest access needs an encryption key. Set LANSURFER_DATA_ENCRYPTION_KEY and restart LANsurfer.")
		return
	}
	if err != nil {
		webmiddleware.LogError(r, operation, err)
		h.render(w, r, http.StatusServiceUnavailable, "The change could not be saved. Try again.")
		return
	}
	http.Redirect(w, r, "/admin/guests", http.StatusSeeOther)
}

// Delete deletes one guest.
func (h *Guests) Delete(w http.ResponseWriter, r *http.Request) {
	h.deleteGuests(w, r, []string{chi.URLParam(r, "userID")}, false)
}

// DeleteSelected deletes the checked guests.
func (h *Guests) DeleteSelected(w http.ResponseWriter, r *http.Request) {
	h.deleteGuests(w, r, r.PostForm["user_id"], false)
}

// DeleteAll deletes every guest.
func (h *Guests) DeleteAll(w http.ResponseWriter, r *http.Request) {
	h.deleteGuests(w, r, nil, true)
}

func (h *Guests) deleteGuests(w http.ResponseWriter, r *http.Request, ids []string, all bool) {
	_ = r.ParseForm()
	if r.PostFormValue("confirm_delete") != "true" {
		http.Error(w, "confirm the deletion", http.StatusBadRequest)
		return
	}
	var deleted []string
	var err error
	if all {
		deleted, err = h.deletion.DeleteAllGuests(r.Context())
	} else {
		if len(ids) == 0 {
			http.Error(w, "select at least one guest", http.StatusBadRequest)
			return
		}
		deleted, err = h.deletion.DeleteGuests(r.Context(), ids)
	}
	switch {
	case errors.Is(err, user.ErrNotFound) && !all:
		http.Error(w, "select valid guests", http.StatusBadRequest)
		return
	case errors.Is(err, user.ErrLastAdmin):
		h.render(w, r, http.StatusConflict, "Another administrator must remain. No guests were deleted.")
		return
	case err != nil:
		webmiddleware.LogError(r, "delete guests", err)
		h.render(w, r, http.StatusServiceUnavailable, "The guests could not be deleted. Try again.")
		return
	}
	if account, ok := webmiddleware.CurrentUser(r.Context()); ok && slices.Contains(deleted, account.ID) {
		http.SetCookie(w, webmiddleware.ExpiredSessionCookie(h.secureCookies))
		http.SetCookie(w, webmiddleware.ExpiredDeviceCookie(h.secureCookies))
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/guests", http.StatusSeeOther)
}
