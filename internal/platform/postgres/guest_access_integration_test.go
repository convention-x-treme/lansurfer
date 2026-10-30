//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/app"
	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	queryuser "github.com/convention-x-treme/lansurfer/internal/query/user"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var guestTestKey = []byte("01234567890123456789012345678901")

type guestEnv struct {
	pool     *pgxpool.Pool
	access   *auth.GuestAccessService
	accounts *app.GuestAccounts
	users    *user.Service
	sessions *auth.SessionService
}

func newGuestEnv(t *testing.T) guestEnv {
	t.Helper()
	pool := registrationTestPool(t)
	users := user.NewService(userpostgres.NewStore(pool))
	return guestEnv{
		pool:     pool,
		access:   auth.NewGuestAccessService(authpostgres.NewGuestAccessStore(pool), guestTestKey),
		accounts: app.NewGuestAccounts(pool, guestTestKey),
		users:    users,
		sessions: auth.NewSessionService(authpostgres.NewSessionStore(pool), users),
	}
}

func (e guestEnv) enable(t *testing.T) string {
	t.Helper()
	if err := e.access.SetEnabled(t.Context(), true); err != nil {
		t.Fatalf("enable guest access: %v", err)
	}
	state, err := e.access.State(t.Context())
	if err != nil || state.Invite == "" {
		t.Fatalf("State() = %#v, %v", state, err)
	}
	return state.Invite
}

func (e guestEnv) join(t *testing.T, invite, nick string) (user.User, auth.BrowserSession) {
	t.Helper()
	account, session, err := e.accounts.Register(t.Context(), invite, nick)
	if err != nil {
		t.Fatalf("Register(%q) error = %v", nick, err)
	}
	return account, session
}

func (e guestEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (e guestEnv) guestIDs(t *testing.T) []string {
	t.Helper()
	guests, err := queryuser.NewStore(e.pool).ListGuests(t.Context())
	if err != nil {
		t.Fatalf("ListGuests() error = %v", err)
	}
	ids := make([]string, len(guests))
	for i, guest := range guests {
		ids[i] = guest.ID
	}
	return ids
}

func (e guestEnv) exists(t *testing.T, id string) bool {
	t.Helper()
	return e.count(t, `SELECT count(*) FROM users WHERE id = $1`, id) == 1
}

// waitForLockWaiter blocks until some other backend waits on a lock.
func waitForLockWaiter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatalf("read lock waiters: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no backend ever waited on a lock")
}

func TestGuestLoginOptionPreference(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	keyless := auth.NewGuestAccessService(authpostgres.NewGuestAccessStore(e.pool), nil)
	state, err := keyless.State(ctx)
	if err != nil || state.ShowOnLogin {
		t.Fatalf("fresh state = %#v, %v", state, err)
	}
	if err := keyless.SetShowOnLogin(ctx, true); err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	if err := e.pool.QueryRow(ctx, `SELECT invite_ciphertext FROM auth_guest_access`).Scan(&ciphertext); err != nil || len(ciphertext) != 0 {
		t.Fatalf("keyless preference created ciphertext: %v", err)
	}
	if err := keyless.SetShowOnLogin(ctx, false); err != nil {
		t.Fatal(err)
	}
	invite := e.enable(t)
	state, err = e.access.State(ctx)
	if err != nil || state.ShowOnLogin {
		t.Fatalf("enabled state = %#v, %v", state, err)
	}
	if err := e.access.SetShowOnLogin(ctx, true); err != nil {
		t.Fatal(err)
	}
	fresh := auth.NewGuestAccessService(authpostgres.NewGuestAccessStore(e.pool), guestTestKey)
	state, err = fresh.State(ctx)
	if err != nil || !state.ShowOnLogin || state.Invite != invite {
		t.Fatalf("persisted state = %#v, %v", state, err)
	}
	if err := fresh.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := fresh.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Rotate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = fresh.State(ctx)
	if err != nil || !state.ShowOnLogin || !state.Enabled || state.Invite == invite {
		t.Fatalf("rotated state = %#v, %v", state, err)
	}
	invite = state.Invite
	if err := fresh.SetShowOnLogin(ctx, false); err != nil {
		t.Fatal(err)
	}
	state, err = fresh.State(ctx)
	if err != nil || state.ShowOnLogin || !state.Enabled || state.Invite != invite {
		t.Fatalf("hidden state = %#v, %v", state, err)
	}
}

