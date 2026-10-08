package handler

import (
	"context"
	"errors"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"net/http/httptest"
	"strings"
	"testing"
)

type lobbyAccessStub struct {
	allowed bool
	err     error
	calls   int
}

func (s *lobbyAccessStub) CanReadLobby(context.Context, string, string) (bool, error) {
	s.calls++
	return s.allowed, s.err
}

type lobbyQueriesStub struct {
	lobby requestquery.GameRequestLobby
	err   error
	calls int
}

func (s *lobbyQueriesStub) Lobby(context.Context, string, string) (requestquery.GameRequestLobby, error) {
	s.calls++
	return s.lobby, s.err
}

func TestLobbyViewDeniesNonMembersBeforeQuery(t *testing.T) {
	access := &lobbyAccessStub{}
	queries := &lobbyQueriesStub{}
	handler := NewLobby(access, queries)
	request := withCurrentUser(withURLParam(httptest.NewRequest("GET", "/game-requests/request-1/lobby", nil), "requestID", "request-1"), user.User{ID: "outsider"})
	response := httptest.NewRecorder()
	handler.View(response, request)
	if response.Code != 403 || queries.calls != 0 {
		t.Fatalf("response = %d %q, query calls = %d; want 403 without query", response.Code, response.Body.String(), queries.calls)
	}
}

func TestLobbyViewReturnsHistoryForAuthorizedMember(t *testing.T) {
	access := &lobbyAccessStub{allowed: true}
	queries := &lobbyQueriesStub{lobby: requestquery.GameRequestLobby{RequestID: "request-1", Entries: []requestquery.LobbyEntry{{
		Source:  requestquery.LobbySourceChat,
		Message: gamerequest.Message{ID: "message-1", AuthorGameNick: "Ada", Body: "ready"},
	}}}}
	request := withCurrentUser(withURLParam(httptest.NewRequest("GET", "/game-requests/request-1/lobby", nil), "requestID", "request-1"), user.User{ID: "member"})
	response := httptest.NewRecorder()
	NewLobby(access, queries).View(response, request)
	if response.Code != 200 || queries.calls != 1 || !strings.Contains(response.Body.String(), "ready") {
		t.Fatalf("response = %d %q, query calls = %d; want authorized Lobby fragment", response.Code, response.Body.String(), queries.calls)
	}
}

func TestLobbyViewDistinguishesMissingAndBackendErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		accessErr  error
		queryErr   error
		wantStatus int
	}{
		{name: "missing request", accessErr: gamerequest.ErrNotFound, wantStatus: 404},
		{name: "query leave race", accessErr: nil, queryErr: gamerequest.ErrNotMember, wantStatus: 403},
		{name: "backend failure", accessErr: errors.New("database down"), wantStatus: 500},
		{name: "query failure", queryErr: errors.New("database down"), wantStatus: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := &lobbyAccessStub{allowed: tc.accessErr == nil, err: tc.accessErr}
			queries := &lobbyQueriesStub{err: tc.queryErr}
			request := withCurrentUser(withURLParam(httptest.NewRequest("GET", "/game-requests/request-1/lobby", nil), "requestID", "request-1"), user.User{ID: "member"})
			response := httptest.NewRecorder()
			NewLobby(access, queries).View(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.wantStatus, response.Body.String())
			}
		})
	}
}
