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
	"strconv"
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
		UserID:       account.ID,
		ProviderKind: "oidc",
		ProviderKey:  "https://identity.example/realms/lan",
		Subject:      "subject-1",
		DisplayName:  "Identity Name",
		Email:        "player@example.test",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	found, err := identities.ByProviderSubject(t.Context(), identity.ProviderKind, identity.ProviderKey, identity.Subject)
	if err != nil || found.UserID != account.ID || found.Email != identity.Email {
		t.Fatalf("find identity = %#v, %v", found, err)
	}
	if _, err := identities.Create(t.Context(), auth.Identity{
		UserID: account.ID, ProviderKind: identity.ProviderKind, ProviderKey: identity.ProviderKey, Subject: identity.Subject,
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

// A Game Nick rename claims the new spelling under the same case-insensitive
// uniqueness invariant as the initial claim, and immediately frees the old
// one for any other player — a pure rename does not reserve history.
func TestGameNickRenameEnforcesUniquenessAndFreesOldNick(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))

	renamer, err := users.Register(t.Context(), "NightOtter")
	if err != nil {
		t.Fatalf("register renamer: %v", err)
	}
	other, err := users.Register(t.Context(), "OtherPlayer")
	if err != nil {
		t.Fatalf("register other player: %v", err)
	}

	if _, err := users.RenameGameNick(t.Context(), other.ID, "nightotter"); !errors.Is(err, user.ErrGameNickTaken) {
		t.Fatalf("case-insensitive collision error = %v, want ErrGameNickTaken", err)
	}

	renamed, err := users.RenameGameNick(t.Context(), renamer.ID, "DaytimeOtter")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.GameNick != "DaytimeOtter" {
		t.Fatalf("renamed.GameNick = %q, want DaytimeOtter", renamed.GameNick)
	}

	// The old nick, exact case, must be immediately reusable by anyone.
	reclaimed, err := users.RenameGameNick(t.Context(), other.ID, "NightOtter")
	if err != nil {
		t.Fatalf("reclaim freed nick: %v", err)
	}
	if reclaimed.GameNick != "NightOtter" {
		t.Fatalf("reclaimed.GameNick = %q, want NightOtter", reclaimed.GameNick)
	}
}

func TestPersistentAuthenticationFlows(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	flows := authpostgres.NewFlowStore(pool)
	service := auth.NewFlowService(flows)
	issuer := "https://identity.example/realms/lan"
	first, err := service.CreateOAuth(t.Context(), auth.OAuthFlowInput{
		State: "state-1", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer,
		Operation: auth.FlowLogin, Nonce: "nonce-1", PKCEVerifier: "verifier-1",
	})
	if err != nil {
		t.Fatalf("create first flow: %v", err)
	}
	second, err := service.CreateOAuth(t.Context(), auth.OAuthFlowInput{
		State: "state-2", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer,
		Operation: auth.FlowLogin, Nonce: "nonce-2", PKCEVerifier: "verifier-2",
	})
	if err != nil || first.ID == second.ID {
		t.Fatalf("create parallel flow = %#v, %v", second, err)
	}

	callback := auth.OAuthCallback{State: "state-1", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer, Operation: auth.FlowLogin}
	wrong := callback
	wrong.BrowserBinding = "other-browser"
	if _, err := service.ConsumeOAuth(t.Context(), wrong); !errors.Is(err, auth.ErrFlowBrowser) {
		t.Fatalf("wrong browser error = %v", err)
	}
	wrong = callback
	wrong.ProviderKey = "https://other.example"
	if _, err := service.ConsumeOAuth(t.Context(), wrong); !errors.Is(err, auth.ErrFlowProvider) {
		t.Fatalf("wrong provider error = %v", err)
	}
	wrong = callback
	wrong.Operation = auth.FlowResolution
	if _, err := service.ConsumeOAuth(t.Context(), wrong); !errors.Is(err, auth.ErrFlowOperation) {
		t.Fatalf("wrong operation error = %v", err)
	}
	consumed, err := service.ConsumeOAuth(t.Context(), callback)
	if err != nil || consumed.Nonce != "nonce-1" || consumed.PKCEVerifier != "verifier-1" {
		t.Fatalf("consume flow = %#v, %v", consumed, err)
	}
	var retainedProtocolSecrets int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM auth_flows
		WHERE id = $1 AND (nonce IS NOT NULL OR pkce_verifier IS NOT NULL)
	`, consumed.ID).Scan(&retainedProtocolSecrets); err != nil {
		t.Fatalf("inspect consumed flow secrets: %v", err)
	}
	if retainedProtocolSecrets != 0 {
		t.Fatal("consumed OAuth flow retained nonce or PKCE verifier")
	}
	if _, err := service.ConsumeOAuth(t.Context(), callback); !errors.Is(err, auth.ErrFlowConsumed) {
		t.Fatalf("replay error = %v", err)
	}

	var firstUserID, secondUserID string
	if err := pool.QueryRow(t.Context(), `INSERT INTO users (game_nick) VALUES ('FlowOwner') RETURNING id::text`).Scan(&firstUserID); err != nil {
		t.Fatalf("create first link user: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `INSERT INTO users (game_nick) VALUES ('OtherOwner') RETURNING id::text`).Scan(&secondUserID); err != nil {
		t.Fatalf("create second link user: %v", err)
	}
	if _, err := service.CreateOAuth(t.Context(), auth.OAuthFlowInput{
		State: "link-state", BrowserBinding: "same-browser", ProviderKind: "discord", ProviderKey: "discord",
		Operation: auth.FlowLink, InitiatingUserID: firstUserID, Nonce: "nonce", PKCEVerifier: "verifier",
	}); err != nil {
		t.Fatalf("create link flow: %v", err)
	}
	linkCallback := auth.OAuthCallback{
		State: "link-state", BrowserBinding: "same-browser", ProviderKind: "discord", ProviderKey: "discord",
		Operation: auth.FlowLink, InitiatingUserID: secondUserID,
	}
	if _, err := service.ConsumeOAuth(t.Context(), linkCallback); !errors.Is(err, auth.ErrFlowInitiatingUser) {
		t.Fatalf("wrong initiating user error = %v", err)
	}
	linkCallback.InitiatingUserID = firstUserID
	if _, err := service.ConsumeOAuth(t.Context(), linkCallback); err != nil {
		t.Fatalf("consume correctly bound link flow: %v", err)
	}

	if _, err := service.CreateOAuth(t.Context(), auth.OAuthFlowInput{
		State: "racing-state", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer,
		Operation: auth.FlowLogin, Nonce: "nonce", PKCEVerifier: "verifier",
	}); err != nil {
		t.Fatalf("create racing flow: %v", err)
	}
	racingCallback := auth.OAuthCallback{
		State: "racing-state", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer, Operation: auth.FlowLogin,
	}
	results := runConcurrently(
		func() error { _, err := service.ConsumeOAuth(t.Context(), racingCallback); return err },
		func() error { _, err := service.ConsumeOAuth(t.Context(), racingCallback); return err },
	)
	assertOneWinner(t, "OAuth callback", results, auth.ErrFlowConsumed)

	stateHash := sha256.Sum256([]byte("expired-state"))
	browserHash := sha256.Sum256([]byte("same-browser"))
	if _, err := flows.CreateOAuth(t.Context(), auth.OAuthFlow{
		StateHash: stateHash, BrowserBindingHash: browserHash, ProviderKind: "oidc", ProviderKey: issuer,
		Operation: auth.FlowLogin, Nonce: "nonce", PKCEVerifier: "verifier", ExpiresAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("create expired flow: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE auth_flows SET created_at = now() - interval '2 minutes', expires_at = now() - interval '1 minute'
		WHERE state_hash = $1
	`, stateHash[:]); err != nil {
		t.Fatalf("expire flow fixture: %v", err)
	}
	if _, err := service.ConsumeOAuth(t.Context(), auth.OAuthCallback{
		State: "expired-state", BrowserBinding: "same-browser", ProviderKind: "oidc", ProviderKey: issuer, Operation: auth.FlowLogin,
	}); !errors.Is(err, auth.ErrFlowExpired) {
		t.Fatalf("expired flow error = %v", err)
	}

	pending, err := service.CreatePending(t.Context(), auth.PendingIdentityInput{
		BrowserBinding: "same-browser", ProviderKind: "discord", ProviderKey: "discord", Subject: "1234",
		Operation: auth.FlowLogin, DisplayName: "Alice Example", GameNickSuggestion: "NightOtter",
	})
	if err != nil {
		t.Fatalf("create pending identity: %v", err)
	}
	for range 2 {
		if _, err := service.Pending(t.Context(), pending.ID, "same-browser"); err != nil {
			t.Fatalf("non-consuming pending lookup: %v", err)
		}
	}
	if _, err := service.Pending(t.Context(), pending.ID, "other-browser"); !errors.Is(err, auth.ErrFlowBrowser) {
		t.Fatalf("pending wrong browser error = %v", err)
	}
	if err := service.ConsumePending(t.Context(), pending.ID, "same-browser"); err != nil {
		t.Fatalf("consume pending identity: %v", err)
	}
	if _, err := service.Pending(t.Context(), pending.ID, "same-browser"); !errors.Is(err, auth.ErrFlowConsumed) {
		t.Fatalf("consumed pending error = %v", err)
	}
}

func TestGameCatalogPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
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
	if before.CatalogAvailable {
		t.Fatal("CatalogAvailable = true before any generation ever activated, want false")
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
	if !after.CatalogAvailable {
		t.Fatal("active generation must mark catalog available")
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
	// The newer generation is started with the raw BeginIGDBGeneration
	// statement rather than store.BeginGeneration: LANsurfer 1.0 is
	// single-instance only and IGDBSyncService.TryStart's in-process mutex
	// mean the store-level API never calls BeginGeneration while a sync it
	// started is still running, so store.BeginGeneration now retires any
	// leftover 'syncing' row first (Task 9 hardening, see
	// TestIGDBBeginGenerationFailsAbandonedSyncingGeneration). Going
	// through the raw statement here still proves this test's real
	// invariant -- that activating an OLDER generation never corrupts an
	// unrelated NEWER 'syncing' row -- without that unrelated hardening
	// retiring firstID before secondID can exist.
	secondID := beginRawIGDBGeneration(t, pool)
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
		// The newer generation is started with the raw BeginIGDBGeneration
		// statement rather than store.BeginGeneration: a second
		// store.BeginGeneration call would run the Task 9 abandoned-generation
		// cleanup first and retire olderID to 'failed' before this test's own
		// FailGeneration(olderID) call below, corrupting the exact scenario
		// this test protects (an older attempt failing after a newer one is
		// already in flight). See beginRawIGDBGeneration's doc comment.
		newerID := beginRawIGDBGeneration(t, pool)
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
		// See the sibling subtest above: the newer generation is started with
		// the raw BeginIGDBGeneration statement rather than
		// store.BeginGeneration so the Task 9 abandoned-generation cleanup
		// does not retire olderID to 'failed' before this test's later
		// ActivateGeneration(olderID) call, which needs olderID to still be
		// 'syncing'.
		newerID := beginRawIGDBGeneration(t, pool)
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

// TestIGDBSearchDoesNotResurrectProviderResultForDeactivatedMaterializedGame
// covers materialize -> deactivate -> search-again: an admin deactivating
// the local Game a provider record already materialized into must not make
// SearchDiscoveryGames treat that provider record as unmaterialized again.
// Before the fix, materialized_provider_rows required local_game.active,
// so deactivating the Game made its mapping "invisible" to that CTE while
// still existing (non-archived) -- the same provider row then reappeared
// through unmaterialized_provider_rows as if brand new, even though Follow
// and Game Request creation would deterministically fail against it (the
// materializer finds the existing mapping and returns the inactive Game).
func TestIGDBSearchDoesNotResurrectProviderResultForDeactivatedMaterializedGame(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	viewer, err := users.Register(t.Context(), "Deactivated Materialized Viewer")
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	games := game.NewService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9801"}, Name: "Deactivated Quest", Slug: "deactivated-quest", GameType: "Main Game"},
	})
	materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "9801"})
	if err != nil {
		t.Fatalf("materialize provider record: %v", err)
	}

	if err := games.SetActive(t.Context(), materialized.ID, false); err != nil {
		t.Fatalf("deactivate local game: %v", err)
	}

	page, err := discovery.Browse(t.Context(), viewer.ID, "deactivated quest")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	got := page.Items
	for _, item := range got {
		if item.Reference.ExternalID == "9801" && !item.Materialized {
			t.Fatalf("provider record for a deactivated materialized Game resurfaced as unmaterialized: %#v", item)
		}
	}
	if len(got) != 0 {
		t.Fatalf("Browse = %#v, want no results: a deactivated Game must stay player-invisible", got)
	}
}

// TestIGDBDisappearanceArchivesIdentityWithoutDeletingLocalGameThenReappearanceRestoresIt
// exercises the plan's Step 1 lifecycle scenario against a materialized (not
// merely mapped) local Game: materialize provider record 100 through
// DiscoveryService.Resolve, activate a complete generation that omits it,
// and assert -- with the exact two queries the plan specifies -- that the
// mapping is archived while the local Game and its data are untouched.
// Reactivating a generation that includes 100 again must clear the
// archival.
func TestIGDBDisappearanceArchivesIdentityWithoutDeletingLocalGameThenReappearanceRestoresIt(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	activateIGDBGeneration(t, gameStore, []game.ProviderGame{testProviderGame("100")})
	materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("materialize provider record 100: %v", err)
	}

	// Activate a complete generation that no longer carries external ID 100.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{testProviderGame("200")})

	var archived bool
	if err := pool.QueryRow(t.Context(), `
		SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100'
	`).Scan(&archived); err != nil {
		t.Fatalf("load archived identity: %v", err)
	}
	if !archived {
		t.Fatal("disappeared IGDB identity was not archived")
	}
	var localGameCount int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM games g JOIN game_external_ids e ON e.game_id=g.id
		WHERE lower(e.provider)='igdb' AND e.external_id='100'
	`).Scan(&localGameCount); err != nil {
		t.Fatalf("count local game joined to the archived identity: %v", err)
	}
	if localGameCount != 1 {
		t.Fatalf("local games joined to the archived identity = %d, want 1: archival must never delete the local Game", localGameCount)
	}
	var name string
	if err := pool.QueryRow(t.Context(), `SELECT name FROM games WHERE id=$1`, materialized.ID).Scan(&name); err != nil {
		t.Fatalf("load local game after archival: %v", err)
	}
	if name != materialized.Name {
		t.Fatalf("local game name after archival = %q, want %q: archival must preserve local data", name, materialized.Name)
	}

	// Activate a complete generation that carries 100 again.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{testProviderGame("100")})
	if err := pool.QueryRow(t.Context(), `
		SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100'
	`).Scan(&archived); err != nil {
		t.Fatalf("load restored identity: %v", err)
	}
	if archived {
		t.Fatal("reappeared IGDB identity remained archived")
	}
}

// TestIGDBMaterializationPersistsEnrichment covers Task 1's baseline case:
// materializing a provider record copies its full presentation snapshot
// (cover, type, platforms, release date, provider-updated-at) into
// game_igdb_enrichment, keyed by the local Game it just materialized.
func TestIGDBMaterializationPersistsEnrichment(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	releaseDate := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	providerUpdatedAt := time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC)
	provider := game.ProviderGame{
		Reference:         game.ProviderReference{Provider: "igdb", ExternalID: "100"},
		Name:              "Enrichment Quest",
		Slug:              "enrichment-quest",
		GameType:          "Main Game",
		FirstReleaseDate:  &releaseDate,
		PlatformNames:     []string{"PC", "Switch"},
		CoverImageID:      "co-old",
		ProviderUpdatedAt: &providerUpdatedAt,
	}
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{provider})

	materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("materialize provider record 100: %v", err)
	}

	var (
		externalID       string
		coverImageID     string
		gameType         string
		platformNames    []string
		firstReleaseDate time.Time
		providerUpdated  time.Time
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT external_id, cover_image_id, game_type, platform_names, first_release_date, provider_updated_at
		FROM game_igdb_enrichment WHERE game_id=$1
	`, materialized.ID).Scan(&externalID, &coverImageID, &gameType, &platformNames, &firstReleaseDate, &providerUpdated); err != nil {
		t.Fatalf("load enrichment for materialized game: %v", err)
	}
	if externalID != "100" {
		t.Fatalf("enrichment external_id = %q, want %q", externalID, "100")
	}
	if coverImageID != provider.CoverImageID {
		t.Fatalf("enrichment cover_image_id = %q, want %q", coverImageID, provider.CoverImageID)
	}
	if gameType != provider.GameType {
		t.Fatalf("enrichment game_type = %q, want %q", gameType, provider.GameType)
	}
	if !slices.Equal(platformNames, provider.PlatformNames) {
		t.Fatalf("enrichment platform_names = %#v, want %#v", platformNames, provider.PlatformNames)
	}
	if !firstReleaseDate.Equal(*provider.FirstReleaseDate) {
		t.Fatalf("enrichment first_release_date = %v, want %v", firstReleaseDate, *provider.FirstReleaseDate)
	}
	if !providerUpdated.Equal(*provider.ProviderUpdatedAt) {
		t.Fatalf("enrichment provider_updated_at = %v, want %v", providerUpdated, *provider.ProviderUpdatedAt)
	}
}