func TestGuestInviteLifecycle(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	guessed := strings.Repeat("A", 43)

	if state, err := e.access.State(ctx); err != nil || state.Enabled || state.Invite != "" {
		t.Fatalf("fresh State() = %#v, %v", state, err)
	}
	if _, _, err := e.accounts.Register(ctx, guessed, "Early"); !errors.Is(err, auth.ErrGuestInviteInvalid) {
		t.Fatalf("Register() while disabled error = %v", err)
	}

	invite := e.enable(t)
	if again, _ := e.access.State(ctx); again.Invite != invite {
		t.Fatalf("revisited invite differs")
	}
	var ciphertext []byte
	if err := e.pool.QueryRow(ctx, `SELECT invite_ciphertext FROM auth_guest_access`).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte(invite)) {
		t.Fatalf("ciphertext stores plaintext or unreadable: %v", err)
	}
	first, firstSession := e.join(t, invite, "Night Otter")

	if err := e.access.Rotate(ctx); err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if _, _, err := e.accounts.Register(ctx, invite, "Stale"); !errors.Is(err, auth.ErrGuestInviteInvalid) {
		t.Fatalf("old invite error = %v", err)
	}
	rotated, _ := e.access.State(ctx)
	if rotated.Invite == invite || !rotated.Enabled {
		t.Fatalf("rotation state = %#v", rotated)
	}

	if err := e.access.SetEnabled(ctx, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := e.accounts.Register(ctx, rotated.Invite, "Late"); !errors.Is(err, auth.ErrGuestInviteInvalid) {
		t.Fatalf("disabled invite error = %v", err)
	}
	if got, err := e.sessions.Authenticate(ctx, firstSession.Token); err != nil || got.ID != first.ID {
		t.Fatalf("existing guest session after disable = %v, %v", got, err)
	}
	if kept, _ := e.access.State(ctx); kept.Invite != rotated.Invite || kept.Enabled {
		t.Fatalf("disabled state = %#v", kept)
	}
	// Rotation while disabled replaces the invite without enabling.
	if err := e.access.Rotate(ctx); err != nil {
		t.Fatalf("Rotate() while disabled: %v", err)
	}
	disabledRotation, _ := e.access.State(ctx)
	if disabledRotation.Enabled || disabledRotation.Invite == rotated.Invite {
		t.Fatalf("disabled rotation state = %#v", disabledRotation)
	}
	if err := e.access.SetEnabled(ctx, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if reenabled, _ := e.access.State(ctx); reenabled.Invite != disabledRotation.Invite {
		t.Fatal("re-enable must restore the same invite")
	}
	e.join(t, disabledRotation.Invite, "Second Otter")

	wrongKey := auth.NewGuestAccessService(authpostgres.NewGuestAccessStore(e.pool), []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"))
	if state, err := wrongKey.State(ctx); err == nil || state.Invite != "" {
		t.Fatalf("wrong key State() = %#v, %v", state, err)
	}
	if err := wrongKey.Rotate(ctx); err == nil {
		t.Fatal("wrong key Rotate() replaced an invite it cannot read")
	}
	if err := auth.NewGuestAccessService(authpostgres.NewGuestAccessStore(e.pool), nil).Rotate(ctx); !errors.Is(err, auth.ErrGuestAccessUnavailable) {
		t.Fatalf("keyless Rotate() error = %v", err)
	}
	if still, _ := e.access.State(ctx); still.Invite != disabledRotation.Invite {
		t.Fatal("failed rotations changed the invite")
	}
}

func TestGuestRegistrationIsAtomic(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	invite := e.enable(t)

	account, session := e.join(t, invite, "Night Otter")
	if account.Role != user.RoleUser {
		t.Fatalf("role = %q", account.Role)
	}
	if got, err := e.sessions.Authenticate(ctx, session.Token); err != nil || got.ID != account.ID {
		t.Fatalf("Authenticate() = %v, %v", got, err)
	}
	if e.count(t, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider_kind = 'guest' AND provider_key = 'guest'`, account.ID) != 1 {
		t.Fatal("guest identity missing")
	}

	for _, nick := range []string{"", strings.Repeat("x", 33), "bad\x00nick", "night otter", "NIGHT OTTER"} {
		if _, _, err := e.accounts.Register(ctx, invite, nick); err == nil {
			t.Fatalf("Register(%q) succeeded", nick)
		}
	}
	if e.count(t, `SELECT count(*) FROM users`) != 1 || e.count(t, `SELECT count(*) FROM auth_sessions`) != 1 {
		t.Fatal("rejected registrations left rows behind")
	}

	if _, err := e.pool.Exec(ctx, `
		CREATE FUNCTION fail_session() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'session insert failed'; END $$;
		CREATE TRIGGER fail_session BEFORE INSERT ON auth_sessions FOR EACH ROW EXECUTE FUNCTION fail_session()`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	if _, _, err := e.accounts.Register(ctx, invite, "Orphan"); err == nil {
		t.Fatal("Register() succeeded although session insert fails")
	}
	if e.count(t, `SELECT count(*) FROM users WHERE game_nick = 'Orphan'`) != 0 || e.count(t, `SELECT count(*) FROM user_identities WHERE provider_kind = 'guest'`) != 1 {
		t.Fatal("failed session persistence left an orphan user or identity")
	}
	if _, err := e.pool.Exec(ctx, `DROP TRIGGER fail_session ON auth_sessions`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	e.join(t, invite, "Orphan")
}

func TestGuestInviteDoesNotAuthenticateExistingAccount(t *testing.T) {
	e := newGuestEnv(t)
	invite := e.enable(t)
	first, _ := e.join(t, invite, "Night Otter")
	sessions := e.count(t, `SELECT count(*) FROM auth_sessions`)

	if _, _, err := e.accounts.Register(t.Context(), invite, "night otter"); !errors.Is(err, user.ErrGameNickTaken) {
		t.Fatalf("Register() existing nick error = %v", err)
	}
	if e.count(t, `SELECT count(*) FROM auth_sessions`) != sessions {
		t.Fatal("a session was issued for an existing nick")
	}
	second, _ := e.join(t, invite, "Other Otter")
	var firstSubject, secondSubject string
	for id, subject := range map[string]*string{first.ID: &firstSubject, second.ID: &secondSubject} {
		if err := e.pool.QueryRow(t.Context(), `SELECT subject FROM user_identities WHERE user_id = $1`, id).Scan(subject); err != nil {
			t.Fatal(err)
		}
	}
	if firstSubject == secondSubject || firstSubject == invite || secondSubject == invite {
		t.Fatal("guest subjects must be unique and independent of the invite")
	}
}

func TestGuestSignupSerializesWithInviteChanges(t *testing.T) {
	for name, mutate := range map[string]func(*auth.GuestAccessService, context.Context) error{
		"rotate":  func(s *auth.GuestAccessService, ctx context.Context) error { return s.Rotate(ctx) },
		"disable": func(s *auth.GuestAccessService, ctx context.Context) error { return s.SetEnabled(ctx, false) },
	} {
		t.Run(name, func(t *testing.T) {
			e := newGuestEnv(t)
			ctx := t.Context()
			invite := e.enable(t)

			tx, err := e.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if err := authpostgres.NewGuestAccessStoreDB(tx).Lock(ctx); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, _, err := e.accounts.Register(ctx, invite, "Queued")
				result <- err
			}()
			waitForLockWaiter(t, e.pool)
			if err := mutate(auth.NewGuestAccessService(authpostgres.NewGuestAccessStoreDB(tx), guestTestKey), ctx); err != nil {
				t.Fatalf("mutate in lock holder: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, auth.ErrGuestInviteInvalid) {
				t.Fatalf("queued registration error = %v", err)
			}
			if e.count(t, `SELECT count(*) FROM users`) != 0 {
				t.Fatal("queued registration created an account")
			}
		})
	}

	t.Run("registration commits before queued change", func(t *testing.T) {
		e := newGuestEnv(t)
		ctx := t.Context()
		e.enable(t)
		tx, err := e.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if err := authpostgres.NewGuestAccessStoreDB(tx).Lock(ctx); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- e.access.SetEnabled(ctx, false) }()
		waitForLockWaiter(t, e.pool)
		if _, err := user.NewService(userpostgres.NewStoreDB(tx)).Register(ctx, "First"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("queued disable: %v", err)
		}
		if e.count(t, `SELECT count(*) FROM users WHERE game_nick = 'First'`) != 1 {
			t.Fatal("registration that owned the lock was lost")
		}
	})
}

type deterministicProvider struct {
	kind    string
	profile auth.Profile
}

func (p deterministicProvider) AuthorizationURL(state, _, _ string) string { return state }
func (p deterministicProvider) Exchange(context.Context, string, string, string) (auth.Profile, error) {
	return p.profile, nil
}
func (p deterministicProvider) ProviderKind() string { return p.kind }
func (p deterministicProvider) ProviderKey() string  { return p.kind }

func (e guestEnv) resolution() *auth.ResolutionService {
	return auth.NewResolutionService(
		authpostgres.NewIdentityStore(e.pool), auth.NewFlowService(authpostgres.NewFlowStore(e.pool)), e.sessions, e.users,
		authpostgres.NewPendingIdentityAttacher(e.pool),
	)
}

// guestWithData signs up a guest who owns a request, joined another, and follows a game.
func (e guestEnv) guestWithData(t *testing.T, invite, nick string, host user.User) (user.User, auth.BrowserSession, string) {
	t.Helper()
	account, session := e.join(t, invite, nick)
	games := game.NewService(gamepostgres.NewStore(e.pool))
	catalog, err := games.Create(t.Context(), "Guest Game "+nick, "guest-game-"+strings.ToLower(strings.ReplaceAll(nick, " ", "-")))
	if err != nil {
		t.Fatal(err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(e.pool), games, nil)
	startsAt := time.Now().UTC().Add(time.Hour)
	owned, err := requests.Create(t.Context(), account.ID, catalog.ID, nil, startsAt, "guest round")
	if err != nil {
		t.Fatal(err)
	}
	hosted, err := requests.Create(t.Context(), host.ID, catalog.ID, nil, startsAt, "host round")
	if err != nil {
		t.Fatal(err)
	}
	if err := requests.Join(t.Context(), hosted.ID, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO game_subscriptions (user_id, game_id) VALUES ($1, $2)`, account.ID, catalog.ID); err != nil {
		t.Fatal(err)
	}
	return account, session, owned.ID
}

func TestGuestConversionPreservesAccount(t *testing.T) {
	for _, kind := range []string{"oidc", "discord", "twitch"} {
		t.Run(kind, func(t *testing.T) {
			e := newGuestEnv(t)
			ctx := t.Context()
			invite := e.enable(t)
			host, err := e.users.Register(ctx, "Host")
			if err != nil {
				t.Fatal(err)
			}
			guest, session, ownedID := e.guestWithData(t, invite, "Guest Smoke", host)
			counts := func() [4]int {
				return [4]int{
					e.count(t, `SELECT count(*) FROM game_requests WHERE creator_id = $1`, guest.ID),
					e.count(t, `SELECT count(*) FROM game_request_members WHERE user_id = $1`, guest.ID),
					e.count(t, `SELECT count(*) FROM game_subscriptions WHERE user_id = $1`, guest.ID),
					e.count(t, `SELECT count(*) FROM game_request_activity WHERE actor_id = $1`, guest.ID),
				}
			}
			before := counts()

			profile := auth.Profile{ProviderKind: kind, ProviderKey: kind, Subject: kind + "-subject", Name: "Provider Name"}
			if _, err := e.resolution().Link(ctx, profile, guest.ID); err != nil {
				t.Fatalf("Link() error = %v", err)
			}
			if counts() != before {
				t.Fatalf("product data changed: %v -> %v", before, counts())
			}
			if got, err := e.sessions.Authenticate(ctx, session.Token); err != nil || got.ID != guest.ID || got.GameNick != "Guest Smoke" {
				t.Fatalf("session after conversion = %v, %v", got, err)
			}
			if e.count(t, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider_kind = $2`, guest.ID, kind) != 1 ||
				e.count(t, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider_kind = 'guest'`, guest.ID) != 0 ||
				len(e.guestIDs(t)) != 0 {
				t.Fatal("conversion did not leave exactly one permanent identity")
			}
			// Same-user duplicate link is idempotent.
			if _, err := e.resolution().Link(ctx, profile, guest.ID); err != nil {
				t.Fatalf("duplicate Link() error = %v", err)
			}
			if e.count(t, `SELECT count(*) FROM user_identities WHERE user_id = $1`, guest.ID) != 1 {
				t.Fatal("duplicate link changed identities")
			}
			// Provider already linked with another subject.
			other := profile
			other.Subject = "another-subject"
			if _, err := e.resolution().Link(ctx, other, guest.ID); !errors.Is(err, auth.ErrProviderAlreadyLinked) {
				t.Fatalf("second subject error = %v", err)
			}
			if !e.exists(t, guest.ID) || e.count(t, `SELECT count(*) FROM game_requests WHERE id = $1`, ownedID) != 1 {
				t.Fatal("guest data vanished")
			}
		})
	}
}

func TestGuestConversionConflictLeavesGuestUntouched(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	invite := e.enable(t)
	owner, err := e.users.Register(ctx, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	profile := auth.Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "taken"}
	if _, err := e.resolution().Link(ctx, profile, owner.ID); err != nil {
		t.Fatal(err)
	}
	guest, session := e.join(t, invite, "Guest")
	if _, err := e.resolution().Link(ctx, profile, guest.ID); !errors.Is(err, auth.ErrIdentityConflict) {
		t.Fatalf("Link() error = %v", err)
	}
	if ids := e.guestIDs(t); len(ids) != 1 || ids[0] != guest.ID {
		t.Fatalf("guest list = %v", ids)
	}
	if _, err := e.sessions.Authenticate(ctx, session.Token); err != nil {
		t.Fatalf("session after conflict: %v", err)
	}
	if e.count(t, `SELECT count(*) FROM user_identities WHERE subject = 'taken' AND user_id = $1`, owner.ID) != 1 {
		t.Fatal("identity moved from its owner")
	}
}

func TestGuestConversionThroughOAuthFlow(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	invite := e.enable(t)
	guest, _ := e.join(t, invite, "Flow Guest")
	provider := deterministicProvider{kind: "discord", profile: auth.Profile{ProviderKind: "discord", ProviderKey: "discord", Subject: "flow-subject", Name: "Flow"}}
	service := auth.NewOAuthService(
		[]auth.ConfiguredProvider{{DisplayName: "Discord", Adapter: provider}},
		auth.NewFlowService(authpostgres.NewFlowStore(e.pool)), e.resolution(),
	)
	start, err := service.Begin(ctx, auth.OAuthStartInput{ProviderKind: "discord", BrowserBinding: "browser", Operation: auth.FlowLink, InitiatingUserID: guest.ID})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	// A different signed-in user cannot complete the flow.
	if _, err := service.Complete(ctx, auth.OAuthCompletion{ProviderKind: "discord", State: start.AuthorizationURL, BrowserBinding: "browser", Code: "code", CurrentUserID: "00000000-0000-0000-0000-000000000000"}); err == nil {
		t.Fatal("Complete() accepted another initiating user")
	}
	start, err = service.Begin(ctx, auth.OAuthStartInput{ProviderKind: "discord", BrowserBinding: "browser", Operation: auth.FlowLink, InitiatingUserID: guest.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(ctx, auth.OAuthCompletion{ProviderKind: "discord", State: start.AuthorizationURL, BrowserBinding: "browser", Code: "code", CurrentUserID: guest.ID}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(e.guestIDs(t)) != 0 || e.count(t, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND provider_kind = 'discord'`, guest.ID) != 1 {
		t.Fatal("OAuth link did not convert the guest")
	}
}

func TestGuestMarkerCannotBeUnlinked(t *testing.T) {
	e := newGuestEnv(t)
	invite := e.enable(t)
	guest, _ := e.join(t, invite, "Marker")
	var identityID string
	if err := e.pool.QueryRow(t.Context(), `SELECT id::text FROM user_identities WHERE user_id = $1`, guest.ID).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	if err := authpostgres.NewIdentityStore(e.pool).Unlink(t.Context(), guest.ID, identityID); !errors.Is(err, auth.ErrIdentityNotFound) {
		t.Fatalf("Unlink(guest) error = %v", err)
	}
	if len(e.guestIDs(t)) != 1 {
		t.Fatal("guest marker was removed")
	}
}

func TestGuestDeletionReusesCompleteAccountDeletion(t *testing.T) {
	e := newGuestEnv(t)
	ctx := t.Context()
	invite := e.enable(t)
	admin, err := e.users.Register(ctx, "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.users.PromoteToAdmin(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	permanent, err := e.users.Register(ctx, "Permanent")
	if err != nil {
		t.Fatal(err)
	}
	g1, _, owned := e.guestWithData(t, invite, "Guest One", admin)
	g2, _, _ := e.guestWithData(t, invite, "Guest Two", admin)
	g3, _, _ := e.guestWithData(t, invite, "Guest Three", admin)
	converted, _ := e.join(t, invite, "Converted")
	if _, err := e.resolution().Link(ctx, auth.Profile{ProviderKind: "twitch", ProviderKey: "twitch", Subject: "c"}, converted.ID); err != nil {
		t.Fatal(err)
	}

	missing := "11111111-1111-1111-1111-111111111111"
	deleted, err := e.accounts.DeleteGuests(ctx, []string{g1.ID, permanent.ID, converted.ID, strings.ToUpper(g1.ID), missing})
	if err != nil || len(deleted) != 1 || deleted[0] != g1.ID {
		t.Fatalf("DeleteGuests() = %v, %v", deleted, err)
	}
	if e.exists(t, g1.ID) || !e.exists(t, permanent.ID) || !e.exists(t, converted.ID) {
		t.Fatal("selection broadened or guest survived")
	}
	for _, query := range []string{
		`SELECT count(*) FROM game_requests WHERE id = $1`,
	} {
		if e.count(t, query, owned) != 0 {
			t.Fatal("owned request survived account deletion")
		}
	}
	for _, query := range []string{
		`SELECT count(*) FROM auth_sessions WHERE user_id = $1`,
		`SELECT count(*) FROM game_subscriptions WHERE user_id = $1`,
		`SELECT count(*) FROM game_request_members WHERE user_id = $1`,
		`SELECT count(*) FROM game_request_activity WHERE actor_id = $1`,
		`SELECT count(*) FROM user_identities WHERE user_id = $1`,
	} {
		if e.count(t, query, g1.ID) != 0 {
			t.Fatalf("%s left rows", query)
		}
	}
	e.join(t, invite, "Guest One") // nick is reusable

	for _, selection := range [][]string{nil, {"not-a-uuid"}} {
		if _, err := e.accounts.DeleteGuests(ctx, selection); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("DeleteGuests(%v) error = %v", selection, err)
		}
	}

	deleted, err = e.accounts.DeleteGuests(ctx, []string{g2.ID})
	if err != nil || len(deleted) != 1 {
		t.Fatalf("selected delete = %v, %v", deleted, err)
	}

	// A batch that would delete every administrator rolls back entirely.
	if _, err := e.users.PromoteToAdmin(ctx, g3.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	before := e.guestIDs(t)
	if _, err := e.accounts.DeleteAllGuests(ctx); !errors.Is(err, user.ErrLastAdmin) {
		t.Fatalf("DeleteAllGuests() error = %v", err)
	}
	if after := e.guestIDs(t); len(after) != len(before) {
		t.Fatalf("failed batch deleted guests: %v -> %v", before, after)
	}

	if _, err := e.pool.Exec(ctx, `UPDATE users SET role = 'admin' WHERE id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err = e.accounts.DeleteAllGuests(ctx)
	if err != nil || len(deleted) != len(before) {
		t.Fatalf("DeleteAllGuests() = %v, %v", deleted, err)
	}
	if len(e.guestIDs(t)) != 0 || !e.exists(t, permanent.ID) || !e.exists(t, converted.ID) || !e.exists(t, admin.ID) {
		t.Fatal("delete-all removed the wrong accounts")
	}
}

func TestGuestConversionSerializesWithDeletion(t *testing.T) {
	t.Run("conversion first", func(t *testing.T) {
		e := newGuestEnv(t)
		ctx := t.Context()
		invite := e.enable(t)
		converted, _ := e.join(t, invite, "Converting")
		stays, _ := e.join(t, invite, "Eligible")

		tx, err := e.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := userpostgres.NewStoreDB(tx).LockByID(ctx, converted.ID); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var deleted []string
		var deleteErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			deleted, deleteErr = e.accounts.DeleteGuests(ctx, []string{converted.ID, stays.ID})
		}()
		waitForLockWaiter(t, e.pool)
		if _, err := authpostgres.NewIdentityStoreDB(tx).Link(ctx, auth.Identity{UserID: converted.ID, ProviderKind: "discord", ProviderKey: "discord", Subject: "won"}); err != nil {
			t.Fatalf("Link() in lock holder: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if deleteErr != nil || len(deleted) != 1 || deleted[0] != stays.ID {
			t.Fatalf("DeleteGuests() = %v, %v", deleted, deleteErr)
		}
		if !e.exists(t, converted.ID) || e.exists(t, stays.ID) {
			t.Fatal("converted account must survive and eligible guest must go")
		}
	})

	t.Run("deletion first", func(t *testing.T) {
		e := newGuestEnv(t)
		ctx := t.Context()
		invite := e.enable(t)
		doomed, _ := e.join(t, invite, "Doomed")

		tx, err := e.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := userpostgres.NewStoreDB(tx).LockByID(ctx, doomed.ID); err != nil {
			t.Fatal(err)
		}
		linkErr := make(chan error, 1)
		go func() {
			_, err := authpostgres.NewIdentityStore(e.pool).Link(ctx, auth.Identity{UserID: doomed.ID, ProviderKind: "discord", ProviderKey: "discord", Subject: "lost"})
			linkErr <- err
		}()
		waitForLockWaiter(t, e.pool)
		if err := user.NewService(userpostgres.NewStoreDB(tx)).DeleteAccount(ctx, doomed.ID, "Doomed"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-linkErr; !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("Link() after deletion error = %v", err)
		}
		if e.count(t, `SELECT count(*) FROM user_identities`) != 0 || e.exists(t, doomed.ID) {
			t.Fatal("linking recreated or kept deleted account data")
		}
	})
}

func TestGuestAccessSurvivesDemoReset(t *testing.T) {
	e := newGuestEnv(t)
	invite := e.enable(t)
	if err := e.access.SetShowOnLogin(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	e.join(t, invite, "Before Reset")
	var before []byte
	if err := e.pool.QueryRow(t.Context(), `SELECT invite_ciphertext FROM auth_guest_access`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := platformpostgres.TruncateForDemoReset(t.Context(), e.pool); err != nil {
		t.Fatalf("reset: %v", err)
	}
	var after []byte
	var enabled, show bool
	if err := e.pool.QueryRow(t.Context(), `SELECT enabled, show_on_login, invite_ciphertext FROM auth_guest_access`).Scan(&enabled, &show, &after); err != nil || !enabled || !show || !bytes.Equal(before, after) {
		t.Fatalf("guest access after reset = enabled %v, show %v, %v", enabled, show, err)
	}
	if e.count(t, `SELECT count(*) FROM users`) != 0 || e.count(t, `SELECT count(*) FROM user_identities`) != 0 || e.count(t, `SELECT count(*) FROM auth_sessions`) != 0 {
		t.Fatal("reset kept guest accounts")
	}
	e.join(t, invite, "After Reset")
}
