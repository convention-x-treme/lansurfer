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
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/event"
	eventpostgres "github.com/convention-x-treme/lansurfer/internal/event/postgres"
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

func TestEventAndGameCatalogPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	events := event.NewService(eventpostgres.NewStore(pool))
	startsAt := time.Date(2027, time.May, 1, 10, 0, 0, 0, time.UTC)
	first, err := events.Create(t.Context(), "CXT 2027", startsAt, startsAt.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("create first event: %v", err)
	}
	second, err := events.Create(t.Context(), "CXT 2028", startsAt.AddDate(1, 0, 0), startsAt.AddDate(1, 0, 2))
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}
	if err := events.SetCurrent(t.Context(), first.ID); err != nil {
		t.Fatalf("set first current: %v", err)
	}
	if err := events.SetCurrent(t.Context(), second.ID); err != nil {
		t.Fatalf("set second current: %v", err)
	}
	updatedFirst, err := events.Update(t.Context(), first.ID, "CXT 2027 Extended", startsAt, startsAt.Add(72*time.Hour))
	if err != nil || updatedFirst.Name != "CXT 2027 Extended" {
		t.Fatalf("update event = %#v, %v", updatedFirst, err)
	}
	listedEvents, err := events.List(t.Context())
	if err != nil || len(listedEvents) != 2 || !listedEvents[0].IsCurrent || listedEvents[0].ID != second.ID || listedEvents[1].IsCurrent {
		t.Fatalf("list events = %#v, %v", listedEvents, err)
	}

	games := game.NewService(gamepostgres.NewStore(pool))
	if _, err := games.Create(t.Context(), "Shared Name", "shared-a"); err != nil {
		t.Fatalf("create first game with shared name: %v", err)
	}
	if _, err := games.Create(t.Context(), "Shared Name", "shared-b"); err != nil {
		t.Fatalf("create second game with shared name: %v", err)
	}
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
	if err != nil || len(allGames) != 3 || allGames[2].Active {
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

func TestIGDBGenerationPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	store := gamepostgres.NewStore(pool)
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	batch := []game.ProviderGame{{
		Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"},
		Name:      "Shared Name",
		Slug:      "shared",
		GameType:  "Main Game",
	}}
	if err := store.StoreProviderGames(t.Context(), generationID, batch); err != nil {
		t.Fatalf("StoreProviderGames: %v", err)
	}
	before, err := store.Status(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Status before activation: %v", err)
	}
	if before.State != game.ProviderNotSynced {
		t.Fatalf("before = %#v", before)
	}
	if err := store.ActivateGeneration(t.Context(), generationID, 1); err != nil {
		t.Fatalf("ActivateGeneration: %v", err)
	}
	after, err := store.Status(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Status after activation: %v", err)
	}
	if after.State != game.ProviderCurrent || after.RecordCount != 1 {
		t.Fatalf("after = %#v", after)
	}
}

