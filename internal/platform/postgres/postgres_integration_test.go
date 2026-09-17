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
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
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
	promoted, err := users.PromoteToAdmin(t.Context(), account.ID)
	if err != nil || promoted.Role != user.RoleAdmin {
		t.Fatalf("promote user = %#v, %v", promoted, err)
	}
	listedUsers, err := users.List(t.Context())
	if err != nil || len(listedUsers) != 1 || listedUsers[0].ID != account.ID || listedUsers[0].Role != user.RoleAdmin {
		t.Fatalf("list users = %#v, %v", listedUsers, err)
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

func TestGameCatalogPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	games := game.NewService(gamepostgres.NewStore(pool))
	warcraft, err := games.Create(t.Context(), "Warcraft III", "warcraft-3")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	alias, err := games.AddAlias(t.Context(), warcraft.ID, "WC3")
	if err != nil {
		t.Fatalf("add alias: %v", err)
	}
	if _, err := games.AddAlias(t.Context(), warcraft.ID, "wc3"); !errors.Is(err, game.ErrAlreadyExists) {
		t.Fatalf("duplicate alias error = %v, want ErrAlreadyExists", err)
	}
	searchResults, err := games.List(t.Context(), "wC3")
	if err != nil || len(searchResults) != 1 || searchResults[0].ID != warcraft.ID || len(searchResults[0].Aliases) != 1 {
		t.Fatalf("alias search results = %#v, %v", searchResults, err)
	}
	if err := games.SetActive(t.Context(), warcraft.ID, false); err != nil {
		t.Fatalf("deactivate game: %v", err)
	}
	updatedGame, err := games.Update(t.Context(), warcraft.ID, "Warcraft III: Reign of Chaos", "warcraft-3")
	if err != nil || updatedGame.Name != "Warcraft III: Reign of Chaos" {
		t.Fatalf("update game = %#v, %v", updatedGame, err)
	}
	allGames, err := games.List(t.Context(), "")
	if err != nil || len(allGames) != 1 || allGames[0].Active {
		t.Fatalf("games after deactivation = %#v, %v", allGames, err)
	}
	if err := games.RemoveAlias(t.Context(), warcraft.ID, alias.ID); err != nil {
		t.Fatalf("remove alias: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO game_external_ids (game_id, provider, external_id)
		VALUES ($1, 'Steam', '123')
	`, warcraft.ID); err != nil {
		t.Fatalf("insert generic external ID: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO game_external_ids (game_id, provider, external_id)
		VALUES ($1, 'steam', '123')
	`, warcraft.ID); err == nil {
		t.Fatal("duplicate provider/external ID unexpectedly succeeded")
	}
}

func TestGameSubscriptionPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	account, err := users.Register(t.Context(), "Subscribed Player")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	subscriptions := game.NewSubscriptionService(gameStore)
	warcraft, err := catalog.Create(t.Context(), "Warcraft III", "warcraft-3")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if _, err := catalog.AddAlias(t.Context(), warcraft.ID, "WC3"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	quake, err := catalog.Create(t.Context(), "Quake III Arena", "quake-3-arena")
	if err != nil {
		t.Fatalf("create second game: %v", err)
	}

	if err := subscriptions.Follow(t.Context(), account.ID, warcraft.ID); err != nil {
		t.Fatalf("follow game: %v", err)
	}
	if err := subscriptions.Follow(t.Context(), account.ID, warcraft.ID); err != nil {
		t.Fatalf("follow game idempotently: %v", err)
	}
	results, err := subscriptions.Browse(t.Context(), account.ID, "wc3")
	if err != nil || len(results) != 1 || results[0].ID != warcraft.ID || !results[0].Subscribed {
		t.Fatalf("browse subscriptions = %#v, %v", results, err)
	}
	mine, err := subscriptions.MyGames(t.Context(), account.ID)
	if err != nil || len(mine) != 1 || mine[0].ID != warcraft.ID {
		t.Fatalf("my games = %#v, %v", mine, err)
	}
	if err := catalog.SetActive(t.Context(), quake.ID, false); err != nil {
		t.Fatalf("deactivate game: %v", err)
	}
	if err := subscriptions.Follow(t.Context(), account.ID, quake.ID); !errors.Is(err, game.ErrNotFound) {
		t.Fatalf("follow inactive game error = %v, want ErrNotFound", err)
	}
	if err := subscriptions.Unfollow(t.Context(), account.ID, warcraft.ID); err != nil {
		t.Fatalf("unfollow game: %v", err)
	}
	mine, err = subscriptions.MyGames(t.Context(), account.ID)
	if err != nil || len(mine) != 0 {
		t.Fatalf("my games after unfollow = %#v, %v", mine, err)
	}
}

func TestGameRequestWorkflowAndConcurrentCapacity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	creator, err := users.Register(t.Context(), "Creator")
	if err != nil {
		t.Fatalf("register creator: %v", err)
	}
	firstJoiner, err := users.Register(t.Context(), "First Joiner")
	if err != nil {
		t.Fatalf("register first joiner: %v", err)
	}
	secondJoiner, err := users.Register(t.Context(), "Second Joiner")
	if err != nil {
		t.Fatalf("register second joiner: %v", err)
	}

	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	subscriptions := game.NewSubscriptionService(gameStore)
	selectedGame, err := catalog.Create(t.Context(), "Warcraft III", "warcraft-3")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if err := subscriptions.Follow(t.Context(), firstJoiner.ID, selectedGame.ID); err != nil {
		t.Fatalf("follow selected game: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	maximum := int32(2)
	created, err := requests.Create(t.Context(), creator.ID, selectedGame.ID, &maximum, startsAt, "Concurrent capacity test")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}

	start := make(chan struct{})
	type joinResult struct {
		userID string
		err    error
	}
	results := make(chan joinResult, 2)
	var wait sync.WaitGroup
	for _, userID := range []string{firstJoiner.ID, secondJoiner.ID} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- joinResult{userID: userID, err: requests.Join(t.Context(), created.ID, userID)}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded, full := 0, 0
	joinedUserID := ""
	for result := range results {
		switch {
		case result.err == nil:
			succeeded++
			joinedUserID = result.userID
		case errors.Is(result.err, gamerequest.ErrFull):
			full++
		default:
			t.Fatalf("concurrent join error = %v", result.err)
		}
	}
	if succeeded != 1 || full != 1 {
		t.Fatalf("concurrent joins: succeeded = %d, full = %d", succeeded, full)
	}

	readStore := gamerequestquery.NewStore(pool)
	detail, err := readStore.ByID(t.Context(), created.ID, creator.ID)
	if err != nil {
		t.Fatalf("read game request: %v", err)
	}
	if detail.MemberCount != 2 || len(detail.Members) != 2 || !detail.ViewerJoined {
		t.Fatalf("request after joins = %#v", detail)
	}
	activeForFollower, err := readStore.ListActive(t.Context(), firstJoiner.ID, gamerequestquery.Filter{})
	if err != nil || len(activeForFollower) != 1 || !activeForFollower[0].ViewerSubscribed {
		t.Fatalf("request subscription projection = %#v, %v", activeForFollower, err)
	}
	if err := requests.Leave(t.Context(), created.ID, creator.ID); !errors.Is(err, gamerequest.ErrCreatorCannotLeave) {
		t.Fatalf("creator leave error = %v", err)
	}
	if err := requests.Leave(t.Context(), created.ID, joinedUserID); err != nil {
		t.Fatalf("member leave: %v", err)
	}
	detail, err = readStore.ByID(t.Context(), created.ID, creator.ID)
	if err != nil || detail.MemberCount != 1 {
		t.Fatalf("request after leave = %#v, %v", detail, err)
	}
	if err := requests.Close(t.Context(), created.ID, firstJoiner.ID); !errors.Is(err, gamerequest.ErrNotCreator) {
		t.Fatalf("non-creator close error = %v", err)
	}
	if err := requests.Close(t.Context(), created.ID, creator.ID); err != nil {
		t.Fatalf("creator close: %v", err)
	}
	if err := requests.Join(t.Context(), created.ID, secondJoiner.ID); !errors.Is(err, gamerequest.ErrNotOpen) {
		t.Fatalf("join closed request error = %v", err)
	}
	unlimited, err := requests.Create(t.Context(), creator.ID, selectedGame.ID, nil, startsAt, "No limit")
	if err != nil {
		t.Fatalf("create unlimited game request: %v", err)
	}
	if err := requests.Cancel(t.Context(), unlimited.ID, creator.ID); err != nil {
		t.Fatalf("creator cancel: %v", err)
	}
	cancelled, err := readStore.ByID(t.Context(), unlimited.ID, creator.ID)
	if err != nil || cancelled.Status != gamerequest.StatusCancelled {
		t.Fatalf("cancelled request = %#v, %v", cancelled, err)
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
	if version != 6 {
		t.Errorf("migration version = %d, want 6", version)
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

// TestGameRequestDuplicateAndOverlappingMutations covers what rapid clicking
// produces at the persistence layer: the same user issuing the same mutation
// twice at once, and conflicting lifecycle mutations racing each other. Each
// pair must resolve to one winner and one deterministic domain error, never to
// a partially applied state.
func TestGameRequestDuplicateAndOverlappingMutations(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	creator, err := users.Register(t.Context(), "Race Creator")
	if err != nil {
		t.Fatalf("register creator: %v", err)
	}
	joiner, err := users.Register(t.Context(), "Race Joiner")
	if err != nil {
		t.Fatalf("register joiner: %v", err)
	}

	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Supreme Commander", "supreme-commander")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	readStore := gamerequestquery.NewStore(pool)

	// Join → Join by one user, as a double-clicked Join button produces.
	duplicateJoin, err := requests.Create(t.Context(), creator.ID, selectedGame.ID, nil, startsAt, "Duplicate join")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	results := runConcurrently(
		func() error { return requests.Join(t.Context(), duplicateJoin.ID, joiner.ID) },
		func() error { return requests.Join(t.Context(), duplicateJoin.ID, joiner.ID) },
	)
	assertOneWinner(t, "duplicate join", results, gamerequest.ErrAlreadyMember)
	detail, err := readStore.ByID(t.Context(), duplicateJoin.ID, joiner.ID)
	if err != nil {
		t.Fatalf("read game request after duplicate join: %v", err)
	}
	if detail.MemberCount != 2 || !detail.ViewerJoined {
		t.Fatalf("duplicate join left member count = %d, viewer joined = %t, want 2 and true", detail.MemberCount, detail.ViewerJoined)
	}

	// Join → Leave toggling: whichever order the database settles on, the
	// membership must match the outcome the caller was told about.
	toggled := runConcurrently(
		func() error { return requests.Leave(t.Context(), duplicateJoin.ID, joiner.ID) },
		func() error { return requests.Join(t.Context(), duplicateJoin.ID, joiner.ID) },
	)
	leaveErr, joinErr := toggled[0], toggled[1]
	if leaveErr != nil && !errors.Is(leaveErr, gamerequest.ErrNotMember) {
		t.Fatalf("toggled leave error = %v", leaveErr)
	}
	if joinErr != nil && !errors.Is(joinErr, gamerequest.ErrAlreadyMember) {
		t.Fatalf("toggled join error = %v", joinErr)
	}
	detail, err = readStore.ByID(t.Context(), duplicateJoin.ID, joiner.ID)
	if err != nil {
		t.Fatalf("read game request after toggle: %v", err)
	}
	if leaveWon := leaveErr == nil && joinErr != nil; leaveWon && detail.ViewerJoined {
		t.Fatal("leave reported success but the membership survived")
	}
	if joinWon := joinErr == nil && leaveErr != nil; joinWon && !detail.ViewerJoined {
		t.Fatal("join reported success but the membership is missing")
	}

	// Close → Cancel by the creator, as repeated lifecycle clicks produce.
	lifecycle, err := requests.Create(t.Context(), creator.ID, selectedGame.ID, nil, startsAt, "Lifecycle race")
	if err != nil {
		t.Fatalf("create lifecycle game request: %v", err)
	}
	lifecycleResults := runConcurrently(
		func() error { return requests.Close(t.Context(), lifecycle.ID, creator.ID) },
		func() error { return requests.Cancel(t.Context(), lifecycle.ID, creator.ID) },
	)
	assertOneWinner(t, "close against cancel", lifecycleResults, gamerequest.ErrNotOpen)
	wantStatus := gamerequest.StatusClosed
	if lifecycleResults[1] == nil {
		wantStatus = gamerequest.StatusCancelled
	}
	lifecycleDetail, err := readStore.ByID(t.Context(), lifecycle.ID, creator.ID)
	if err != nil {
		t.Fatalf("read lifecycle game request: %v", err)
	}
	if lifecycleDetail.Status != wantStatus {
		t.Fatalf("lifecycle status = %q, want %q", lifecycleDetail.Status, wantStatus)
	}
	if err := requests.Close(t.Context(), lifecycle.ID, creator.ID); !errors.Is(err, gamerequest.ErrNotOpen) {
		t.Fatalf("repeated close error = %v, want ErrNotOpen", err)
	}

	// A member of a request that is no longer open cannot be added by a late
	// click that was already in flight when the creator closed it.
	if err := requests.Join(t.Context(), lifecycle.ID, joiner.ID); !errors.Is(err, gamerequest.ErrNotOpen) {
		t.Fatalf("join after close error = %v, want ErrNotOpen", err)
	}
}

// runConcurrently starts every mutation at the same moment and returns their
// errors positionally.
func runConcurrently(mutations ...func() error) []error {
	results := make([]error, len(mutations))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index, mutation := range mutations {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index] = mutation()
		}()
	}
	close(start)
	wait.Wait()
	return results
}

func assertOneWinner(t *testing.T, name string, results []error, wantConflict error) {
	t.Helper()

	succeeded, conflicted := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, wantConflict):
			conflicted++
		default:
			t.Fatalf("%s: unexpected error = %v", name, err)
		}
	}
	if succeeded != 1 || conflicted != len(results)-1 {
		t.Fatalf("%s: succeeded = %d, conflicted = %d, want 1 and %d", name, succeeded, conflicted, len(results)-1)
	}
}

