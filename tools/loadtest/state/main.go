package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/metrics"
	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
	"github.com/jackc/pgx/v5/pgxpool"
)

type session struct {
	UserID string `json:"user_id"`
}
type fixtures struct {
	Games []string `json:"games"`
}
type control struct {
	RequestID string `json:"request_id"`
}
type snapshot struct {
	RequestID     string `json:"request_id"`
	Status        string `json:"status"`
	MaxPlayers    int32  `json:"max_players"`
	MemberCount   int    `json:"member_count"`
	ViewerJoined  bool   `json:"viewer_joined"`
	ChatCount     int    `json:"chat_count"`
	ActivityCount int    `json:"activity_count"`
}

func main() {
	mode := flag.String("mode", "", "prepare, snapshot, audit, or metrics-key")
	sessionsPath := flag.String("sessions", "", "private session JSONL path")
	fixturesPath := flag.String("fixtures", "", "fixture JSON path")
	output := flag.String("output", "", "private output path")
	requestID := flag.String("request", "", "Game Request ID")
	viewerID := flag.String("viewer-id", "", "viewer user ID")
	flag.Parse()
	if err := run(context.Background(), *mode, *sessionsPath, *fixturesPath, *output, *requestID, *viewerID); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest state:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode, sessionsPath, fixturesPath, output, requestID, viewerID string) error {
	if mode != "prepare" && mode != "snapshot" && mode != "audit" && mode != "metrics-key" {
		return errors.New("-mode must be prepare, snapshot, audit, or metrics-key")
	}
	cfg, err := target.DatabaseConfig()
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("connect to isolated load-test database")
	}
	defer pool.Close()
	if mode == "metrics-key" {
		return ensureMetricsKey(ctx, pool, output)
	}
	if mode == "prepare" {
		if sessionsPath == "" || fixturesPath == "" || output == "" {
			return errors.New("prepare requires -sessions, -fixtures, and -output")
		}
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err != nil {
			return errors.New("cannot prepare pg_stat_statements for load-test profiling")
		}
		return prepare(ctx, pool, sessionsPath, fixturesPath, output)
	}
	if requestID == "" {
		if output == "" {
			return errors.New("snapshot and audit require -request or -output control file")
		}
		data, err := os.ReadFile(output)
		if err != nil {
			return errors.New("cannot read control request file")
		}
		var saved control
		if json.Unmarshal(data, &saved) != nil || saved.RequestID == "" {
			return errors.New("invalid control request file")
		}
		requestID = saved.RequestID
	}
	if mode == "snapshot" {
		return writeSnapshot(ctx, pool, requestID, viewerID)
	}
	return audit(ctx, pool, requestID)
}