func TestIGDBGenerationRejectsWritesAfterActivation(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	store := gamepostgres.NewStore(pool)
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{testProviderGame("100")}); err != nil {
		t.Fatalf("StoreProviderGames before activation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), generationID, 1); err != nil {
		t.Fatalf("ActivateGeneration: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{testProviderGame("101")}); err == nil {
		t.Fatal("StoreProviderGames after activation unexpectedly succeeded")
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM igdb_game_index WHERE generation_id=$1`, generationID).Scan(&count); err != nil {
		t.Fatalf("count active generation records: %v", err)
	}
	if count != 1 {
		t.Fatalf("active generation record count = %d, want 1", count)
	}
}

func TestIGDBGenerationActivationWaitsForAcceptedWrite(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		CREATE FUNCTION block_igdb_test_insert() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(730031);
			RETURN NEW;
		END $$;
		CREATE TRIGGER block_igdb_test_insert
		BEFORE INSERT ON igdb_game_index
		FOR EACH ROW EXECUTE FUNCTION block_igdb_test_insert();
	`); err != nil {
		t.Fatalf("install blocking insert trigger: %v", err)
	}

	store := gamepostgres.NewStore(pool)
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	blocker, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin advisory-lock transaction: %v", err)
	}
	blockerOpen := true
	defer func() {
		if blockerOpen {
			_ = blocker.Rollback(context.Background())
		}
	}()
	if _, err := blocker.Exec(t.Context(), `SELECT pg_advisory_xact_lock(730031)`); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}

	storeDone := make(chan error, 1)
	go func() {
		storeDone <- store.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{testProviderGame("100")})
	}()
	waitForAdvisoryWaiter(t, pool)

	activationDone := make(chan error, 1)
	go func() {
		activationDone <- store.ActivateGeneration(t.Context(), generationID, 1)
	}()
	var activationErr error
	activationReturnedEarly := false
	select {
	case activationErr = <-activationDone:
		activationReturnedEarly = true
	case <-time.After(200 * time.Millisecond):
	}
	if err := blocker.Commit(t.Context()); err != nil {
		t.Fatalf("release advisory lock: %v", err)
	}
	blockerOpen = false
	if err := <-storeDone; err != nil {
		t.Fatalf("StoreProviderGames: %v", err)
	}
	if !activationReturnedEarly {
		activationErr = <-activationDone
	}
	if activationErr != nil {
		t.Fatalf("ActivateGeneration: %v", activationErr)
	}
	if activationReturnedEarly {
		t.Fatal("ActivateGeneration returned while an accepted staged write was still in flight")
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM igdb_game_index WHERE generation_id=$1`, generationID).Scan(&count); err != nil {
		t.Fatalf("count active generation records: %v", err)
	}
	if count != 1 {
		t.Fatalf("active generation record count = %d, want 1", count)
	}
}

func TestIGDBActivationPreservesNewerSyncingGenerationAndCleansRetiredGeneration(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := gamepostgres.NewStore(pool)
	firstID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin first generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), firstID, []game.ProviderGame{testProviderGame("100")}); err != nil {
		t.Fatalf("store first generation: %v", err)
	}
	secondID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin second generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), firstID, 1); err != nil {
		t.Fatalf("activate first generation: %v", err)
	}
	var secondStatus string
	if err := pool.QueryRow(t.Context(), `SELECT status FROM igdb_sync_generations WHERE id=$1`, secondID).Scan(&secondStatus); err != nil {
		t.Fatalf("load newer generation after first activation: %v", err)
	}
	if secondStatus != "syncing" {
		t.Fatalf("newer generation status = %q, want syncing", secondStatus)
	}
	if err := store.StoreProviderGames(t.Context(), secondID, []game.ProviderGame{testProviderGame("200")}); err != nil {
		t.Fatalf("store newer generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), secondID, 1); err != nil {
		t.Fatalf("activate second generation: %v", err)
	}
	var firstExists, secondExists bool
	if err := pool.QueryRow(t.Context(), `
		SELECT EXISTS(SELECT 1 FROM igdb_sync_generations WHERE id=$1),
		       EXISTS(SELECT 1 FROM igdb_sync_generations WHERE id=$2)
	`, firstID, secondID).Scan(&firstExists, &secondExists); err != nil {
		t.Fatalf("load generation cleanup state: %v", err)
	}
	if firstExists || !secondExists {
		t.Fatalf("generation existence after replacement = first:%t second:%t", firstExists, secondExists)
	}
}

func TestIGDBLatestAttemptOutcomeRemainsCorrelated(t *testing.T) {
	t.Run("older failure does not replace newer in-flight attempt", func(t *testing.T) {
		pool := newTestPool(t)
		if err := Migrate(t.Context(), pool, testLogger()); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
		store := gamepostgres.NewStore(pool)
		olderID, err := store.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin older generation: %v", err)
		}
		newerID, err := store.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin newer generation: %v", err)
		}
		if err := store.FailGeneration(t.Context(), olderID, "older failed"); err != nil {
			t.Fatalf("fail older generation: %v", err)
		}
		status, err := store.Status(t.Context(), time.Now().UTC())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if status.State != game.ProviderNotSynced || status.LastError != "" {
			t.Fatalf("status after older failure = %#v", status)
		}
		var latestID string
		if err := pool.QueryRow(t.Context(), `SELECT latest_attempt_generation_id::text FROM igdb_provider_state`).Scan(&latestID); err != nil {
			t.Fatalf("load latest attempt: %v", err)
		}
		if latestID != newerID {
			t.Fatalf("latest attempt ID = %q, want %q", latestID, newerID)
		}
	})

	t.Run("older success does not erase newer failure", func(t *testing.T) {
		pool := newTestPool(t)
		if err := Migrate(t.Context(), pool, testLogger()); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
		store := gamepostgres.NewStore(pool)
		olderID, err := store.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin older generation: %v", err)
		}
		if err := store.StoreProviderGames(t.Context(), olderID, []game.ProviderGame{testProviderGame("100")}); err != nil {
			t.Fatalf("store older generation: %v", err)
		}
		newerID, err := store.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin newer generation: %v", err)
		}
		if err := store.FailGeneration(t.Context(), newerID, "newest failed"); err != nil {
			t.Fatalf("fail newer generation: %v", err)
		}
		if err := store.ActivateGeneration(t.Context(), olderID, 1); err != nil {
			t.Fatalf("activate older generation: %v", err)
		}
		status, err := store.Status(t.Context(), time.Now().UTC())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if status.State != game.ProviderFailed || status.LastError != "newest failed" || status.RecordCount != 1 {
			t.Fatalf("status after older success = %#v", status)
		}
	})
}

func TestIGDBFailurePreservesActiveGenerationAndSanitizesError(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := gamepostgres.NewStore(pool)
	activeID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin active generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), activeID, []game.ProviderGame{testProviderGame("100")}); err != nil {
		t.Fatalf("store active generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), activeID, 1); err != nil {
		t.Fatalf("activate first generation: %v", err)
	}
	failedID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin failed generation: %v", err)
	}
	failure := "line one\nline two\r\n" + strings.Repeat("界", 600)
	if err := store.FailGeneration(t.Context(), failedID, failure); err != nil {
		t.Fatalf("FailGeneration: %v", err)
	}
	status, err := store.Status(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != game.ProviderFailed || status.RecordCount != 1 || status.LastError == "" {
		t.Fatalf("status after failed refresh = %#v", status)
	}
	if strings.ContainsAny(status.LastError, "\r\n") || len([]rune(status.LastError)) != 500 || !strings.HasPrefix(status.LastError, "line one line two ") {
		t.Fatalf("sanitized error length=%d value=%q", len([]rune(status.LastError)), status.LastError)
	}
	var gotActiveID, generationError string
	if err := pool.QueryRow(t.Context(), `
		SELECT state.active_generation_id::text, generation.error
		FROM igdb_provider_state state
		JOIN igdb_sync_generations generation ON generation.id=$1
	`, failedID).Scan(&gotActiveID, &generationError); err != nil {
		t.Fatalf("load active and failed generation: %v", err)
	}
	if gotActiveID != activeID || generationError != status.LastError {
		t.Fatalf("failure state = active:%q error:%q", gotActiveID, generationError)
	}
}

func TestIGDBStatusStaleBoundary(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := gamepostgres.NewStore(pool)
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), generationID, 0); err != nil {
		t.Fatalf("ActivateGeneration: %v", err)
	}
	lastSuccess := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(t.Context(), `UPDATE igdb_provider_state SET last_success_at=$1`, lastSuccess); err != nil {
		t.Fatalf("set last success: %v", err)
	}
	boundary, err := store.Status(t.Context(), lastSuccess.Add(36*time.Hour))
	if err != nil {
		t.Fatalf("Status at boundary: %v", err)
	}
	if boundary.State != game.ProviderCurrent {
		t.Fatalf("status at stale boundary = %#v", boundary)
	}
	stale, err := store.Status(t.Context(), lastSuccess.Add(36*time.Hour+time.Microsecond))
	if err != nil {
		t.Fatalf("Status after boundary: %v", err)
	}
	if stale.State != game.ProviderStale {
		t.Fatalf("status after stale boundary = %#v", stale)
	}
}

func TestIGDBActivationArchivesAndRestoresExternalIdentity(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	games := game.NewService(gamepostgres.NewStore(pool))
	localGame, err := games.Create(t.Context(), "Materialized", "materialized")
	if err != nil {
		t.Fatalf("create local game: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO game_external_ids(game_id, provider, external_id) VALUES($1, 'igdb', '100')`, localGame.ID); err != nil {
		t.Fatalf("insert IGDB external identity: %v", err)
	}
	store := gamepostgres.NewStore(pool)
	absentID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin absent generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), absentID, 0); err != nil {
		t.Fatalf("activate absent generation: %v", err)
	}
	var archived bool
	if err := pool.QueryRow(t.Context(), `SELECT archived_at IS NOT NULL FROM game_external_ids WHERE external_id='100'`).Scan(&archived); err != nil {
		t.Fatalf("load archived identity: %v", err)
	}
	if !archived {
		t.Fatal("missing IGDB identity was not archived")
	}
	presentID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin present generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), presentID, []game.ProviderGame{testProviderGame("100")}); err != nil {
		t.Fatalf("store present generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), presentID, 1); err != nil {
		t.Fatalf("activate present generation: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT archived_at IS NOT NULL FROM game_external_ids WHERE external_id='100'`).Scan(&archived); err != nil {
		t.Fatalf("load restored identity: %v", err)
	}
	if archived {
		t.Fatal("returned IGDB identity remained archived")
	}
}

func TestDiscoverySearchPostgreSQL(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	viewer, err := users.Register(t.Context(), "Discovery Viewer")
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}
	other, err := users.Register(t.Context(), "Other Player")
	if err != nil {
		t.Fatalf("register other player: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, nil, gameStore)

	t.Run("empty search never touches the discovery repository", func(t *testing.T) {
		local, err := catalog.Create(t.Context(), "Local Only Game", "local-only-game")
		if err != nil {
			t.Fatalf("create local game: %v", err)
		}
		got, err := discovery.Browse(t.Context(), viewer.ID, "")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		found := false
		for _, item := range got {
			if item.LocalGameID == local.ID {
				found = true
			}
			if (item.Reference != game.ProviderReference{}) {
				t.Fatalf("empty search unexpectedly returned a provider reference: %#v", item)
			}
		}
		if !found {
			t.Fatalf("empty search browse = %#v, want local game present", got)
		}
	})

	t.Run("non-empty search still finds local games without an active IGDB generation", func(t *testing.T) {
		local, err := catalog.Create(t.Context(), "Never Synced Game", "never-synced-game")
		if err != nil {
			t.Fatalf("create local game: %v", err)
		}
		got, err := discovery.Browse(t.Context(), viewer.ID, "never synced")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(got) != 1 || got[0].LocalGameID != local.ID || !got[0].Materialized {
			t.Fatalf("Browse = %#v", got)
		}
	})

	t.Run("active generation only, hidden types excluded", func(t *testing.T) {
		activeID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin active generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), activeID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9001"}, Name: "Quest Runner", Slug: "quest-runner", GameType: "Main Game"},
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9002"}, Name: "Quest Runner DLC", Slug: "quest-runner-dlc", GameType: "DLC Addon"},
		}); err != nil {
			t.Fatalf("store active generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), activeID, 2); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		// A newer, still-syncing generation must be invisible to search.
		syncingID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin syncing generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), syncingID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9003"}, Name: "Quest Runner Beta", Slug: "quest-runner-beta", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store syncing generation games: %v", err)
		}

		got, err := discovery.Browse(t.Context(), viewer.ID, "quest runner")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(got) != 1 || got[0].Name != "Quest Runner" || got[0].Materialized {
			t.Fatalf("Browse = %#v", got)
		}
	})

	t.Run("materialized provider row returns exactly once as the local Game", func(t *testing.T) {
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9101"}, Name: "Provider Name", Slug: "provider-name", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, 1); err != nil {
			t.Fatalf("activate generation: %v", err)
		}
		localGame, err := catalog.Create(t.Context(), "Authoritative Local Name", "authoritative-local-name")
		if err != nil {
			t.Fatalf("create local game: %v", err)
		}
		if _, err := pool.Exec(t.Context(), `INSERT INTO game_external_ids(game_id, provider, external_id) VALUES($1, 'igdb', '9101')`, localGame.ID); err != nil {
			t.Fatalf("insert external identity: %v", err)
		}
		subscriptions := game.NewSubscriptionService(gameStore)
		if err := subscriptions.Follow(t.Context(), viewer.ID, localGame.ID); err != nil {
			t.Fatalf("follow local game: %v", err)
		}

		viewerResult, err := discovery.Browse(t.Context(), viewer.ID, "authoritative")
		if err != nil {
			t.Fatalf("Browse (viewer): %v", err)
		}
		if len(viewerResult) != 1 || !viewerResult[0].Materialized || viewerResult[0].LocalGameID != localGame.ID ||
			viewerResult[0].Name != "Authoritative Local Name" || !viewerResult[0].Subscribed {
			t.Fatalf("Browse (viewer) = %#v", viewerResult)
		}

		otherResult, err := discovery.Browse(t.Context(), other.ID, "authoritative")
		if err != nil {
			t.Fatalf("Browse (other): %v", err)
		}
		if len(otherResult) != 1 || otherResult[0].Subscribed {
			t.Fatalf("Browse (other) = %#v", otherResult)
		}

		// Search by the provider's own (stale) search_text, not the local
		// name/alias, so the provider_matches CTE actually matches this row
		// and the local_rows NOT EXISTS dedup against provider_rows is the
		// thing keeping the result count at one, not the absence of a
		// provider-side match.
		providerTextResult, err := discovery.Browse(t.Context(), viewer.ID, "provider name")
		if err != nil {
			t.Fatalf("Browse (provider search text): %v", err)
		}
		if len(providerTextResult) != 1 || !providerTextResult[0].Materialized ||
			providerTextResult[0].LocalGameID != localGame.ID || providerTextResult[0].Name != "Authoritative Local Name" {
			t.Fatalf("Browse (provider search text) = %#v", providerTextResult)
		}
	})

	t.Run("materialized row survives a later hidden Game Type drift", func(t *testing.T) {
		firstGenerationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin first generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), firstGenerationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9401"}, Name: "Vintage Bundle Master", Slug: "vintage-bundle-master", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store first generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), firstGenerationID, 1); err != nil {
			t.Fatalf("activate first generation: %v", err)
		}
		localGame, err := catalog.Create(t.Context(), "Renamed Local Edition", "renamed-local-edition")
		if err != nil {
			t.Fatalf("create local game: %v", err)
		}
		if _, err := pool.Exec(t.Context(), `INSERT INTO game_external_ids(game_id, provider, external_id) VALUES($1, 'igdb', '9401')`, localGame.ID); err != nil {
			t.Fatalf("insert external identity: %v", err)
		}

		// A later refresh reclassifies the same external ID under a hidden
		// Game Type (spec: provider metadata may drift post-materialization
		// without renaming/relabeling the authoritative local Game).
		secondGenerationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin second generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), secondGenerationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9401"}, Name: "Vintage Bundle Master", Slug: "vintage-bundle-master", GameType: "Bundle"},
		}); err != nil {
			t.Fatalf("store second generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), secondGenerationID, 1); err != nil {
			t.Fatalf("activate second generation: %v", err)
		}

		// Search only matches the provider's stale search_text, never the
		// current local name/alias, so the materialized-row exemption from
		// type filtering is what has to surface it.
		got, err := discovery.Browse(t.Context(), viewer.ID, "vintage bundle master")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(got) != 1 || !got[0].Materialized || got[0].LocalGameID != localGame.ID ||
			got[0].Name != "Renamed Local Edition" || got[0].GameType != "Bundle" {
			t.Fatalf("Browse = %#v", got)
		}
	})

	t.Run("provider rows are capped at 50", func(t *testing.T) {
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		batch := make([]game.ProviderGame, 0, 60)
		for i := range 60 {
			externalID := strconv.Itoa(9200 + i)
			batch = append(batch, game.ProviderGame{
				Reference: game.ProviderReference{Provider: "igdb", ExternalID: externalID},
				Name:      "Capped Result " + externalID,
				Slug:      "capped-result-" + externalID,
				GameType:  "Main Game",
			})
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, batch); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, int64(len(batch))); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		got, err := discovery.Browse(t.Context(), viewer.ID, "capped result")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(got) != 50 {
			t.Fatalf("len(got) = %d, want 50", len(got))
		}
	})
}

