//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/app"
	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAccountRegistrationIsAtomic(t *testing.T) {
	pool := registrationTestPool(t)
	flows := auth.NewFlowService(authpostgres.NewFlowStore(pool))
	sessions := auth.NewSessionService(authpostgres.NewSessionStore(pool), user.NewService(userpostgres.NewStore(pool)))
	registration := app.NewAccountRegistration(pool, sessions)

	pending, err := flows.CreatePending(t.Context(), auth.PendingIdentityInput{
		BrowserBinding: "browser", ProviderKind: "discord", ProviderKey: "discord", Subject: "discord-1",
		Operation: auth.FlowLogin, DisplayName: "Alice Example", GameNickSuggestion: "NightOtter",
	})
	if err != nil {
		t.Fatalf("create pending identity: %v", err)
	}
	account, session, err := registration.Register(t.Context(), pending.ID, "browser", "NightOtter")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if account.GameNick != "NightOtter" || session.Token == "" {
		t.Fatalf("registration = %#v, %#v", account, session)
	}

	duplicate, err := flows.CreatePending(t.Context(), auth.PendingIdentityInput{
		BrowserBinding: "browser", ProviderKind: "discord", ProviderKey: "discord", Subject: "discord-1",
		Operation: auth.FlowLogin, DisplayName: "Alice Example",
	})
	if err != nil {
		t.Fatalf("create duplicate pending identity: %v", err)
	}
	// A second pending identity for a subject that's already linked to an
	// account (a stale/replayed pending row, or a race narrowly missed by
	// the concurrent case below) resolves into that existing account rather
	// than failing outright: no orphan "OrphanNick" user, and no new
	// identity, just a fresh session for the account that already exists.
	duplicateAccount, duplicateSession, err := registration.Register(t.Context(), duplicate.ID, "browser", "OrphanNick")
	if err != nil {
		t.Fatalf("duplicate registration error = %v, want nil (resolves to the existing account)", err)
	}
	if duplicateAccount.ID != account.ID || duplicateAccount.GameNick != "NightOtter" || duplicateSession.Token == "" {
		t.Fatalf("duplicate registration = %#v, %#v, want the original account %#v", duplicateAccount, duplicateSession, account)
	}
	var orphanUsers int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM users WHERE game_nick = 'OrphanNick'`).Scan(&orphanUsers); err != nil {
		t.Fatalf("count rolled-back user: %v", err)
	}
	if orphanUsers != 0 {
		t.Fatalf("rolled-back users = %d, want 0", orphanUsers)
	}
	if _, err := flows.Pending(t.Context(), duplicate.ID, "browser"); err != nil {
		t.Fatalf("resolved-race registration unexpectedly consumed the pending identity: %v", err)
	}
}

func TestAccountRegistrationConcurrentGameNickClaimHasOneWinner(t *testing.T) {
	pool := registrationTestPool(t)
	flows := auth.NewFlowService(authpostgres.NewFlowStore(pool))
	registration := app.NewAccountRegistration(pool, auth.NewSessionService(authpostgres.NewSessionStore(pool), user.NewService(userpostgres.NewStore(pool))))
	pending := make([]auth.PendingIdentity, 2)
	for index, subject := range []string{"discord-a", "discord-b"} {
		created, err := flows.CreatePending(t.Context(), auth.PendingIdentityInput{
			BrowserBinding: "browser", ProviderKind: "discord", ProviderKey: "discord", Subject: subject, Operation: auth.FlowLogin,
		})
		if err != nil {
			t.Fatalf("create pending identity %d: %v", index, err)
		}
		pending[index] = created
	}
	start := make(chan struct{})
	results := make([]error, 2)
	var wait sync.WaitGroup
	for index := range pending {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, _, results[index] = registration.Register(context.Background(), pending[index].ID, "browser", "SharedNick")
		}()
	}
	close(start)
	wait.Wait()
	winners, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, user.ErrGameNickTaken) {
			conflicts++
		} else {
			t.Fatalf("unexpected registration error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners = %d, conflicts = %d", winners, conflicts)
	}
}

// Two pending identities for the same underlying provider subject (e.g. two
// browser tabs that each independently completed the same Discord login)
// racing to register must both succeed and sign into the same account,
// rather than the loser failing outright with the winner's database
// conflict.
func TestAccountRegistrationConcurrentIdentityClaimResolvesBothIntoOneAccount(t *testing.T) {
	pool := registrationTestPool(t)
	flows := auth.NewFlowService(authpostgres.NewFlowStore(pool))
	registration := app.NewAccountRegistration(pool, auth.NewSessionService(authpostgres.NewSessionStore(pool), user.NewService(userpostgres.NewStore(pool))))
	pending := make([]auth.PendingIdentity, 2)
	for index := range pending {
		created, err := flows.CreatePending(t.Context(), auth.PendingIdentityInput{
			BrowserBinding: "browser", ProviderKind: "discord", ProviderKey: "discord", Subject: "same-discord-id", Operation: auth.FlowLogin,
		})
		if err != nil {
			t.Fatalf("create pending identity %d: %v", index, err)
		}
		pending[index] = created
	}
	start := make(chan struct{})
	accounts := make([]user.User, 2)
	sessions := make([]auth.BrowserSession, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range pending {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			accounts[index], sessions[index], errs[index] = registration.Register(context.Background(), pending[index].ID, "browser", []string{"FirstNick", "SecondNick"}[index])
		}()
	}
	close(start)
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("registration %d error = %v, want nil (idempotent race)", index, err)
		}
	}
	if accounts[0].ID != accounts[1].ID || accounts[0].ID == "" {
		t.Fatalf("accounts = %#v, want both to resolve to the same non-empty account", accounts)
	}
	if sessions[0].Token == "" || sessions[1].Token == "" || sessions[0].Token == sessions[1].Token {
		t.Fatalf("sessions = %#v, want two distinct non-empty tokens", sessions)
	}
	var users int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users = %d, want one committed account", users)
	}
}

// Two identical concurrent "prove existing account" submissions (a
// double-submitted form) attaching the same pending identity to the same
// proven account must never make the loser fail with a false "belongs to
// another account" conflict, and must attach the identity exactly once.
//
// Depending on OS scheduling, the two attempts either interleave tightly
// enough that both pass AttachPending's own pre-check before either commits
// (exercising the Create-time ErrIdentityExists race this test exists to
// cover) or one finishes completely — including consuming the pending row —
// before the other starts (a legitimate ErrFlowConsumed rejection, not a
// bug). Both outcomes are acceptable; a false conflict is not.
func TestAttachPendingConcurrentRaceResolvesIdempotently(t *testing.T) {
	pool := registrationTestPool(t)
	var userID string
	if err := pool.QueryRow(t.Context(), `INSERT INTO users (game_nick) VALUES ('NightOtter') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("create proven user: %v", err)
	}
	flows := auth.NewFlowService(authpostgres.NewFlowStore(pool))
	pending, err := flows.CreatePending(t.Context(), auth.PendingIdentityInput{
		BrowserBinding: "browser", ProviderKind: "discord", ProviderKey: "discord", Subject: "discord-1", Operation: auth.FlowResolution,
	})
	if err != nil {
		t.Fatalf("create pending identity: %v", err)
	}
	attacher := authpostgres.NewPendingIdentityAttacher(pool)

	start := make(chan struct{})
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range errs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs[index] = attacher.AttachPending(context.Background(), pending.ID, "browser", userID, "oidc", "https://issuer.example")
		}()
	}
	close(start)
	wait.Wait()
	for index, err := range errs {
		if errors.Is(err, auth.ErrIdentityConflict) || errors.Is(err, auth.ErrProviderAlreadyLinked) {
			t.Fatalf("attach %d error = %v, want no false conflict from the concurrent race", index, err)
		}
		if err != nil && !errors.Is(err, auth.ErrFlowConsumed) {
			t.Fatalf("attach %d unexpected error = %v", index, err)
		}
	}

	identities, err := authpostgres.NewIdentityStore(pool).ListByUser(t.Context(), userID)
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	if len(identities) != 1 || identities[0].ProviderKind != "discord" || identities[0].Subject != "discord-1" {
		t.Fatalf("identities = %#v, want exactly one discord identity attached once", identities)
	}
}

