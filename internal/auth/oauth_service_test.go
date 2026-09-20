package auth

import (
	"context"
	"reflect"
	"testing"
)

func TestOAuthServiceListsTwitchAfterExistingProviders(t *testing.T) {
	service := NewOAuthService([]ConfiguredProvider{
		{Adapter: &stubOAuthProvider{kind: "twitch", key: "twitch"}},
		{Adapter: &stubOAuthProvider{kind: "discord", key: "discord"}},
		{Adapter: &stubOAuthProvider{kind: "oidc", key: "issuer"}},
	}, &stubOAuthFlows{}, &stubIdentityResolver{})
	want := []ProviderAvailability{
		{Kind: "oidc", DisplayName: "oidc"},
		{Kind: "discord", DisplayName: "discord"},
		{Kind: "twitch", DisplayName: "twitch"},
	}
	if got := service.Providers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Providers() = %#v, want %#v", got, want)
	}
}

func TestValidateProfileAcceptsTwitch(t *testing.T) {
	if err := validateProfile(Profile{ProviderKind: "twitch", ProviderKey: "twitch", Subject: "123"}); err != nil {
		t.Fatalf("validateProfile() error = %v", err)
	}
}

func TestOAuthServicePersistsFlowBeforeAuthorizationRedirect(t *testing.T) {
	provider := &stubOAuthProvider{kind: "discord", key: "discord", authorizationURL: "https://discord.example/authorize"}
	flows := &stubOAuthFlows{}
	service := NewOAuthService([]ConfiguredProvider{{DisplayName: "Discord", Adapter: provider}}, flows, &stubIdentityResolver{})
	service.generateToken = sequenceTokens("state", "nonce", "verifier")

	start, err := service.Begin(t.Context(), OAuthStartInput{
		ProviderKind: "discord", Operation: FlowLink, BrowserBinding: "browser", InitiatingUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if start.AuthorizationURL != provider.authorizationURL || flows.created.State != "state" || flows.created.BrowserBinding != "browser" ||
		flows.created.Operation != FlowLink || flows.created.InitiatingUserID != "user-1" {
		t.Fatalf("start/flow = %#v, %#v", start, flows.created)
	}
	if provider.state != "state" || provider.nonce != "nonce" || provider.verifier != "verifier" {
		t.Fatalf("provider secrets = %q, %q, %q", provider.state, provider.nonce, provider.verifier)
	}
}

func TestOAuthServiceConsumesCallbackBeforeProviderExchange(t *testing.T) {
	events := []string{}
	provider := &stubOAuthProvider{kind: "oidc", key: "https://issuer.example", events: &events, profile: Profile{
		ProviderKind: "oidc", ProviderKey: "https://issuer.example", Subject: "subject",
	}}
	flows := &stubOAuthFlows{events: &events, consumed: OAuthFlow{
		Operation: FlowLogin, ProviderKind: "oidc", ProviderKey: "https://issuer.example", Nonce: "nonce", PKCEVerifier: "verifier",
	}}
	resolver := &stubIdentityResolver{resolved: Resolution{Pending: PendingIdentity{ID: "pending"}}}
	service := NewOAuthService([]ConfiguredProvider{{DisplayName: "CXT account", Adapter: provider}}, flows, resolver)

	result, err := service.Complete(t.Context(), OAuthCompletion{
		ProviderKind: "oidc", State: "state", BrowserBinding: "browser", Code: "code",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(events) != 2 || events[0] != "consume" || events[1] != "exchange" {
		t.Fatalf("events = %v", events)
	}
	if result.Pending.ID != "pending" || resolver.resolveBrowser != "browser" || resolver.alternative {
		t.Fatalf("result/resolver = %#v, %#v", result, resolver)
	}
}

func TestOAuthServiceDispatchesLinkAndResolutionOperations(t *testing.T) {
	provider := &stubOAuthProvider{kind: "discord", key: "discord", profile: Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "123"}}
	tests := []struct {
		name string
		flow OAuthFlow
		want string
	}{
		{name: "link", flow: OAuthFlow{Operation: FlowLink, InitiatingUserID: "user-1"}, want: "link"},
		{name: "resolution", flow: OAuthFlow{Operation: FlowResolution, ParentFlowID: "pending-1"}, want: "proof"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &stubIdentityResolver{}
			service := NewOAuthService([]ConfiguredProvider{{DisplayName: "Discord", Adapter: provider}}, &stubOAuthFlows{consumed: test.flow}, resolver)
			_, err := service.Complete(t.Context(), OAuthCompletion{ProviderKind: "discord", State: "state", BrowserBinding: "browser", Code: "code", CurrentUserID: test.flow.InitiatingUserID})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			if resolver.called != test.want {
				t.Fatalf("resolver call = %q, want %q", resolver.called, test.want)
			}
		})
	}
}

type stubOAuthProvider struct {
	kind, key, authorizationURL string
	state, nonce, verifier      string
	profile                     Profile
	events                      *[]string
}

func (s *stubOAuthProvider) ProviderKind() string { return s.kind }
func (s *stubOAuthProvider) ProviderKey() string  { return s.key }
func (s *stubOAuthProvider) AuthorizationURL(state, nonce, verifier string) string {
	s.state, s.nonce, s.verifier = state, nonce, verifier
	return s.authorizationURL
}
func (s *stubOAuthProvider) Exchange(context.Context, string, string, string) (Profile, error) {
	if s.events != nil {
		*s.events = append(*s.events, "exchange")
	}
	return s.profile, nil
}

type stubOAuthFlows struct {
	created  OAuthFlowInput
	consumed OAuthFlow
	events   *[]string
}

func (s *stubOAuthFlows) CreateOAuth(_ context.Context, input OAuthFlowInput) (OAuthFlow, error) {
	s.created = input
	return OAuthFlow{ID: "flow"}, nil
}
func (s *stubOAuthFlows) ConsumeProviderCallback(context.Context, ProviderCallback) (OAuthFlow, error) {
	if s.events != nil {
		*s.events = append(*s.events, "consume")
	}
	return s.consumed, nil
}

type stubIdentityResolver struct {
	called         string
	resolved       Resolution
	resolveBrowser string
	alternative    bool
}

func (s *stubIdentityResolver) ResolveLogin(_ context.Context, _ Profile, browser string, alternative bool) (Resolution, error) {
	s.called, s.resolveBrowser, s.alternative = "login", browser, alternative
	return s.resolved, nil
}
func (s *stubIdentityResolver) Link(context.Context, Profile, string) (Identity, error) {
	s.called = "link"
	return Identity{}, nil
}
func (s *stubIdentityResolver) ProveExisting(context.Context, Profile, string, string) (Resolution, error) {
	s.called = "proof"
	return s.resolved, nil
}

func sequenceTokens(tokens ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := tokens[index]
		index++
		return value, nil
	}
}
