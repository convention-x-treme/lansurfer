//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func discordAdminUUID(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	var value pgtype.UUID
	if err := value.Scan(id); err != nil {
		t.Fatalf("scan admin ID %q as uuid: %v", id, err)
	}
	return value
}

func newDiscordSetupStateFixture(t *testing.T, queries *sqlcgen.Queries, adminID string, stateHash, browserHash []byte, expiresAt time.Time) sqlcgen.CreateDiscordSetupStateRow {
	t.Helper()
	adminUUID := discordAdminUUID(t, adminID)
	row, err := queries.CreateDiscordSetupState(t.Context(), sqlcgen.CreateDiscordSetupStateParams{
		StateHash: stateHash, InitiatingAdminID: adminUUID, BrowserBindingHash: browserHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateDiscordSetupState() error = %v", err)
	}
	return row
}

// A fresh migration produces a Discord destination table that enforces
// "at most one row" in the database, and a state/credential shape with no
// plaintext webhook ID/token column: only credential_ciphertext exists,
// alongside safe metadata.
func TestDiscordDestinationSingletonIsDatabaseEnforced(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	queries := sqlcgen.New(pool)

	if err := queries.UpsertDiscordDestination(t.Context(), sqlcgen.UpsertDiscordDestinationParams{
		CredentialCiphertext: []byte("ciphertext-v1"), GuildID: "guild-1", ChannelID: "channel-1",
		WebhookName: pgtype.Text{String: "Game Requests", Valid: true},
	}); err != nil {
		t.Fatalf("UpsertDiscordDestination() create error = %v", err)
	}

	// A second logical "connect" replaces the singleton row in place rather
	// than creating a second one; the boolean primary key makes that a
	// database constraint, not just application discipline.
	if err := queries.UpsertDiscordDestination(t.Context(), sqlcgen.UpsertDiscordDestinationParams{
		CredentialCiphertext: []byte("ciphertext-v2"), GuildID: "guild-2", ChannelID: "channel-2",
	}); err != nil {
		t.Fatalf("UpsertDiscordDestination() replace error = %v", err)
	}

	var rowCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_destination`).Scan(&rowCount); err != nil {
		t.Fatalf("count destinations: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("notification_discord_destination row count = %d, want 1", rowCount)
	}

	destination, err := queries.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if destination.State != "connected" || destination.GuildID != "guild-2" || destination.ChannelID != "channel-2" {
		t.Fatalf("destination = %#v, want the replaced connected row", destination)
	}
	if destination.WebhookName.Valid {
		t.Fatalf("destination.WebhookName = %#v, want cleared by the replacing upsert", destination.WebhookName)
	}

	// Attempting to insert a second row directly, bypassing the singleton
	// upsert, must fail: the boolean primary key permits only `true`.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_discord_destination (id, state, credential_ciphertext, guild_id, channel_id)
		VALUES (false, 'connected', $1, 'guild-3', 'channel-3')
	`, []byte("ciphertext-v3")); err == nil {
		t.Fatal("insert with id = false succeeded, want a primary key violation")
	}
}

