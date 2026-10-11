package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type sample struct {
	SchemaVersion      int                `json:"schema_version"`
	Kind               string             `json:"kind"`
	ConfiguredDuration string             `json:"configured_duration"`
	Interval           string             `json:"interval"`
	ElapsedSeconds     float64            `json:"elapsed_seconds"`
	At                 time.Time          `json:"at"`
	Metrics            map[string]float64 `json:"metrics"`
	DB                 dbSample           `json:"database"`
	Pool               map[string]uint64  `json:"pool"`
	DatabaseDelta      dbSample           `json:"database_delta"`
	ProfileRequests    uint64             `json:"profile_requests_total"`
	InvalidWindow      bool               `json:"invalid_window"`
	InvalidReason      string             `json:"invalid_reason,omitempty"`
	Containers         []containerSample  `json:"containers"`
}
type summary struct {
	SchemaVersion      int    `json:"schema_version"`
	Kind               string `json:"kind"`
	ConfiguredDuration string `json:"configured_duration"`
	Interval           string `json:"interval"`
	ActualDuration     string `json:"actual_duration"`
	Samples            int    `json:"samples"`
	SampleErrors       int    `json:"sample_errors"`
	InvalidWindows     int    `json:"invalid_windows"`
}
type sampleFailure struct {
	SchemaVersion      int       `json:"schema_version"`
	Kind               string    `json:"kind"`
	ConfiguredDuration string    `json:"configured_duration"`
	Interval           string    `json:"interval"`
	ElapsedSeconds     float64   `json:"elapsed_seconds"`
	At                 time.Time `json:"at"`
	Error              string    `json:"error"`
}
type dbSample struct {
	Connections     []connection     `json:"connections"`
	LocksNotGranted int64            `json:"locks_not_granted"`
	Database        databaseStats    `json:"database_stats"`
	WALBytes        int64            `json:"wal_bytes"`
	Tables          []tableStats     `json:"tables"`
	Statements      []statementStats `json:"statement_families"`
	PostmasterStart time.Time        `json:"-"`
}
type connection struct {
	State, WaitEventType, WaitEvent string
	Count                           int64
}
type databaseStats struct {
	Commits        int64 `json:"commits"`
	Rollbacks      int64 `json:"rollbacks"`
	BlocksRead     int64 `json:"blocks_read"`
	BlocksHit      int64 `json:"blocks_hit"`
	TuplesReturned int64 `json:"tuples_returned"`
	TuplesFetched  int64 `json:"tuples_fetched"`
	TuplesInserted int64 `json:"tuples_inserted"`
	TuplesUpdated  int64 `json:"tuples_updated"`
	TuplesDeleted  int64 `json:"tuples_deleted"`
}
type tableStats struct {
	Table      string `json:"table"`
	SeqScans   int64  `json:"seq_scans"`
	IndexScans int64  `json:"index_scans"`
	Inserts    int64  `json:"inserts"`
	Updates    int64  `json:"updates"`
	Deletes    int64  `json:"deletes"`
	LiveRows   int64  `json:"live_rows"`
	DeadRows   int64  `json:"dead_rows"`
}
type statementStats struct {
	Family              string  `json:"family"`
	Calls               int64   `json:"calls"`
	TotalMS             float64 `json:"total_ms"`
	MeanMS              float64 `json:"mean_ms"`
	MaxExecMSCumulative float64 `json:"max_exec_ms_cumulative"`
	Rows                float64 `json:"rows"`
	BlockRead           float64 `json:"block_read"`
	BlockHit            float64 `json:"block_hit"`
	WALBytes            float64 `json:"wal_bytes"`
}
type containerSample struct {
	Role             string  `json:"role"`
	Available        bool    `json:"available"`
	CPUPercent       float64 `json:"cpu_percent,omitempty"`
	MemoryBytes      uint64  `json:"memory_bytes,omitempty"`
	MemoryLimitBytes uint64  `json:"memory_limit_bytes,omitempty"`
	Error            string  `json:"error,omitempty"`
}

const profileRequestMetric = "lansurfer_http_requests_total"