// TestIGDBRefreshUpdatesOnlyNonEmptyEnrichment covers the last-known-good
// merge: a later generation's thinner provider row (empty type, empty
// platforms, no release date) must overwrite only the fields it actually
// carries non-empty values for -- cover and provider-updated-at here --
// and must never blank out a previously stored non-empty field.
func TestIGDBRefreshUpdatesOnlyNonEmptyEnrichment(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	oldRelease := time.Date(2019, time.January, 1, 0, 0, 0, 0, time.UTC)
	oldUpdatedAt := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:         game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:              "Refresh Quest",
			Slug:              "refresh-quest",
			GameType:          "Main Game",
			FirstReleaseDate:  &oldRelease,
			PlatformNames:     []string{"PC"},
			CoverImageID:      "co-old",
			ProviderUpdatedAt: &oldUpdatedAt,
		},
	})
	materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("materialize provider record 100: %v", err)
	}

	// Generation 2 carries the same ID with a newer cover/provider-updated-at
	// but an otherwise thinner provider row: empty type, empty platforms, no
	// release date.
	newUpdatedAt := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:         game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:              "Refresh Quest",
			Slug:              "refresh-quest",
			CoverImageID:      "co-new",
			ProviderUpdatedAt: &newUpdatedAt,
		},
	})

	var (
		coverImageID     string
		gameType         string
		platformNames    []string
		firstReleaseDate time.Time
		providerUpdated  time.Time
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT cover_image_id, game_type, platform_names, first_release_date, provider_updated_at
		FROM game_igdb_enrichment WHERE game_id=$1
	`, materialized.ID).Scan(&coverImageID, &gameType, &platformNames, &firstReleaseDate, &providerUpdated); err != nil {
		t.Fatalf("load enrichment after refresh: %v", err)
	}
	if coverImageID != "co-new" {
		t.Fatalf("enrichment cover_image_id = %q, want %q: must track the newer non-empty value", coverImageID, "co-new")
	}
	if !providerUpdated.Equal(newUpdatedAt) {
		t.Fatalf("enrichment provider_updated_at = %v, want %v: must track the newer non-empty value", providerUpdated, newUpdatedAt)
	}
	if gameType != "Main Game" {
		t.Fatalf("enrichment game_type = %q, want the previous non-empty value %q", gameType, "Main Game")
	}
	if !slices.Equal(platformNames, []string{"PC"}) {
		t.Fatalf("enrichment platform_names = %#v, want the previous non-empty value %#v", platformNames, []string{"PC"})
	}
	if !firstReleaseDate.Equal(oldRelease) {
		t.Fatalf("enrichment first_release_date = %v, want the previous non-empty value %v", firstReleaseDate, oldRelease)
	}
}

// TestIGDBDisappearanceKeepsEnrichmentAndReappearanceRefreshesIt covers the
// enrichment side of the disappearance/reappearance lifecycle already
// exercised for external ID archival above: while a materialized provider
// record is missing from the active generation, its last-known-good
// enrichment must stay exactly as it was, and reappearing with fresh data
// must refresh it again.
func TestIGDBDisappearanceKeepsEnrichmentAndReappearanceRefreshesIt(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:    game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:         "Disappearance Quest",
			Slug:         "disappearance-quest",
			GameType:     "Main Game",
			CoverImageID: "co-old",
		},
	})
	materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "100"})
	if err != nil {
		t.Fatalf("materialize provider record 100: %v", err)
	}

	// Activate a generation that omits 100 entirely.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{testProviderGame("200")})

	var archived bool
	if err := pool.QueryRow(t.Context(), `
		SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100'
	`).Scan(&archived); err != nil {
		t.Fatalf("load archived identity: %v", err)
	}
	if !archived {
		t.Fatal("disappeared IGDB identity was not archived")
	}
	var coverImageID string
	if err := pool.QueryRow(t.Context(), `
		SELECT cover_image_id FROM game_igdb_enrichment WHERE game_id=$1
	`, materialized.ID).Scan(&coverImageID); err != nil {
		t.Fatalf("load enrichment while disappeared: %v", err)
	}
	if coverImageID != "co-old" {
		t.Fatalf("enrichment cover_image_id while disappeared = %q, want %q: disappearance must keep last-known-good enrichment", coverImageID, "co-old")
	}

	// Activate a later generation that carries 100 again with fresh data.
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{
			Reference:    game.ProviderReference{Provider: "igdb", ExternalID: "100"},
			Name:         "Disappearance Quest",
			Slug:         "disappearance-quest",
			GameType:     "Main Game",
			CoverImageID: "co-returned",
		},
	})

	if err := pool.QueryRow(t.Context(), `
		SELECT archived_at IS NOT NULL FROM game_external_ids WHERE lower(provider)='igdb' AND external_id='100'
	`).Scan(&archived); err != nil {
		t.Fatalf("load restored identity: %v", err)
	}
	if archived {
		t.Fatal("reappeared IGDB identity remained archived")
	}
	if err := pool.QueryRow(t.Context(), `
		SELECT cover_image_id FROM game_igdb_enrichment WHERE game_id=$1
	`, materialized.ID).Scan(&coverImageID); err != nil {
		t.Fatalf("load enrichment after reappearance: %v", err)
	}
	if coverImageID != "co-returned" {
		t.Fatalf("enrichment cover_image_id after reappearance = %q, want %q", coverImageID, "co-returned")
	}
}

// TestGamePresentationEnrichmentPersistsThroughLocalBrowseAndMyGames covers
// Task 2: the local Browse and My Games paths must read the last-known-good
// game_igdb_enrichment snapshot for a materialized-via-IGDB Game, not just
// the provider-search path. The test never deactivates or otherwise touches
// provider data after materialization, so the assertions can only be
// satisfied by the local read itself being snapshot-backed.
func TestGamePresentationEnrichmentPersistsThroughLocalBrowseAndMyGames(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	viewer, err := users.Register(t.Context(), "Presentation Viewer")
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})
	subscriptions := game.NewSubscriptionService(gameStore)

	releaseDate := time.Date(2002, time.July, 3, 0, 0, 0, 0, time.UTC)
	provider := game.ProviderGame{
		Reference:        game.ProviderReference{Provider: "igdb", ExternalID: "9500"},
		Name:             "Presentation Quest",
		Slug:             "presentation-quest",
		GameType:         "Main Game",
		FirstReleaseDate: &releaseDate,
		PlatformNames:    []string{"PC (Microsoft Windows)"},
		CoverImageID:     "co1abc",
	}
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{provider})

	materialized, err := discovery.Resolve(t.Context(), provider.Reference)
	if err != nil {
		t.Fatalf("materialize provider record: %v", err)
	}
	if err := subscriptions.Follow(t.Context(), viewer.ID, materialized.ID); err != nil {
		t.Fatalf("follow materialized game: %v", err)
	}

	assertPresentation := func(t *testing.T, label string, presentation game.GamePresentation) {
		t.Helper()
		if presentation.CoverImageID != provider.CoverImageID {
			t.Fatalf("%s CoverImageID = %q, want %q", label, presentation.CoverImageID, provider.CoverImageID)
		}
		if presentation.GameType != provider.GameType {
			t.Fatalf("%s GameType = %q, want %q", label, presentation.GameType, provider.GameType)
		}
		if !slices.Equal(presentation.PlatformNames, provider.PlatformNames) {
			t.Fatalf("%s PlatformNames = %#v, want %#v", label, presentation.PlatformNames, provider.PlatformNames)
		}
		if presentation.FirstReleaseDate == nil || !presentation.FirstReleaseDate.Equal(*provider.FirstReleaseDate) {
			t.Fatalf("%s FirstReleaseDate = %v, want %v", label, presentation.FirstReleaseDate, *provider.FirstReleaseDate)
		}
	}

	browsed, err := gameStore.Browse(t.Context(), viewer.ID, "")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	var browsedGame *game.SubscriptionGame
	for i := range browsed {
		if browsed[i].ID == materialized.ID {
			browsedGame = &browsed[i]
		}
	}
	if browsedGame == nil {
		t.Fatalf("Browse = %#v, want materialized game present", browsed)
	}
	assertPresentation(t, "Browse", browsedGame.GamePresentation)

	myGames, err := gameStore.MyGames(t.Context(), viewer.ID)
	if err != nil {
		t.Fatalf("MyGames: %v", err)
	}
	var myGame *game.SubscriptionGame
	for i := range myGames {
		if myGames[i].ID == materialized.ID {
			myGame = &myGames[i]
		}
	}
	if myGame == nil {
		t.Fatalf("MyGames = %#v, want materialized game present", myGames)
	}
	assertPresentation(t, "MyGames", myGame.GamePresentation)
}

// TestGameRequestPresentationIncludesLocalEnrichment covers Task 6: the
// Game Request read model's Game field must carry the same last-known-good
// game_igdb_enrichment snapshot /games already shows (Task 2's
// game.GamePresentation), for both ListActive and ByID. Enrichment is
// touched only through materialization, never written by the gamerequest
// module itself, proving the join reads the game module's own snapshot
// rather than gamerequest owning a copy of it.
func TestGameRequestPresentationIncludesLocalEnrichment(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Enrichment Request Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	provider := game.ProviderGame{
		Reference:    game.ProviderReference{Provider: "igdb", ExternalID: "9600"},
		Name:         "Warcraft III",
		Slug:         "warcraft-iii",
		GameType:     "Main Game",
		CoverImageID: "co1abc",
	}
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{provider})

	materialized, err := discovery.Resolve(t.Context(), provider.Reference)
	if err != nil {
		t.Fatalf("materialize provider record: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	created, err := requests.Create(t.Context(), host.ID, materialized.ID, nil, startsAt, "Enrichment round")
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	assertRequestGame := func(t *testing.T, label string, request gamerequestquery.Request) {
		t.Helper()
		if request.Game.CoverImageID != "co1abc" ||
			request.Game.Name != "Warcraft III" ||
			request.Game.GameType != "Main Game" {
			t.Fatalf("%s game = %#v", label, request.Game)
		}
		if request.GameID != materialized.ID {
			t.Fatalf("%s game ID = %q, want %q", label, request.GameID, materialized.ID)
		}
	}

	readStore := gamerequestquery.NewStore(pool)
	active, err := readStore.ListActive(t.Context(), host.ID, gamerequestquery.Filter{})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	index := slices.IndexFunc(active, func(request gamerequestquery.Request) bool { return request.ID == created.ID })
	if index < 0 {
		t.Fatalf("created request missing from ListActive: %#v", active)
	}
	assertRequestGame(t, "ListActive", active[index])

	detail, err := readStore.ByID(t.Context(), created.ID, host.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	assertRequestGame(t, "ByID", detail)
}

// TestIGDBBeginGenerationFailsAbandonedSyncingGeneration exercises the
// plan's Step 2 lifecycle scenario: a generation left 'syncing' by a process
// that exited before completing or failing it (simulated by inserting the
// row directly, as no crash can be induced from a test) must not linger
// forever. The next call to BeginGeneration -- made through a freshly
// constructed Store, as a restarted process would -- must retire it before
// starting its own generation, and a subsequent successful sync through
// that fresh Store must activate normally with provider state pointing at
// the new generation.
func TestIGDBBeginGenerationFailsAbandonedSyncingGeneration(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var abandonedID string
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO igdb_sync_generations (status) VALUES ('syncing') RETURNING id::text
	`).Scan(&abandonedID); err != nil {
		t.Fatalf("insert abandoned syncing generation: %v", err)
	}

	store := gamepostgres.NewStore(pool)
	newID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("BeginGeneration: %v", err)
	}
	if newID == abandonedID {
		t.Fatalf("BeginGeneration returned the abandoned generation ID")
	}

	var abandonedStatus, abandonedError string
	if err := pool.QueryRow(t.Context(), `
		SELECT status, COALESCE(error, '') FROM igdb_sync_generations WHERE id=$1
	`, abandonedID).Scan(&abandonedStatus, &abandonedError); err != nil {
		t.Fatalf("load abandoned generation after BeginGeneration: %v", err)
	}
	if abandonedStatus != "failed" || abandonedError != "previous synchronization did not complete" {
		t.Fatalf("abandoned generation status=%q error=%q, want failed/%q", abandonedStatus, abandonedError, "previous synchronization did not complete")
	}

	if err := store.StoreProviderGames(t.Context(), newID, []game.ProviderGame{testProviderGame("300")}); err != nil {
		t.Fatalf("store provider games for the new generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), newID, 1); err != nil {
		t.Fatalf("activate the new generation: %v", err)
	}

	var newStatus string
	if err := pool.QueryRow(t.Context(), `SELECT status FROM igdb_sync_generations WHERE id=$1`, newID).Scan(&newStatus); err != nil {
		t.Fatalf("load new generation status: %v", err)
	}
	if newStatus != "complete" {
		t.Fatalf("new generation status = %q, want complete", newStatus)
	}
	var activeID string
	if err := pool.QueryRow(t.Context(), `SELECT active_generation_id::text FROM igdb_provider_state`).Scan(&activeID); err != nil {
		t.Fatalf("load active generation: %v", err)
	}
	if activeID != newID {
		t.Fatalf("active generation = %q, want the new generation %q", activeID, newID)
	}
}

