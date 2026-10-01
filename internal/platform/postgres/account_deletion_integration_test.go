//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
)

func TestDeleteAccountRemovesEverythingAttributableAndFreesGameNick(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Departing Host")
	if err != nil {
		t.Fatalf("register host: %v", err)
	}
	guest, err := users.Register(t.Context(), "Staying Guest")
	if err != nil {
		t.Fatalf("register guest: %v", err)
	}

	games := game.NewService(gamepostgres.NewStore(pool))
	catalogGame, err := games.Create(t.Context(), "Warcraft III", "warcraft-iii")
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), games, nil)
	startsAt := time.Now().UTC().Add(time.Hour)
	owned, err := requests.Create(t.Context(), host.ID, catalogGame.ID, nil, startsAt, "host round")
	if err != nil {
		t.Fatalf("create owned request: %v", err)
	}
	if err := requests.Join(t.Context(), owned.ID, guest.ID); err != nil {
		t.Fatalf("guest joins owned request: %v", err)
	}
	other, err := requests.Create(t.Context(), guest.ID, catalogGame.ID, nil, startsAt, "guest round")
	if err != nil {
		t.Fatalf("create guest request: %v", err)
	}
	if err := requests.Join(t.Context(), other.ID, host.ID); err != nil {
		t.Fatalf("host joins guest request: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO game_subscriptions (user_id, game_id) VALUES ($1, $2)`, host.ID, catalogGame.ID); err != nil {
		t.Fatalf("follow game: %v", err)
	}
	// Work already materialized for the host's activity must not block deletion.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_work (activity_sequence, game_request_id, actor_id, kind, occurred_at)
		SELECT sequence, game_request_id, actor_id, kind, occurred_at FROM game_request_activity WHERE actor_id = $1`, host.ID); err != nil {
		t.Fatalf("materialize notification work: %v", err)
	}
	sessions := auth.NewSessionService(authpostgres.NewSessionStore(pool), users)
	session, err := sessions.NewSession(t.Context(), host.ID)
	if err != nil {
		t.Fatalf("host session: %v", err)
	}

	if err := users.DeleteAccount(t.Context(), host.ID, "wrong"); !errors.Is(err, user.ErrGameNickMismatch) {
		t.Fatalf("mismatched confirmation error = %v", err)
	}
	if err := users.DeleteAccount(t.Context(), host.ID, "Departing Host"); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	// Gone with the account: the user, its session, follows, its own request
	// (and with it the guest's membership there), and its activity everywhere.
	for name, query := range map[string]string{
		"users":                 `SELECT count(*) FROM users WHERE id = $1`,
		"game_subscriptions":    `SELECT count(*) FROM game_subscriptions WHERE user_id = $1`,
		"game_requests":         `SELECT count(*) FROM game_requests WHERE creator_id = $1`,
		"game_request_members":  `SELECT count(*) FROM game_request_members WHERE user_id = $1`,
		"game_request_activity": `SELECT count(*) FROM game_request_activity WHERE actor_id = $1`,
		"notification_work":     `SELECT count(*) FROM notification_work WHERE actor_id = $1`,
		"auth_sessions":         `SELECT count(*) FROM auth_sessions WHERE user_id = $1`,
	} {
		var remaining int
		if err := pool.QueryRow(t.Context(), query, host.ID).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("%s rows left for deleted user = %d, %v", name, remaining, err)
		}
	}
	var ownedLeft int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_requests WHERE id = $1`, owned.ID).Scan(&ownedLeft); err != nil || ownedLeft != 0 {
		t.Errorf("deleted user's request survived: %d, %v", ownedLeft, err)
	}
	if _, err := sessions.Authenticate(t.Context(), session.Token); err == nil {
		t.Error("deleted user's session still authenticates")
	}

	// Everyone else is untouched: the guest keeps their request, now without
	// the departed member.
	var guestRequests, guestMembers int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_requests WHERE id = $1`, other.ID).Scan(&guestRequests); err != nil || guestRequests != 1 {
		t.Errorf("guest request count = %d, %v", guestRequests, err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_request_members WHERE game_request_id = $1`, other.ID).Scan(&guestMembers); err != nil || guestMembers != 1 {
		t.Errorf("guest request members = %d, %v", guestMembers, err)
	}

	// The nick is claimable immediately, case-insensitively, by anyone.
	if _, err := users.RenameGameNick(t.Context(), guest.ID, "departing host"); err != nil {
		t.Errorf("deleted Game Nick not released: %v", err)
	}
}

func TestDeleteAccountKeepsTheLastAdministrator(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	first, _ := users.Register(t.Context(), "First Admin")
	second, _ := users.Register(t.Context(), "Second Admin")
	for _, account := range []user.User{first, second} {
		if _, err := users.PromoteToAdmin(t.Context(), account.ID); err != nil {
			t.Fatalf("promote %s: %v", account.GameNick, err)
		}
	}

	if err := users.DeleteAccount(t.Context(), first.ID, "First Admin"); err != nil {
		t.Fatalf("deleting one of two administrators: %v", err)
	}
	if err := users.DeleteAccount(t.Context(), second.ID, "Second Admin"); !errors.Is(err, user.ErrLastAdmin) {
		t.Fatalf("deleting the last administrator error = %v, want ErrLastAdmin", err)
	}
	if _, err := users.ByID(t.Context(), second.ID); err != nil {
		t.Fatalf("last administrator must survive: %v", err)
	}
}

// Every foreign key into users must cascade, otherwise a feature that adds a
// user-linked table silently makes account deletion fail.
func TestNoForeignKeyToUsersRestrictsAccountDeletion(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	rows, err := pool.Query(t.Context(), `
		SELECT conrelid::regclass::text, conname
		FROM pg_constraint
		WHERE contype = 'f' AND confrelid = 'users'::regclass AND confdeltype <> 'c'`)
	if err != nil {
		t.Fatalf("query foreign keys: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, name string
		if err := rows.Scan(&table, &name); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s.%s references users without ON DELETE CASCADE", table, name)
	}
}
