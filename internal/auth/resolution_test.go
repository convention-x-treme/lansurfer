package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

func TestResolveLoginKnownIdentityRefreshesMetadataAndCreatesSession(t *testing.T) {
	identities := &stubIdentityStore{found: Identity{UserID: "user-1"}}
	flows := &stubResolutionFlows{}
	sessions := &stubSessionCreator{session: BrowserSession{Token: "session"}}
	resolver := NewResolutionService(identities, flows, sessions, &stubUsers{byID: user.User{ID: "user-1", GameNick: "NightOtter"}}, &stubPendingAttacher{})

	result, err := resolver.ResolveLogin(t.Context(), Profile{
		ProviderKind: "discord", ProviderKey: "discord", Subject: "123", Name: "Alice Example", Email: "same@example.test",
	}, "browser", true)
	if err != nil {
		t.Fatalf("ResolveLogin() error = %v", err)
	}
	if result.Account.ID != "user-1" || result.Session.Token != "session" || flows.created.Subject != "" {
		t.Fatalf("result = %#v, pending = %#v", result, flows.created)
	}
	if identities.updatedName != "Alice Example" || identities.updatedEmail != "same@example.test" || sessions.userID != "user-1" {
		t.Fatalf("metadata/session = %q, %q, %q", identities.updatedName, identities.updatedEmail, sessions.userID)
	}
}

func TestResolveLoginUnknownIdentityCreatesPendingWithoutUser(t *testing.T) {
	identities := &stubIdentityStore{findErr: ErrIdentityNotFound}
	flows := &stubResolutionFlows{}
	users := &stubUsers{}
	resolver := NewResolutionService(identities, flows, &stubSessionCreator{}, users, &stubPendingAttacher{})

	result, err := resolver.ResolveLogin(t.Context(), Profile{
		ProviderKind: "oidc", ProviderKey: "https://issuer.example", Subject: "subject",
		Name: "Alice Example", Email: "alice@example.test", GameNickSuggestion: "NightOtter",
	}, "browser", true)
	if err != nil {
		t.Fatalf("ResolveLogin() error = %v", err)
	}
	if result.Pending.Subject != "subject" || !result.OfferExistingAccountProof || users.registerCalls != 0 {
		t.Fatalf("result = %#v, register calls = %d", result, users.registerCalls)
	}
	if flows.created.DisplayName != "Alice Example" || flows.created.Email != "alice@example.test" || flows.created.GameNickSuggestion != "NightOtter" {
		t.Fatalf("pending identity = %#v", flows.created)
	}
}

func TestLinkRejectsIdentityOwnedByAnotherUser(t *testing.T) {
	resolver := NewResolutionService(
		&stubIdentityStore{found: Identity{UserID: "other-user"}}, &stubResolutionFlows{}, &stubSessionCreator{}, &stubUsers{}, &stubPendingAttacher{},
	)
	_, err := resolver.Link(t.Context(), Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "123"}, "current-user")
	if !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("Link() error = %v, want ErrIdentityConflict", err)
	}
}

func TestLinkRejectsSameProviderReplacement(t *testing.T) {
	identities := &stubIdentityStore{
		findErr: ErrIdentityNotFound,
		listed:  []Identity{{UserID: "user-1", ProviderKind: "discord", ProviderKey: "discord", Subject: "old-subject"}},
	}
	resolver := NewResolutionService(identities, &stubResolutionFlows{}, &stubSessionCreator{}, &stubUsers{}, &stubPendingAttacher{})
	_, err := resolver.Link(t.Context(), Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "new-subject"}, "user-1")
	if !errors.Is(err, ErrProviderAlreadyLinked) || identities.created.Subject != "" {
		t.Fatalf("Link() error/created = %v, %#v", err, identities.created)
	}
}

func TestExistingAccountProofRequiresKnownOtherProvider(t *testing.T) {
	resolver := NewResolutionService(
		&stubIdentityStore{findErr: ErrIdentityNotFound}, &stubResolutionFlows{}, &stubSessionCreator{}, &stubUsers{}, &stubPendingAttacher{},
	)
	_, err := resolver.ProveExisting(t.Context(), Profile{
		ProviderKind: "discord", ProviderKey: "discord", Subject: "unknown",
	}, "pending", "browser")
	if !errors.Is(err, ErrResolutionIdentityUnknown) {
		t.Fatalf("ProveExisting() error = %v", err)
	}
}

func TestExistingAccountProofAtomicallyAttachesOriginalPendingIdentity(t *testing.T) {
	identities := &stubIdentityStore{found: Identity{UserID: "user-1"}}
	flows := &stubResolutionFlows{pending: PendingIdentity{
		ID: "pending", ProviderKind: "oidc", ProviderKey: "https://issuer.example", Subject: "new-subject",
	}}
	attacher := &stubPendingAttacher{}
	resolver := NewResolutionService(
		identities, flows, &stubSessionCreator{session: BrowserSession{Token: "session"}},
		&stubUsers{byID: user.User{ID: "user-1", GameNick: "NightOtter"}}, attacher,
	)

	result, err := resolver.ProveExisting(t.Context(), Profile{
		ProviderKind: "discord", ProviderKey: "discord", Subject: "known-discord",
		Name: "Alice Example",
	}, "pending", "browser")
	if err != nil {
		t.Fatalf("ProveExisting() error = %v", err)
	}
	if result.Account.ID != "user-1" || result.Session.Token != "session" {
		t.Fatalf("result = %#v", result)
	}
	if attacher.pendingID != "pending" || attacher.browserBinding != "browser" || attacher.userID != "user-1" || attacher.proofKind != "discord" || attacher.proofKey != "discord" {
		t.Fatalf("attachment = %#v", attacher)
	}
	if identities.updatedName != "Alice Example" {
		t.Fatalf("proof metadata was not refreshed: %#v", identities)
	}
}

type stubResolutionFlows struct {
	created PendingIdentityInput
	pending PendingIdentity
}

func (s *stubResolutionFlows) CreatePending(_ context.Context, input PendingIdentityInput) (PendingIdentity, error) {
	s.created = input
	return PendingIdentity{ID: "pending", ProviderKind: input.ProviderKind, ProviderKey: input.ProviderKey, Subject: input.Subject}, nil
}

func (s *stubResolutionFlows) Pending(context.Context, string, string) (PendingIdentity, error) {
	return s.pending, nil
}

func (*stubResolutionFlows) ConsumePending(context.Context, string, string) error { return nil }

type stubSessionCreator struct {
	session BrowserSession
	userID  string
}

type stubPendingAttacher struct {
	pendingID, browserBinding, userID, proofKind, proofKey string
	err                                                    error
}

func (s *stubPendingAttacher) AttachPending(_ context.Context, pendingID, browserBinding, userID, proofKind, proofKey string) error {
	s.pendingID, s.browserBinding, s.userID, s.proofKind, s.proofKey = pendingID, browserBinding, userID, proofKind, proofKey
	return s.err
}

func (s *stubSessionCreator) NewSession(_ context.Context, userID string) (BrowserSession, error) {
	s.userID = userID
	return s.session, nil
}