// TestIGDBFailedGenerationsDoNotAccumulateAcrossRetries covers repeated
// late-failing full syncs (e.g. the provider connection drops partway
// through a large batch): each failing attempt stages rows into
// igdb_game_index before failing, and nothing previously deleted that data
// except a later successful activation. A generation's igdb_sync_generations
// row is only useful for operator diagnosis until the next sync attempt
// begins -- igdb_provider_state.last_error/last_attempt_succeeded already
// hold the current failure state independently, and BeginIGDBGeneration
// resets both the moment a new attempt starts -- so BeginGeneration must
// clean up every non-active terminal generation before that point.
func TestIGDBFailedGenerationsDoNotAccumulateAcrossRetries(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := gamepostgres.NewStore(pool)

	activeID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin active generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), activeID, []game.ProviderGame{testProviderGame("9700")}); err != nil {
		t.Fatalf("store active generation: %v", err)
	}
	if err := store.ActivateGeneration(t.Context(), activeID, 1); err != nil {
		t.Fatalf("activate active generation: %v", err)
	}

	firstFailedID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin first failing generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), firstFailedID, []game.ProviderGame{testProviderGame("9701"), testProviderGame("9702")}); err != nil {
		t.Fatalf("store first failing generation: %v", err)
	}
	if err := store.FailGeneration(t.Context(), firstFailedID, "late failure one"); err != nil {
		t.Fatalf("fail first generation: %v", err)
	}

	secondFailedID, err := store.BeginGeneration(t.Context())
	if err != nil {
		t.Fatalf("begin second failing generation: %v", err)
	}
	if err := store.StoreProviderGames(t.Context(), secondFailedID, []game.ProviderGame{testProviderGame("9703")}); err != nil {
		t.Fatalf("store second failing generation: %v", err)
	}
	if err := store.FailGeneration(t.Context(), secondFailedID, "late failure two"); err != nil {
		t.Fatalf("fail second generation: %v", err)
	}

	status, err := store.Status(t.Context(), time.Now().UTC())
	if err != nil {
		t.Fatalf("Status after second failure: %v", err)
	}
	if status.State != game.ProviderFailed || status.RecordCount != 1 || status.LastError != "late failure two" {
		t.Fatalf("status after second failure = %#v", status)
	}

	// The next sync attempt (manual retry or the daily scheduler) triggers
	// cleanup of generations that are no longer active. A generation that
	// igdb_provider_state.latest_attempt_generation_id still points to when
	// its own retry begins cannot be deleted yet -- that column has a FK to
	// this table, and it is the exact row this very call may itself be
	// about to fail-abandon or supersede -- so cleanup lags one attempt
	// behind: firstFailedID (two attempts stale) is cleaned up once
	// secondFailedID begins, but secondFailedID (still the latest attempt
	// at that point) is only cleaned up once a generation after IT begins.
	if _, err := store.BeginGeneration(t.Context()); err != nil {
		t.Fatalf("begin third generation: %v", err)
	}
	if _, err := store.BeginGeneration(t.Context()); err != nil {
		t.Fatalf("begin fourth generation: %v", err)
	}

	for _, failedID := range []string{firstFailedID, secondFailedID} {
		var exists bool
		if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM igdb_sync_generations WHERE id=$1)`, failedID).Scan(&exists); err != nil {
			t.Fatalf("check failed generation %s existence: %v", failedID, err)
		}
		if exists {
			t.Fatalf("failed generation %s was not cleaned up by the next sync attempt", failedID)
		}
		var indexRows int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM igdb_game_index WHERE generation_id=$1`, failedID).Scan(&indexRows); err != nil {
			t.Fatalf("count failed generation %s index rows: %v", failedID, err)
		}
		if indexRows != 0 {
			t.Fatalf("failed generation %s index rows = %d, want 0 (cascaded delete)", failedID, indexRows)
		}
	}

	// The active generation, its data, and the operator status it drives
	// must be completely unaffected by the cleanup.
	var activeStatus string
	var activeIndexRows int
	if err := pool.QueryRow(t.Context(), `SELECT status FROM igdb_sync_generations WHERE id=$1`, activeID).Scan(&activeStatus); err != nil {
		t.Fatalf("load active generation status: %v", err)
	}
	if activeStatus != "complete" {
		t.Fatalf("active generation status = %q, want complete", activeStatus)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM igdb_game_index WHERE generation_id=$1`, activeID).Scan(&activeIndexRows); err != nil {
		t.Fatalf("count active generation index rows: %v", err)
	}
	if activeIndexRows != 1 {
		t.Fatalf("active generation index rows = %d, want 1", activeIndexRows)
	}
	var activeGenerationID string
	if err := pool.QueryRow(t.Context(), `SELECT active_generation_id::text FROM igdb_provider_state`).Scan(&activeGenerationID); err != nil {
		t.Fatalf("load provider state active generation: %v", err)
	}
	if activeGenerationID != activeID {
		t.Fatalf("provider state active generation = %q, want %q", activeGenerationID, activeID)
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
	discovery := game.NewDiscoveryService(gameStore, nil, gameStore, storeProviderStatus{store: gameStore})

	t.Run("empty search never touches the discovery repository", func(t *testing.T) {
		local, err := catalog.Create(t.Context(), "Local Only Game", "local-only-game")
		if err != nil {
			t.Fatalf("create local game: %v", err)
		}
		page, err := discovery.Browse(t.Context(), viewer.ID, "")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
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
		page, err := discovery.Browse(t.Context(), viewer.ID, "never synced")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
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

		page, err := discovery.Browse(t.Context(), viewer.ID, "quest runner")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
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

		viewerPage, err := discovery.Browse(t.Context(), viewer.ID, "authoritative")
		if err != nil {
			t.Fatalf("Browse (viewer): %v", err)
		}
		viewerResult := viewerPage.Items
		if len(viewerResult) != 1 || !viewerResult[0].Materialized || viewerResult[0].LocalGameID != localGame.ID ||
			viewerResult[0].Name != "Authoritative Local Name" || !viewerResult[0].Subscribed {
			t.Fatalf("Browse (viewer) = %#v", viewerResult)
		}

		otherPage, err := discovery.Browse(t.Context(), other.ID, "authoritative")
		if err != nil {
			t.Fatalf("Browse (other): %v", err)
		}
		otherResult := otherPage.Items
		if len(otherResult) != 1 || otherResult[0].Subscribed {
			t.Fatalf("Browse (other) = %#v", otherResult)
		}

		// Search by the provider's own (stale) search_text, not the local
		// name/alias, so the provider_matches CTE actually matches this row
		// and dedup by identity_key is the thing keeping the result count at
		// one, not the absence of a provider-side match.
		providerTextPage, err := discovery.Browse(t.Context(), viewer.ID, "provider name")
		if err != nil {
			t.Fatalf("Browse (provider search text): %v", err)
		}
		providerTextResult := providerTextPage.Items
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
		page, err := discovery.Browse(t.Context(), viewer.ID, "vintage bundle master")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 1 || !got[0].Materialized || got[0].LocalGameID != localGame.ID ||
			got[0].Name != "Renamed Local Edition" || got[0].GameType != "Bundle" {
			t.Fatalf("Browse = %#v", got)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 item 8: the repository fetches at
	// most limit+1=51 rows, returns the first 50, and sets HasMore=true only
	// when a 51st row actually exists.
	t.Run("61 matching provider rows are capped at 50 with HasMore set", func(t *testing.T) {
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		batch := make([]game.ProviderGame, 0, 61)
		for i := range 61 {
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

		page, err := discovery.Browse(t.Context(), viewer.ID, "capped result")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(page.Items) != 50 {
			t.Fatalf("len(page.Items) = %d, want 50", len(page.Items))
		}
		if !page.HasMore {
			t.Fatal("HasMore = false, want true: a 51st matching row exists")
		}
	})

	t.Run("exactly 50 matching provider rows report HasMore=false", func(t *testing.T) {
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		batch := make([]game.ProviderGame, 0, 50)
		for i := range 50 {
			externalID := strconv.Itoa(9300 + i)
			batch = append(batch, game.ProviderGame{
				Reference: game.ProviderReference{Provider: "igdb", ExternalID: externalID},
				Name:      "Exact Fifty Result " + externalID,
				Slug:      "exact-fifty-result-" + externalID,
				GameType:  "Main Game",
			})
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, batch); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, int64(len(batch))); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "exact fifty result")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if len(page.Items) != 50 {
			t.Fatalf("len(page.Items) = %d, want 50", len(page.Items))
		}
		if page.HasMore {
			t.Fatal("HasMore = true, want false: exactly 50 matching rows exist, no 51st")
		}
	})

	t.Run("percent and underscore in the search term are matched literally, not as LIKE wildcards", func(t *testing.T) {
		// The provider search predicate uses LIKE '%<escaped term>%' (backed
		// by the search_text gin_trgm index) instead of strpos(), so the
		// search term's own LIKE special characters (%, _, \) must be
		// escaped before the wildcards are added around it. Otherwise a
		// literal '%' or '_' typed by a player would behave like a
		// multi/single-character wildcard instead of matching only itself.
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9501"}, Name: "100% Orange Juice", Slug: "100-orange-juice", GameType: "Main Game"},
			// If '%' were left unescaped, the pattern built from "100% orange"
			// would become '%100% orange%', whose middle '%' is a wildcard
			// that also matches this unrelated title.
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9502"}, Name: "1000 Orange Juice Deluxe", Slug: "1000-orange-juice-deluxe", GameType: "Main Game"},
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9503"}, Name: "Sonic_Adventure", Slug: "sonic-underscore-adventure", GameType: "Main Game"},
			// If '_' were left unescaped, it would be a single-character
			// wildcard that also matches this unrelated title.
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9504"}, Name: "SonicXAdventure", Slug: "sonic-x-adventure", GameType: "Main Game"},
			// Both titles contain a literal percent. Only 9505 starts with the
			// literal query "foo%"; without escaping in the prefix-ranking
			// LIKE, 9506 is incorrectly classified as a prefix and wins by name.
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9505"}, Name: "Foo% Zebra", Slug: "foo-zebra", GameType: "Main Game"},
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9506"}, Name: "FooA foo% Alpha", Slug: "fooa-alpha", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, 6); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		// (a) a normal substring search, with no special characters, still
		// matches correctly.
		normalPage, err := discovery.Browse(t.Context(), viewer.ID, "orange juice")
		if err != nil {
			t.Fatalf("Browse (normal substring): %v", err)
		}
		normal := normalPage.Items
		if len(normal) != 2 {
			t.Fatalf("Browse (normal substring) = %#v, want both Orange Juice titles", normal)
		}

		// (b) a literal '%' in the search term matches only the literal
		// character, not "anything".
		percentPage, err := discovery.Browse(t.Context(), viewer.ID, "100% orange")
		if err != nil {
			t.Fatalf("Browse (percent): %v", err)
		}
		percentSearch := percentPage.Items
		if len(percentSearch) != 1 || percentSearch[0].Reference.ExternalID != "9501" {
			t.Fatalf("Browse (percent) = %#v, want only the literal '100%% Orange Juice' match", percentSearch)
		}

		// (b) a literal '_' in the search term matches only the literal
		// character, not "any single character".
		underscorePage, err := discovery.Browse(t.Context(), viewer.ID, "sonic_adventure")
		if err != nil {
			t.Fatalf("Browse (underscore): %v", err)
		}
		underscoreSearch := underscorePage.Items
		if len(underscoreSearch) != 1 || underscoreSearch[0].Reference.ExternalID != "9503" {
			t.Fatalf("Browse (underscore) = %#v, want only the literal 'Sonic_Adventure' match", underscoreSearch)
		}

		prefixPage, err := discovery.Browse(t.Context(), viewer.ID, "foo%")
		if err != nil {
			t.Fatalf("Browse (literal prefix): %v", err)
		}
		if len(prefixPage.Items) < 2 || prefixPage.Items[0].Reference.ExternalID != "9505" {
			t.Fatalf("Browse (literal prefix) = %#v, want literal prefix before contains", prefixPage.Items)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 items 1-2: an exact name/alias
	// match outranks a prefix match, which outranks a plain contains match,
	// across local Games sharing no other distinguishing rank/subscribed/
	// materialized signal.
	t.Run("exact match outranks prefix match outranks contains match", func(t *testing.T) {
		if _, err := catalog.Create(t.Context(), "Rank Contains The Word Portalcraft", "rank-contains-the-word-portalcraft"); err != nil {
			t.Fatalf("create contains-rank game: %v", err)
		}
		if _, err := catalog.Create(t.Context(), "Portal Quest", "rank-portal-quest"); err != nil {
			t.Fatalf("create prefix-rank game: %v", err)
		}
		if _, err := catalog.Create(t.Context(), "Portal", "rank-portal-exact"); err != nil {
			t.Fatalf("create exact-rank game: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "portal")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 3 {
			t.Fatalf("Browse = %#v, want the three rank-fixture games", got)
		}
		if got[0].Name != "Portal" || got[1].Name != "Portal Quest" || got[2].Name != "Rank Contains The Word Portalcraft" {
			t.Fatalf("Browse order = [%s, %s, %s], want exact, then prefix, then contains",
				got[0].Name, got[1].Name, got[2].Name)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 item 3: within one match_rank, a
	// followed local Game outranks an unfollowed one.
	t.Run("within one rank, followed outranks unfollowed", func(t *testing.T) {
		// Alphabetically "Alpha" sorts before "Zulu", so the followed game
		// (Zulu) only comes first if subscribed DESC really is applied ahead
		// of the lower(name) tie-break, not because of name order alone.
		unfollowed, err := catalog.Create(t.Context(), "Subscribe Rank Alpha", "subscribe-rank-alpha")
		if err != nil {
			t.Fatalf("create unfollowed game: %v", err)
		}
		followed, err := catalog.Create(t.Context(), "Subscribe Rank Zulu", "subscribe-rank-zulu")
		if err != nil {
			t.Fatalf("create followed game: %v", err)
		}
		subscriptions := game.NewSubscriptionService(gameStore)
		if err := subscriptions.Follow(t.Context(), viewer.ID, followed.ID); err != nil {
			t.Fatalf("follow game: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "subscribe rank")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 2 || got[0].LocalGameID != followed.ID || got[1].LocalGameID != unfollowed.ID {
			t.Fatalf("Browse = %#v, want followed game first", got)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 item 4: within one match_rank (and
	// equal subscribed status), a materialized result outranks an
	// unmaterialized provider-only result.
	t.Run("within one rank, materialized outranks unmaterialized", func(t *testing.T) {
		// Alphabetically "Alpha" sorts before "Zulu", so the materialized
		// game (Zulu) only comes first if materialized DESC really is
		// applied ahead of the lower(name) tie-break, not because of name
		// order alone.
		materializedLocal, err := catalog.Create(t.Context(), "Sortorder Zulu Pick", "sortorder-zulu-pick")
		if err != nil {
			t.Fatalf("create materialized game: %v", err)
		}

		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9601"}, Name: "Sortorder Alpha Pick", Slug: "sortorder-alpha-pick", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, 1); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "sortorder")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 2 || got[0].LocalGameID != materializedLocal.ID || got[1].Materialized {
			t.Fatalf("Browse = %#v, want the materialized local Game first", got)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 item 5: once match_rank,
	// subscribed, and materialized all tie, ordering falls back to
	// lower(name) and then the stable identity_key.
	t.Run("final tie breaks on name then stable identity", func(t *testing.T) {
		generationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), generationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9702"}, Name: "Tiebreak Twin", Slug: "tiebreak-twin-9702", GameType: "Main Game"},
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9701"}, Name: "Tiebreak Twin", Slug: "tiebreak-twin-9701", GameType: "Main Game"},
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9703"}, Name: "Tiebreak Alpha", Slug: "tiebreak-alpha", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), generationID, 3); err != nil {
			t.Fatalf("activate generation: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "tiebreak")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 3 {
			t.Fatalf("Browse = %#v, want the three tiebreak fixtures", got)
		}
		if got[0].Name != "Tiebreak Alpha" {
			t.Fatalf("Browse[0] = %#v, want the alphabetically first name", got[0])
		}
		if got[1].Reference.ExternalID != "9701" || got[2].Reference.ExternalID != "9702" {
			t.Fatalf("Browse[1:] = [%s, %s], want the identical-name pair ordered by stable identity",
				got[1].Reference.ExternalID, got[2].Reference.ExternalID)
		}
	})

	// TestDiscoverySearchPostgreSQL Step 3 item 6: a materialized result's
	// presentation (cover, type, platforms, release date) always comes from
	// the last-known-good game_igdb_enrichment snapshot, never directly from
	// whatever the currently active generation's raw provider row holds.
	t.Run("materialized presentation comes from enrichment, not the live provider row", func(t *testing.T) {
		firstGenerationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin first generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), firstGenerationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9801"}, Name: "Enrichment Source Of Truth", Slug: "enrichment-source-of-truth", GameType: "Main Game", CoverImageID: "co-original"},
		}); err != nil {
			t.Fatalf("store first generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), firstGenerationID, 1); err != nil {
			t.Fatalf("activate first generation: %v", err)
		}
		// The shared discovery instance in this test function has no
		// materializer (see its construction above); materialize directly
		// through the store, which is itself a MaterializationRepository.
		if _, err := gameStore.Materialize(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "9801"}); err != nil {
			t.Fatalf("materialize provider record: %v", err)
		}

		// A later refresh reports no cover at all for the same external ID.
		// RefreshIGDBEnrichmentFromGeneration's last-known-good merge keeps
		// the original non-empty cover in game_igdb_enrichment even though
		// the active generation's own raw row now has none.
		secondGenerationID, err := gameStore.BeginGeneration(t.Context())
		if err != nil {
			t.Fatalf("begin second generation: %v", err)
		}
		if err := gameStore.StoreProviderGames(t.Context(), secondGenerationID, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9801"}, Name: "Enrichment Source Of Truth", Slug: "enrichment-source-of-truth", GameType: "Main Game"},
		}); err != nil {
			t.Fatalf("store second generation games: %v", err)
		}
		if err := gameStore.ActivateGeneration(t.Context(), secondGenerationID, 1); err != nil {
			t.Fatalf("activate second generation: %v", err)
		}

		page, err := discovery.Browse(t.Context(), viewer.ID, "enrichment source of truth")
		if err != nil {
			t.Fatalf("Browse: %v", err)
		}
		got := page.Items
		if len(got) != 1 || !got[0].Materialized || got[0].CoverImageID != "co-original" {
			t.Fatalf("Browse = %#v, want the last-known-good cover from game_igdb_enrichment", got)
		}
	})
}

// TestDiscoverySelectPostgreSQL covers the read-only composer selection
// DiscoveryService.Select adds in Task 5: it never materializes (no
// Materialize call anywhere in this test), only reads through
// DiscoveryRepository.FindLocal/FindProvider.
func TestDiscoverySelectPostgreSQL(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	viewer, err := users.Register(t.Context(), "Select Viewer")
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	subscriptions := game.NewSubscriptionService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	// Assertion 1: local selection returns local name/slug plus retained
	// enrichment plus the current viewer's own Follow state.
	t.Run("local selection returns local name/slug, retained enrichment, and viewer Follow state", func(t *testing.T) {
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31001"}, Name: "Select Local Source", Slug: "select-local-source", GameType: "Main Game", CoverImageID: "co-select-local"},
		})
		materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "31001"})
		if err != nil {
			t.Fatalf("materialize provider record: %v", err)
		}
		if err := subscriptions.Follow(t.Context(), viewer.ID, materialized.ID); err != nil {
			t.Fatalf("follow local game: %v", err)
		}

		item, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{LocalGameID: materialized.ID})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if item.LocalGameID != materialized.ID || item.Name != materialized.Name || item.Slug != materialized.Slug {
			t.Fatalf("Select = %#v, want the local Game's own name/slug", item)
		}
		if item.CoverImageID != "co-select-local" || item.GameType != "Main Game" {
			t.Fatalf("Select = %#v, want the retained IGDB enrichment snapshot", item)
		}
		if !item.Subscribed || !item.Materialized {
			t.Fatalf("Select = %#v, want Subscribed and Materialized true for the following viewer", item)
		}

		otherViewer, err := users.Register(t.Context(), "Select Non-Follower")
		if err != nil {
			t.Fatalf("register other viewer: %v", err)
		}
		otherItem, err := discovery.Select(t.Context(), otherViewer.ID, game.GameSelection{LocalGameID: materialized.ID})
		if err != nil {
			t.Fatalf("Select (other viewer): %v", err)
		}
		if otherItem.Subscribed {
			t.Fatalf("Select (other viewer) = %#v, want Subscribed false for a non-follower", otherItem)
		}
	})

	// Assertion 2: an unmaterialized provider selection returns the
	// active-generation provider presentation and never inserts into games
	// or game_external_ids -- Select must stay read-only.
	t.Run("unmaterialized provider selection returns active-generation presentation without inserting a local Game", func(t *testing.T) {
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31002"}, Name: "Select Provider Only", Slug: "select-provider-only", GameType: "Main Game", CoverImageID: "co-select-provider"},
		})

		item, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31002"}})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if item.Materialized {
			t.Fatalf("Select = %#v, want an unmaterialized provider selection", item)
		}
		if item.LocalGameID != "" {
			t.Fatalf("Select = %#v, want no local Game ID for an unmaterialized selection", item)
		}
		if item.Name != "Select Provider Only" || item.Slug != "select-provider-only" || item.CoverImageID != "co-select-provider" {
			t.Fatalf("Select = %#v, want the active-generation provider presentation", item)
		}
		if item.Reference.ExternalID != "31002" {
			t.Fatalf("Select = %#v, want the requested provider reference", item)
		}

		var gameCount, mappingCount int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM games WHERE slug = 'select-provider-only'`).Scan(&gameCount); err != nil {
			t.Fatalf("count games: %v", err)
		}
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_external_ids WHERE provider = 'igdb' AND external_id = '31002'`).Scan(&mappingCount); err != nil {
			t.Fatalf("count external ids: %v", err)
		}
		if gameCount != 0 || mappingCount != 0 {
			t.Fatalf("gameCount=%d mappingCount=%d, want 0/0: a read-only selection must never materialize", gameCount, mappingCount)
		}
	})

	// Assertion 3: a materialized provider reference returns the linked
	// local Game's own presentation, consistently, without ever inserting a
	// second mapping.
	t.Run("materialized provider reference returns the linked local Game's presentation once", func(t *testing.T) {
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31003"}, Name: "Select Materialized Source", Slug: "select-materialized-source", GameType: "Main Game"},
		})
		materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "31003"})
		if err != nil {
			t.Fatalf("materialize provider record: %v", err)
		}

		first, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31003"}})
		if err != nil {
			t.Fatalf("Select (1st): %v", err)
		}
		second, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31003"}})
		if err != nil {
			t.Fatalf("Select (2nd): %v", err)
		}
		if !first.Materialized || first.LocalGameID != materialized.ID {
			t.Fatalf("Select (1st) = %#v, want the materialized Game %s", first, materialized.ID)
		}
		if second.LocalGameID != first.LocalGameID || second.Name != first.Name || !second.Materialized {
			t.Fatalf("Select (2nd) = %#v, want the same stable materialized result as %#v", second, first)
		}

		var mappingCount int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_external_ids WHERE provider = 'igdb' AND external_id = '31003'`).Scan(&mappingCount); err != nil {
			t.Fatalf("count external ids: %v", err)
		}
		if mappingCount != 1 {
			t.Fatalf("mappingCount = %d, want 1: Select must never insert a second mapping", mappingCount)
		}
	})

	// Assertion 4: a provider record absent from the active generation
	// reports ErrProviderRecordNotFound.
	t.Run("a provider record absent from the active generation returns ErrProviderRecordNotFound", func(t *testing.T) {
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31004"}, Name: "Select Present Sibling", Slug: "select-present-sibling", GameType: "Main Game"},
		})

		if _, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31099"}}); !errors.Is(err, game.ErrProviderRecordNotFound) {
			t.Fatalf("Select: err = %v, want %v", err, game.ErrProviderRecordNotFound)
		}
	})

	// Assertion 5: a deactivated materialized local Game must not
	// resurface as an unmaterialized provider choice -- selecting its
	// provider reference must report ErrProviderRecordNotFound, not a
	// duplicate unmaterialized card.
	t.Run("a deactivated materialized local Game is not resurrected as an unmaterialized provider choice", func(t *testing.T) {
		activateIGDBGeneration(t, gameStore, []game.ProviderGame{
			{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31005"}, Name: "Select Deactivated Source", Slug: "select-deactivated-source", GameType: "Main Game"},
		})
		materialized, err := discovery.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "31005"})
		if err != nil {
			t.Fatalf("materialize provider record: %v", err)
		}
		if err := catalog.SetActive(t.Context(), materialized.ID, false); err != nil {
			t.Fatalf("deactivate local game: %v", err)
		}

		if _, err := discovery.Select(t.Context(), viewer.ID, game.GameSelection{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "31005"}}); !errors.Is(err, game.ErrProviderRecordNotFound) {
			t.Fatalf("Select: err = %v, want %v: a deactivated materialized Game must not resurface as unmaterialized", err, game.ErrProviderRecordNotFound)
		}
	})
}

