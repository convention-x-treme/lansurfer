package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
)

const authFlowLifetime = 10 * time.Minute

var (
	ErrFlowNotFound       = errors.New("authentication flow not found")
	ErrFlowExpired        = errors.New("authentication flow expired")
	ErrFlowConsumed       = errors.New("authentication flow already consumed")
	ErrFlowBrowser        = errors.New("authentication flow browser mismatch")
	ErrFlowProvider       = errors.New("authentication flow provider mismatch")
	ErrFlowOperation      = errors.New("authentication flow operation mismatch")
	ErrFlowInitiatingUser = errors.New("authentication flow user mismatch")
)

type FlowOperation string

const (
	FlowLogin      FlowOperation = "login"
	FlowLink       FlowOperation = "link"
	FlowResolution FlowOperation = "resolution"
)

type OAuthFlowInput struct {
	State, BrowserBinding                 string
	ProviderKind, ProviderKey             string
	Operation                             FlowOperation
	InitiatingUserID, Nonce, PKCEVerifier string
	ParentFlowID                          string
}

type OAuthFlow struct {
	ID                                    string
	StateHash, BrowserBindingHash         [sha256.Size]byte
	ProviderKind, ProviderKey             string
	Operation                             FlowOperation
	InitiatingUserID, Nonce, PKCEVerifier string
	ParentFlowID                          string
	ExpiresAt                             time.Time
}

type OAuthCallback struct {
	State, BrowserBinding     string
	ProviderKind, ProviderKey string
	Operation                 FlowOperation
	InitiatingUserID          string
}

type OAuthCallbackHashes struct {
	StateHash, BrowserBindingHash [sha256.Size]byte
	ProviderKind, ProviderKey     string
	Operation                     FlowOperation
	InitiatingUserID              string
	Now                           time.Time
}

type ProviderCallback struct {
	State, BrowserBinding     string
	ProviderKind, ProviderKey string
	CurrentUserID             string
}

type ProviderCallbackHashes struct {
	StateHash, BrowserBindingHash [sha256.Size]byte
	ProviderKind, ProviderKey     string
	CurrentUserID                 string
	Now                           time.Time
}

type PendingIdentityInput struct {
	BrowserBinding                         string
	ProviderKind, ProviderKey, Subject     string
	Operation                              FlowOperation
	InitiatingUserID, ParentFlowID         string
	DisplayName, Email, GameNickSuggestion string
}

type PendingIdentity struct {
	ID, ProviderKind, ProviderKey, Subject string
	Operation                              FlowOperation
	InitiatingUserID, ParentFlowID         string
	DisplayName, Email, GameNickSuggestion string
	BrowserBindingHash                     [sha256.Size]byte
	ExpiresAt                              time.Time
}

type FlowRepository interface {
	CreateOAuth(context.Context, OAuthFlow) (OAuthFlow, error)
	ConsumeOAuth(context.Context, OAuthCallbackHashes) (OAuthFlow, error)
	ConsumeProviderCallback(context.Context, ProviderCallbackHashes) (OAuthFlow, error)
	CreatePending(context.Context, PendingIdentity) (PendingIdentity, error)
	Pending(context.Context, string, [sha256.Size]byte, time.Time) (PendingIdentity, error)
	ConsumePending(context.Context, string, [sha256.Size]byte, time.Time) error
}

func (s *FlowService) ConsumeProviderCallback(ctx context.Context, callback ProviderCallback) (OAuthFlow, error) {
	return s.repository.ConsumeProviderCallback(ctx, ProviderCallbackHashes{
		StateHash: hashFlowSecret(callback.State), BrowserBindingHash: hashFlowSecret(callback.BrowserBinding),
		ProviderKind: callback.ProviderKind, ProviderKey: callback.ProviderKey,
		CurrentUserID: callback.CurrentUserID, Now: s.now(),
	})
}

type FlowService struct {
	repository FlowRepository
	now        func() time.Time
}

func NewFlowService(repository FlowRepository) *FlowService {
	return &FlowService{repository: repository, now: time.Now}
}

func (s *FlowService) CreateOAuth(ctx context.Context, input OAuthFlowInput) (OAuthFlow, error) {
	flow := OAuthFlow{
		StateHash: hashFlowSecret(input.State), BrowserBindingHash: hashFlowSecret(input.BrowserBinding),
		ProviderKind: input.ProviderKind, ProviderKey: input.ProviderKey, Operation: input.Operation,
		InitiatingUserID: input.InitiatingUserID, Nonce: input.Nonce, PKCEVerifier: input.PKCEVerifier,
		ParentFlowID: input.ParentFlowID, ExpiresAt: s.now().Add(authFlowLifetime),
	}
	return s.repository.CreateOAuth(ctx, flow)
}

func (s *FlowService) ConsumeOAuth(ctx context.Context, callback OAuthCallback) (OAuthFlow, error) {
	return s.repository.ConsumeOAuth(ctx, OAuthCallbackHashes{
		StateHash: hashFlowSecret(callback.State), BrowserBindingHash: hashFlowSecret(callback.BrowserBinding),
		ProviderKind: callback.ProviderKind, ProviderKey: callback.ProviderKey, Operation: callback.Operation,
		InitiatingUserID: callback.InitiatingUserID, Now: s.now(),
	})
}

func (s *FlowService) CreatePending(ctx context.Context, input PendingIdentityInput) (PendingIdentity, error) {
	pending := PendingIdentity{
		ProviderKind: input.ProviderKind, ProviderKey: input.ProviderKey, Subject: input.Subject,
		Operation: input.Operation, InitiatingUserID: input.InitiatingUserID, ParentFlowID: input.ParentFlowID,
		DisplayName: input.DisplayName, Email: input.Email, GameNickSuggestion: input.GameNickSuggestion,
		BrowserBindingHash: hashFlowSecret(input.BrowserBinding), ExpiresAt: s.now().Add(authFlowLifetime),
	}
	return s.repository.CreatePending(ctx, pending)
}

func (s *FlowService) Pending(ctx context.Context, id, browserBinding string) (PendingIdentity, error) {
	return s.repository.Pending(ctx, id, hashFlowSecret(browserBinding), s.now())
}

func (s *FlowService) ConsumePending(ctx context.Context, id, browserBinding string) error {
	return s.repository.ConsumePending(ctx, id, hashFlowSecret(browserBinding), s.now())
}

func hashFlowSecret(value string) [sha256.Size]byte { return sha256.Sum256([]byte(value)) }