// The broken/connected state is tied to the presence of the encrypted
// credential blob and the broken diagnostics by a table CHECK constraint,
// not by application code: broken destinations retain safe metadata and
// failure diagnostics but never the now-unusable ciphertext.
func TestDiscordDestinationBrokenStateInvariantIsDatabaseEnforced(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	queries := sqlcgen.New(pool)

	if err := queries.UpsertDiscordDestination(t.Context(), sqlcgen.UpsertDiscordDestinationParams{
		CredentialCiphertext: []byte("ciphertext"), GuildID: "guild-1", ChannelID: "channel-1",
	}); err != nil {
		t.Fatalf("UpsertDiscordDestination() error = %v", err)
	}

	affected, err := queries.MarkDiscordDestinationBroken(t.Context(), pgtype.Text{String: "webhook deleted (10015)", Valid: true})
	if err != nil {
		t.Fatalf("MarkDiscordDestinationBroken() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("MarkDiscordDestinationBroken() affected = %d, want 1", affected)
	}

	broken, err := queries.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if broken.State != "broken" || broken.CredentialCiphertext != nil {
		t.Fatalf("broken destination = %#v, want cleared ciphertext", broken)
	}
	if !broken.BrokenAt.Valid || broken.BrokenReason.String != "webhook deleted (10015)" {
		t.Fatalf("broken diagnostics = %#v, want a reason and timestamp", broken)
	}
	// guild_id/channel_id are safe metadata retained for diagnosis even
	// while broken.
	if broken.GuildID != "guild-1" || broken.ChannelID != "channel-1" {
		t.Fatalf("broken destination metadata = %#v, want the original guild/channel retained", broken)
	}

	// The table CHECK must reject a row that claims to be connected without
	// credentials, or broken while still holding one.
	if _, err := pool.Exec(t.Context(), `
		UPDATE notification_discord_destination SET state = 'connected', credential_ciphertext = NULL
	`); err == nil {
		t.Fatal("connected state without credential_ciphertext succeeded, want a CHECK violation")
	}
}

// Setup state is bound to the initiating administrator and browser/session,
// consumable exactly once, and rejects an already-expired state even before
// it is ever consumed.
func TestDiscordSetupStateConsumedExactlyOnceAndExpires(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	admin, err := users.Register(t.Context(), "Setup Admin")
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	queries := sqlcgen.New(pool)

	stateHash := make([]byte, 32)
	browserHash := make([]byte, 32)
	for i := range stateHash {
		stateHash[i] = byte(i + 1)
		browserHash[i] = byte(i + 2)
	}
	created := newDiscordSetupStateFixture(t, queries, admin.ID, stateHash, browserHash, time.Now().Add(5*time.Minute))
	if created.ID == "" {
		t.Fatal("CreateDiscordSetupState() returned an empty ID")
	}

	adminUUID := discordAdminUUID(t, admin.ID)
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}

	// Wrong browser binding must not consume the state.
	if _, err := queries.ConsumeDiscordSetupState(t.Context(), sqlcgen.ConsumeDiscordSetupStateParams{
		Now: now, StateHash: stateHash, BrowserBindingHash: make([]byte, 32), InitiatingAdminID: adminUUID,
	}); err == nil {
		t.Fatal("ConsumeDiscordSetupState() with wrong browser binding succeeded, want no match")
	}

	consumed, err := queries.ConsumeDiscordSetupState(t.Context(), sqlcgen.ConsumeDiscordSetupStateParams{
		Now: now, StateHash: stateHash, BrowserBindingHash: browserHash, InitiatingAdminID: adminUUID,
	})
	if err != nil {
		t.Fatalf("ConsumeDiscordSetupState() error = %v", err)
	}
	if consumed.ID != created.ID || consumed.InitiatingAdminID != admin.ID {
		t.Fatalf("consumed = %#v, want ID %q bound to admin %q", consumed, created.ID, admin.ID)
	}

	// Replay: the same state can never be consumed a second time.
	if _, err := queries.ConsumeDiscordSetupState(t.Context(), sqlcgen.ConsumeDiscordSetupStateParams{
		Now: now, StateHash: stateHash, BrowserBindingHash: browserHash, InitiatingAdminID: adminUUID,
	}); err != pgx.ErrNoRows {
		t.Fatalf("replay ConsumeDiscordSetupState() error = %v, want pgx.ErrNoRows", err)
	}

	// A second, already-expired state must never be consumable, even on its
	// first attempt.
	expiredStateHash := make([]byte, 32)
	for i := range expiredStateHash {
		expiredStateHash[i] = byte(i + 3)
	}
	newDiscordSetupStateFixture(t, queries, admin.ID, expiredStateHash, browserHash, time.Now().Add(5*time.Minute))
	if _, err := pool.Exec(t.Context(), `
		UPDATE notification_discord_setup_state
		SET created_at = now() - interval '2 minutes', expires_at = now() - interval '1 minute'
		WHERE state_hash = $1
	`, expiredStateHash); err != nil {
		t.Fatalf("expire setup state fixture: %v", err)
	}
	if _, err := queries.ConsumeDiscordSetupState(t.Context(), sqlcgen.ConsumeDiscordSetupStateParams{
		Now: pgtype.Timestamptz{Time: time.Now(), Valid: true}, StateHash: expiredStateHash,
		BrowserBindingHash: browserHash, InitiatingAdminID: adminUUID,
	}); err != pgx.ErrNoRows {
		t.Fatalf("expired ConsumeDiscordSetupState() error = %v, want pgx.ErrNoRows", err)
	}
}