func TestIGDBMaterializationPostgreSQL(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore)

	// Two distinct provider records deliberately share one display name AND
	// one provider slug: materialization must never merge by name, and the
	// deterministic "-igdb-<external ID>" suffix must break the slug tie.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:             "Shared Display Name",
			Slug:             "shared-display-name",
			GameType:         "Main Game",
			AlternativeNames: []string{"Shared Alt", "  shared alt  ", "", strings.Repeat("x", 121)},
		},
		{
			Reference: game.ProviderReference{Provider: "igdb", ExternalID: "101"},
			Name:      "Shared Display Name",
			Slug:      "shared-display-name",
			GameType:  "Main Game",
		},
	})

	first, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("Resolve first: %v", err)
	}
	again, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	second, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "101"})
	if err != nil {
		t.Fatalf("Resolve second: %v", err)
	}
	if first.ID != again.ID {
		t.Fatalf("same ID produced %s/%s", first.ID, again.ID)
	}
	if first.ID == second.ID || first.Slug == second.Slug {
		t.Fatalf("distinct provider games collapsed: %#v %#v", first, second)
	}
	if first.Name != "Shared Display Name" || second.Name != "Shared Display Name" {
		t.Fatalf("local names = %q/%q, want the provider display name on both", first.Name, second.Name)
	}
	if first.Slug != "shared-display-name" {
		t.Fatalf("first.Slug = %q, want the unused provider slug", first.Slug)
	}
	if second.Slug != "shared-display-name-igdb-101" {
		t.Fatalf("second.Slug = %q, want the deterministic IGDB suffix", second.Slug)
	}

	var gameCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM games WHERE name='Shared Display Name'`).Scan(&gameCount); err != nil {
		t.Fatalf("count materialized games: %v", err)
	}
	if gameCount != 2 {
		t.Fatalf("games with the shared name = %d, want 2", gameCount)
	}

	t.Run("aliases copy only provider names that satisfy local validation", func(t *testing.T) {
		rows, err := pool.Query(t.Context(), `SELECT alias FROM game_aliases WHERE game_id=$1 ORDER BY alias`, first.ID)
		if err != nil {
			t.Fatalf("load aliases: %v", err)
		}
		defer rows.Close()
		aliases := []string{}
		for rows.Next() {
			var alias string
			if err := rows.Scan(&alias); err != nil {
				t.Fatalf("scan alias: %v", err)
			}
			aliases = append(aliases, alias)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate aliases: %v", err)
		}
		if len(aliases) != 1 || aliases[0] != "Shared Alt" {
			t.Fatalf("aliases = %#v, want only the single valid alternative name", aliases)
		}
	})

	t.Run("a zero-padded external ID resolves the existing game instead of duplicating it", func(t *testing.T) {
		padded, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "0100"})
		if err != nil {
			t.Fatalf("Resolve padded: %v", err)
		}
		if padded.ID != first.ID || padded.Slug != first.Slug {
			t.Fatalf("padded = %#v, want the existing local game %#v", padded, first)
		}
		// The provider index stores the canonical igdb_id::text form, so a
		// stray '0100' mapping would both duplicate the Game and be archived
		// by every later sync.
		var mappings int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM game_external_ids
			WHERE lower(btrim(provider))='igdb' AND external_id IN ('100', '0100')
		`).Scan(&mappings); err != nil {
			t.Fatalf("count mappings for provider record 100: %v", err)
		}
		if mappings != 1 {
			t.Fatalf("mappings for provider record 100 = %d, want 1", mappings)
		}
		var canonical string
		if err := pool.QueryRow(t.Context(), `SELECT external_id FROM game_external_ids WHERE game_id=$1`, first.ID).Scan(&canonical); err != nil {
			t.Fatalf("load mapping external ID: %v", err)
		}
		if canonical != "100" {
			t.Fatalf("stored external ID = %q, want the canonical %q", canonical, "100")
		}
	})

	t.Run("unknown provider records are never fabricated into local games", func(t *testing.T) {
		if _, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "999999"}); !errors.Is(err, game.ErrProviderRecordNotFound) {
			t.Fatalf("Resolve unknown = %v, want %v", err, game.ErrProviderRecordNotFound)
		}
		var count int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_external_ids WHERE external_id='999999'`).Scan(&count); err != nil {
			t.Fatalf("count unknown mappings: %v", err)
		}
		if count != 0 {
			t.Fatalf("mappings for an unknown provider record = %d, want 0", count)
		}
	})

	t.Run("a later provider refresh never renames the authoritative local game", func(t *testing.T) {
		if _, err := catalog.Update(t.Context(), first.ID, "Renamed Locally", first.Slug); err != nil {
			t.Fatalf("rename local game: %v", err)
		}
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{
				Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"},
				Name:      "Provider Renamed It",
				Slug:      "provider-renamed-it",
				GameType:  "Main Game",
			},
		})
		resolved, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
		if err != nil {
			t.Fatalf("Resolve after refresh: %v", err)
		}
		if resolved.ID != first.ID || resolved.Name != "Renamed Locally" || resolved.Slug != first.Slug {
			t.Fatalf("resolved = %#v, want the unchanged local game %s", resolved, first.ID)
		}
	})

	t.Run("resolving an archived identity restores it without recreating the game", func(t *testing.T) {
		var archived bool
		if err := pool.QueryRow(t.Context(), `SELECT archived_at IS NOT NULL FROM game_external_ids WHERE external_id='101'`).Scan(&archived); err != nil {
			t.Fatalf("load archived identity: %v", err)
		}
		if !archived {
			t.Fatal("external ID 101 was not archived by the refresh that dropped it")
		}
		// Bring 101 back into the active generation, but leave the mapping
		// archived so Resolve itself has to clear it (spec section 9 step 2).
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{
				Reference: game.ProviderReference{Provider: "igdb", ExternalID: "101"},
				Name:      "Shared Display Name",
				Slug:      "shared-display-name-again",
				GameType:  "Main Game",
			},
		})
		if _, err := pool.Exec(t.Context(), `UPDATE game_external_ids SET archived_at=now() WHERE external_id='101'`); err != nil {
			t.Fatalf("re-archive identity: %v", err)
		}
		resolved, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "101"})
		if err != nil {
			t.Fatalf("Resolve archived: %v", err)
		}
		if resolved.ID != second.ID || resolved.Slug != second.Slug {
			t.Fatalf("resolved = %#v, want the existing local game %s", resolved, second.ID)
		}
		if err := pool.QueryRow(t.Context(), `SELECT archived_at IS NOT NULL FROM game_external_ids WHERE external_id='101'`).Scan(&archived); err != nil {
			t.Fatalf("load restored identity: %v", err)
		}
		if archived {
			t.Fatal("resolving a present provider record left the identity archived")
		}
	})
}

// TestIGDBMaterializationConvergesOnSlugConflict proves the common race: two
// or more players materialize the same provider record at once, every racer
// derives the same deterministic slug, and the games.slug unique index rejects
// all but one. An advisory-lock barrier on the local Game insert holds every
// racer inside its own transaction, past the already-materialized check, until
// all of them are there. Without that barrier the first racer normally commits
// before the others even begin, and the uniqueness race is never exercised at
// all: the sabotaged implementation passes such a test.
func TestIGDBMaterializationConvergesOnSlugConflict(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore)
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:             "Contended Game",
			Slug:             "contended-game",
			GameType:         "Main Game",
			AlternativeNames: []string{"Contended", "CG"},
		},
	})
	installAdvisoryInsertBarrier(t, pool, "games", 730032)
	release := holdAdvisoryLock(t, pool, 730032)

	const racers = 4
	results := make([]game.Game, racers)
	failures := make([]error, racers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(racers)
	for i := range racers {
		go func() {
			defer waitGroup.Done()
			results[i], failures[i] = discovery.Resolve(context.Background(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
		}()
	}
	waitForAdvisoryWaiters(t, pool, 730032, racers)
	release()
	waitGroup.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("concurrent Resolve %d: %v", i, err)
		}
	}
	for i, resolved := range results {
		if resolved.ID != results[0].ID || resolved.Slug != results[0].Slug {
			t.Fatalf("concurrent Resolve %d = %#v, want the same local game as %#v", i, resolved, results[0])
		}
	}
	if results[0].ID == "" || results[0].Slug != "contended-game" || results[0].Name != "Contended Game" {
		t.Fatalf("converged game = %#v", results[0])
	}

	mappings, games, aliases := countMaterializedState(t, pool, "100")
	if mappings != 1 || games != 1 || aliases != 2 {
		t.Fatalf("mappings=%d games=%d aliases=%d, want 1/1/2", mappings, games, aliases)
	}
}

// TestIGDBMaterializationConvergesOnExternalIDConflict drives the guard the
// design names as final: the game_external_ids (provider, external_id) unique
// index. Reaching it needs two racers that pick different local slugs for one
// provider record, which happens when a generation swap changes the provider
// slug while the first racer is still in flight. Both racers are then held at
// the external-identity insert, so exactly one mapping can win and the loser
// must re-read that winner rather than fail.
func TestIGDBMaterializationConvergesOnExternalIDConflict(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore)
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:      "Contended Game",
			Slug:      "contended-game",
			GameType:  "Main Game",
		},
	})
	installAdvisoryInsertBarrier(t, pool, "game_external_ids", 730033)
	release := holdAdvisoryLock(t, pool, 730033)

	var results [2]game.Game
	var failures [2]error
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		results[0], failures[0] = discovery.Resolve(context.Background(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	}()
	waitForAdvisoryWaiters(t, pool, 730033, 1)

	// The provider record keeps its identity but gains a new slug, so the
	// second racer creates a differently slugged local Game and only collides
	// on the external identity itself.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference: game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:      "Contended Game",
			Slug:      "contended-game-v2",
			GameType:  "Main Game",
		},
	})
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		results[1], failures[1] = discovery.Resolve(context.Background(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	}()
	waitForAdvisoryWaiters(t, pool, 730033, 2)
	release()
	waitGroup.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("concurrent Resolve %d: %v", i, err)
		}
	}
	if results[0].ID != results[1].ID || results[0].Slug != results[1].Slug {
		t.Fatalf("racers diverged: %#v %#v", results[0], results[1])
	}
	if results[0].Slug != "contended-game" && results[0].Slug != "contended-game-v2" {
		t.Fatalf("converged slug = %q", results[0].Slug)
	}

	mappings, games, _ := countMaterializedState(t, pool, "100")
	if mappings != 1 || games != 1 {
		t.Fatalf("mappings=%d games=%d, want 1/1 (the losing racer's local Game must roll back)", mappings, games)
	}
	var mappedID string
	if err := pool.QueryRow(t.Context(), `SELECT game_id::text FROM game_external_ids WHERE external_id='100'`).Scan(&mappedID); err != nil {
		t.Fatalf("load surviving mapping: %v", err)
	}
	if mappedID != results[0].ID {
		t.Fatalf("mapped game %s != resolved game %s", mappedID, results[0].ID)
	}
}

// TestIGDBMaterializationUnsynchronizedRacers repeats the same race without an
// artificial barrier, so the production code path runs exactly as deployed.
// It is a smoke test: the barrier-driven tests above are what actually prove
// the conflict handling.
func TestIGDBMaterializationUnsynchronizedRacers(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore)
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:             "Contended Game",
			Slug:             "contended-game",
			GameType:         "Main Game",
			AlternativeNames: []string{"Contended", "CG"},
		},
	})

	const racers = 8
	results := make([]game.Game, racers)
	failures := make([]error, racers)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(racers)
	for i := range racers {
		go func() {
			defer waitGroup.Done()
			<-start
			results[i], failures[i] = discovery.Resolve(context.Background(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
		}()
	}
	close(start)
	waitGroup.Wait()

	for i, err := range failures {
		if err != nil {
			t.Fatalf("concurrent Resolve %d: %v", i, err)
		}
	}
	for i, resolved := range results {
		if resolved.ID != results[0].ID || resolved.Slug != results[0].Slug {
			t.Fatalf("concurrent Resolve %d = %#v, want the same local game as %#v", i, resolved, results[0])
		}
	}
	mappings, games, aliases := countMaterializedState(t, pool, "100")
	if mappings != 1 || games != 1 || aliases != 2 {
		t.Fatalf("mappings=%d games=%d aliases=%d, want 1/1/2", mappings, games, aliases)
	}
}

func activateIGDBGeneration(t *testing.T, store *gamepostgres.Store, providerGames []game.ProviderGame) {
	t.Helper()
	generationID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), generationID, providerGames); err != nil {
		t.Fatalf("store generation games: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), generationID, int64(len(providerGames))); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
}

// installAdvisoryInsertBarrier makes every INSERT into table wait for the
// advisory lock key, which lets a test park concurrent transactions at an
// exact statement instead of hoping the Go scheduler interleaves them.
func installAdvisoryInsertBarrier(t *testing.T, pool *pgxpool.Pool, table string, key int64) {
	t.Helper()
	name := "block_" + table + "_insert"
	statement := "CREATE FUNCTION " + name + "() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(" +
		strconv.FormatInt(key, 10) + "); RETURN NEW; END $$; CREATE TRIGGER " + name +
		" BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION " + name + "();"
	if _, err := pool.Exec(t.Context(), statement); err != nil {
		t.Fatalf("install %s barrier: %v", table, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP TRIGGER "+name+" ON "+table); err != nil {
			t.Logf("drop %s barrier: %v", table, err)
		}
	})
}

func holdAdvisoryLock(t *testing.T, pool *pgxpool.Pool, key int64) func() {
	t.Helper()
	blocker, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin advisory-lock transaction: %v", err)
	}
	open := true
	t.Cleanup(func() {
		if open {
			_ = blocker.Rollback(context.Background())
		}
	})
	if _, err := blocker.Exec(t.Context(), `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}
	return func() {
		t.Helper()
		if err := blocker.Commit(t.Context()); err != nil {
			t.Fatalf("release advisory lock: %v", err)
		}
		open = false
	}
}

