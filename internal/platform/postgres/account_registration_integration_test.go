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
	if _, _, err := registration.Register(t.Context(), duplicate.ID, "browser", "OrphanNick"); !errors.Is(err, auth.ErrIdentityExists) {
		t.Fatalf("duplicate registration error = %v", err)
	}
	var orphanUsers int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM users WHERE game_nick = 'OrphanNick'`).Scan(&orphanUsers); err != nil {
		t.Fatalf("count rolled-back user: %v", err)
	}
	if orphanUsers != 0 {
		t.Fatalf("rolled-back users = %d, want 0", orphanUsers)
	}
	if _, err := flows.Pending(t.Context(), duplicate.ID, "browser"); err != nil {
		t.Fatalf("failed registration consumed pending identity: %v", err)
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

func TestAccountRegistrationConcurrentIdentityClaimHasOneWinner(t *testing.T) {
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
	results := make([]error, 2)
	var wait sync.WaitGroup
	for index := range pending {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, _, results[index] = registration.Register(context.Background(), pending[index].ID, "browser", []string{"FirstNick", "SecondNick"}[index])
		}()
	}
	close(start)
	wait.Wait()
	winners, conflicts := 0, 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, auth.ErrIdentityExists) {
			conflicts++
		} else {
			t.Fatalf("unexpected registration error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners = %d, conflicts = %d", winners, conflicts)
	}
	var users int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users = %d, want one committed account", users)
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
