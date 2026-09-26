package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

func TestAccountShowRendersLinkedIdentitiesAndLinkableProviders(t *testing.T) {
	identities := &stubIdentityLookup{identities: []auth.Identity{
		{ID: "identity-1", ProviderKind: "discord", DisplayName: "Alice Example", Email: "alice@example.test"},
	}}
	authentication := &stubAuthentication{providers: []auth.ProviderAvailability{
		{Kind: "discord", DisplayName: "Discord"}, {Kind: "oidc", DisplayName: "CXT account"},
	}}
	handler := NewAccount(identities, &stubUnlinker{}, &stubAvatarSelector{}, &stubGameNickClaims{}, authentication, nil, branding.Default())
	request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/account", nil), user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	handler.Show(response, request)

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "NightOtter") {
		t.Fatalf("status/body = %d, %q", response.Code, body)
	}
	if !strings.Contains(body, "Alice Example") || !strings.Contains(body, "alice@example.test") {
		t.Fatalf("private identity metadata not rendered: %q", body)
	}
	if !strings.Contains(body, "Only sign-in method") {
		t.Fatalf("a single identity must not offer unlink: %q", body)
	}
	if !strings.Contains(body, `action="/account/link/oidc"`) || strings.Contains(body, `action="/account/link/discord"`) {
		t.Fatalf("only the unlinked configured provider should be offered to link: %q", body)
	}
}

func TestAccountShowOffersUnlinkWithMultipleIdentities(t *testing.T) {
	identities := &stubIdentityLookup{identities: []auth.Identity{
		{ID: "identity-1", ProviderKind: "discord"},
		{ID: "identity-2", ProviderKind: "oidc"},
	}}
	handler := NewAccount(identities, &stubUnlinker{}, &stubAvatarSelector{}, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/account", nil), user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	handler.Show(response, request)

	if strings.Contains(response.Body.String(), "Only sign-in method") {
		t.Fatalf("an account with two identities must offer unlink for both: %q", response.Body.String())
	}
	if strings.Count(response.Body.String(), `action="/account/identities/`) != 2 {
		t.Fatalf("expected two unlink forms: %q", response.Body.String())
	}
}

func TestAccountRenameGameNickRejectsInvalidInput(t *testing.T) {
	claims := &stubGameNickClaims{err: user.ErrGameNickTaken}
	handler := NewAccount(&stubIdentityLookup{}, &stubUnlinker{}, &stubAvatarSelector{}, claims, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/game-nick", strings.NewReader("game_nick=Taken")), user.User{ID: "user-1", GameNick: "NightOtter"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.RenameGameNick(response, request)

	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "already taken") {
		t.Fatalf("status/body = %d, %q", response.Code, response.Body.String())
	}
	if claims.userID != "user-1" || claims.gameNick != "Taken" {
		t.Fatalf("claim call = %#v", claims)
	}
}

func TestAccountRenameGameNickRedirectsOnSuccess(t *testing.T) {
	handler := NewAccount(&stubIdentityLookup{}, &stubUnlinker{}, &stubAvatarSelector{}, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/game-nick", strings.NewReader("game_nick=NewNick")), user.User{ID: "user-1", GameNick: "NightOtter"})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.RenameGameNick(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAccountUnlinkRedirectsWithErrorOnFailure(t *testing.T) {
	unlinker := &stubUnlinker{err: auth.ErrIdentityNotFound}
	handler := NewAccount(&stubIdentityLookup{}, unlinker, &stubAvatarSelector{}, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(withURLParam(httptest.NewRequest(http.MethodPost, "/account/identities/identity-1/unlink", nil), "identityID", "identity-1"), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.Unlink(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account?unlink_error=1" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
	if unlinker.userID != "user-1" || unlinker.identityID != "identity-1" {
		t.Fatalf("unlink call = %#v", unlinker)
	}
}

func TestAccountUnlinkRedirectsToAccountOnSuccess(t *testing.T) {
	handler := NewAccount(&stubIdentityLookup{}, &stubUnlinker{}, &stubAvatarSelector{}, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(withURLParam(httptest.NewRequest(http.MethodPost, "/account/identities/identity-1/unlink", nil), "identityID", "identity-1"), user.User{ID: "user-1"})
	response := httptest.NewRecorder()

	handler.Unlink(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/account" {
		t.Fatalf("status/location = %d, %q", response.Code, response.Header().Get("Location"))
	}
}

func TestAccountShowRendersAvatarChoicesAndSameOriginAvatar(t *testing.T) {
	identities := &stubIdentityLookup{identities: []auth.Identity{
		{ID: "identity-1", ProviderKind: "discord", AvatarRef: "0123456789abcdef0123456789abcdef", AvatarKey: "0123456789abcdef0123456789abcdef", AvatarSelected: true},
		{ID: "identity-2", ProviderKind: "oidc"},
	}}
	handler := NewAccount(identities, &stubUnlinker{}, &stubAvatarSelector{}, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
	request := withCurrentUser(httptest.NewRequest(http.MethodGet, "/account", nil), user.User{ID: "user-1", GameNick: "NightOtter"})
	response := httptest.NewRecorder()

	handler.Show(response, request)

	body := response.Body.String()
	if !strings.Contains(body, `src="/media/avatar/0123456789abcdef0123456789abcdef"`) || strings.Contains(body, "cdn.discordapp.com") {
		t.Fatalf("avatar must be same-origin: %q", body)
	}
	if !strings.Contains(body, `value="identity-1" checked`) || !strings.Contains(body, `value="identity-2" disabled`) {
		t.Fatalf("avatar choices = %q", body)
	}
}

func TestAccountSelectAvatar(t *testing.T) {
	tests := []struct {
		name, form, wantIdentity string
		err                      error
		wantStatus               int
	}{
		{"identity", "identity=identity-1", "identity-1", nil, http.StatusSeeOther},
		{"default", "identity=default", "", nil, http.StatusSeeOther},
		{"foreign identity", "identity=other", "other", auth.ErrIdentityNotFound, http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector := &stubAvatarSelector{err: test.err}
			handler := NewAccount(&stubIdentityLookup{}, &stubUnlinker{}, selector, &stubGameNickClaims{}, &stubAuthentication{}, nil, branding.Default())
			request := withCurrentUser(httptest.NewRequest(http.MethodPost, "/account/avatar", strings.NewReader(test.form)), user.User{ID: "user-1"})
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()

			handler.SelectAvatar(response, request)

			if response.Code != test.wantStatus || selector.userID != "user-1" || selector.identityID != test.wantIdentity {
				t.Fatalf("status = %d, call = %#v", response.Code, selector)
			}
		})
	}
}

type stubAvatarSelector struct {
	userID, identityID string
	err                error
}

func (s *stubAvatarSelector) SelectAvatar(_ context.Context, userID, identityID string) error {
	s.userID, s.identityID = userID, identityID
	return s.err
}

type stubUnlinker struct {
	userID, identityID string
	err                error
}

func (s *stubUnlinker) Unlink(_ context.Context, userID, identityID string) error {
	s.userID, s.identityID = userID, identityID
	return s.err
}
