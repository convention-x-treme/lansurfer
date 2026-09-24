//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification/postgres/sqlcgen"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
