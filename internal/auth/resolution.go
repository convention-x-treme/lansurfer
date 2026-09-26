package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/user"
)

var (
	ErrIdentityConflict          = errors.New("identity belongs to another account")
	ErrProviderAlreadyLinked     = errors.New("provider already linked")
	ErrResolutionIdentityUnknown = errors.New("account proof identity is unknown")
)

type resolutionFlows interface {
	CreatePending(context.Context, PendingIdentityInput) (PendingIdentity, error)
	Pending(context.Context, string, string) (PendingIdentity, error)
	ConsumePending(context.Context, string, string) error
}

type sessionCreator interface {
	NewSession(context.Context, string) (BrowserSession, error)
}

type userLookup interface {
	ByID(context.Context, string) (user.User, error)
}

// PendingIdentityAttacher atomically attaches a browser-bound pending identity
// to the user proven by a different provider and consumes the pending state.
type PendingIdentityAttacher interface {
	AttachPending(context.Context, string, string, string, string, string) error
}

type Resolution struct {
	Account                   user.User
	Session                   BrowserSession
	Pending                   PendingIdentity
	OfferExistingAccountProof bool
}

type ResolutionService struct {
	identities IdentityStore
	flows      resolutionFlows
	sessions   sessionCreator
	users      userLookup
	attacher   PendingIdentityAttacher
}

func NewResolutionService(identities IdentityStore, flows resolutionFlows, sessions sessionCreator, users userLookup, attacher PendingIdentityAttacher) *ResolutionService {
	return &ResolutionService{identities: identities, flows: flows, sessions: sessions, users: users, attacher: attacher}
}

func (s *ResolutionService) ResolveLogin(ctx context.Context, profile Profile, browserBinding string, alternativeProviderAvailable bool) (Resolution, error) {
	profile = normalizeProfile(profile)
	if err := validateProfile(profile); err != nil {
		return Resolution{}, err
	}
	identity, err := s.identities.ByProviderSubject(ctx, profile.ProviderKind, profile.ProviderKey, profile.Subject)
	if err == nil {
		if err := s.identities.UpdateMetadata(ctx, profile); err != nil {
			return Resolution{}, fmt.Errorf("refresh identity metadata: %w", err)
		}
		account, err := s.users.ByID(ctx, identity.UserID)
		if err != nil {
			return Resolution{}, err
		}
		session, err := s.sessions.NewSession(ctx, account.ID)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Account: account, Session: session}, nil
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return Resolution{}, fmt.Errorf("find identity: %w", err)
	}
	pending, err := s.flows.CreatePending(ctx, PendingIdentityInput{
		BrowserBinding: browserBinding, ProviderKind: profile.ProviderKind, ProviderKey: profile.ProviderKey,
		Subject: profile.Subject, Operation: FlowLogin, DisplayName: profile.Name, Email: profile.Email,
		GameNickSuggestion: profile.GameNickSuggestion, AvatarRef: profile.AvatarRef,
	})
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Pending: pending, OfferExistingAccountProof: alternativeProviderAvailable}, nil
}

func (s *ResolutionService) Link(ctx context.Context, profile Profile, userID string) (Identity, error) {
	profile = normalizeProfile(profile)
	if err := validateProfile(profile); err != nil {
		return Identity{}, err
	}
	existing, err := s.identities.ByProviderSubject(ctx, profile.ProviderKind, profile.ProviderKey, profile.Subject)
	if err == nil {
		if existing.UserID != userID {
			return Identity{}, ErrIdentityConflict
		}
		if err := s.identities.UpdateMetadata(ctx, profile); err != nil {
			return Identity{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return Identity{}, err
	}
	linked, err := s.identities.ListByUser(ctx, userID)
	if err != nil {
		return Identity{}, err
	}
	for _, identity := range linked {
		if identity.ProviderKind == profile.ProviderKind && identity.ProviderKey == profile.ProviderKey {
			return Identity{}, ErrProviderAlreadyLinked
		}
	}
	identity, err := s.identities.Create(ctx, Identity{
		UserID: userID, ProviderKind: profile.ProviderKind, ProviderKey: profile.ProviderKey,
		Subject: profile.Subject, DisplayName: profile.Name, Email: profile.Email, AvatarRef: profile.AvatarRef,
	})
	if errors.Is(err, ErrIdentityExists) {
		// The pre-check above raced with a concurrent identical link request
		// (double-click, two tabs) and lost. Resolve it the same way the
		// ByProviderSubject branch above would have, instead of assuming a
		// real conflict: a same-user race completes idempotently.
		raced, raceErr := s.identities.ByProviderSubject(ctx, profile.ProviderKind, profile.ProviderKey, profile.Subject)
		if raceErr != nil {
			return Identity{}, fmt.Errorf("resolve concurrent identity link: %w", raceErr)
		}
		if raced.UserID != userID {
			return Identity{}, ErrIdentityConflict
		}
		return raced, nil
	}
	return identity, err
}

func (s *ResolutionService) ProveExisting(ctx context.Context, proof Profile, pendingID, browserBinding string) (Resolution, error) {
	proof = normalizeProfile(proof)
	if err := validateProfile(proof); err != nil {
		return Resolution{}, err
	}
	proofIdentity, err := s.identities.ByProviderSubject(ctx, proof.ProviderKind, proof.ProviderKey, proof.Subject)
	if errors.Is(err, ErrIdentityNotFound) {
		return Resolution{}, ErrResolutionIdentityUnknown
	}
	if err != nil {
		return Resolution{}, err
	}
	if err := s.identities.UpdateMetadata(ctx, proof); err != nil {
		return Resolution{}, fmt.Errorf("refresh proof identity metadata: %w", err)
	}
	pending, err := s.flows.Pending(ctx, pendingID, browserBinding)
	if err != nil {
		return Resolution{}, err
	}
	if pending.ProviderKind == proof.ProviderKind && pending.ProviderKey == proof.ProviderKey {
		return Resolution{}, ErrProviderAlreadyLinked
	}
	if s.attacher == nil {
		return Resolution{}, errors.New("pending identity attacher is unavailable")
	}
	if err := s.attacher.AttachPending(ctx, pendingID, browserBinding, proofIdentity.UserID, proof.ProviderKind, proof.ProviderKey); err != nil {
		return Resolution{}, err
	}
	account, err := s.users.ByID(ctx, proofIdentity.UserID)
	if err != nil {
		return Resolution{}, err
	}
	session, err := s.sessions.NewSession(ctx, account.ID)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Account: account, Session: session}, nil
}
