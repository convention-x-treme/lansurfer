//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestAuthenticationPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	account, err := users.Register(t.Context(), "LAN Player")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	if account.Role != user.RoleUser || account.ID == "" || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
		t.Fatalf("account = %#v", account)
	}

	identities := authpostgres.NewIdentityStore(pool)
	identity, err := identities.Create(t.Context(), auth.Identity{
		UserID:      account.ID,
		Provider:    "https://identity.example/realms/lan",
		Subject:     "subject-1",
		DisplayName: "Identity Name",
		Email:       "player@example.test",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	found, err := identities.ByProviderSubject(t.Context(), identity.Provider, identity.Subject)
	if err != nil || found.UserID != account.ID || found.Email != identity.Email {
		t.Fatalf("find identity = %#v, %v", found, err)
	}
	if _, err := identities.Create(t.Context(), auth.Identity{
		UserID: account.ID, Provider: identity.Provider, Subject: identity.Subject,
	}); !errors.Is(err, auth.ErrIdentityExists) {
		t.Fatalf("duplicate identity error = %v, want ErrIdentityExists", err)
	}

	sessions := authpostgres.NewSessionStore(pool)
	tokenHash := sha256.Sum256([]byte("opaque-token"))
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if err := sessions.Create(t.Context(), tokenHash, account.ID, expiresAt); err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessions = authpostgres.NewSessionStore(pool)
	resolved, err := sessions.Resolve(t.Context(), tokenHash)
	if err != nil || resolved.UserID != account.ID || !resolved.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("resolve session = %#v, %v", resolved, err)
	}
	if err := sessions.Delete(t.Context(), tokenHash); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := sessions.Resolve(t.Context(), tokenHash); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("resolve revoked session error = %v, want ErrSessionNotFound", err)
	}

	expiredHash := sha256.Sum256([]byte("expired-token"))
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO auth_sessions (id_hash, user_id, expires_at, created_at)
		VALUES ($1, $2, now() - interval '1 minute', now() - interval '2 minutes')
	`, expiredHash[:], account.ID); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}
	if _, err := sessions.Resolve(t.Context(), expiredHash); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("resolve expired session error = %v, want ErrSessionNotFound", err)
	}
}

func TestMigrateAndReadyCheck(t *testing.T) {
	pool := newTestPool(t)
	logger := testLogger()
	migrationFS := productionMigrationFS(t)

	if err := migrateFS(t.Context(), pool, migrationFS, logger); err != nil {
		t.Fatalf("first migrateFS() error = %v", err)
	}
	if err := ReadyCheck(pool)(t.Context()); err != nil {
		t.Fatalf("ReadyCheck() error = %v", err)
	}

	database := stdlib.OpenDBFromPool(pool)
	defer database.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrationFS)
	if err != nil {
		t.Fatalf("create goose provider: %v", err)
	}
	version, err := provider.GetDBVersion(t.Context())
	if err != nil {
		t.Fatalf("get migration version: %v", err)
	}
	if version != 2 {
		t.Errorf("migration version = %d, want 2", version)
	}

	if err := migrateFS(t.Context(), pool, migrationFS, logger); err != nil {
		t.Fatalf("second migrateFS() error = %v", err)
	}
}

func TestMigratePropagatesInvalidMigration(t *testing.T) {
	pool := newTestPool(t)
	badMigrations := fstest.MapFS{
		"00001_invalid.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nTHIS IS NOT SQL;\n")},
	}

	if err := migrateFS(t.Context(), pool, badMigrations, testLogger()); err == nil {
		t.Fatal("migrateFS() error = nil, want invalid migration error")
	}
}

func TestMigrateWaitsForAdvisoryLock(t *testing.T) {
	pool := newTestPool(t)
	lockConnection, err := pgx.ConnectConfig(t.Context(), pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatalf("open lock connection: %v", err)
	}
	defer lockConnection.Close(context.Background())

	if _, err := lockConnection.Exec(t.Context(), "SELECT pg_advisory_lock($1)", migrationAdvisoryLockID); err != nil {
		t.Fatalf("acquire test advisory lock: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	err = migrateFS(ctx, pool, productionMigrationFS(t), testLogger())
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("migrateFS() error = %v, want context deadline exceeded", err)
	}

	if _, err := lockConnection.Exec(t.Context(), "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockID); err != nil {
		t.Fatalf("release test advisory lock: %v", err)
	}
	if err := migrateFS(t.Context(), pool, productionMigrationFS(t), testLogger()); err != nil {
		t.Fatalf("migrateFS() after unlock error = %v", err)
	}
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("LANSURFER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LANSURFER_TEST_DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open test admin pool: %v", err)
	}
	defer adminPool.Close()

	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schemaName := "lansurfer_test_" + hex.EncodeToString(randomBytes)
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupPool, err := pgxpool.New(context.Background(), databaseURL)
		if err != nil {
			t.Errorf("open cleanup pool: %v", err)
			return
		}
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET search_path TO "+identifier+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatalf("open isolated test pool: %v", err)
	}
	if err := pool.Ping(t.Context()); err != nil {
		pool.Close()
		t.Fatalf("ping isolated test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func productionMigrationFS(t *testing.T) fs.FS {
	t.Helper()

	migrationFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("open embedded migrations: %v", err)
	}
	return migrationFS
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