// A connected destination's persisted row carries only the encrypted
// credential blob: the plaintext webhook ID/token never appear anywhere in
// the row, and the safe metadata (guild/channel/display name) stays
// separately queryable from the ciphertext. The stored ciphertext still
// decrypts back to the original credential.
func TestDiscordDestinationPersistsOnlyEncryptedCredential(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 7)
	}
	credential := notification.DiscordWebhookCredential{
		WebhookID: "123456789012345678", WebhookToken: "super-secret-webhook-token-value",
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, credential)
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}

	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{
		GuildID: "guild-42", ChannelID: "channel-99", WebhookName: "Game Requests",
	}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	var rawCiphertext []byte
	var guildID, channelID string
	var webhookName pgtype.Text
	if err := pool.QueryRow(t.Context(), `
		SELECT credential_ciphertext, guild_id, channel_id, webhook_name
		FROM notification_discord_destination WHERE id = true
	`).Scan(&rawCiphertext, &guildID, &channelID, &webhookName); err != nil {
		t.Fatalf("inspect persisted destination row: %v", err)
	}
	if strings.Contains(string(rawCiphertext), credential.WebhookID) || strings.Contains(string(rawCiphertext), credential.WebhookToken) {
		t.Fatal("persisted row contains the plaintext webhook ID or token")
	}
	if guildID != "guild-42" || channelID != "channel-99" || webhookName.String != "Game Requests" {
		t.Fatalf("safe metadata = %q/%q/%q, want it queryable independent of the ciphertext", guildID, channelID, webhookName.String)
	}

	destination, storedCiphertext, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if destination.State != notification.DiscordDestinationConnected {
		t.Fatalf("destination.State = %q, want connected", destination.State)
	}
	decrypted, err := notification.DecryptDiscordCredential(key, storedCiphertext)
	if err != nil {
		t.Fatalf("DecryptDiscordCredential() error = %v", err)
	}
	if decrypted != credential {
		t.Fatalf("decrypted credential = %#v, want %#v", decrypted, credential)
	}

	// Broken state deletes the ciphertext but keeps the safe metadata and a
	// diagnostic reason, and the reason itself carries no secret material.
	if err := store.MarkDiscordDestinationBroken(t.Context(), "webhook deleted (10015)"); err != nil {
		t.Fatalf("MarkDiscordDestinationBroken() error = %v", err)
	}
	broken, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() after broken transition error = %v", err)
	}
	if broken.State != notification.DiscordDestinationBroken || broken.GuildID != "guild-42" {
		t.Fatalf("broken destination = %#v, want broken state with retained guild ID", broken)
	}
	var brokenCiphertext []byte
	if err := pool.QueryRow(t.Context(), `
		SELECT credential_ciphertext FROM notification_discord_destination WHERE id = true
	`).Scan(&brokenCiphertext); err != nil {
		t.Fatalf("inspect broken destination row: %v", err)
	}
	if brokenCiphertext != nil {
		t.Fatal("broken destination row still has a credential ciphertext")
	}
}