// The forward migration to the single-event core keeps prototype Game Requests
// intact: same id, creator, members, status, and timestamps, with their history
// reconstructed as durable activity. Legacy Event rows stay in the database.
func TestForwardMigrationPreservesPrototypeGameRequests(t *testing.T) {
	pool := newTestPool(t)
	logger := testLogger()
	migrationFS := productionMigrationFS(t)

	database := stdlib.OpenDBFromPool(pool)
	defer database.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrationFS, goose.WithSlog(logger))
	if err != nil {
		t.Fatalf("create goose provider: %v", err)
	}
	// Version 5 is the prototype schema, where a Game Request still belongs to
	// an Event.
	if _, err := provider.UpTo(t.Context(), 5); err != nil {
		t.Fatalf("migrate to the prototype schema: %v", err)
	}

	var eventID, gameID, creatorID, joinerID, openID, closedID string
	var createdAt, joinedAt, updatedAt time.Time
	if err := pool.QueryRow(t.Context(), `
		WITH inserted_event AS (
			INSERT INTO events (name, starts_at, ends_at, is_current)
			VALUES ('Prototype LAN', now() - interval '1 day', now() + interval '1 day', true)
			RETURNING id
		), inserted_game AS (
			INSERT INTO games (name, slug) VALUES ('Quake', 'quake') RETURNING id
		), inserted_creator AS (
			INSERT INTO users (display_name) VALUES ('Prototype Host') RETURNING id
		), inserted_joiner AS (
			INSERT INTO users (display_name) VALUES ('Prototype Guest') RETURNING id
		), open_request AS (
			INSERT INTO game_requests (event_id, game_id, creator_id, max_players, starts_at, description, created_at, updated_at)
			SELECT inserted_event.id, inserted_game.id, inserted_creator.id, 4,
			       now() + interval '2 hours', 'Prototype round',
			       now() - interval '3 hours', now() - interval '3 hours'
			FROM inserted_event, inserted_game, inserted_creator
			RETURNING id, created_at
		), closed_request AS (
			INSERT INTO game_requests (event_id, game_id, creator_id, starts_at, status, created_at, updated_at)
			SELECT inserted_event.id, inserted_game.id, inserted_creator.id,
			       now() - interval '5 hours', 'closed',
			       now() - interval '9 hours', now() - interval '6 hours'
			FROM inserted_event, inserted_game, inserted_creator
			RETURNING id, updated_at
		), creator_membership AS (
			INSERT INTO game_request_members (game_request_id, user_id, joined_at)
			SELECT open_request.id, inserted_creator.id, open_request.created_at
			FROM open_request, inserted_creator
			RETURNING joined_at
		), joiner_membership AS (
			INSERT INTO game_request_members (game_request_id, user_id, joined_at)
			SELECT open_request.id, inserted_joiner.id, now() - interval '2 hours'
			FROM open_request, inserted_joiner
			RETURNING joined_at
		), closed_membership AS (
			INSERT INTO game_request_members (game_request_id, user_id, joined_at)
			SELECT closed_request.id, inserted_creator.id, now() - interval '9 hours'
			FROM closed_request, inserted_creator
			RETURNING joined_at
		)
		SELECT inserted_event.id::text, inserted_game.id::text, inserted_creator.id::text,
		       inserted_joiner.id::text, open_request.id::text, closed_request.id::text,
		       open_request.created_at, joiner_membership.joined_at, closed_request.updated_at
		FROM inserted_event, inserted_game, inserted_creator, inserted_joiner,
		     open_request, closed_request, creator_membership, joiner_membership, closed_membership
	`).Scan(&eventID, &gameID, &creatorID, &joinerID, &openID, &closedID, &createdAt, &joinedAt, &updatedAt); err != nil {
		t.Fatalf("seed prototype data: %v", err)
	}

	if err := migrateFS(t.Context(), pool, migrationFS, logger); err != nil {
		t.Fatalf("migrate to the single-event core: %v", err)
	}

	readStore := gamerequestquery.NewStore(pool)
	preserved, err := readStore.ByID(t.Context(), openID, joinerID)
	if err != nil {
		t.Fatalf("read preserved game request: %v", err)
	}
	if preserved.CreatorID != creatorID || preserved.MemberCount != 2 || !preserved.ViewerJoined ||
		preserved.Status != gamerequest.StatusOpen || preserved.Description != "Prototype round" ||
		!preserved.CreatedAt.Equal(createdAt) {
		t.Fatalf("preserved request = %#v", preserved)
	}
	if got := activityKinds(preserved.Activity); !slices.Equal(got, []gamerequest.ActivityKind{gamerequest.ActivityJoined, gamerequest.ActivityCreated}) {
		t.Fatalf("backfilled activity = %v", got)
	}
	if !preserved.Activity[0].OccurredAt.Equal(joinedAt) || preserved.Activity[0].ActorID != joinerID {
		t.Fatalf("backfilled join entry = %#v", preserved.Activity[0])
	}

	closed, err := readStore.ByID(t.Context(), closedID, creatorID)
	if err != nil {
		t.Fatalf("read preserved closed request: %v", err)
	}
	if closed.Status != gamerequest.StatusClosed {
		t.Fatalf("closed request status = %q", closed.Status)
	}
	if len(closed.Activity) == 0 || closed.Activity[0].Kind != gamerequest.ActivityClosed ||
		!closed.Activity[0].OccurredAt.Equal(updatedAt) {
		t.Fatalf("closed request activity = %#v", closed.Activity)
	}

	// Legacy Event rows are documented as kept, not silently discarded.
	var legacyEvents int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM events WHERE id = $1", eventID).Scan(&legacyEvents); err != nil {
		t.Fatalf("count legacy events: %v", err)
	}
	if legacyEvents != 1 {
		t.Fatalf("legacy event rows = %d, want 1", legacyEvents)
	}
	// The request itself no longer carries an Event association.
	var columns int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'game_requests' AND column_name = 'event_id'
	`).Scan(&columns); err != nil {
		t.Fatalf("inspect game_requests columns: %v", err)
	}
	if columns != 0 {
		t.Fatal("game_requests still carries event_id after the single-event migration")
	}
}

// Durable activity is written in the same transaction as the change it
// describes, which is what makes it usable as Track E's notification intent: a
// rejected mutation leaves neither the change nor an entry, and a committed one
// survives a restart because it is only ever read back from PostgreSQL.
func TestGameRequestActivityDiscoveryAndTrending(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Activity Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Activity Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}
	outsider, err := users.Register(t.Context(), "Activity Outsider")
	if err != nil {
		t.Fatalf("register outsider: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	subscriptions := game.NewSubscriptionService(gameStore)
	warcraft, err := catalog.Create(t.Context(), "Warcraft III", "warcraft-3")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	if _, err := catalog.AddAlias(t.Context(), warcraft.ID, "WC3"); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	quake, err := catalog.Create(t.Context(), "Quake", "quake")
	if err != nil {
		t.Fatalf("create second game: %v", err)
	}
	if err := subscriptions.Follow(t.Context(), guest.ID, warcraft.ID); err != nil {
		t.Fatalf("follow game: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	readStore := gamerequestquery.NewStore(pool)
	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	limit := int32(2)
	tracked, err := requests.Create(t.Context(), host.ID, warcraft.ID, &limit, startsAt, "Tracked round")
	if err != nil {
		t.Fatalf("create tracked request: %v", err)
	}
	if err := requests.Join(t.Context(), tracked.ID, guest.ID); err != nil {
		t.Fatalf("join tracked request: %v", err)
	}
	// Rejected mutations: a duplicate join, a join into a full request, and a
	// close by someone who is not the creator.
	if err := requests.Join(t.Context(), tracked.ID, guest.ID); !errors.Is(err, gamerequest.ErrAlreadyMember) {
		t.Fatalf("duplicate join error = %v", err)
	}
	if err := requests.Join(t.Context(), tracked.ID, outsider.ID); !errors.Is(err, gamerequest.ErrFull) {
		t.Fatalf("join full request error = %v", err)
	}
	if err := requests.Close(t.Context(), tracked.ID, guest.ID); !errors.Is(err, gamerequest.ErrNotCreator) {
		t.Fatalf("non-creator close error = %v", err)
	}
	if err := requests.Leave(t.Context(), tracked.ID, guest.ID); err != nil {
		t.Fatalf("leave tracked request: %v", err)
	}
	if err := requests.Close(t.Context(), tracked.ID, host.ID); err != nil {
		t.Fatalf("close tracked request: %v", err)
	}

	detail, err := readStore.ByID(t.Context(), tracked.ID, host.ID)
	if err != nil {
		t.Fatalf("read tracked request: %v", err)
	}
	wantKinds := []gamerequest.ActivityKind{
		gamerequest.ActivityClosed, gamerequest.ActivityLeft,
		gamerequest.ActivityJoined, gamerequest.ActivityCreated,
	}
	if got := activityKinds(detail.Activity); !slices.Equal(got, wantKinds) {
		t.Fatalf("activity = %v, want %v (rejected mutations must not record anything)", got, wantKinds)
	}
	// Leaving removed the membership but not the history of the round.
	if detail.MemberCount != 1 {
		t.Fatalf("member count after leave = %d, want 1", detail.MemberCount)
	}
	if detail.Activity[1].ActorName != "Activity Guest" || detail.Activity[1].ActorID != guest.ID {
		t.Fatalf("leave entry = %#v", detail.Activity[1])
	}

	// A rolled-back write leaves neither the request nor an activity entry.
	before := countActivity(t, pool)
	if _, err := requests.Create(t.Context(), host.ID, "not-a-uuid", nil, startsAt, ""); err == nil {
		t.Fatal("create with an unknown game unexpectedly succeeded")
	}
	if after := countActivity(t, pool); after != before {
		t.Fatalf("failed create recorded activity: %d entries, want %d", after, before)
	}

	// Discovery filters.
	open, err := requests.Create(t.Context(), host.ID, warcraft.ID, nil, startsAt, "Open round")
	if err != nil {
		t.Fatalf("create open request: %v", err)
	}
	full, err := requests.Create(t.Context(), host.ID, quake.ID, ptr(int32(1)), startsAt, "Full round")
	if err != nil {
		t.Fatalf("create full request: %v", err)
	}

	byAlias, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Search: "wC3"})
	if err != nil || len(byAlias) != 1 || byAlias[0].ID != open.ID {
		t.Fatalf("alias search = %#v, %v", byAlias, err)
	}
	followed, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{FollowedOnly: true})
	if err != nil || len(followed) != 1 || followed[0].ID != open.ID {
		t.Fatalf("followed filter = %#v, %v", followed, err)
	}
	withPlaces, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{OpenPlacesOnly: true})
	if err != nil {
		t.Fatalf("open places filter: %v", err)
	}
	if slices.ContainsFunc(withPlaces, func(request gamerequestquery.Request) bool { return request.ID == full.ID }) {
		t.Fatalf("open places filter kept a full request: %#v", withPlaces)
	}
	if !slices.ContainsFunc(withPlaces, func(request gamerequestquery.Request) bool { return request.ID == open.ID }) {
		t.Fatalf("open places filter dropped a request with free places: %#v", withPlaces)
	}
	if _, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Search: strings.Repeat("x", 121)}); !errors.Is(err, gamerequestquery.ErrInvalidSearch) {
		t.Fatalf("overlong search error = %v", err)
	}

	// Trending ranks from recorded activity inside the stated window; entries
	// older than the window drop out instead of the window widening.
	trending, err := readStore.Trending(t.Context())
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(trending) == 0 || trending[0].GameName != "Warcraft III" {
		t.Fatalf("trending = %#v", trending)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE game_request_activity
		SET occurred_at = now() - $1::interval
	`, gamerequestquery.TrendingWindow+time.Hour); err != nil {
		t.Fatalf("age activity beyond the window: %v", err)
	}
	quiet, err := readStore.Trending(t.Context())
	if err != nil {
		t.Fatalf("trending after ageing: %v", err)
	}
	if len(quiet) != 0 {
		t.Fatalf("trending outside the window = %#v, want an empty ranking", quiet)
	}
}

func activityKinds(entries []gamerequestquery.Activity) []gamerequest.ActivityKind {
	kinds := make([]gamerequest.ActivityKind, 0, len(entries))
	for _, entry := range entries {
		kinds = append(kinds, entry.Kind)
	}
	return kinds
}

func countActivity(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var total int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM game_request_activity").Scan(&total); err != nil {
		t.Fatalf("count game request activity: %v", err)
	}
	return total
}

func ptr[T any](value T) *T { return &value }
