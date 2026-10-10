// Command race fires many simultaneous, CSRF-correct joins at one Game Request
// of the isolated load-test instance and then checks, in the isolated database,
// that capacity was never exceeded, no membership is duplicated, and no free
// place was left unused. Only counts are printed; session tokens never are.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const baseURL = "http://127.0.0.1:18080"

type player struct{ userID, token string }

func main() {
	request := flag.String("request", "", "Game Request ID (UUID) to race on")
	joiners := flag.Int("joiners", 100, "simultaneous joining players")
	sessions := flag.String("sessions", "", "path to local test sessions JSONL")
	repeat := flag.Int("repeat", 1, "identical simultaneous submissions per player (2+ tests double-submit)")
	flag.Parse()
	if err := run(*request, *joiners, *repeat, *sessions); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest race:", err)
		os.Exit(1)
	}
}

// verify checks the capacity and membership invariants after a join burst.
// maxPlayers <= 0 means unlimited. Every free place must have been taken when
// there were enough joiners, and never more.
func verify(maxPlayers, before, joiners, joined, after, duplicates int) error {
	if duplicates > 0 {
		return fmt.Errorf("%d duplicate memberships", duplicates)
	}
	if maxPlayers > 0 && after > maxPlayers {
		return fmt.Errorf("overfilled: %d members, capacity %d", after, maxPlayers)
	}
	if after != before+joined {
		return fmt.Errorf("members %d != before %d + successful joins %d", after, before, joined)
	}
	want := joiners
	if maxPlayers > 0 && maxPlayers-before < want {
		want = maxPlayers - before
	}
	if joined != want {
		return fmt.Errorf("%d joins succeeded, expected exactly %d", joined, want)
	}
	return nil
}

func run(requestID string, joiners, repeat int, sessionsPath string) error {
	if requestID == "" || sessionsPath == "" || joiners < 2 || repeat < 1 {
		return errors.New("-request and -sessions are required and -joiners must be at least 2")
	}
	databaseURL := os.Getenv("LANSURFER_LOADTEST_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("set LANSURFER_LOADTEST_DATABASE_URL explicitly; production LANSURFER_DATABASE_URL is ignored")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse load-test database URL: %w", err)
	}
	if cfg.ConnConfig.Database != "lansurfer_loadtest" {
		return errors.New("refusing to run: database must be named exactly lansurfer_loadtest")
	}
	players, err := readPlayers(sessionsPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to load-test database: %w", err)
	}
	defer pool.Close()

	var maxPlayers, before int
	if err := pool.QueryRow(ctx, `SELECT coalesce(max_players, 0), (SELECT count(*) FROM game_request_members WHERE game_request_id = r.id)
		FROM game_requests r WHERE r.id = $1 AND r.status = 'open'`, requestID).Scan(&maxPlayers, &before); err != nil {
		return fmt.Errorf("load open Game Request: %w", err)
	}
	rows, err := pool.Query(ctx, `SELECT user_id::text FROM game_request_members WHERE game_request_id = $1`, requestID)
	if err != nil {
		return fmt.Errorf("load members: %w", err)
	}
	members := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		members[id] = true
	}
	rows.Close()
	var racers []player
	for _, p := range players {
		if !members[p.userID] && len(racers) < joiners {
			racers = append(racers, p)
		}
	}
	if len(racers) < joiners {
		return fmt.Errorf("only %d non-member players available for %d joiners", len(racers), joiners)
	}

	target, _ := url.Parse(baseURL)
	clients := make([]*http.Client, len(racers))
	tokens := make([]string, len(racers))
	var setup sync.WaitGroup
	var setupErr error
	var setupMu sync.Mutex
	for i, p := range racers {
		jar, _ := cookiejar.New(nil)
		jar.SetCookies(target, []*http.Cookie{{Name: "lansurfer_session", Value: p.token, Path: "/"}})
		clients[i] = &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		setup.Add(1)
		go func() { // a normal page GET makes LANsurfer issue the CSRF cookie
			defer setup.Done()
			resp, err := clients[i].Get(baseURL + "/game-requests/")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					err = fmt.Errorf("page status %d", resp.StatusCode)
				}
			}
			for _, c := range clients[i].Jar.Cookies(target) {
				if c.Name == "lansurfer_csrf" {
					tokens[i] = c.Value
				}
			}
			if err == nil && tokens[i] == "" {
				err = errors.New("no CSRF cookie issued")
			}
			if err != nil {
				setupMu.Lock()
				setupErr = err
				setupMu.Unlock()
			}
		}()
	}
	setup.Wait()
	if setupErr != nil {
		return fmt.Errorf("prepare players: %w", setupErr)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	joined, conflicts := 0, 0
	other := map[string]int{}
	for i := range racers {
		for range repeat {
			wg.Add(1)
			go func() {
				defer wg.Done()
				form := url.Values{"csrf_token": {tokens[i]}, "return_to": {"list"}}
				req, _ := http.NewRequest(http.MethodPost, baseURL+"/game-requests/"+requestID+"/join", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("HX-Request", "true")
				<-start
				resp, err := clients[i].Do(req)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					other["transport error"]++
					return
				}
				resp.Body.Close()
				switch {
				case resp.StatusCode == http.StatusOK && resp.Header.Get("HX-Retarget") == "#live-notices":
					conflicts++
				case resp.StatusCode == http.StatusOK:
					joined++
				default:
					other[fmt.Sprintf("HTTP %d", resp.StatusCode)]++
				}
			}()
		}
	}
	close(start)
	wg.Wait()

	var after, duplicates int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) - count(DISTINCT user_id) FROM game_request_members WHERE game_request_id = $1`, requestID).Scan(&after, &duplicates); err != nil {
		return fmt.Errorf("count members: %w", err)
	}
	fmt.Printf("race: request=%s capacity=%d members_before=%d joiners=%d joined=%d conflicts=%d other=%v members_after=%d duplicates=%d\n",
		requestID, maxPlayers, before, joiners, joined, conflicts, other, after, duplicates)
	if len(other) > 0 {
		return fmt.Errorf("unexpected responses: %v", other)
	}
	if err := verify(maxPlayers, before, joiners, joined, after, duplicates); err != nil {
		return fmt.Errorf("INVARIANT VIOLATED: %w", err)
	}
	fmt.Println("race: invariants hold")
	return nil
}

func readPlayers(path string) ([]player, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open sessions file: %w", err)
	}
	defer f.Close()
	var out []player
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var r struct {
			UserID string `json:"user_id"`
			Token  string `json:"session_token"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil || r.UserID == "" || r.Token == "" {
			return nil, errors.New("invalid sessions file line")
		}
		out = append(out, player{userID: r.UserID, token: r.Token})
	}
	return out, scanner.Err()
}