func waitForAdvisoryWaiters(t *testing.T, pool *pgxpool.Pool, key int64, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	waiting := 0
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM pg_locks
			WHERE locktype='advisory' AND NOT granted
			  AND classid=($1::bigint >> 32)::oid AND objid=($1::bigint & 4294967295)::oid
		`, key).Scan(&waiting); err != nil {
			t.Fatalf("count advisory lock waiters: %v", err)
		}
		if waiting >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d advisory lock waiters on %d, saw %d", want, key, waiting)
}

func countMaterializedState(t *testing.T, pool *pgxpool.Pool, externalID string) (mappings, games, aliases int) {
	t.Helper()
	if err := pool.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM game_external_ids WHERE lower(btrim(provider))='igdb' AND external_id=$1),
		       (SELECT count(*) FROM games),
		       (SELECT count(*) FROM game_aliases)
	`, externalID).Scan(&mappings, &games, &aliases); err != nil {
		t.Fatalf("count materialized state: %v", err)
	}
	return mappings, games, aliases
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

	events := event.NewService(eventpostgres.NewStore(pool))
	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	currentEvent, err := events.Create(t.Context(), "CXT Test", startsAt.Add(-time.Hour), startsAt.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := events.SetCurrent(t.Context(), currentEvent.ID); err != nil {
		t.Fatalf("set current event: %v", err)
	}
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

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, events, nil)
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
	activeForFollower, err := readStore.ListActive(t.Context(), firstJoiner.ID)
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
	// Concurrency tests deliberately hold several transactions open at once.
	// The default maximum tracks the host CPU count, which would starve those
	// tests (and deadlock a barrier that waits for every racer) on small CI
	// machines, so give the pool fixed headroom instead.
	poolConfig.MaxConns = 16
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

func testProviderGame(externalID string) game.ProviderGame {
	return game.ProviderGame{
		Reference: game.ProviderReference{Provider: "igdb", ExternalID: externalID},
		Name:      "Provider Game " + externalID,
		Slug:      "provider-game-" + externalID,
		GameType:  "Main Game",
	}
}

func waitForAdvisoryWaiter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatalf("check advisory lock waiter: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for staged insert to block on advisory lock")
}