// Unlinking a player's last two identities at once must never leave zero
// identities linked. The persistence layer, not the web UI (which never
// offers an unlink control for a player's last identity), is what actually
// protects this invariant against a race.
func TestUnlinkIdentityRaceLeavesAtLeastOneLinked(t *testing.T) {
	pool := registrationTestPool(t)
	identities := authpostgres.NewIdentityStore(pool)
	var userID string
	if err := pool.QueryRow(t.Context(), `INSERT INTO users (game_nick) VALUES ('NightOtter') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	oidc, err := identities.Create(t.Context(), auth.Identity{UserID: userID, ProviderKind: "oidc", ProviderKey: "https://issuer.example", Subject: "subject-1"})
	if err != nil {
		t.Fatalf("create oidc identity: %v", err)
	}
	discord, err := identities.Create(t.Context(), auth.Identity{UserID: userID, ProviderKind: "discord", ProviderKey: "discord", Subject: "subject-2"})
	if err != nil {
		t.Fatalf("create discord identity: %v", err)
	}

	targets := []string{oidc.ID, discord.ID}
	start := make(chan struct{})
	results := make([]error, 2)
	var wait sync.WaitGroup
	for index := range targets {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index] = identities.Unlink(context.Background(), userID, targets[index])
		}()
	}
	close(start)
	wait.Wait()
	winners, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, auth.ErrIdentityNotFound) {
			conflicts++
		} else {
			t.Fatalf("unexpected unlink error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners = %d, conflicts = %d, want 1 and 1", winners, conflicts)
	}

	remaining, err := identities.ListByUser(t.Context(), userID)
	if err != nil {
		t.Fatalf("list remaining identities: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining identities = %d, want exactly 1 (never zero)", len(remaining))
	}
}

func registrationTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("LANSURFER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LANSURFER_TEST_DATABASE_URL is not set")
	}
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("random schema name: %v", err)
	}
	schema := "registration_" + hex.EncodeToString(random)
	admin, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	if _, err := admin.Exec(t.Context(), `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	if err := platformpostgres.Migrate(t.Context(), pool, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})
	return pool
}