// TestDiscoveryProviderDisabledAfterSuccessfulSyncOnlyReturnsLocalGames
// covers the scenario an operator hits by removing IGDB credentials after a
// generation already synchronized successfully: the active generation is
// still sitting in PostgreSQL, so gating must happen from the
// ProviderStatusSource the composition root wires (disabledIGDBSync when no
// IGDB credentials are configured), not merely by the sync service being
// idle. It builds a second DiscoveryService over the same store, wired to a
// disabledProviderStatus fake, mirroring exactly what a restart with IGDB
// credentials removed would wire -- rather than mutating the enabled
// instance -- and doubles as Step 3 item 7's PostgreSQL coverage that
// include_provider=false returns no unmaterialized provider rows even
// though an active generation still exists.
func TestDiscoveryProviderDisabledAfterSuccessfulSyncOnlyReturnsLocalGames(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	viewer, err := users.Register(t.Context(), "Disabled Provider Viewer")
	if err != nil {
		t.Fatalf("register viewer: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	enabled := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

	local, err := catalog.Create(t.Context(), "Quest Runner Local", "quest-runner-local")
	if err != nil {
		t.Fatalf("create local game: %v", err)
	}
	activateIGDBGeneration(t, gameStore, []game.ProviderGame{
		{Reference: game.ProviderReference{Provider: "igdb", ExternalID: "9601"}, Name: "Quest Runner Provider", Slug: "quest-runner-provider", GameType: "Main Game"},
	})

	// While IGDB is still enabled, the search finds both the local game and
	// the unmaterialized provider result.
	beforePage, err := enabled.Browse(t.Context(), viewer.ID, "quest runner")
	if err != nil {
		t.Fatalf("Browse before disabling: %v", err)
	}
	before := beforePage.Items
	if len(before) != 2 {
		t.Fatalf("Browse before disabling = %#v, want the local game and the provider result", before)
	}

	// An operator removes IGDB credentials and restarts; the composition
	// root now wires a DiscoveryService over a disabled ProviderStatusSource,
	// but the previously synchronized generation is still sitting in
	// PostgreSQL untouched.
	disabled := game.NewDiscoveryService(gameStore, gameStore, gameStore, disabledProviderStatus{})

	afterPage, err := disabled.Browse(t.Context(), viewer.ID, "quest runner")
	if err != nil {
		t.Fatalf("Browse after disabling: %v", err)
	}
	after := afterPage.Items
	if len(after) != 1 || after[0].LocalGameID != local.ID || (after[0].Reference != game.ProviderReference{}) {
		t.Fatalf("Browse after disabling = %#v, want only the local game with no provider reference", after)
	}

	// Resolving/following the still-persisted provider record must also
	// fail now instead of materializing stale IGDB data.
	if _, err := disabled.Resolve(t.Context(), game.ProviderReference{Provider: "igdb", ExternalID: "9601"}); !errors.Is(err, game.ErrProviderRecordNotFound) {
		t.Fatalf("Resolve after disabling: err = %v, want %v", err, game.ErrProviderRecordNotFound)
	}
}

func TestIGDBMaterializationPostgreSQL(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})

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
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})
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
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})
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
	discovery := game.NewDiscoveryService(gameStore, gameStore, gameStore, storeProviderStatus{store: gameStore})
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