func main() {
	duration := flag.Duration("duration", time.Minute, "profiling duration")
	interval := flag.Duration("interval", 5*time.Second, "sample interval")
	keyPath := flag.String("metrics-key-file", "", "private bearer key file")
	output := flag.String("output", "", "private JSONL output path")
	flag.Parse()
	if err := run(*duration, *interval, *keyPath, *output); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest profile:", err)
		os.Exit(1)
	}
}

func run(duration, interval time.Duration, keyPath, output string) error {
	if duration <= 0 || interval <= 0 || interval > 5*time.Second || keyPath == "" || output == "" {
		return errors.New("positive -duration, -interval up to 5s, -metrics-key-file and -output are required")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return errors.New("cannot read private metrics key")
	}
	info, err := os.Stat(keyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || len(key) == 0 {
		return errors.New("metrics key file must be a private regular file")
	}
	cfg, err := target.DatabaseConfig()
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return errors.New("connect to isolated load-test database")
	}
	defer pool.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private profile output")
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	ctx, cancel := context.WithTimeout(context.Background(), duration+10*time.Second)
	defer cancel()
	started := time.Now()
	metricsKey := strings.TrimSpace(string(key))
	var previous *dbSample
	samples, sampleErrors, invalidWindows, err := collectSamples(ctx, duration, interval, started, writer, func() (bool, error) {
		invalid, err := writeSample(ctx, pool, client, metricsKey, writer, duration, interval, started, &previous)
		return invalid, err
	})
	if err != nil {
		return err
	}
	if err := json.NewEncoder(writer).Encode(summary{SchemaVersion: 1, Kind: "summary", ConfiguredDuration: duration.String(), Interval: interval.String(), ActualDuration: time.Since(started).String(), Samples: samples, SampleErrors: sampleErrors, InvalidWindows: invalidWindows}); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if sampleErrors > 0 {
		return fmt.Errorf("profile completed with %d failed samples; inspect JSONL output", sampleErrors)
	}
	return nil
}

func collectSamples(ctx context.Context, duration, interval time.Duration, started time.Time, writer *bufio.Writer, sampleFn func() (bool, error)) (samples, failures, invalid int, err error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	deadline := started.Add(duration)
	for {
		bad, e := sampleFn()
		if e != nil {
			failures++
			err = json.NewEncoder(writer).Encode(sampleFailure{SchemaVersion: 1, Kind: "sample_error", ConfiguredDuration: duration.String(), Interval: interval.String(), ElapsedSeconds: time.Since(started).Seconds(), At: time.Now().UTC(), Error: sanitizeError(e)})
		} else {
			samples++
			if bad {
				invalid++
			}
		}
		if err != nil {
			return
		}
		if err = writer.Flush(); err != nil {
			return
		}
		if time.Until(deadline) <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			break
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			break
		}
	}
	return
}

func writeSample(ctx context.Context, pool *pgxpool.Pool, client *http.Client, key string, out *bufio.Writer, duration, interval time.Duration, started time.Time, previous **dbSample) (bool, error) {
	metrics, err := scrape(client, key)
	if err != nil {
		return false, err
	}
	db, err := readDB(ctx, pool)
	if err != nil {
		return false, err
	}
	dbDelta := dbSample{}
	invalid := false
	reason := ""
	if *previous == nil {
		invalid = true
		reason = "baseline"
	} else if !db.PostmasterStart.Equal((*previous).PostmasterStart) {
		invalid = true
		reason = "postgres_restart"
	} else {
		dbDelta, invalid = deltaDB(db, **previous)
		if invalid {
			reason = "counter_reset"
		}
	}
	ps := pool.Stat()
	poolMetrics := map[string]uint64{"acquired_connections": uint64(ps.AcquiredConns()), "max_connections": uint64(ps.MaxConns()), "empty_acquires": uint64(ps.EmptyAcquireCount()), "canceled_acquires": uint64(ps.CanceledAcquireCount()), "empty_acquire_wait_nanoseconds": uint64(ps.EmptyAcquireWaitTime())}
	containers := dockerMetrics()
	*previous = &db
	err = json.NewEncoder(out).Encode(sample{SchemaVersion: 1, Kind: "sample", ConfiguredDuration: duration.String(), Interval: interval.String(), ElapsedSeconds: time.Since(started).Seconds(), At: time.Now().UTC(), Metrics: metrics, DB: db, Pool: poolMetrics, DatabaseDelta: dbDelta, ProfileRequests: uint64(metrics[profileRequestMetric]), InvalidWindow: invalid, InvalidReason: reason, Containers: containers})
	return invalid, err
}

