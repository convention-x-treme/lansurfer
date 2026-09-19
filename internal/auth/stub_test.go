package auth

import (
	"context"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

// stubIdentityStore and stubUsers back the ResolutionService/SessionService
// unit tests in resolution_test.go and session_test.go.

type stubIdentityStore struct {
	found        Identity
	findErr      error
	created      Identity
	createErr    error
	updatedName  string
	updatedEmail string
	listed       []Identity
}

func (s *stubIdentityStore) ByProviderSubject(context.Context, string, string, string) (Identity, error) {
	return s.found, s.findErr
}

func (s *stubIdentityStore) ListByUser(context.Context, string) ([]Identity, error) {
	return s.listed, nil
}

func (s *stubIdentityStore) Create(_ context.Context, identity Identity) (Identity, error) {
	s.created = identity
	return identity, s.createErr
}

func (s *stubIdentityStore) UpdateMetadata(_ context.Context, _, _, _, name, email string) error {
	s.updatedName, s.updatedEmail = name, email
	return nil
}

func (*stubIdentityStore) Unlink(context.Context, string, string) error { return nil }

type stubUsers struct {
	registered     user.User
	registeredName string
	registerCalls  int
	byID           user.User
	byIDErr        error
}

func (s *stubUsers) Register(_ context.Context, gameNick string) (user.User, error) {
	s.registerCalls++
	s.registeredName = gameNick
	return s.registered, nil
}

func (s *stubUsers) ByID(context.Context, string) (user.User, error) {
	return s.byID, s.byIDErr
}