// beginRawIGDBGeneration inserts a new 'syncing' generation and points
// igdb_provider_state.latest_attempt_generation_id at it using exactly the
// statement store.BeginGeneration issues internally, but skips the Task 9
// abandoned-generation cleanup that now precedes it. It exists only so a
// test can construct two simultaneously-'syncing' generations -- a state
// store.BeginGeneration itself will no longer produce, since single-instance
// 1.0 and IGDBSyncService's in-process mutex mean that method is never
// called while a sync it started is still running.
func beginRawIGDBGeneration(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(t.Context(), `
		WITH generation AS (
		    INSERT INTO igdb_sync_generations (status)
		    VALUES ('syncing')
		    RETURNING id, started_at
		), provider_state AS (
		    UPDATE igdb_provider_state state
		    SET latest_attempt_generation_id = generation.id,
		        last_attempt_at = generation.started_at,
		        last_attempt_succeeded = NULL,
		        last_error = NULL
		    FROM generation
		    WHERE state.singleton
		    RETURNING state.singleton
		)
		SELECT generation.id::text
		FROM generation
		CROSS JOIN provider_state
	`).Scan(&id); err != nil {
		t.Fatalf("begin raw IGDB generation: %v", err)
	}
	return id
}

// storeProviderStatus adapts *gamepostgres.Store's ProviderIndexRepository.
// Status (which takes an explicit "now") to the game.ProviderStatusSource
// contract DiscoveryService consumes, exactly the shape the composition
// root's real IGDBSyncService wraps the same store with.
type storeProviderStatus struct {
	store *gamepostgres.Store
}

func (s storeProviderStatus) Status(ctx context.Context) (game.ProviderStatus, error) {
	return s.store.Status(ctx, time.Now())
}

// disabledProviderStatus always reports the provider as disabled, mirroring
// the composition root's disabledIGDBSync when no IGDB credentials are
// configured.
type disabledProviderStatus struct{}

func (disabledProviderStatus) Status(context.Context) (game.ProviderStatus, error) {
	return game.ProviderStatus{State: game.ProviderDisabled}, nil
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
	if version != 10 {
		t.Errorf("migration version = %d, want 10", version)
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
// intact regardless of the Event state they were created under: same id,
// creator, members, status, and timestamps, with their history reconstructed as
// durable activity. Legacy Event rows stay in the database.
//
// The Event state matters because the prototype list query required
// `events.is_current`. A request created under an event that was never selected,
// or under one that has since been replaced, was invisible before the migration
// and must be discoverable after it. That is the product claim the single-event
// core makes: Event selection stops being a precondition for normal behavior.
func TestForwardMigrationPreservesPrototypeGameRequests(t *testing.T) {
	tests := map[string]struct {
		current  bool
		startsAt string
		endsAt   string
	}{
		"current event":    {current: true, startsAt: "now() - interval '1 day'", endsAt: "now() + interval '1 day'"},
		"historical event": {current: false, startsAt: "now() - interval '30 days'", endsAt: "now() - interval '28 days'"},
		"no current event": {current: false, startsAt: "now() + interval '30 days'", endsAt: "now() + interval '32 days'"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newTestPool(t)
			migrationFS := productionMigrationFS(t)
			migrateToPrototypeSchema(t, pool, migrationFS)

			fixture := seedPrototypeGameRequests(t, pool, test.current, test.startsAt, test.endsAt)

			if err := migrateFS(t.Context(), pool, migrationFS, testLogger()); err != nil {
				t.Fatalf("migrate to the single-event core: %v", err)
			}

			readStore := gamerequestquery.NewStore(pool)
			preserved, err := readStore.ByID(t.Context(), fixture.openID, fixture.joinerID)
			if err != nil {
				t.Fatalf("read preserved game request: %v", err)
			}
			if preserved.CreatorID != fixture.creatorID || preserved.MemberCount != 2 || !preserved.ViewerJoined ||
				preserved.Status != gamerequest.StatusOpen || preserved.Description != "Prototype round" ||
				!preserved.CreatedAt.Equal(fixture.createdAt) {
				t.Fatalf("preserved request = %#v", preserved)
			}
			if got := activityKinds(preserved.Activity); !slices.Equal(got, []gamerequest.ActivityKind{gamerequest.ActivityJoined, gamerequest.ActivityCreated}) {
				t.Fatalf("backfilled activity = %v", got)
			}
			if !preserved.Activity[0].OccurredAt.Equal(fixture.joinedAt) || preserved.Activity[0].ActorID != fixture.joinerID {
				t.Fatalf("backfilled join entry = %#v", preserved.Activity[0])
			}

			// The point of the track: discovery no longer depends on an Event
			// being selected, so the request is listed under every Event state.
			active, err := readStore.ListActive(t.Context(), fixture.joinerID, gamerequestquery.Filter{})
			if err != nil {
				t.Fatalf("list active game requests: %v", err)
			}
			if !slices.ContainsFunc(active, func(request gamerequestquery.Request) bool { return request.ID == fixture.openID }) {
				t.Fatalf("preserved request is not discoverable under the %q fixture: %#v", name, active)
			}

			closed, err := readStore.ByID(t.Context(), fixture.closedID, fixture.creatorID)
			if err != nil {
				t.Fatalf("read preserved closed request: %v", err)
			}
			if closed.Status != gamerequest.StatusClosed {
				t.Fatalf("closed request status = %q", closed.Status)
			}
			if len(closed.Activity) == 0 || closed.Activity[0].Kind != gamerequest.ActivityClosed ||
				!closed.Activity[0].OccurredAt.Equal(fixture.updatedAt) {
				t.Fatalf("closed request activity = %#v", closed.Activity)
			}

			// Legacy Event rows are documented as kept, not silently discarded.
			var legacyEvents int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM events WHERE id = $1", fixture.eventID).Scan(&legacyEvents); err != nil {
				t.Fatalf("count legacy events: %v", err)
			}
			if legacyEvents != 1 {
				t.Fatalf("legacy event rows = %d, want 1", legacyEvents)
			}
			assertNoEventAssociation(t, pool)

			// Reconstructed history describes activity that happened before
			// there was a record of it. A notification consumer must be able to
			// tell it apart from something that just happened, or the first
			// start after an upgrade delivers the whole archive.
			var live int
			if err := pool.QueryRow(t.Context(),
				"SELECT count(*) FROM game_request_activity WHERE origin <> 'backfill'").Scan(&live); err != nil {
				t.Fatalf("count non-backfill activity: %v", err)
			}
			if live != 0 {
				t.Fatalf("migration produced %d activity rows not marked as backfill, want 0", live)
			}
		})
	}

	// A fresh installation has no prototype rows to carry forward at all.
	t.Run("fresh installation", func(t *testing.T) {
		pool := newTestPool(t)
		if err := Migrate(t.Context(), pool, testLogger()); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
		var requests, activity int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM game_requests").Scan(&requests); err != nil {
			t.Fatalf("count game requests: %v", err)
		}
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM game_request_activity").Scan(&activity); err != nil {
			t.Fatalf("count game request activity: %v", err)
		}
		if requests != 0 || activity != 0 {
			t.Fatalf("fresh installation = %d requests, %d activity entries, want 0 and 0", requests, activity)
		}
		assertNoEventAssociation(t, pool)
	})
}