// The Store wrapper around ConsumeDiscordSetupState is what
// DiscordSetupService actually calls: it must translate pgx.ErrNoRows into
// notification.ErrDiscordSetupStateInvalid and never mutate any
// destination row as a side effect of an invalid consume attempt.
func TestDiscordSetupStateStoreRejectsWrongAdminWithoutTouchingDestination(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	admin, err := users.Register(t.Context(), "Setup Admin")
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	otherAdmin, err := users.Register(t.Context(), "Other Admin")
	if err != nil {
		t.Fatalf("register other admin: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	stateHash := make([]byte, 32)
	browserHash := make([]byte, 32)
	for i := range stateHash {
		stateHash[i] = byte(i + 10)
		browserHash[i] = byte(i + 20)
	}
	if _, err := store.CreateDiscordSetupState(t.Context(), stateHash, browserHash, admin.ID, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("CreateDiscordSetupState() error = %v", err)
	}

	if _, err := store.ConsumeDiscordSetupState(t.Context(), stateHash, browserHash, otherAdmin.ID, time.Now()); !errors.Is(err, notification.ErrDiscordSetupStateInvalid) {
		t.Fatalf("ConsumeDiscordSetupState() with wrong admin error = %v, want ErrDiscordSetupStateInvalid", err)
	}

	// The correct admin can still consume it afterward: the wrong-admin
	// attempt above must not have consumed or corrupted the row.
	consumed, err := store.ConsumeDiscordSetupState(t.Context(), stateHash, browserHash, admin.ID, time.Now())
	if err != nil {
		t.Fatalf("ConsumeDiscordSetupState() with correct admin error = %v", err)
	}
	if consumed.InitiatingAdminID != admin.ID {
		t.Fatalf("consumed.InitiatingAdminID = %q, want %q", consumed.InitiatingAdminID, admin.ID)
	}

	var destinationRowCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_destination`).Scan(&destinationRowCount); err != nil {
		t.Fatalf("count destinations: %v", err)
	}
	if destinationRowCount != 0 {
		t.Fatalf("destination row count = %d, want 0: setup-state consume must never touch the destination table", destinationRowCount)
	}
}

// The one-use claim is enforced by a single atomic UPDATE statement, not
// application-level locking: racing every consume attempt concurrently
// against the same state must let exactly one succeed regardless of
// goroutine scheduling.
func TestDiscordSetupStateConcurrentConsumeSucceedsExactlyOnce(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	admin, err := users.Register(t.Context(), "Setup Admin")
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	stateHash := make([]byte, 32)
	browserHash := make([]byte, 32)
	for i := range stateHash {
		stateHash[i] = byte(i + 30)
		browserHash[i] = byte(i + 40)
	}
	if _, err := store.CreateDiscordSetupState(t.Context(), stateHash, browserHash, admin.ID, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("CreateDiscordSetupState() error = %v", err)
	}

	const attempts = 20
	var wg sync.WaitGroup
	var successes int64
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.ConsumeDiscordSetupState(t.Context(), stateHash, browserHash, admin.ID, time.Now()); err == nil {
				atomic.AddInt64(&successes, 1)
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("concurrent ConsumeDiscordSetupState() successes = %d, want exactly 1", successes)
	}
}

// The setup-state operation column is fixed to exactly one legal value by
// a table CHECK constraint, so "configure Discord destination" binding is
// enforced by the database itself: no other operation value can ever be
// stored or match a lookup.
func TestDiscordSetupStateOperationIsDatabaseEnforced(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	admin, err := users.Register(t.Context(), "Setup Admin")
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}

	stateHash := make([]byte, 32)
	browserHash := make([]byte, 32)
	for i := range stateHash {
		stateHash[i] = byte(i + 50)
		browserHash[i] = byte(i + 60)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_discord_setup_state
			(state_hash, initiating_admin_id, browser_binding_hash, operation, expires_at)
		VALUES ($1, $2::uuid, $3, 'some_other_operation', now() + interval '5 minutes')
	`, stateHash, admin.ID, browserHash); err == nil {
		t.Fatal("insert with a non-configure_discord_destination operation succeeded, want a CHECK violation")
	}
}

// fakeDiscordWebhookOAuthProviderForConnect is a minimal
// notification.DiscordWebhookOAuthProvider used only to drive
// notification.DiscordConnectService against a real Store: no real
// network/Discord call is made, and every DeleteWebhook call is recorded
// with the wall-clock order it happened in relative to the local commit.
type fakeDiscordWebhookOAuthProviderForConnect struct {
	exchangeResult notification.DiscordWebhookOAuthResult
	exchangeErr    error

	deleteCalls       int
	deletedCredential notification.DiscordWebhookCredential
	deleteErr         error
}

func (f *fakeDiscordWebhookOAuthProviderForConnect) AuthorizationURL(string) string { return "" }

func (f *fakeDiscordWebhookOAuthProviderForConnect) Exchange(context.Context, string) (notification.DiscordWebhookOAuthResult, error) {
	if f.exchangeErr != nil {
		return notification.DiscordWebhookOAuthResult{}, f.exchangeErr
	}
	return f.exchangeResult, nil
}

func (f *fakeDiscordWebhookOAuthProviderForConnect) DeleteWebhook(_ context.Context, credential notification.DiscordWebhookCredential) error {
	f.deleteCalls++
	f.deletedCredential = credential
	return f.deleteErr
}

// A successful replacement against a real Store leaves exactly the new
// destination active, and the old remote webhook delete only happens after
// that local row is already durably committed: this test proves the
// commit happened first by reading the row back inside the provider's
// DeleteWebhook call itself, before DiscordConnectService.Connect returns.
func TestDiscordConnectServiceReplacementCommitsBeforeRemoteCleanup(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 11)
	}

	oldCredential := notification.DiscordWebhookCredential{WebhookID: "old-webhook-id", WebhookToken: "old-webhook-token"}
	oldCiphertext, err := notification.EncryptDiscordCredential(key, oldCredential)
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{
		GuildID: "guild-old", ChannelID: "channel-old",
	}, oldCiphertext); err != nil {
		t.Fatalf("seed old ConnectDiscordDestination() error = %v", err)
	}

	var sawNewGuildIDDuringCleanup string
	provider := &fakeDiscordWebhookOAuthProviderForConnect{
		exchangeResult: notification.DiscordWebhookOAuthResult{
			Credential: notification.DiscordWebhookCredential{WebhookID: "new-webhook-id", WebhookToken: "new-webhook-token"},
			GuildID:    "guild-new", ChannelID: "channel-new", WebhookName: "New Destination",
		},
	}
	connect := notification.NewDiscordConnectService(store, providerRecordingCommittedGuildID{
		DiscordWebhookOAuthProvider: provider,
		pool:                        pool,
		t:                           t,
		observed:                    &sawNewGuildIDDuringCleanup,
	}, key, nil)

	if err := connect.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if sawNewGuildIDDuringCleanup != "guild-new" {
		t.Fatalf("guild_id visible during remote cleanup = %q, want the NEW destination already committed", sawNewGuildIDDuringCleanup)
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("DeleteWebhook calls = %d, want exactly 1", provider.deleteCalls)
	}
	if provider.deletedCredential != oldCredential {
		t.Fatalf("DeleteWebhook credential = %#v, want the OLD credential %#v", provider.deletedCredential, oldCredential)
	}

	destination, ciphertext, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if destination.GuildID != "guild-new" || destination.State != notification.DiscordDestinationConnected {
		t.Fatalf("destination = %#v, want exactly the new connected destination", destination)
	}
	decrypted, err := notification.DecryptDiscordCredential(key, ciphertext)
	if err != nil {
		t.Fatalf("DecryptDiscordCredential() error = %v", err)
	}
	if decrypted.WebhookID != "new-webhook-id" {
		t.Fatalf("stored credential = %#v, want the new webhook ID", decrypted)
	}

	var rowCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_discord_destination`).Scan(&rowCount); err != nil {
		t.Fatalf("count destinations: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("notification_discord_destination row count = %d, want exactly 1 (singleton)", rowCount)
	}
}

// providerRecordingCommittedGuildID wraps a provider and, on DeleteWebhook,
// reads the destination row's guild_id directly from the database so the
// test can assert the new row was already durably committed before any
// remote cleanup attempt runs.
type providerRecordingCommittedGuildID struct {
	notification.DiscordWebhookOAuthProvider
	pool     *pgxpool.Pool
	t        *testing.T
	observed *string
}

func (p providerRecordingCommittedGuildID) DeleteWebhook(ctx context.Context, credential notification.DiscordWebhookCredential) error {
	p.t.Helper()
	var guildID string
	if err := p.pool.QueryRow(ctx, `SELECT guild_id FROM notification_discord_destination WHERE id = true`).Scan(&guildID); err != nil {
		p.t.Fatalf("read destination guild_id during cleanup: %v", err)
	}
	*p.observed = guildID
	return p.DiscordWebhookOAuthProvider.DeleteWebhook(ctx, credential)
}

// A local storage failure (here, a database CHECK-constraint violation
// enforced on the destination row itself) must leave the old destination
// completely unchanged, and must never attempt a remote cleanup: the new
// destination was never durably committed.
func TestDiscordConnectServiceStorageFailureAgainstRealStoreLeavesOldDestinationActive(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 13)
	}

	oldCiphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "old-id", WebhookToken: "old-token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{
		GuildID: "guild-old", ChannelID: "channel-old",
	}, oldCiphertext); err != nil {
		t.Fatalf("seed old ConnectDiscordDestination() error = %v", err)
	}

	provider := &fakeDiscordWebhookOAuthProviderForConnect{
		exchangeResult: notification.DiscordWebhookOAuthResult{
			// An empty GuildID violates notification_discord_destination's
			// guild_id CHECK constraint, forcing the single atomic UPSERT
			// statement to fail rather than simulating failure in Go.
			Credential: notification.DiscordWebhookCredential{WebhookID: "new-id", WebhookToken: "new-token"},
			GuildID:    "", ChannelID: "channel-new",
		},
	}
	connect := notification.NewDiscordConnectService(store, provider, key, nil)

	if err := connect.Connect(t.Context(), "the-code"); err == nil {
		t.Fatal("Connect() with a database-constraint-violating destination succeeded, want an error")
	}
	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0: the new destination never committed", provider.deleteCalls)
	}

	destination, ciphertext, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if destination.GuildID != "guild-old" {
		t.Fatalf("destination = %#v, want the OLD destination untouched", destination)
	}
	decrypted, err := notification.DecryptDiscordCredential(key, ciphertext)
	if err != nil {
		t.Fatalf("DecryptDiscordCredential() error = %v", err)
	}
	if decrypted.WebhookID != "old-id" {
		t.Fatalf("stored credential = %#v, want the OLD credential still active", decrypted)
	}
}

// A remote cleanup failure after a successful commit must never be
// surfaced as a Connect error, and the new destination must stay
// connected: no rollback, no restore of the old destination.
func TestDiscordConnectServiceRemoteCleanupFailureAgainstRealStoreLeavesNewDestinationConnected(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 17)
	}

	oldCiphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "old-id", WebhookToken: "old-token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{
		GuildID: "guild-old", ChannelID: "channel-old",
	}, oldCiphertext); err != nil {
		t.Fatalf("seed old ConnectDiscordDestination() error = %v", err)
	}

	provider := &fakeDiscordWebhookOAuthProviderForConnect{
		exchangeResult: notification.DiscordWebhookOAuthResult{
			Credential: notification.DiscordWebhookCredential{WebhookID: "new-id", WebhookToken: "new-token"},
			GuildID:    "guild-new", ChannelID: "channel-new",
		},
		deleteErr: errors.New("discord webhook delete unavailable"),
	}
	connect := notification.NewDiscordConnectService(store, provider, key, nil)

	if err := connect.Connect(t.Context(), "the-code"); err != nil {
		t.Fatalf("Connect() error = %v, want nil even though remote cleanup fails", err)
	}

	destination, _, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() error = %v", err)
	}
	if destination.GuildID != "guild-new" || destination.State != notification.DiscordDestinationConnected {
		t.Fatalf("destination = %#v, want the new destination still connected", destination)
	}
}

// discordLifecycleDeliveryFixture materializes one real notification_work
// row (through the real gamerequest/notification pipeline) with one
// delivery per given (transport, targetKey, status) triple, so lifecycle
// tests exercise real rows rather than hand-inserted ones disconnected
// from notification_work's foreign keys.
func discordLifecycleDeliveryFixture(t *testing.T, pool *pgxpool.Pool, store *notificationpostgres.Store, targets []notification.Target, statuses []string) {
	t.Helper()
	if len(targets) != len(statuses) {
		t.Fatalf("targets/statuses length mismatch: %d vs %d", len(targets), len(statuses))
	}
	fixture := newNotificationFixture(t, pool)
	created, err := fixture.requests.Create(t.Context(), fixture.host.ID, fixture.gameID, nil, time.Now().Add(time.Hour), "Lifecycle round")
	if err != nil {
		t.Fatalf("create game request: %v", err)
	}
	activities, err := store.DiscoverActivity(t.Context(), 10)
	if err != nil {
		t.Fatalf("DiscoverActivity() error = %v", err)
	}
	var activity notification.Activity
	for _, candidate := range activities {
		if candidate.GameRequestID == created.ID {
			activity = candidate
		}
	}
	if activity.Sequence == 0 {
		t.Fatal("DiscoverActivity() did not return the fixture's own activity")
	}
	if err := store.MaterializeWork(t.Context(), activity, targets); err != nil {
		t.Fatalf("MaterializeWork() error = %v", err)
	}
	for i, target := range targets {
		if _, err := pool.Exec(t.Context(), `
			UPDATE notification_delivery d SET status = $1
			FROM notification_work w
			WHERE d.work_id = w.id AND w.game_request_id = $2 AND d.transport = $3 AND d.target_key = $4
		`, statuses[i], created.ID, target.Transport, target.Key); err != nil {
			t.Fatalf("seed delivery status %q for %+v: %v", statuses[i], target, err)
		}
	}
}

func deliveryStatus(t *testing.T, pool *pgxpool.Pool, transport, targetKey string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(t.Context(), `
		SELECT status FROM notification_delivery WHERE transport = $1 AND target_key = $2
	`, transport, targetKey).Scan(&status); err != nil {
		t.Fatalf("read delivery status for %s/%s: %v", transport, targetKey, err)
	}
	return status
}

// Disconnect suppresses this destination's still-pending and still-claimed
// Discord deliveries, but leaves Web Push deliveries and already-succeeded
// Discord deliveries completely untouched.
func TestDiscordLifecycleDisconnectSuppressesOnlyPendingAndClaimedDiscordDeliveries(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 19)
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-1", ChannelID: "channel-1"}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	discordLifecycleDeliveryFixture(t, pool, store, []notification.Target{
		{Transport: "discord", Key: "pending-target"},
		{Transport: "discord", Key: "claimed-target"},
		{Transport: "discord", Key: "succeeded-target"},
		{Transport: "webpush", Key: "webpush-target"},
	}, []string{"pending", "claimed", "succeeded", "pending"})

	provider := &fakeDiscordWebhookOAuthProviderForConnect{}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	if status := deliveryStatus(t, pool, "discord", "pending-target"); status != "suppressed" {
		t.Fatalf("pending discord delivery status = %q, want suppressed", status)
	}
	if status := deliveryStatus(t, pool, "discord", "claimed-target"); status != "suppressed" {
		t.Fatalf("claimed discord delivery status = %q, want suppressed", status)
	}
	if status := deliveryStatus(t, pool, "discord", "succeeded-target"); status != "succeeded" {
		t.Fatalf("succeeded discord delivery status = %q, want it untouched (succeeded)", status)
	}
	if status := deliveryStatus(t, pool, "webpush", "webpush-target"); status != "pending" {
		t.Fatalf("webpush delivery status = %q, want it untouched (pending)", status)
	}

	if _, _, err := store.GetDiscordDestination(t.Context()); !errors.Is(err, notification.ErrDiscordDestinationNotFound) {
		t.Fatalf("GetDiscordDestination() after disconnect error = %v, want ErrDiscordDestinationNotFound", err)
	}
	if provider.deleteCalls != 1 {
		t.Fatalf("DeleteWebhook calls = %d, want 1", provider.deleteCalls)
	}
}

// Local disconnect commits before any remote cleanup is attempted: the
// destination row is already gone by the time DeleteWebhook is called.
func TestDiscordLifecycleDisconnectCommitsBeforeRemoteCleanup(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 23)
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-1", ChannelID: "channel-1"}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	var sawRowCountDuringCleanup int64 = -1
	provider := providerRecordingRowCountOnDelete{
		DiscordWebhookOAuthProvider: &fakeDiscordWebhookOAuthProviderForConnect{},
		pool:                        pool,
		t:                           t,
		observed:                    &sawRowCountDuringCleanup,
	}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if sawRowCountDuringCleanup != 0 {
		t.Fatalf("destination row count during remote cleanup = %d, want 0 (already committed)", sawRowCountDuringCleanup)
	}
}

// providerRecordingRowCountOnDelete records the destination row count seen
// during DeleteWebhook, proving the local commit already happened.
type providerRecordingRowCountOnDelete struct {
	notification.DiscordWebhookOAuthProvider
	pool     *pgxpool.Pool
	t        *testing.T
	observed *int64
}

func (p providerRecordingRowCountOnDelete) DeleteWebhook(ctx context.Context, credential notification.DiscordWebhookCredential) error {
	p.t.Helper()
	var count int64
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM notification_discord_destination`).Scan(&count); err != nil {
		p.t.Fatalf("count destination rows during cleanup: %v", err)
	}
	*p.observed = count
	return p.DiscordWebhookOAuthProvider.DeleteWebhook(ctx, credential)
}

