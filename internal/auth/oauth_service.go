package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type OAuthProvider interface {
	Provider
	ProviderKind() string
	ProviderKey() string
}

type ConfiguredProvider struct {
	DisplayName string
	Adapter     OAuthProvider
}

type ProviderAvailability struct {
	Kind, DisplayName string
}

type OAuthStartInput struct {
	ProviderKind, BrowserBinding   string
	Operation                      FlowOperation
	InitiatingUserID, ParentFlowID string
}

type OAuthStart struct {
	AuthorizationURL string
}

type OAuthCompletion struct {
	ProviderKind, State, BrowserBinding, Code, CurrentUserID string
}

type OAuthResult struct {
	Operation FlowOperation
	Resolution
}

type oauthFlowService interface {
	CreateOAuth(context.Context, OAuthFlowInput) (OAuthFlow, error)
	ConsumeProviderCallback(context.Context, ProviderCallback) (OAuthFlow, error)
}

type identityResolver interface {
	ResolveLogin(context.Context, Profile, string, bool) (Resolution, error)
	Link(context.Context, Profile, string) (Identity, error)
	ProveExisting(context.Context, Profile, string, string) (Resolution, error)
}

type OAuthService struct {
	providers     map[string]ConfiguredProvider
	flows         oauthFlowService
	resolver      identityResolver
	generateToken func() (string, error)
}

func NewOAuthService(providers []ConfiguredProvider, flows oauthFlowService, resolver identityResolver) *OAuthService {
	configured := make(map[string]ConfiguredProvider, len(providers))
	for _, provider := range providers {
		if provider.Adapter == nil {
			continue
		}
		provider.DisplayName = strings.TrimSpace(provider.DisplayName)
		if provider.DisplayName == "" {
			provider.DisplayName = provider.Adapter.ProviderKind()
		}
		configured[provider.Adapter.ProviderKind()] = provider
	}
	return &OAuthService{providers: configured, flows: flows, resolver: resolver, generateToken: randomToken}
}

func (s *OAuthService) Available() bool { return s != nil && len(s.providers) > 0 }

func (s *OAuthService) Providers() []ProviderAvailability {
	result := make([]ProviderAvailability, 0, 3)
	for _, kind := range []string{"oidc", "discord", "twitch"} {
		if provider, ok := s.providers[kind]; ok {
			result = append(result, ProviderAvailability{Kind: kind, DisplayName: provider.DisplayName})
		}
	}
	return result
}