func scrape(client *http.Client, key string) (map[string]float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:18080/metrics", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("metrics scrape failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("metrics scrape returned status %d", resp.StatusCode)
	}
	return parseMetrics(resp.Body)
}

// parseMetrics sums plain samples per family and keeps HTTP histograms per
// method and chi route template for the bounded set in profiledRoutes.
func parseMetrics(body io.Reader) (map[string]float64, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(body)
	if err != nil {
		return nil, errors.New("metrics response could not be parsed")
	}
	result := map[string]float64{}
	for name, family := range families {
		if name == "process_start_time_seconds" || name == "pg_postmaster_start_time_seconds" {
			if len(family.Metric) == 1 && family.Metric[0].Gauge != nil {
				result[name] = family.Metric[0].Gauge.GetValue()
			}
			continue
		}
		for _, m := range family.Metric {
			if m.Gauge != nil {
				result[name] += m.Gauge.GetValue()
			}
			if m.Counter != nil {
				result[name] += m.Counter.GetValue()
			}
			if m.Untyped != nil {
				result[name] += m.Untyped.GetValue()
			}
			if m.Histogram != nil {
				labels := map[string]string{}
				for _, label := range m.Label {
					labels[label.GetName()] = label.GetValue()
				}
				method, route := labels["method"], labels["route"]
				if !profiledRoutes[method+" "+route] {
					continue
				}
				base := name + "{method=" + method + ",route=" + route + "}"
				result[base+"_count"] += float64(m.Histogram.GetSampleCount())
				result[base+"_sum"] += m.Histogram.GetSampleSum()
				for _, bucket := range m.Histogram.Bucket {
					result[base+"_bucket_"+fmt.Sprint(bucket.GetUpperBound())] += float64(bucket.GetCumulativeCount())
				}
			}
		}
	}
	return result, nil
}

// profiledRoutes is the fixed set of method + chi route template pairs the
// load tests exercise. Values must equal the labels LANsurfer exports; the
// fixed set also bounds the number of keys per sample.
var profiledRoutes = map[string]bool{
	"GET /":                                  true,
	"GET /games":                             true,
	"GET /game-requests":                     true,
	"GET /game-requests/new":                 true,
	"GET /game-requests/refresh":             true,
	"POST /game-requests":                    true,
	"GET /game-requests/{requestID}":         true,
	"GET /game-requests/{requestID}/refresh": true,
	"GET /game-requests/{requestID}/lobby":   true,
	"POST /game-requests/{requestID}/join":   true,
	"POST /game-requests/{requestID}/leave":  true,
	"POST /game-requests/{requestID}/chat":   true,
	"GET /events/game-requests":              true,
	"GET /metrics":                           true,
	"GET /readyz":                            true,
	"GET /healthz":                           true,
}

