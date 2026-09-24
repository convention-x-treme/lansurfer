//go:build integration

package postgres_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/app"
	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

const (
	discordHash      = "0123456789abcdef0123456789abcdef"
	newerDiscordHash = "fedcba9876543210fedcba9876543210"
	twitchPicture    = "https://static-cdn.jtvnw.net/jtv_user_pictures/otter.png"
)

// The avatar selection lifecycle: a new account starts with its creating
// identity's picture; linking, logging in through another provider, and
// picture refreshes never change the choice; only selected pictures resolve
// publicly; unlinking the selected identity falls back to Default.
func TestAvatarSelectionLifecycle(t *testing.T) {
	pool := registrationTestPool(t)
	ctx := t.Context()
	users := user.NewService(userpostgres.NewStore(pool))
	sessions := auth.NewSessionService(authpostgres.NewSessionStore(pool), users)
	flows := auth.NewFlowService(authpostgres.NewFlowStore(pool))
	identities := authpostgres.NewIdentityStore(pool)
	resolution := auth.NewResolutionService(identities, flows, sessions, users, authpostgres.NewPendingIdentityAttacher(pool))

	discordLogin := auth.Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "d-1", Name: "Otter", AvatarRef: discordHash}
	pendingLogin, err := resolution.ResolveLogin(ctx, discordLogin, "browser", false)
	if err != nil {
		t.Fatalf("ResolveLogin: %v", err)
	}
	account, _, err := app.NewAccountRegistration(pool, sessions).Register(ctx, pendingLogin.Pending.ID, "browser", "NightOtter")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	discordKey := auth.AvatarKey("discord", "d-1", discordHash)
	assertSelectedKey(t, identities, account.ID, discordKey)

	twitch, err := resolution.Link(ctx, auth.Profile{ProviderKind: "twitch", ProviderKey: "twitch", Subject: "t-1", AvatarRef: twitchPicture}, account.ID)
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if _, err := resolution.ResolveLogin(ctx, auth.Profile{ProviderKind: "twitch", ProviderKey: "twitch", Subject: "t-1", AvatarRef: twitchPicture}, "browser", false); err != nil {
		t.Fatalf("Twitch login: %v", err)
	}
	assertSelectedKey(t, identities, account.ID, discordKey)

	twitchKey := auth.AvatarKey("twitch", "t-1", twitchPicture)
	if known, err := identities.SelectedAvatarKnown(ctx, twitchKey); err != nil || known {
		t.Fatalf("unselected Twitch avatar known = %v, %v", known, err)
	}

	// A refreshed Discord picture moves the selected key with it.
	discordLogin.AvatarRef = newerDiscordHash
	if _, err := resolution.ResolveLogin(ctx, discordLogin, "browser", false); err != nil {
		t.Fatalf("Discord re-login: %v", err)
	}
	newerKey := auth.AvatarKey("discord", "d-1", newerDiscordHash)
	assertSelectedKey(t, identities, account.ID, newerKey)
	if known, _ := identities.SelectedAvatarKnown(ctx, discordKey); known {
		t.Fatal("stale Discord key is still known")
	}
	stored, err := identities.SelectedAvatarByKey(ctx, newerKey)
	if err != nil || stored != (auth.StoredAvatar{ProviderKind: "discord", Subject: "d-1", AvatarRef: newerDiscordHash}) {
		t.Fatalf("SelectedAvatarByKey = %#v, %v", stored, err)
	}

	if err := identities.SelectAvatar(ctx, account.ID, twitch.ID); err != nil {
		t.Fatalf("select Twitch: %v", err)
	}
	assertSelectedKey(t, identities, account.ID, twitchKey)

	if err := identities.Unlink(ctx, account.ID, twitch.ID); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	assertSelectedKey(t, identities, account.ID, "")
	if known, _ := identities.SelectedAvatarKnown(ctx, twitchKey); known {
		t.Fatal("unlinked avatar is still known")
	}
}

func TestAvatarSelectionRejectsForeignAndPictureless(t *testing.T) {
	pool := registrationTestPool(t)
	ctx := t.Context()
	users := user.NewService(userpostgres.NewStore(pool))
	identities := authpostgres.NewIdentityStore(pool)
	owner, _ := users.Register(ctx, "Owner")
	other, _ := users.Register(ctx, "Other")
	selected, err := identities.Create(ctx, auth.Identity{UserID: owner.ID, ProviderKind: "discord", ProviderKey: "discord", Subject: "d-owner", AvatarRef: discordHash, AvatarSelected: true})
	if err != nil {
		t.Fatalf("create owner identity: %v", err)
	}
	pictureless, _ := identities.Create(ctx, auth.Identity{UserID: owner.ID, ProviderKind: "oidc", ProviderKey: "oidc", Subject: "o-owner"})
	foreign, _ := identities.Create(ctx, auth.Identity{UserID: other.ID, ProviderKind: "discord", ProviderKey: "discord", Subject: "d-other", AvatarRef: newerDiscordHash})

	for name, identityID := range map[string]string{"foreign": foreign.ID, "pictureless": pictureless.ID, "malformed": "nope"} {
		if err := identities.SelectAvatar(ctx, owner.ID, identityID); !errors.Is(err, auth.ErrIdentityNotFound) {
			t.Errorf("%s: SelectAvatar error = %v, want ErrIdentityNotFound", name, err)
		}
	}
	assertSelectedKey(t, identities, owner.ID, auth.AvatarKey("discord", "d-owner", discordHash))

	if err := identities.SelectAvatar(ctx, owner.ID, ""); err != nil {
		t.Fatalf("select Default: %v", err)
	}
	assertSelectedKey(t, identities, owner.ID, "")

	// Concurrent selections of different identities leave exactly one.
	second, _ := identities.Create(ctx, auth.Identity{UserID: owner.ID, ProviderKind: "twitch", ProviderKey: "twitch", Subject: "t-owner", AvatarRef: twitchPicture})
	var wait sync.WaitGroup
	for range 10 {
		for _, id := range []string{selected.ID, second.ID} {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_ = identities.SelectAvatar(ctx, owner.ID, id)
			}()
		}
	}
	wait.Wait()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND avatar_selected`, owner.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("selected identities = %d, %v, want 1", count, err)
	}
}

func assertSelectedKey(t *testing.T, identities *authpostgres.IdentityStore, userID, want string) {
	t.Helper()
	got, err := identities.SelectedAvatarKey(t.Context(), userID)
	if err != nil || got != want {
		t.Fatalf("SelectedAvatarKey = %q, %v, want %q", got, err, want)
	}
}
