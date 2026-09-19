package auth

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

func TestFlowServiceHashesOAuthSecretsBeforePersistence(t *testing.T) {
	repository := &recordingFlowRepository{}
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	service := NewFlowService(repository)
	service.now = func() time.Time { return now }

	flow, err := service.CreateOAuth(t.Context(), OAuthFlowInput{
		State: "raw-state", BrowserBinding: "raw-browser", ProviderKind: "oidc",
		ProviderKey: "https://identity.example/realms/lan", Operation: FlowLogin,
		Nonce: "nonce", PKCEVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("CreateOAuth() error = %v", err)
	}
	if repository.oauth.StateHash != sha256.Sum256([]byte("raw-state")) ||
		repository.oauth.BrowserBindingHash != sha256.Sum256([]byte("raw-browser")) {
		t.Fatalf("stored hashes = %x, %x", repository.oauth.StateHash, repository.oauth.BrowserBindingHash)
	}
	if flow.ExpiresAt != now.Add(10*time.Minute) {
		t.Fatalf("ExpiresAt = %v", flow.ExpiresAt)
	}
}

func TestFlowServiceHashesSecretsOnOAuthConsumption(t *testing.T) {
	repository := &recordingFlowRepository{oauthResult: OAuthFlow{ID: "flow-1"}}
	service := NewFlowService(repository)

	flow, err := service.ConsumeOAuth(t.Context(), OAuthCallback{
		State: "raw-state", BrowserBinding: "raw-browser", ProviderKind: "discord",
		ProviderKey: "discord", Operation: FlowLink, InitiatingUserID: "user-1",
	})
	if err != nil || flow.ID != "flow-1" {
		t.Fatalf("ConsumeOAuth() = %#v, %v", flow, err)
	}
	if repository.callback.StateHash != sha256.Sum256([]byte("raw-state")) ||
		repository.callback.BrowserBindingHash != sha256.Sum256([]byte("raw-browser")) {
		t.Fatalf("lookup hashes = %x, %x", repository.callback.StateHash, repository.callback.BrowserBindingHash)
	}
}

func TestFlowServiceConsumesProviderCallbackWithoutCookieOperationState(t *testing.T) {
	repository := &recordingFlowRepository{oauthResult: OAuthFlow{ID: "flow-1", Operation: FlowResolution}}
	service := NewFlowService(repository)

	flow, err := service.ConsumeProviderCallback(t.Context(), ProviderCallback{
		State: "raw-state", BrowserBinding: "raw-browser", ProviderKind: "discord",
		ProviderKey: "discord", CurrentUserID: "",
	})
	if err != nil || flow.Operation != FlowResolution {
		t.Fatalf("ConsumeProviderCallback() = %#v, %v", flow, err)
	}
	if repository.providerCallback.StateHash != sha256.Sum256([]byte("raw-state")) ||
		repository.providerCallback.BrowserBindingHash != sha256.Sum256([]byte("raw-browser")) {
		t.Fatalf("lookup hashes = %x, %x", repository.providerCallback.StateHash, repository.providerCallback.BrowserBindingHash)
	}
}

type recordingFlowRepository struct {
	oauth            OAuthFlow
	callback         OAuthCallbackHashes
	providerCallback ProviderCallbackHashes
	oauthResult      OAuthFlow
}

func (r *recordingFlowRepository) ConsumeProviderCallback(_ context.Context, callback ProviderCallbackHashes) (OAuthFlow, error) {
	r.providerCallback = callback
	return r.oauthResult, nil
}

func (r *recordingFlowRepository) CreateOAuth(_ context.Context, flow OAuthFlow) (OAuthFlow, error) {
	r.oauth = flow
	return flow, nil
}

func (r *recordingFlowRepository) ConsumeOAuth(_ context.Context, callback OAuthCallbackHashes) (OAuthFlow, error) {
	r.callback = callback
	return r.oauthResult, nil
}

func (*recordingFlowRepository) CreatePending(context.Context, PendingIdentity) (PendingIdentity, error) {
	return PendingIdentity{}, nil
}

func (*recordingFlowRepository) Pending(context.Context, string, [sha256.Size]byte, time.Time) (PendingIdentity, error) {
	return PendingIdentity{}, nil
}

func (*recordingFlowRepository) ConsumePending(context.Context, string, [sha256.Size]byte, time.Time) error {
	return nil
}
