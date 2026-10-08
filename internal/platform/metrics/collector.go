// Package metrics exposes aggregate-only Prometheus metrics. Nothing here
// carries per-user data, raw URLs or query text.
package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// scrapeTimeout bounds all database work for a single scrape, independent of
// normal request traffic.
const scrapeTimeout = 3 * time.Second

const (
	sourceAggregates = "aggregates"
	sourceTotals     = "product_totals"
)

// totalKinds maps metrics_product_totals.kind to its counter descriptor.
var totalKinds = []struct {
	kind string
	desc *prometheus.Desc
}{
	{"created", newDesc("lansurfer_game_requests_created_total", "Game requests ever created.")},
	{"joined", newDesc("lansurfer_game_request_joins_total", "Game request joins ever recorded.")},
	{"left", newDesc("lansurfer_game_request_leaves_total", "Game request leaves ever recorded.")},
	{"cancelled", newDesc("lansurfer_game_requests_cancelled_total", "Game requests ever cancelled.")},
	{"closed", newDesc("lansurfer_game_requests_closed_total", "Game requests ever closed.")},
	{"started", newDesc("lansurfer_games_started_total", "Games ever started.")},
}

func newDesc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labels, nil)
}

var (
	descUsers       = newDesc("lansurfer_users", "Registered users.")
	descSessions    = newDesc("lansurfer_browser_sessions", "Unexpired browser sessions.")
	descReqOpen     = newDesc("lansurfer_game_requests_open", "Game requests in open state.")
	descReqPlaying  = newDesc("lansurfer_game_requests_playing", "Game requests in playing state.")
	descReqActive   = newDesc("lansurfer_game_requests_active", "Game requests open or playing.")
	descFollows     = newDesc("lansurfer_game_follows", "Game follow subscriptions.")
	descCollectedOK = newDesc("lansurfer_metrics_collection_success",
		"1 if the last database source collection succeeded, else 0.", "source")

	descPoolMax          = newDesc("lansurfer_db_pool_max_connections", "Configured maximum pool size.")
	descPoolTotal        = newDesc("lansurfer_db_pool_total_connections", "Connections in the pool (idle, acquired, constructing).")
	descPoolAcquired     = newDesc("lansurfer_db_pool_acquired_connections", "Connections currently acquired.")
	descPoolIdle         = newDesc("lansurfer_db_pool_idle_connections", "Idle connections.")
	descPoolConstructing = newDesc("lansurfer_db_pool_constructing_connections", "Connections being established.")
	descPoolAcquires     = newDesc("lansurfer_db_pool_acquires_total", "Successful connection acquires.")
	descPoolEmpty        = newDesc("lansurfer_db_pool_empty_acquires_total", "Acquires that had to wait for a connection.")
	descPoolAcquireSecs  = newDesc("lansurfer_db_pool_acquire_seconds_total", "Total time spent in successful acquires.")
	descPoolEmptyWait    = newDesc("lansurfer_db_pool_empty_acquire_wait_seconds_total", "Total time waiting in empty acquires.")
	descPoolCanceled     = newDesc("lansurfer_db_pool_canceled_acquires_total", "Acquires canceled by context.")
	descPoolNewConns     = newDesc("lansurfer_db_pool_new_connections_total", "Connections ever established.")
	descPoolLifetime     = newDesc("lansurfer_db_pool_max_lifetime_destroyed_total", "Connections destroyed for exceeding max lifetime.")
	descPoolIdleDestroy  = newDesc("lansurfer_db_pool_max_idle_destroyed_total", "Connections destroyed for exceeding max idle time.")
)

// dbCollector reads pool statistics (no I/O) and database aggregates at
// scrape time only. Normal traffic never touches it.
type dbCollector struct {
	pool *pgxpool.Pool
}

func (c *dbCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		descUsers, descSessions, descReqOpen, descReqPlaying, descReqActive, descFollows, descCollectedOK,
		descPoolMax, descPoolTotal, descPoolAcquired, descPoolIdle, descPoolConstructing,
		descPoolAcquires, descPoolEmpty, descPoolAcquireSecs, descPoolEmptyWait,
		descPoolCanceled, descPoolNewConns, descPoolLifetime, descPoolIdleDestroy,
	} {
		ch <- d
	}
	for _, t := range totalKinds {
		ch <- t.desc
	}
}

