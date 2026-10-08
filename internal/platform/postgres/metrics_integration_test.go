//go:build integration

package postgres

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/metrics"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func TestMetricsAccessKeySingletonHashOnlyAndRotation(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := metrics.NewStore(pool)
	if ok, err := store.Configured(t.Context()); err != nil || ok {
		t.Fatalf("Configured() before rotate = %v, %v", ok, err)
	}
	if ok, err := store.Valid(t.Context(), base64.RawURLEncoding.EncodeToString(make([]byte, 32))); err != nil || ok {
		t.Fatalf("Valid() without key = %v, %v", ok, err)
	}

	first, err := store.Rotate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(first)
	if err != nil || len(raw) != 32 {
		t.Fatalf("key %q is not 32 base64url bytes: %v", first, err)
	}
	var count int
	var stored []byte
	if err := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM metrics_access_key), key_hash FROM metrics_access_key`).Scan(&count, &stored); err != nil {
		t.Fatal(err)
	}
	if want := sha256.Sum256(raw); count != 1 || string(stored) != string(want[:]) {
		t.Fatalf("stored rows=%d hash match=%v, want one SHA-256 hash", count, string(stored) == string(want[:]))
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO metrics_access_key (singleton, key_hash) VALUES (false, $1)`, stored); err == nil {
		t.Fatal("second key row accepted")
	}
	if _, err := pool.Exec(t.Context(), `UPDATE metrics_access_key SET key_hash = '\x00'`); err == nil {
		t.Fatal("short hash accepted")
	}

	for _, bad := range []string{"", "short", first + "A", first[:42], first[:42] + "!", first + "\n"} {
		if ok, err := store.Valid(t.Context(), bad); err != nil || ok {
			t.Fatalf("Valid(%q) = %v, %v", bad, ok, err)
		}
	}
	if ok, err := store.Valid(t.Context(), first); err != nil || !ok {
		t.Fatalf("Valid(first) = %v, %v", ok, err)
	}

	// A different Store (another process) rotating takes effect immediately.
	second, err := metrics.NewStore(pool).Rotate(t.Context())
	if err != nil || second == first {
		t.Fatalf("Rotate() = %q, %v", second, err)
	}
	if ok, _ := store.Valid(t.Context(), first); ok {
		t.Fatal("old key still valid after rotation")
	}
	if ok, err := store.Valid(t.Context(), second); err != nil || !ok {
		t.Fatalf("Valid(second) = %v, %v", ok, err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM metrics_access_key`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows after rotation = %d, %v", count, err)
	}
}

func metricsTotals(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	response := httptest.NewRecorder()
	metrics.New(pool).ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(response.Body)
	if err != nil {
		t.Fatalf("invalid Prometheus exposition: %v", err)
	}
	totals := map[string]int64{}
	for kind, name := range map[string]string{
		"created":   "lansurfer_game_requests_created_total",
		"joined":    "lansurfer_game_request_joins_total",
		"left":      "lansurfer_game_request_leaves_total",
		"cancelled": "lansurfer_game_requests_cancelled_total",
		"closed":    "lansurfer_game_requests_closed_total",
		"started":   "lansurfer_games_started_total",
	} {
		family := families[name]
		if family == nil || len(family.Metric) != 1 {
			t.Fatalf("missing aggregate counter %s", name)
		}
		totals[kind] = int64(family.Metric[0].GetCounter().GetValue())
	}
	return totals
}

func TestMetricsProductTotalsAreTransactionalAndDurable(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if got := metricsTotals(t, pool); len(got) != 6 {
		t.Fatalf("seeded kinds = %v, want six", got)
	}

	users := user.NewService(userpostgres.NewStore(pool))
	host, err := users.Register(t.Context(), "Metrics Host")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := users.Register(t.Context(), "Metrics Guest")
	if err != nil {
		t.Fatal(err)
	}
	catalog := game.NewService(gamepostgres.NewStore(pool))
	selected, err := catalog.Create(t.Context(), "Metrics Game", "metrics-game")
	if err != nil {
		t.Fatal(err)
	}
	requests := gamerequest.NewService(gamerequestpostgres.NewStore(pool), catalog, nil)
	startsAt := time.Now().UTC().Add(time.Hour)
	create := func() string {
		r, err := requests.Create(t.Context(), host.ID, selected.ID, nil, startsAt, "metrics")
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}

	played, cancelled := create(), create()
	for _, step := range []func() error{
		func() error { return requests.Join(t.Context(), played, guest.ID) },
		func() error { return requests.Leave(t.Context(), played, guest.ID) },
		func() error { return requests.Start(t.Context(), played, host.ID) },
		func() error { return requests.Close(t.Context(), played, host.ID) },
		func() error { return requests.Cancel(t.Context(), cancelled, host.ID) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]int64{"created": 2, "joined": 1, "left": 1, "started": 1, "closed": 1, "cancelled": 1}
	assertTotals := func(label string) {
		t.Helper()
		got := metricsTotals(t, pool)
		for kind, n := range want {
			if got[kind] != n {
				t.Fatalf("%s: %s = %d, want %d (all %v)", label, kind, got[kind], n, got)
			}
		}
	}
	assertTotals("after lifecycle")

	// Rejected / no-op mutations count nothing (started only once per open->playing).
	if requests.Start(t.Context(), played, host.ID) == nil || requests.Cancel(t.Context(), cancelled, host.ID) == nil ||
		requests.Leave(t.Context(), cancelled, guest.ID) == nil {
		t.Fatal("expected rejected mutations")
	}
	assertTotals("after rejected mutations")

	// Rolled-back activity and backfill-origin rows do not count.
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO game_request_activity (game_request_id, actor_id, kind) VALUES ($1, $2, 'joined')`, played, guest.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO game_request_activity (game_request_id, actor_id, kind, origin) VALUES ($1, $2, 'joined', 'backfill')`, played, guest.ID); err != nil {
		t.Fatal(err)
	}
	assertTotals("after rollback and backfill")

	// Restart (re-migrate, fresh connections) and account deletion keep totals.
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, host.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM game_request_activity WHERE kind <> 'joined'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("activity left after deletion = %d, %v", remaining, err)
	}
	assertTotals("after account deletion")
}

func TestMetricsUpgradeRetainsHistoricalLowerBound(t *testing.T) {
	pool := newTestPool(t)
	migrations := productionMigrationFS(t)
	files, err := fs.ReadDir(migrations, ".")
	if err != nil {
		t.Fatal(err)
	}
	prior := fstest.MapFS{}
	for _, file := range files {
		if file.Name() >= "00029" {
			continue
		}
		data, err := fs.ReadFile(migrations, file.Name())
		if err != nil {
			t.Fatal(err)
		}
		prior[file.Name()] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(t.Context(), pool, prior, testLogger()); err != nil {
		t.Fatal(err)
	}
	var requestID, actorID string
	if err := pool.QueryRow(t.Context(), `
		WITH actor AS (INSERT INTO users(game_nick) VALUES ('HistoricalHost') RETURNING id),
		game AS (INSERT INTO games(name, slug) VALUES ('HistoricalGame', 'historical-game') RETURNING id),
		request AS (INSERT INTO game_requests(game_id, creator_id, starts_at) SELECT game.id, actor.id, now() FROM game, actor RETURNING id, creator_id)
		SELECT id::text, creator_id::text FROM request`).Scan(&requestID, &actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO game_request_activity(game_request_id, actor_id, kind, origin) VALUES ($1,$2,'created','backfill'), ($1,$2,'joined','live')`, requestID, actorID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"upgrade", "deletion", "restart"} {
		if phase == "deletion" {
			if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, actorID); err != nil {
				t.Fatal(err)
			}
		}
		if phase == "restart" {
			if err := Migrate(t.Context(), pool, testLogger()); err != nil {
				t.Fatal(err)
			}
		}
		got := metricsTotals(t, pool)
		if got["created"] != 1 || got["joined"] != 1 {
			t.Fatalf("%s totals = %v", phase, got)
		}
	}
}