func readDB(ctx context.Context, pool *pgxpool.Pool) (dbSample, error) {
	var out dbSample
	if err := pool.QueryRow(ctx, `SELECT pg_postmaster_start_time()`).Scan(&out.PostmasterStart); err != nil {
		return out, err
	}
	rows, err := pool.Query(ctx, `SELECT coalesce(state,'unknown'),coalesce(wait_event_type,''),coalesce(wait_event,''),count(*) FROM pg_stat_activity WHERE datname=current_database() GROUP BY 1,2,3`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v connection
		if err := rows.Scan(&v.State, &v.WaitEventType, &v.WaitEvent, &v.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Connections = append(out.Connections, v)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, rows.Err()
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted`).Scan(&out.LocksNotGranted); err != nil {
		return out, err
	}
	if err := pool.QueryRow(ctx, `SELECT xact_commit,xact_rollback,blks_read,blks_hit,tup_returned,tup_fetched,tup_inserted,tup_updated,tup_deleted FROM pg_stat_database WHERE datname=current_database()`).Scan(&out.Database.Commits, &out.Database.Rollbacks, &out.Database.BlocksRead, &out.Database.BlocksHit, &out.Database.TuplesReturned, &out.Database.TuplesFetched, &out.Database.TuplesInserted, &out.Database.TuplesUpdated, &out.Database.TuplesDeleted); err != nil {
		return out, err
	}
	if err := pool.QueryRow(ctx, `SELECT wal_bytes FROM pg_stat_wal`).Scan(&out.WALBytes); err != nil {
		return out, err
	}
	rows, err = pool.Query(ctx, `SELECT relname,seq_scan,idx_scan,n_tup_ins,n_tup_upd,n_tup_del,n_live_tup,n_dead_tup FROM pg_stat_user_tables WHERE relname IN ('auth_sessions','game_requests','game_request_members','game_request_activity','game_request_messages') ORDER BY relname`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v tableStats
		if err := rows.Scan(&v.Table, &v.SeqScans, &v.IndexScans, &v.Inserts, &v.Updates, &v.Deletes, &v.LiveRows, &v.DeadRows); err != nil {
			rows.Close()
			return out, err
		}
		out.Tables = append(out.Tables, v)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, rows.Err()
	}
	rows, err = pool.Query(ctx, `SELECT CASE WHEN lower(query) LIKE 'update auth_sessions%' AND position('last_seen_at' in lower(query))>0 THEN 'session_resolve' WHEN lower(query) LIKE 'select%' AND lower(query) LIKE '%game_requests%' THEN 'request_read' WHEN lower(query) LIKE 'insert%' OR lower(query) LIKE 'update%' OR lower(query) LIKE 'delete%' THEN 'request_write' ELSE 'other' END, sum(calls)::bigint,sum(total_exec_time),sum(total_exec_time)/greatest(sum(calls),1),max(max_exec_time),sum(rows),sum(shared_blks_read),sum(shared_blks_hit),sum(wal_bytes) FROM pg_stat_statements GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v statementStats
		if err := rows.Scan(&v.Family, &v.Calls, &v.TotalMS, &v.MeanMS, &v.MaxExecMSCumulative, &v.Rows, &v.BlockRead, &v.BlockHit, &v.WALBytes); err != nil {
			rows.Close()
			return out, err
		}
		out.Statements = append(out.Statements, v)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, rows.Err()
	}
	return out, nil
}

func deltaDB(cur, prev dbSample) (dbSample, bool) {
	out := dbSample{PostmasterStart: cur.PostmasterStart}
	invalid := false
	d := func(a, b int64) int64 {
		if a < b {
			invalid = true
			return 0
		}
		return a - b
	}
	out.Database = databaseStats{Commits: d(cur.Database.Commits, prev.Database.Commits), Rollbacks: d(cur.Database.Rollbacks, prev.Database.Rollbacks), BlocksRead: d(cur.Database.BlocksRead, prev.Database.BlocksRead), BlocksHit: d(cur.Database.BlocksHit, prev.Database.BlocksHit), TuplesReturned: d(cur.Database.TuplesReturned, prev.Database.TuplesReturned), TuplesFetched: d(cur.Database.TuplesFetched, prev.Database.TuplesFetched), TuplesInserted: d(cur.Database.TuplesInserted, prev.Database.TuplesInserted), TuplesUpdated: d(cur.Database.TuplesUpdated, prev.Database.TuplesUpdated), TuplesDeleted: d(cur.Database.TuplesDeleted, prev.Database.TuplesDeleted)}
	out.WALBytes = d(cur.WALBytes, prev.WALBytes)
	oldTables := map[string]tableStats{}
	for _, v := range prev.Tables {
		oldTables[v.Table] = v
	}
	for _, v := range cur.Tables {
		p := oldTables[v.Table]
		out.Tables = append(out.Tables, tableStats{Table: v.Table, SeqScans: d(v.SeqScans, p.SeqScans), IndexScans: d(v.IndexScans, p.IndexScans), Inserts: d(v.Inserts, p.Inserts), Updates: d(v.Updates, p.Updates), Deletes: d(v.Deletes, p.Deletes), LiveRows: v.LiveRows, DeadRows: v.DeadRows})
	}
	oldStatements := map[string]statementStats{}
	for _, v := range prev.Statements {
		oldStatements[v.Family] = v
	}
	for _, v := range cur.Statements {
		p := oldStatements[v.Family]
		out.Statements = append(out.Statements, statementStats{Family: v.Family, Calls: d(v.Calls, p.Calls), TotalMS: deltaFloat(v.TotalMS, p.TotalMS, &invalid), MeanMS: v.MeanMS, MaxExecMSCumulative: v.MaxExecMSCumulative, Rows: deltaFloat(v.Rows, p.Rows, &invalid), BlockRead: deltaFloat(v.BlockRead, p.BlockRead, &invalid), BlockHit: deltaFloat(v.BlockHit, p.BlockHit, &invalid), WALBytes: deltaFloat(v.WALBytes, p.WALBytes, &invalid)})
	}
	return out, invalid
}
func deltaFloat(a, b float64, invalid *bool) float64 {
	if a < b {
		*invalid = true
		return 0
	}
	return a - b
}

func dockerMetrics() []containerSample {
	roles := []struct{ role, filter string }{
		{"app", "label=com.docker.compose.service=lansurfer"},
		{"postgres", "label=com.docker.compose.service=postgres"},
		{"k6", "label=com.lansurfer.loadtest.role=k6"},
	}
	out := make([]containerSample, 0, len(roles))
	for _, item := range roles {
		args := []string{"ps", "-q", "--filter", item.filter}
		if item.role != "k6" {
			args = append(args, "--filter", "label=com.docker.compose.project=lansurfer-loadtest")
		}
		id, err := exec.Command("docker", args...).Output()
		if err != nil || len(strings.TrimSpace(string(id))) == 0 {
			out = append(out, containerSample{Role: item.role, Error: "unavailable"})
			continue
		}
		fields := strings.Fields(string(id))
		if len(fields) != 1 {
			out = append(out, containerSample{Role: item.role, Error: "ambiguous"})
			continue
		}
		raw, err := exec.Command("docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}", fields[0]).Output()
		if err != nil {
			out = append(out, containerSample{Role: item.role, Error: "unavailable"})
			continue
		}
		cpuText, memText, ok := strings.Cut(strings.TrimSpace(string(raw)), "|")
		parts := strings.Split(memText, "/")
		cpu, cpuErr := strconv.ParseFloat(strings.TrimSuffix(cpuText, "%"), 64)
		if !ok || len(parts) != 2 || cpuErr != nil {
			out = append(out, containerSample{Role: item.role, Error: "parse_failed"})
			continue
		}
		used, err := parseBytes(strings.TrimSpace(parts[0]))
		if err != nil {
			out = append(out, containerSample{Role: item.role, Error: "parse_failed"})
			continue
		}
		limit, _ := parseBytes(strings.TrimSpace(parts[1]))
		out = append(out, containerSample{Role: item.role, Available: true, CPUPercent: cpu, MemoryBytes: used, MemoryLimitBytes: limit})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out
}
func parseBytes(v string) (uint64, error) {
	var n float64
	var unit string
	if _, err := fmt.Sscanf(v, "%f%s", &n, &unit); err != nil {
		return 0, err
	}
	scale := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "KiB": 1024, "MB": 1e6, "MiB": 1048576, "GB": 1e9, "GiB": 1073741824}[unit]
	if scale == 0 {
		return 0, errors.New("unknown unit")
	}
	return uint64(n * scale), nil
}
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(strings.ToLower(s), "password") || strings.Contains(s, "Bearer ") {
		return "sample failed"
	}
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