// TestForwardMigrationIntroducesGameNickAndAuthFlows protects the populated
// prototype upgrade: existing sessions and issuer namespaces must survive,
// while the former public display name is deliberately not carried forward.
func TestForwardMigrationIntroducesGameNickAndAuthFlows(t *testing.T) {
	pool := newTestPool(t)
	migrationFS := productionMigrationFS(t)
	migrateToVersion(t, pool, migrationFS, 8)

	var userID string
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO users (display_name) VALUES ('Alice Example') RETURNING id::text
	`).Scan(&userID); err != nil {
		t.Fatalf("insert prototype user: %v", err)
	}
	issuer := "https://identity.example/realms/lan"
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO user_identities (user_id, provider, subject, display_name, email)
		VALUES ($1, $2, 'subject-1', 'Alice Example', 'alice@example.test')
	`, userID, issuer); err != nil {
		t.Fatalf("insert prototype identity: %v", err)
	}
	sessionHash := sha256.Sum256([]byte("pre-track-b-session"))
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO auth_sessions (id_hash, user_id, expires_at)
		VALUES ($1, $2, now() + interval '1 day')
	`, sessionHash[:], userID); err != nil {
		t.Fatalf("insert prototype session: %v", err)
	}

	if err := migrateFS(t.Context(), pool, migrationFS, testLogger()); err != nil {
		t.Fatalf("migrate to Track B schema: %v", err)
	}

	var gameNick *string
	if err := pool.QueryRow(t.Context(), `SELECT game_nick FROM users WHERE id = $1`, userID).Scan(&gameNick); err != nil {
		t.Fatalf("read upgraded game nick: %v", err)
	}
	if gameNick != nil {
		t.Fatalf("upgraded game nick = %q, want NULL", *gameNick)
	}
	var displayNameColumns int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'display_name'
	`).Scan(&displayNameColumns); err != nil {
		t.Fatalf("inspect users columns: %v", err)
	}
	if displayNameColumns != 0 {
		t.Fatal("users.display_name remains after migration")
	}

	var providerKind, providerKey, subject string
	if err := pool.QueryRow(t.Context(), `
		SELECT provider_kind, provider_key, subject FROM user_identities WHERE user_id = $1
	`, userID).Scan(&providerKind, &providerKey, &subject); err != nil {
		t.Fatalf("read upgraded identity: %v", err)
	}
	if providerKind != "oidc" || providerKey != issuer || subject != "subject-1" {
		t.Fatalf("upgraded identity = (%q, %q, %q)", providerKind, providerKey, subject)
	}
	var sessionUserID string
	if err := pool.QueryRow(t.Context(), `
		SELECT user_id::text FROM auth_sessions WHERE id_hash = $1 AND expires_at > now()
	`, sessionHash[:]).Scan(&sessionUserID); err != nil {
		t.Fatalf("resolve upgraded session: %v", err)
	}
	if sessionUserID != userID {
		t.Fatalf("upgraded session user = %q, want %q", sessionUserID, userID)
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO users (game_nick) VALUES ('NightOtter')`); err != nil {
		t.Fatalf("insert game nick: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO users (game_nick) VALUES ('nightotter')`); err == nil {
		t.Fatal("case-insensitive duplicate game nick unexpectedly succeeded")
	}

	browserHash := sha256.Sum256([]byte("browser-binding"))
	stateHash := sha256.Sum256([]byte("oauth-state"))
	var flowID string
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO auth_flows (
			phase, operation, provider_kind, provider_key, state_hash, browser_binding_hash,
			nonce, pkce_verifier, expires_at
		) VALUES ('oauth', 'login', 'oidc', $1, $2, $3, 'nonce', 'verifier', now() + interval '10 minutes')
		RETURNING id::text
	`, issuer, stateHash[:], browserHash[:]).Scan(&flowID); err != nil {
		t.Fatalf("create oauth flow: %v", err)
	}
	var consumed string
	if err := pool.QueryRow(t.Context(), `
		UPDATE auth_flows SET consumed_at = now(), nonce = NULL, pkce_verifier = NULL
		WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING id::text
	`, flowID).Scan(&consumed); err != nil || consumed != flowID {
		t.Fatalf("consume oauth flow = %q, %v", consumed, err)
	}
	if err := pool.QueryRow(t.Context(), `
		UPDATE auth_flows SET consumed_at = now(), nonce = NULL, pkce_verifier = NULL
		WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
		RETURNING id::text
	`, flowID).Scan(&consumed); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("replay flow error = %v, want no rows", err)
	}
}

type prototypeFixture struct {
	eventID, creatorID, joinerID, openID, closedID string
	createdAt, joinedAt, updatedAt                 time.Time
}

// migrateToPrototypeSchema stops at version 5, the last schema in which a Game
// Request still belongs to an Event.
func migrateToPrototypeSchema(t *testing.T, pool *pgxpool.Pool, migrationFS fs.FS) {
	migrateToVersion(t, pool, migrationFS, 5)
}

func migrateToVersion(t *testing.T, pool *pgxpool.Pool, migrationFS fs.FS, version int64) {
	t.Helper()

	database := stdlib.OpenDBFromPool(pool)
	defer database.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, database, migrationFS, goose.WithSlog(testLogger()))
	if err != nil {
		t.Fatalf("create goose provider: %v", err)
	}
	if _, err := provider.UpTo(t.Context(), version); err != nil {
		t.Fatalf("migrate to schema version %d: %v", version, err)
	}
}

func seedPrototypeGameRequests(t *testing.T, pool *pgxpool.Pool, current bool, startsAt, endsAt string) prototypeFixture {
	t.Helper()

	var fixture prototypeFixture
	if err := pool.QueryRow(t.Context(), `
		WITH inserted_event AS (
			INSERT INTO events (name, starts_at, ends_at, is_current)
			VALUES ('Prototype LAN', `+startsAt+`, `+endsAt+`, $1)
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
		SELECT inserted_event.id::text, inserted_creator.id::text,
		       inserted_joiner.id::text, open_request.id::text, closed_request.id::text,
		       open_request.created_at, joiner_membership.joined_at, closed_request.updated_at
		FROM inserted_event, inserted_game, inserted_creator, inserted_joiner,
		     open_request, closed_request, creator_membership, joiner_membership, closed_membership
	`, current).Scan(&fixture.eventID, &fixture.creatorID, &fixture.joinerID,
		&fixture.openID, &fixture.closedID,
		&fixture.createdAt, &fixture.joinedAt, &fixture.updatedAt); err != nil {
		t.Fatalf("seed prototype data: %v", err)
	}
	return fixture
}

func assertNoEventAssociation(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

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
	// Everything written while the application is running is deliverable work;
	// only the migration's reconstruction is not.
	var backfilled int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM game_request_activity WHERE origin <> 'live'").Scan(&backfilled); err != nil {
		t.Fatalf("count backfilled activity: %v", err)
	}
	if backfilled != 0 {
		t.Fatalf("%d rows written at runtime are marked as backfill, want 0", backfilled)
	}
	// Leaving removed the membership but not the history of the round.
	if detail.MemberCount != 1 {
		t.Fatalf("member count after leave = %d, want 1", detail.MemberCount)
	}
	if detail.Activity[1].ActorGameNick != "Activity Guest" || detail.Activity[1].ActorID != guest.ID {
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
	followed, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Scope: gamerequestquery.ScopeMyGames})
	if err != nil || len(followed) != 1 || followed[0].ID != open.ID {
		t.Fatalf("My Games scope = %#v, %v", followed, err)
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
	// Joined and Created by me describe the viewer's own relationship to a
	// request, so the same rows answer differently for two players. The host
	// created both and is their first member; the guest has joined one and
	// created none.
	if err := requests.Join(t.Context(), open.ID, guest.ID); err != nil {
		t.Fatalf("join open request: %v", err)
	}
	joinedByGuest, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Scope: gamerequestquery.ScopeJoined})
	if err != nil || len(joinedByGuest) != 1 || joinedByGuest[0].ID != open.ID {
		t.Fatalf("Joined scope for the guest = %#v, %v", joinedByGuest, err)
	}
	if mineForGuest, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Scope: gamerequestquery.ScopeMine}); err != nil || len(mineForGuest) != 0 {
		t.Fatalf("Created by me scope returned requests the guest did not create: %#v, %v", mineForGuest, err)
	}
	mineForHost, err := readStore.ListActive(t.Context(), host.ID, gamerequestquery.Filter{Scope: gamerequestquery.ScopeMine})
	if err != nil || len(mineForHost) != 2 {
		t.Fatalf("Created by me scope for the host = %#v, %v", mineForHost, err)
	}
	// Scope narrows the audience and free places narrows capacity, so the two
	// combine rather than overriding each other.
	mineWithPlaces, err := readStore.ListActive(t.Context(), host.ID, gamerequestquery.Filter{Scope: gamerequestquery.ScopeMine, OpenPlacesOnly: true})
	if err != nil || len(mineWithPlaces) != 1 || mineWithPlaces[0].ID != open.ID {
		t.Fatalf("Created by me with free places = %#v, %v", mineWithPlaces, err)
	}
	// An unknown scope widens to the default view instead of returning nothing.
	if all, err := readStore.ListActive(t.Context(), guest.ID, gamerequestquery.Filter{Scope: gamerequestquery.ParseScope("nonsense")}); err != nil || len(all) != 2 {
		t.Fatalf("unknown scope = %#v, %v", all, err)
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

// A committed Game Request change keeps its notification intent across a
// restart that happens before anything delivered it. This is the half of the
// A/E contract that the rollback case cannot show: the rollback proves nothing
// survives a failure, and this proves everything survives a process ending.
//
// The restart is real rather than simulated. The pool that wrote the activity
// is closed, and a second pool opens against the same schema, so nothing the
// first connection held in memory can satisfy the read.
func TestCommittedActivitySurvivesRestartBeforeDelivery(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Restart Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Restart Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Unreal Tournament", "unreal-tournament")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	created, err := requests.Create(t.Context(), host.ID, selectedGame.ID, nil, time.Now().UTC().Add(time.Hour), "Survives a restart")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	if err := requests.Join(t.Context(), created.ID, guest.ID); err != nil {
		t.Fatalf("join game request: %v", err)
	}
	if err := requests.Close(t.Context(), created.ID, host.ID); err != nil {
		t.Fatalf("close game request: %v", err)
	}

	schemaName := currentSchema(t, pool)
	before := readActivityCursor(t, pool, created.ID)
	if len(before) != 3 {
		t.Fatalf("activity before restart = %#v, want 3 entries", before)
	}

	// The process ends here, with the intent recorded and nothing delivered.
	pool.Close()
	restarted := reopenTestPool(t, schemaName)

	after := readActivityCursor(t, restarted, created.ID)
	if !slices.Equal(before, after) {
		t.Fatalf("activity after restart = %#v, want %#v", after, before)
	}
	// Track E replays from `sequence`, so the order and the cursor values
	// themselves have to be the same ones it would have seen before the restart.
	for index := 1; index < len(after); index++ {
		if after[index].sequence <= after[index-1].sequence {
			t.Fatalf("activity sequence is not monotonic after restart: %#v", after)
		}
	}
	detail, err := gamerequestquery.NewStore(restarted).ByID(t.Context(), created.ID, host.ID)
	if err != nil {
		t.Fatalf("read game request after restart: %v", err)
	}
	wantKinds := []gamerequest.ActivityKind{gamerequest.ActivityClosed, gamerequest.ActivityJoined, gamerequest.ActivityCreated}
	if got := activityKinds(detail.Activity); !slices.Equal(got, wantKinds) {
		t.Fatalf("activity kinds after restart = %v, want %v", got, wantKinds)
	}
}

type activityCursor struct {
	sequence int64
	kind     string
	actorID  string
}

func readActivityCursor(t *testing.T, pool *pgxpool.Pool, requestID string) []activityCursor {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT sequence, kind, actor_id::text
		FROM game_request_activity
		WHERE game_request_id = $1::uuid
		ORDER BY sequence
	`, requestID)
	if err != nil {
		t.Fatalf("read activity cursor: %v", err)
	}
	defer rows.Close()

	var entries []activityCursor
	for rows.Next() {
		var entry activityCursor
		if err := rows.Scan(&entry.sequence, &entry.kind, &entry.actorID); err != nil {
			t.Fatalf("scan activity cursor: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate activity cursor: %v", err)
	}
	return entries
}

