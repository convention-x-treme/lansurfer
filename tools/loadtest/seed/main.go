// Command seed creates test-only players and normal LANsurfer browser sessions.
// It MUST only be used with a disposable database named lansurfer_loadtest.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
	"github.com/jackc/pgx/v5/pgxpool"
)

type record struct {
	UserID    string `json:"user_id"`
	GameNick  string `json:"game_nick"`
	Session   string `json:"session_token"`
	ExpiresAt string `json:"expires_at"`
}

func main() {
	count := flag.Int("users", 400, "number of unique test players (1-2000)")
	output := flag.String("output", "", "new file for sensitive session tokens (must not exist)")
	prefix := flag.String("prefix", "LoadTest", "game-nick prefix")
	flag.Parse()
	if err := run(*count, *output, *prefix); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest seed:", err)
		os.Exit(1)
	}
}

func run(count int, output, prefix string) error {
	if count < 1 || count > 2000 {
		return errors.New("-users must be between 1 and 2000")
	}
	if output == "" {
		return errors.New("-output is required (choose a private path outside Git)")
	}
	if _, err := user.ValidateGameNick(fmt.Sprintf("%s_%04d", prefix, count)); err != nil {
		return fmt.Errorf("invalid -prefix: %w", err)
	}
	cfg, err := target.DatabaseConfig()
	if err != nil {
		return err
	}

	// Create the credential file before writing any records, with owner-only permissions.
	// O_EXCL prevents accidentally overwriting existing credentials.
	file, err := os.OpenFile(filepath.Clean(output), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create protected token file: %w", err)
	}
	defer file.Close()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(output)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to load-test database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping load-test database: %w", err)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	sessions := auth.NewSessionService(authpostgres.NewSessionStore(pool), users)
	writer := bufio.NewWriter(file)
	for i := 1; i <= count; i++ {
		nick := fmt.Sprintf("%s_%04d", prefix, i)
		player, err := users.Register(ctx, nick)
		if err != nil {
			return fmt.Errorf("create %s: %w (use a fresh database or different prefix)", nick, err)
		}
		session, err := sessions.NewSession(ctx, player.ID)
		if err != nil {
			return fmt.Errorf("create session for %s: %w", nick, err)
		}
		if err := json.NewEncoder(writer).Encode(record{
			UserID: player.ID, GameNick: nick, Session: session.Token,
			ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return fmt.Errorf("write session file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush session file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync session file: %w", err)
	}
	success = true
	fmt.Printf("created %d LANsurfer test users and sessions; credentials stored at %s (mode 0600)\n", count, output)
	return nil
}
