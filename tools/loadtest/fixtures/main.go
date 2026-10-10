// Command fixtures creates realistic local Games, follows, Game Requests, and
// memberships for the isolated load-test database, through the normal domain
// services. It reuses the existing seeded users (IDs only, never tokens) and
// MUST only run against the disposable database lansurfer_loadtest.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
	"github.com/jackc/pgx/v5/pgxpool"
)

var gameNames = []string{
	"Counter-Strike 2", "Warcraft III", "Age of Empires II", "Minecraft", "Rocket League",
	"Left 4 Dead 2", "Trackmania", "Quake III Arena", "Unreal Tournament", "Mario Kart 8",
	"Halo: Combat Evolved", "Worms Armageddon", "Diablo II", "StarCraft", "Team Fortress 2",
	"Garry's Mod", "Valheim", "Deep Rock Galactic", "Overcooked 2", "Brawlhalla",
}

// plannedRequest describes one Game Request before any database write.
type plannedRequest struct {
	Kind       string // open, nearly_full, playing, empty
	GameIndex  int
	Creator    string
	Joiners    []string // members besides the creator
	MaxPlayers int32
}

// planRequests deterministically distributes kinds (4/10 open, 3/10
// nearly_full, 1/10 playing, 2/10 empty). Members are distinct users.
func planRequests(users, games []string, n int, seed uint64) []plannedRequest {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	plan := make([]plannedRequest, 0, n)
	for i := range n {
		p := plannedRequest{GameIndex: rng.IntN(len(games)), MaxPlayers: int32(4 + rng.IntN(5))}
		var members int
		switch i % 10 {
		case 0, 1, 2, 3:
			p.Kind, members = "open", 1+rng.IntN(int(p.MaxPlayers)-2)
		case 4, 5, 6:
			p.Kind, members = "nearly_full", int(p.MaxPlayers)-1
		case 7:
			p.Kind, members = "playing", 2+rng.IntN(int(p.MaxPlayers)-2)
		default:
			p.Kind, members = "empty", 1
		}
		order := rng.Perm(len(users))
		p.Creator = users[order[0]]
		for _, j := range order[1:members] {
			p.Joiners = append(p.Joiners, users[j])
		}
		plan = append(plan, p)
	}
	return plan
}

type fixtureFile struct {
	Games    []string            `json:"games"`
	Requests map[string][]string `json:"requests"` // kind -> request IDs
	Dataset  string              `json:"dataset,omitempty"`
	Scale    *scaleProgress      `json:"scale_progress,omitempty"`
}

func main() {
	games := flag.Int("games", 20, "number of local Games (1-20)")
	requests := flag.Int("requests", 60, "number of Game Requests")
	sessions := flag.String("sessions", "", "sessions JSONL (only user_id is read)")
	output := flag.String("output", "", "new fixtures JSON (request IDs only, mode 0600)")
	extendFrom := flag.String("extend-from", "", "existing private fixture manifest")
	dataset := flag.String("dataset", "", "fixture dataset identity (large-v1)")
	messagesPerRequest := flag.Int("messages-per-request", 4, "marked messages per large-v1 request")
	flag.Parse()
	if *dataset != "" || *extendFrom != "" {
		if err := runScale(*games, *requests, *messagesPerRequest, *sessions, *extendFrom, *output, *dataset); err != nil {
			fmt.Fprintln(os.Stderr, "loadtest fixtures:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*games, *requests, *sessions, *output); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest fixtures:", err)
		os.Exit(1)
	}
}

func run(gameCount, requestCount int, sessionsPath, output string) error {
	if gameCount < 1 || gameCount > len(gameNames) || requestCount < 1 {
		return fmt.Errorf("-games must be 1-%d and -requests at least 1", len(gameNames))
	}
	if sessionsPath == "" || output == "" {
		return errors.New("-sessions and -output are required")
	}
	cfg, err := target.DatabaseConfig()
	if err != nil {
		return err
	}
	users, err := readUserIDs(sessionsPath)
	if err != nil {
		return err
	}
	if len(users) < 9 {
		return errors.New("need at least 9 seeded users")
	}
	// O_EXCL doubles as the idempotency guard: fixtures are created once.
	file, err := os.OpenFile(filepath.Clean(output), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create fixtures file (already seeded?): %w", err)
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

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	follows := game.NewSubscriptionService(gameStore)
	service := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)

	out := fixtureFile{Requests: map[string][]string{}}
	for _, name := range gameNames[:gameCount] {
		created, err := catalog.Create(ctx, name, strings.ReplaceAll(strings.ToLower(strings.NewReplacer(":", "", "'", "").Replace(name)), " ", "-"))
		if err != nil {
			return fmt.Errorf("create game %q: %w", name, err)
		}
		out.Games = append(out.Games, created.ID)
	}
	// Each user follows 2-4 Games, deterministically.
	for i, userID := range users {
		for k := range 2 + i%3 {
			if err := follows.Follow(ctx, userID, out.Games[(i+k*7)%len(out.Games)]); err != nil {
				return fmt.Errorf("follow: %w", err)
			}
		}
	}
	for i, p := range planRequests(users, out.Games, requestCount, 139) {
		max := p.MaxPlayers
		created, err := service.Create(ctx, p.Creator, out.Games[p.GameIndex], &max, time.Now().UTC().Add(time.Duration(30+i*7)*time.Minute), "load-test "+p.Kind)
		if err != nil {
			return fmt.Errorf("create request %d: %w", i, err)
		}
		for _, member := range p.Joiners {
			if err := service.Join(ctx, created.ID, member); err != nil {
				return fmt.Errorf("join request %d: %w", i, err)
			}
		}
		if p.Kind == "playing" {
			if err := service.Start(ctx, created.ID, p.Creator); err != nil {
				return fmt.Errorf("start request %d: %w", i, err)
			}
		}
		out.Requests[p.Kind] = append(out.Requests[p.Kind], created.ID)
	}
	if err := json.NewEncoder(file).Encode(out); err != nil {
		return fmt.Errorf("write fixtures: %w", err)
	}
	success = true
	fmt.Printf("created %d games, %d requests (open=%d nearly_full=%d playing=%d empty=%d); ids in %s\n",
		len(out.Games), requestCount, len(out.Requests["open"]), len(out.Requests["nearly_full"]), len(out.Requests["playing"]), len(out.Requests["empty"]), output)
	return nil
}

// readUserIDs reads only the user_id field; session tokens are never held.
func readUserIDs(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open sessions file: %w", err)
	}
	defer f.Close()
	var ids []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var r struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil || r.UserID == "" {
			return nil, errors.New("invalid sessions file line")
		}
		ids = append(ids, r.UserID)
	}
	return ids, scanner.Err()
}
