package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/game"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/go-chi/chi/v5"
)

type sessionInformationFake struct {
	result                        gamerequest.SessionInformation
	err                           error
	gotRequest, gotUser, gotEntry string
	input                         gamerequest.SessionInformationInput
}

func (f *sessionInformationFake) SessionInformation(context.Context, string, string) (gamerequest.SessionInformation, error) {
	return f.result, f.err
}
func (f *sessionInformationFake) CreateSessionInformationEntry(_ context.Context, r, u string, i gamerequest.SessionInformationInput) error {
	f.gotRequest, f.gotUser, f.input = r, u, i
	return nil
}
func (f *sessionInformationFake) UpdateSessionInformationEntry(_ context.Context, r, e, u string, i gamerequest.SessionInformationInput) error {
	f.gotRequest, f.gotEntry, f.gotUser, f.input = r, e, u, i
	return nil
}
func (f *sessionInformationFake) DeleteSessionInformationEntry(_ context.Context, r, e, u string) error {
	f.gotRequest, f.gotEntry, f.gotUser = r, e, u
	return nil
}
func (f *sessionInformationFake) CanSessionInformation(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestSessionInformationFragmentRequiresCurrentRead(t *testing.T) {
	fake := &sessionInformationFake{err: gamerequest.ErrNotMember}
	h := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default(), fake)
	r := chi.NewRouter()
	r.Get("/{requestID}/session-information", h.SessionInformation)
	req := withCurrentUser(httptest.NewRequest(http.MethodGet, "/request-1/session-information", nil), user.User{ID: "viewer-1"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Join this Game Request to see session information.") {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}

func TestSessionInformationFragmentIgnoresStaleEditID(t *testing.T) {
	fake := &sessionInformationFake{
		result: gamerequest.SessionInformation{
			CanEdit: true,
			Entries: []gamerequest.SessionInformationEntry{{ID: "current-entry", Key: "Server", Value: "address"}},
		},
	}
	h := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default(), fake)
	r := chi.NewRouter()
	r.Get("/{requestID}/session-information", h.SessionInformation)
	req := withCurrentUser(httptest.NewRequest(http.MethodGet, "/request-1/session-information?session_edit=deleted-entry", nil), user.User{ID: "host-1"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "data-session-information-form") ||
		!strings.Contains(w.Body.String(), "Server") {
		t.Fatalf("stale editor response = %d %s", w.Code, w.Body.String())
	}
}

func TestDetailRefreshIgnoresStaleSessionEditID(t *testing.T) {
	fake := &sessionInformationFake{
		result: gamerequest.SessionInformation{
			CanEdit: true,
			Entries: []gamerequest.SessionInformationEntry{{ID: "current-entry", Key: "Server", Value: "address"}},
		},
	}
	queries := fakeGameRequestQueries{request: requestquery.Request{
		ID: "request-1", Game: game.GamePresentation{Name: "Warcraft III"},
		CreatorID: "host-1", Status: gamerequest.StatusOpen,
	}}
	h := NewGameRequests(&fakeGameRequestCommands{}, queries, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default(), fake)
	r := chi.NewRouter()
	r.Get("/{requestID}/refresh", h.RefreshView)
	req := withCurrentUser(httptest.NewRequest(http.MethodGet, "/request-1/refresh?session_edit=deleted-entry", nil), user.User{ID: "host-1"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="game-request-detail"`) ||
		strings.Contains(w.Body.String(), "data-session-information-form") ||
		!strings.Contains(w.Body.String(), "Server") {
		t.Fatalf("stale detail refresh = %d %s", w.Code, w.Body.String())
	}
}

func TestSessionInformationMutationUsesScopedIdentifiers(t *testing.T) {
	fake := &sessionInformationFake{}
	h := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default(), fake)
	r := chi.NewRouter()
	r.Post("/{requestID}/session-information/{entryID}", h.UpdateSessionInformation)
	req := withCurrentUser(httptest.NewRequest(http.MethodPost, "/request-1/session-information/entry-1", strings.NewReader("key=Room&value=Table")), user.User{ID: "host-1"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if fake.gotRequest != "request-1" || fake.gotEntry != "entry-1" || fake.gotUser != "host-1" || fake.input.Key != "Room" {
		t.Fatalf("mutation call = %+v", fake)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestSessionInformationMutationReadsSecretFlag(t *testing.T) {
	fake := &sessionInformationFake{}
	h := NewGameRequests(&fakeGameRequestCommands{}, fakeGameRequestQueries{}, &fakeGameResolver{}, &fakeGameRequestDiscovery{}, nil, branding.Default(), fake)
	r := chi.NewRouter()
	r.Post("/{requestID}/session-information", h.CreateSessionInformation)
	req := withCurrentUser(httptest.NewRequest(http.MethodPost, "/request-1/session-information", strings.NewReader("key=Lobby&value=hidden&is_secret=true")), user.User{ID: "host-1"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !fake.input.IsSecret {
		t.Fatalf("secret flag = false, want true")
	}
}