func prepare(ctx context.Context, pool *pgxpool.Pool, sessionsPath, fixturesPath, output string) error {
	people, gameID, err := loadControlInputs(sessionsPath, fixturesPath)
	if err != nil {
		return err
	}
	host, trigger := people[len(people)-2].UserID, people[len(people)-1].UserID
	saved, err := os.ReadFile(output)
	if err == nil {
		var existing control
		if json.Unmarshal(saved, &existing) != nil || existing.RequestID == "" {
			return errors.New("invalid existing control request file")
		}
		return reuseControl(ctx, pool, existing.RequestID, gameID, host, trigger)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect control request file")
	}
	if _, err := os.Stat(output); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("control request path is unavailable")
	}
	var hostExists, triggerExists, gameExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1), EXISTS(SELECT 1 FROM users WHERE id=$2), EXISTS(SELECT 1 FROM games WHERE id=$3 AND active)`, host, trigger, gameID).Scan(&hostExists, &triggerExists, &gameExists); err != nil || !hostExists || !triggerExists || !gameExists {
		return errors.New("control identities or fixture game are unavailable")
	}
	service := gamerequest.NewService(gamerequestpostgres.NewStore(pool), game.NewService(gamepostgres.NewStore(pool)), nil)
	maxPlayers := int32(8)
	created, err := service.Create(ctx, host, gameID, &maxPlayers, time.Now().UTC().Add(30*time.Minute), "loadtest phase2 control")
	if err != nil {
		return errors.New("cannot create control Game Request")
	}
	if err := reuseControl(ctx, pool, created.ID, gameID, host, trigger); err != nil {
		return err
	}
	return writeExclusive(output, control{RequestID: created.ID})
}

func loadControlInputs(sessionsPath, fixturesPath string) ([]session, string, error) {
	data, err := os.ReadFile(sessionsPath)
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("cannot read session file")
	}
	var people []session
	seen := make(map[string]struct{})
	for _, line := range bytesLines(data) {
		var p session
		if json.Unmarshal(line, &p) != nil || p.UserID == "" {
			return nil, "", errors.New("invalid session identity file")
		}
		if _, ok := seen[p.UserID]; ok {
			return nil, "", errors.New("duplicate session identity")
		}
		seen[p.UserID] = struct{}{}
		people = append(people, p)
	}
	var input fixtures
	data, err = os.ReadFile(fixturesPath)
	if err != nil || json.Unmarshal(data, &input) != nil || len(input.Games) == 0 {
		return nil, "", errors.New("invalid fixture file")
	}
	if len(people) < 2 {
		return nil, "", errors.New("need two session identities")
	}
	return people, input.Games[0], nil
}

func reuseControl(ctx context.Context, pool *pgxpool.Pool, requestID, gameID, host, trigger string) error {
	var actualGame, creator, status, description string
	var maxPlayers int32
	var hostMember, triggerMember bool
	var members int
	err := pool.QueryRow(ctx, `SELECT r.game_id::text, r.creator_id::text, r.status, r.description, r.max_players,
		EXISTS(SELECT 1 FROM game_request_members m WHERE m.game_request_id=r.id AND m.user_id=r.creator_id),
		EXISTS(SELECT 1 FROM game_request_members m WHERE m.game_request_id=r.id AND m.user_id=$2::uuid),
		(SELECT count(*) FROM game_request_members m WHERE m.game_request_id=r.id)
		FROM game_requests r WHERE r.id=$1::uuid`, requestID, trigger).Scan(&actualGame, &creator, &status, &description, &maxPlayers, &hostMember, &triggerMember, &members)
	if err != nil {
		return errors.New("saved control request is missing or invalid")
	}
	if actualGame != gameID || creator != host || status != "open" || description != "loadtest phase2 control" || maxPlayers != 8 || !hostMember {
		return errors.New("saved control request does not match private fixtures")
	}
	if members-btoi(triggerMember) >= int(maxPlayers) {
		return errors.New("saved control request has no free place for the trigger player; archive its control file and prepare a new request")
	}
	if triggerMember {
		service := gamerequest.NewService(gamerequestpostgres.NewStore(pool), game.NewService(gamepostgres.NewStore(pool)), nil)
		if err := service.Leave(ctx, requestID, trigger); err != nil {
			return errors.New("cannot reset control trigger membership")
		}
	}
	return nil
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bytesLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func writeExclusive(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create private output directory")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private output file")
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(value); err != nil {
		_ = os.Remove(path)
		return errors.New("cannot write private output file")
	}
	return nil
}

func ensureMetricsKey(ctx context.Context, pool *pgxpool.Pool, path string) error {
	if path == "" {
		return errors.New("metrics-key requires -output key path")
	}
	store := metrics.NewStore(pool)
	configured, err := store.Configured(ctx)
	if err != nil {
		return errors.New("cannot inspect metrics configuration")
	}
	if configured {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("metrics key already configured; private local key file is unavailable")
		}
		return nil
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("metrics key file exists or cannot be inspected; refusing to rotate key")
	}
	key, err := store.Rotate(ctx)
	if err != nil {
		return errors.New("cannot create metrics access key")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create private metrics directory")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private metrics key file")
	}
	if _, err := file.WriteString(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return errors.New("cannot write private metrics key file")
	}
	return file.Close()
}

func writeSnapshot(ctx context.Context, pool *pgxpool.Pool, requestID, viewerID string) error {
	var out snapshot
	err := pool.QueryRow(ctx, `SELECT r.id::text, r.status, r.max_players,
		(SELECT count(*) FROM game_request_members m WHERE m.game_request_id=r.id),
		EXISTS(SELECT 1 FROM game_request_members m WHERE m.game_request_id=r.id AND m.user_id=NULLIF($2,'')::uuid),
		(SELECT count(*) FROM game_request_messages c WHERE c.game_request_id=r.id),
		(SELECT count(*) FROM game_request_activity a WHERE a.game_request_id=r.id)
		FROM game_requests r WHERE r.id=$1`, requestID, viewerID).Scan(&out.RequestID, &out.Status, &out.MaxPlayers, &out.MemberCount, &out.ViewerJoined, &out.ChatCount, &out.ActivityCount)
	if err != nil {
		return errors.New("control request snapshot failed")
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func audit(ctx context.Context, pool *pgxpool.Pool, requestID string) error {
	var badCapacity, duplicates, missingHosts, controlRows, badControl int
	queries := []string{
		`SELECT count(*) FROM game_requests r WHERE r.max_players>0 AND (SELECT count(*) FROM game_request_members m WHERE m.game_request_id=r.id)>r.max_players`,
		`SELECT count(*) FROM (SELECT game_request_id,user_id FROM game_request_members GROUP BY 1,2 HAVING count(*)>1) d`,
		`SELECT count(*) FROM game_requests r WHERE NOT EXISTS(SELECT 1 FROM game_request_members m WHERE m.game_request_id=r.id AND m.user_id=r.creator_id)`,
	}
	for i, dest := range []*int{&badCapacity, &duplicates, &missingHosts} {
		if err := pool.QueryRow(ctx, queries[i]).Scan(dest); err != nil {
			return errors.New("database invariant audit query failed")
		}
	}
	// Count the control row itself too: zero violations among zero rows is not a pass.
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE NOT EXISTS(SELECT 1 FROM game_request_members m WHERE m.game_request_id=r.id AND m.user_id=r.creator_id) OR r.max_players<>8 OR r.status<>'open' OR r.description<>'loadtest phase2 control') FROM game_requests r WHERE r.id=$1`, requestID).Scan(&controlRows, &badControl); err != nil {
		return errors.New("database invariant audit query failed")
	}
	result := auditResult(badCapacity, duplicates, missingHosts, controlRows, badControl)
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return errors.New("cannot encode audit result")
	}
	if auditFailed(result) {
		return errors.New("database invariant audit failed")
	}
	return nil
}

func auditResult(badCapacity, duplicates, missingHosts, controlRows, badControl int) map[string]int {
	missingControl := 0
	if controlRows != 1 {
		missingControl = 1
	}
	return map[string]int{"capacity_violations": badCapacity, "duplicate_memberships": duplicates, "missing_host_memberships": missingHosts, "control_host_violations": badControl, "control_request_missing": missingControl}
}

func auditFailed(result map[string]int) bool {
	for _, violations := range result {
		if violations > 0 {
			return true
		}
	}
	return false
}