func (s *OAuthService) Begin(ctx context.Context, input OAuthStartInput) (OAuthStart, error) {
	provider, ok := s.providers[input.ProviderKind]
	if !ok || s.flows == nil {
		return OAuthStart{}, ErrUnavailable
	}
	if strings.TrimSpace(input.BrowserBinding) == "" {
		return OAuthStart{}, ErrFlowBrowser
	}
	switch input.Operation {
	case FlowLogin:
		if input.InitiatingUserID != "" || input.ParentFlowID != "" {
			return OAuthStart{}, ErrFlowOperation
		}
	case FlowLink:
		if input.InitiatingUserID == "" || input.ParentFlowID != "" {
			return OAuthStart{}, ErrFlowInitiatingUser
		}
	case FlowResolution:
		if input.ParentFlowID == "" || input.InitiatingUserID != "" {
			return OAuthStart{}, ErrFlowOperation
		}
	default:
		return OAuthStart{}, ErrFlowOperation
	}
	state, err := s.generateToken()
	if err != nil {
		return OAuthStart{}, fmt.Errorf("generate OAuth state: %w", err)
	}
	nonce, err := s.generateToken()
	if err != nil {
		return OAuthStart{}, fmt.Errorf("generate OAuth nonce: %w", err)
	}
	verifier, err := s.generateToken()
	if err != nil {
		return OAuthStart{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	_, err = s.flows.CreateOAuth(ctx, OAuthFlowInput{
		State: state, BrowserBinding: input.BrowserBinding,
		ProviderKind: provider.Adapter.ProviderKind(), ProviderKey: provider.Adapter.ProviderKey(),
		Operation: input.Operation, InitiatingUserID: input.InitiatingUserID, ParentFlowID: input.ParentFlowID,
		Nonce: nonce, PKCEVerifier: verifier,
	})
	if err != nil {
		return OAuthStart{}, err
	}
	return OAuthStart{AuthorizationURL: provider.Adapter.AuthorizationURL(state, nonce, verifier)}, nil
}

// Decline consumes the flow of a callback the user declined at the provider and
// returns its operation. State, browser binding and (for Link) the signed-in
// user are validated exactly as in Complete; no provider exchange happens.
func (s *OAuthService) Decline(ctx context.Context, completion OAuthCompletion) (FlowOperation, error) {
	provider, ok := s.providers[completion.ProviderKind]
	if !ok || s.flows == nil {
		return "", ErrUnavailable
	}
	if completion.State == "" || completion.BrowserBinding == "" {
		return "", ErrInvalidCallback
	}
	flow, err := s.flows.ConsumeProviderCallback(ctx, ProviderCallback{
		State: completion.State, BrowserBinding: completion.BrowserBinding,
		ProviderKind: provider.Adapter.ProviderKind(), ProviderKey: provider.Adapter.ProviderKey(),
		CurrentUserID: completion.CurrentUserID,
	})
	if err != nil {
		return "", err
	}
	return flow.Operation, nil
}

func (s *OAuthService) Complete(ctx context.Context, completion OAuthCompletion) (OAuthResult, error) {
	provider, ok := s.providers[completion.ProviderKind]
	if !ok || s.flows == nil || s.resolver == nil {
		return OAuthResult{}, ErrUnavailable
	}
	if completion.State == "" || completion.BrowserBinding == "" || completion.Code == "" {
		return OAuthResult{}, ErrInvalidCallback
	}
	flow, err := s.flows.ConsumeProviderCallback(ctx, ProviderCallback{
		State: completion.State, BrowserBinding: completion.BrowserBinding,
		ProviderKind: provider.Adapter.ProviderKind(), ProviderKey: provider.Adapter.ProviderKey(),
		CurrentUserID: completion.CurrentUserID,
	})
	if err != nil {
		return OAuthResult{}, err
	}
	profile, err := provider.Adapter.Exchange(ctx, completion.Code, flow.PKCEVerifier, flow.Nonce)
	if err != nil {
		return OAuthResult{}, fmt.Errorf("%w: exchange provider callback: %w", ErrInvalidCallback, err)
	}
	profile = normalizeProfile(profile)
	if err := validateProfile(profile); err != nil {
		return OAuthResult{}, err
	}
	if profile.ProviderKind != provider.Adapter.ProviderKind() || profile.ProviderKey != provider.Adapter.ProviderKey() {
		return OAuthResult{}, ErrInvalidProfile
	}

	result := OAuthResult{Operation: flow.Operation}
	switch flow.Operation {
	case FlowLogin:
		result.Resolution, err = s.resolver.ResolveLogin(ctx, profile, completion.BrowserBinding, len(s.providers) > 1)
	case FlowLink:
		if flow.InitiatingUserID == "" || flow.InitiatingUserID != completion.CurrentUserID {
			return OAuthResult{}, ErrFlowInitiatingUser
		}
		_, err = s.resolver.Link(ctx, profile, flow.InitiatingUserID)
	case FlowResolution:
		result.Resolution, err = s.resolver.ProveExisting(ctx, profile, flow.ParentFlowID, completion.BrowserBinding)
	default:
		err = ErrFlowOperation
	}
	if err != nil {
		if errors.Is(err, ErrIdentityExists) {
			err = ErrIdentityConflict
		}
		return OAuthResult{}, err
	}
	return result, nil
}