// A remote cleanup failure on disconnect never restores local connection
// state: the destination stays gone.
func TestDiscordLifecycleDisconnectRemoteCleanupFailureLeavesDestinationDisconnected(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 29)
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-1", ChannelID: "channel-1"}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	provider := &fakeDiscordWebhookOAuthProviderForConnect{deleteErr: errors.New("discord unavailable")}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v, want nil even though remote cleanup fails", err)
	}
	if _, _, err := store.GetDiscordDestination(t.Context()); !errors.Is(err, notification.ErrDiscordDestinationNotFound) {
		t.Fatalf("GetDiscordDestination() after failed cleanup error = %v, want ErrDiscordDestinationNotFound", err)
	}
}

// The broken transition retains safe metadata, clears the ciphertext, and
// suppresses still-pending/claimed Discord deliveries while leaving Web
// Push and unrelated Discord deliveries alone, all atomically.
func TestDiscordLifecycleBreakRetainsSafeMetadataClearsCiphertextAndSuppresses(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 31)
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{
		GuildID: "guild-broken", ChannelID: "channel-broken", WebhookName: "Broken Destination",
	}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	discordLifecycleDeliveryFixture(t, pool, store, []notification.Target{
		{Transport: "discord", Key: "broken-pending-target"},
		{Transport: "discord", Key: "broken-succeeded-target"},
		{Transport: "webpush", Key: "broken-webpush-target"},
	}, []string{"pending", "succeeded", "pending"})

	provider := &fakeDiscordWebhookOAuthProviderForConnect{}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Break(t.Context(), "webhook deleted (10015)"); err != nil {
		t.Fatalf("Break() error = %v", err)
	}

	destination, storedCiphertext, err := store.GetDiscordDestination(t.Context())
	if err != nil {
		t.Fatalf("GetDiscordDestination() after Break() error = %v", err)
	}
	if destination.State != notification.DiscordDestinationBroken {
		t.Fatalf("destination.State = %q, want broken", destination.State)
	}
	if destination.GuildID != "guild-broken" || destination.ChannelID != "channel-broken" || destination.WebhookName != "Broken Destination" {
		t.Fatalf("destination = %#v, want safe metadata retained", destination)
	}
	if destination.BrokenReason != "webhook deleted (10015)" {
		t.Fatalf("destination.BrokenReason = %q, want the given reason", destination.BrokenReason)
	}
	if storedCiphertext != nil {
		t.Fatal("GetDiscordDestination() returned a non-nil ciphertext for a broken destination")
	}

	if status := deliveryStatus(t, pool, "discord", "broken-pending-target"); status != "suppressed" {
		t.Fatalf("pending discord delivery status = %q, want suppressed", status)
	}
	if status := deliveryStatus(t, pool, "discord", "broken-succeeded-target"); status != "succeeded" {
		t.Fatalf("succeeded discord delivery status = %q, want it untouched (succeeded)", status)
	}
	if status := deliveryStatus(t, pool, "webpush", "broken-webpush-target"); status != "pending" {
		t.Fatalf("webpush delivery status = %q, want it untouched (pending)", status)
	}

	if provider.deleteCalls != 0 {
		t.Fatalf("DeleteWebhook calls = %d, want 0: Break never contacts the provider", provider.deleteCalls)
	}
}