func currentSchema(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var schemaName string
	if err := pool.QueryRow(t.Context(), "SELECT current_schema()").Scan(&schemaName); err != nil {
		t.Fatalf("read current schema: %v", err)
	}
	return schemaName
}

// reopenTestPool opens a second pool against an existing test schema, standing
// in for the application starting again over the same database.
func reopenTestPool(t *testing.T, schemaName string) *pgxpool.Pool {
	t.Helper()

	poolConfig, err := pgxpool.ParseConfig(os.Getenv("LANSURFER_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	identifier := pgx.Identifier{schemaName}.Sanitize()
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET search_path TO "+identifier+", public")
		return err
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatalf("reopen test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Leaving removes current membership but not the record of having been there.
// Issue #14 names join -> leave -> rejoin as the case that separates the two:
// the player is a member once, and the history holds both joins and the leave.
func TestGameRequestActivityKeepsHistoryAcrossRejoin(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Rejoin Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Rejoin Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selectedGame, err := catalog.Create(t.Context(), "Trackmania", "trackmania")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	readStore := gamerequestquery.NewStore(pool)
	created, err := requests.Create(t.Context(), host.ID, selectedGame.ID, nil, time.Now().UTC().Add(time.Hour), "Rejoinable round")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}

	if err := requests.Join(t.Context(), created.ID, guest.ID); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if err := requests.Leave(t.Context(), created.ID, guest.ID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if err := requests.Join(t.Context(), created.ID, guest.ID); err != nil {
		t.Fatalf("rejoin: %v", err)
	}

	detail, err := readStore.ByID(t.Context(), created.ID, guest.ID)
	if err != nil {
		t.Fatalf("read rejoined request: %v", err)
	}
	want := []gamerequest.ActivityKind{
		gamerequest.ActivityJoined, gamerequest.ActivityLeft,
		gamerequest.ActivityJoined, gamerequest.ActivityCreated,
	}
	if got := activityKinds(detail.Activity); !slices.Equal(got, want) {
		t.Fatalf("activity = %v, want %v", got, want)
	}
	if detail.MemberCount != 2 || !detail.ViewerJoined {
		t.Fatalf("after rejoin member count = %d, viewer joined = %t, want 2 and true", detail.MemberCount, detail.ViewerJoined)
	}
	appearances := 0
	for _, member := range detail.Members {
		if member.ID == guest.ID {
			appearances++
		}
	}
	if appearances != 1 {
		t.Fatalf("rejoined player appears %d times in the member list, want 1", appearances)
	}
	// The rejoin is a new entry rather than the old one resurfacing.
	if detail.Activity[0].OccurredAt.Before(detail.Activity[1].OccurredAt) {
		t.Fatalf("rejoin entry is older than the leave it followed: %#v", detail.Activity[:2])
	}
}

// The navigation's sidebar reads three things PostgreSQL has to get right:
// reading history is per-viewer and deduplicated by request, the counts state
// the active catalog rather than every row ever written, and a deleted Game
// Request takes its view rows with it instead of leaving the sidebar pointing
// at a request that no longer exists.
func TestNavigationReadingHistoryAndCountsPersistence(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	reader, err := users.Register(t.Context(), "History Reader")
	if err != nil {
		t.Fatalf("register reader: %v", err)
	}
	other, err := users.Register(t.Context(), "Other Player")
	if err != nil {
		t.Fatalf("register other player: %v", err)
	}

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	subscriptions := game.NewSubscriptionService(gameStore)
	warcraft, err := catalog.Create(t.Context(), "Warcraft III", "warcraft-3")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	quake, err := catalog.Create(t.Context(), "Quake III Arena", "quake-3-arena")
	if err != nil {
		t.Fatalf("create second game: %v", err)
	}
	retired, err := catalog.Create(t.Context(), "Retired Game", "retired-game")
	if err != nil {
		t.Fatalf("create third game: %v", err)
	}

	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	queries := gamerequestquery.NewStore(pool)

	first, err := requests.Create(t.Context(), reader.ID, warcraft.ID, nil, time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatalf("create first request: %v", err)
	}
	second, err := requests.Create(t.Context(), reader.ID, quake.ID, nil, time.Now().Add(2*time.Hour), "")
	if err != nil {
		t.Fatalf("create second request: %v", err)
	}

	// Re-reading a request moves it back to the top rather than appending a
	// second entry, so the list stays a set of distinct requests.
	for _, view := range []struct{ requestID, userID string }{
		{second.ID, reader.ID},
		{first.ID, reader.ID},
		{second.ID, reader.ID},
		{first.ID, other.ID},
	} {
		if err := requests.RecordView(t.Context(), view.requestID, view.userID); err != nil {
			t.Fatalf("record view: %v", err)
		}
	}

	history, err := queries.RecentlyViewed(t.Context(), reader.ID)
	if err != nil {
		t.Fatalf("recently viewed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("recently viewed = %#v, want 2 distinct requests", history)
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Fatalf("recently viewed order = %q, %q, want newest view first", history[0].ID, history[1].ID)
	}
	if history[0].GameName != "Quake III Arena" || history[0].Status != gamerequest.StatusOpen {
		t.Fatalf("recently viewed entry = %#v", history[0])
	}

	// History is private: one player's reading never appears in another's.
	otherHistory, err := queries.RecentlyViewed(t.Context(), other.ID)
	if err != nil || len(otherHistory) != 1 || otherHistory[0].ID != first.ID {
		t.Fatalf("other player history = %#v, %v", otherHistory, err)
	}

	// A closed request stays in history: it is still what the player read.
	if err := requests.Close(t.Context(), first.ID, reader.ID); err != nil {
		t.Fatalf("close request: %v", err)
	}
	history, err = queries.RecentlyViewed(t.Context(), reader.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history after close = %#v, %v", history, err)
	}
	if history[1].ID != first.ID || history[1].Status != gamerequest.StatusClosed {
		t.Fatalf("closed history entry = %#v", history[1])
	}

	if err := subscriptions.Follow(t.Context(), reader.ID, warcraft.ID); err != nil {
		t.Fatalf("follow game: %v", err)
	}
	total, err := subscriptions.CountCatalog(t.Context())
	if err != nil || total != 3 {
		t.Fatalf("catalog count = %d, %v, want 3", total, err)
	}
	followed, err := subscriptions.CountFollowed(t.Context(), reader.ID)
	if err != nil || followed != 1 {
		t.Fatalf("followed count = %d, %v, want 1", followed, err)
	}
	if followed, err := subscriptions.CountFollowed(t.Context(), other.ID); err != nil || followed != 0 {
		t.Fatalf("other player followed count = %d, %v, want 0", followed, err)
	}

	// Both counts state the active catalog, matching what Games and My Games
	// themselves list: deactivating a Game removes it from both.
	if err := subscriptions.Follow(t.Context(), reader.ID, retired.ID); err != nil {
		t.Fatalf("follow third game: %v", err)
	}
	if err := catalog.SetActive(t.Context(), retired.ID, false); err != nil {
		t.Fatalf("deactivate game: %v", err)
	}
	if total, err := subscriptions.CountCatalog(t.Context()); err != nil || total != 2 {
		t.Fatalf("catalog count after deactivation = %d, %v, want 2", total, err)
	}
	if followed, err := subscriptions.CountFollowed(t.Context(), reader.ID); err != nil || followed != 1 {
		t.Fatalf("followed count after deactivation = %d, %v, want 1", followed, err)
	}

	// Views belong to their Game Request: deleting one must not leave the
	// sidebar linking to a request that is gone.
	if _, err := pool.Exec(t.Context(), "DELETE FROM game_request_activity WHERE game_request_id = $1", second.ID); err != nil {
		t.Fatalf("delete activity: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "DELETE FROM game_requests WHERE id = $1", second.ID); err != nil {
		t.Fatalf("delete request: %v", err)
	}
	history, err = queries.RecentlyViewed(t.Context(), reader.ID)
	if err != nil || len(history) != 1 || history[0].ID != first.ID {
		t.Fatalf("history after request deletion = %#v, %v", history, err)
	}

	// A view of a request that does not exist is rejected by the foreign key
	// rather than silently stored.
	if err := requests.RecordView(t.Context(), second.ID, reader.ID); !errors.Is(err, gamerequest.ErrNotFound) {
		t.Fatalf("record view of deleted request = %v, want ErrNotFound", err)
	}
}