func (c *dbCollector) Collect(ch chan<- prometheus.Metric) {
	c.collectPool(ch)

	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()

	ch <- success(sourceAggregates, c.collectAggregates(ctx, ch))
	ch <- success(sourceTotals, c.collectTotals(ctx, ch))
}

func success(source string, ok bool) prometheus.Metric {
	v := 0.0
	if ok {
		v = 1
	}
	return prometheus.MustNewConstMetric(descCollectedOK, prometheus.GaugeValue, v, source)
}

func (c *dbCollector) collectPool(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	g, ct := prometheus.GaugeValue, prometheus.CounterValue
	for _, m := range []struct {
		d *prometheus.Desc
		t prometheus.ValueType
		v float64
	}{
		{descPoolMax, g, float64(s.MaxConns())},
		{descPoolTotal, g, float64(s.TotalConns())},
		{descPoolAcquired, g, float64(s.AcquiredConns())},
		{descPoolIdle, g, float64(s.IdleConns())},
		{descPoolConstructing, g, float64(s.ConstructingConns())},
		{descPoolAcquires, ct, float64(s.AcquireCount())},
		{descPoolEmpty, ct, float64(s.EmptyAcquireCount())},
		{descPoolAcquireSecs, ct, s.AcquireDuration().Seconds()},
		{descPoolEmptyWait, ct, s.EmptyAcquireWaitTime().Seconds()},
		{descPoolCanceled, ct, float64(s.CanceledAcquireCount())},
		{descPoolNewConns, ct, float64(s.NewConnsCount())},
		{descPoolLifetime, ct, float64(s.MaxLifetimeDestroyCount())},
		{descPoolIdleDestroy, ct, float64(s.MaxIdleDestroyCount())},
	} {
		ch <- prometheus.MustNewConstMetric(m.d, m.t, m.v)
	}
}

const aggregatesSQL = `
SELECT
    (SELECT count(*) FROM users),
    (SELECT count(*) FROM auth_sessions WHERE expires_at > now()),
    count(*) FILTER (WHERE status = 'open'),
    count(*) FILTER (WHERE status = 'playing'),
    count(*) FILTER (WHERE status IN ('open', 'playing')),
    (SELECT count(*) FROM game_subscriptions)
FROM game_requests`

// collectAggregates emits nothing unless every value was read.
func (c *dbCollector) collectAggregates(ctx context.Context, ch chan<- prometheus.Metric) bool {
	var users, sessions, open, playing, active, follows int64
	err := c.pool.QueryRow(ctx, aggregatesSQL).Scan(&users, &sessions, &open, &playing, &active, &follows)
	if err != nil {
		return false
	}
	for _, m := range []struct {
		d *prometheus.Desc
		v int64
	}{
		{descUsers, users}, {descSessions, sessions}, {descReqOpen, open},
		{descReqPlaying, playing}, {descReqActive, active}, {descFollows, follows},
	} {
		ch <- prometheus.MustNewConstMetric(m.d, prometheus.GaugeValue, float64(m.v))
	}
	return true
}

// collectTotals emits durable counters; kinds with no row yet read as 0.
func (c *dbCollector) collectTotals(ctx context.Context, ch chan<- prometheus.Metric) bool {
	totals := make(map[string]int64, len(totalKinds))
	rows, err := c.pool.Query(ctx, `
		SELECT kind, sum(total)::bigint FROM (
			SELECT kind, total FROM metrics_product_totals
			UNION ALL
			SELECT kind, count(*) FROM game_request_activity
			WHERE origin = 'live' AND kind <> 'request_updated' GROUP BY kind
		) totals GROUP BY kind`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var total int64
		if err := rows.Scan(&kind, &total); err != nil {
			return false
		}
		totals[kind] = total
	}
	err = rows.Err()
	if err != nil {
		return false
	}
	for _, t := range totalKinds {
		ch <- prometheus.MustNewConstMetric(t.desc, prometheus.CounterValue, float64(totals[t.kind]))
	}
	return true
}
