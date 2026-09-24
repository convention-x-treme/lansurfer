package auth

import (
	"context"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

// stubIdentityStore and stubUsers back the ResolutionService/SessionService
// unit tests in resolution_test.go and session_test.go.

type stubIdentityStore struct {
	found         Identity
	findErr       error
	created       Identity
	createErr     error
	updatedName   string
	updatedEmail  string
	updatedAvatar string
	listed        []Identity

	// byProviderSubjectCalls/raceWinner let a test simulate a concurrent
	// identity-creation race: the first ByProviderSubject call (the initial
	// existence check) returns found/findErr as usual, and every call after
	// that returns raceWinner instead, standing in for what a concurrent
	// request committed between this Create's own pre-check and its insert.
	byProviderSubjectCalls int
	raceWinner             Identity
}

func (s *stubIdentityStore) ByProviderSubject(context.Context, string, string, string) (Identity, error) {
	s.byProviderSubjectCalls++
	if s.byProviderSubjectCalls > 1 {
		return s.raceWinner, nil
	}
	return s.found, s.findErr
}

func (s *stubIdentityStore) ListByUser(context.Context, string) ([]Identity, error) {
	return s.listed, nil
}

func (s *stubIdentityStore) Create(_ context.Context, identity Identity) (Identity, error) {
	s.created = identity
	return identity, s.createErr
}

func (s *stubIdentityStore) UpdateMetadata(_ context.Context, profile Profile) error {
	s.updatedName, s.updatedEmail, s.updatedAvatar = profile.Name, profile.Email, profile.AvatarRef
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
