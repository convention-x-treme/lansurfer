package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
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