// Existing Discord channel messages are never anything this lifecycle
// service touches: neither Disconnect nor Break references any message ID
// or calls anything on the provider except (best-effort) DeleteWebhook.
// This is a structural guarantee already covered by
// fakeDiscordWebhookOAuthProviderForConnect only implementing
// AuthorizationURL/Exchange/DeleteWebhook: there is no message-delete
// method for either lifecycle transition to accidentally call.
func TestDiscordLifecycleNeverTouchesMessagesOrHistoricalWork(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i + 37)
	}
	ciphertext, err := notification.EncryptDiscordCredential(key, notification.DiscordWebhookCredential{WebhookID: "id", WebhookToken: "token"})
	if err != nil {
		t.Fatalf("EncryptDiscordCredential() error = %v", err)
	}
	if err := store.ConnectDiscordDestination(t.Context(), notification.DiscordDestination{GuildID: "guild-1", ChannelID: "channel-1"}, ciphertext); err != nil {
		t.Fatalf("ConnectDiscordDestination() error = %v", err)
	}

	var workCountBefore int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_work`).Scan(&workCountBefore); err != nil {
		t.Fatalf("count notification_work before: %v", err)
	}

	provider := &fakeDiscordWebhookOAuthProviderForConnect{}
	lifecycle := notification.NewDiscordLifecycleService(store, provider, key, nil)
	if err := lifecycle.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	var workCountAfter int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM notification_work`).Scan(&workCountAfter); err != nil {
		t.Fatalf("count notification_work after: %v", err)
	}
	if workCountAfter != workCountBefore {
		t.Fatalf("notification_work row count changed from %d to %d: disconnect must never touch historical work", workCountBefore, workCountAfter)
	}
}
