# LANsurfer Grafana dashboard

Import [lansurfer.json](lansurfer.json) through Grafana's **Dashboards > New > Import** (upload the JSON file). Select an existing Prometheus data source in the dashboard's **Prometheus** selector, then the LANsurfer **Job** and **Instance**. The default job is `lansurfer`, matching the [scrape example](../../README.md#prometheus-metrics).

Validated with Grafana 13.0.2 using classic dashboard JSON (`schemaVersion: 39`), its built-in Prometheus data source, and standard Stat/Time series panels. No plugins, recording rules, additional LANsurfer services, or application changes are needed. Set the Prometheus data source's scrape interval to the real scrape interval (30 seconds in the example). The dashboard refreshes every 30 seconds and uses `$__rate_interval` for counter rates.

The Metrics API Key belongs in Prometheus's scrape configuration, not this dashboard or Grafana's data source credentials. Grafana connects to Prometheus, which authenticates to LANsurfer. Generate/rotate the key in LANsurfer's **Administration > Metrics**; keep the credentials file and monitoring endpoints private.

## Reading the dashboard

- **Overview:** successful scrape, optional SQL collection health, pool utilization, active Game Requests, registered users, and process uptime. A healthy scrape does not prove the SQL aggregates were collected; check both indicators. Scrape failure can mean network failure, a stopped process, a missing/rotated key, or an authentication database failure. It is not a complete end-user availability probe.
- **HTTP:** request rate by status, 5xx fraction, latency percentiles, slowest route templates, and in-flight requests. Traffic and latency exclude scrape/readiness/liveness endpoints, static assets/media, and the long-lived SSE route. In-flight requests include SSE: open streams are expected and do not imply stuck page requests. Percentiles describe completed requests and can be unstable with sparse traffic; there is no useful latency/error ratio before requests have been observed.
- **PostgreSQL pool:** connections versus configured capacity, mean acquire/wait durations, and acquisition/wait/cancellation/connection-churn rates. Mean wait duration divides wait time by waiting acquisitions, not all acquisitions. Pool metrics concern LANsurfer's pgx pool, not PostgreSQL server CPU, locks, storage, or query latency. Sustained high utilization with waits or cancellations warrants investigation; the displayed 80%/95% thresholds are examples, not universal capacity limits.
- **Go/process:** CPU cores consumed, resident/heap memory, goroutines/threads, and file descriptor utilization. CPU can exceed one core. Runtime, HTTP, and pgx counters are process-local; `rate()` handles normal counter resets on restarts.
- **Usage and outcomes:** open/playing requests, users/unexpired browser sessions/follows, event rates, and six durable cumulative counters. Browser sessions are not online users. Host auto-membership is not a join; only the explicit first `open -> playing` transition counts as a started game.

## Reporting and failure caveats

Job and Instance intentionally allow **one value only**, with no **All** option. Database-backed values may be shared by several scrape targets; summing them would double-count users and event totals. Select one target for one LANsurfer installation. Aggregating several independent installations requires a deliberate reporting query, not enabling **All**.

Durable totals are raw latest cumulative values, **not events within the selected dashboard time range**. They survive application restarts and account/history deletion. Upgrade totals are a historical lower bound because deleted or never-recorded pre-upgrade history cannot be recovered; an older database restore can move totals backwards. Event-rate plots use sampled changes and are not exact event reports. Prometheus retention and scrape gaps limit historical analysis.

Missing metrics remain **No data**; the dashboard does not replace a failed SQL collector's missing product values with reassuring zeroes. A brief lack of rate data is normal after startup until enough samples exist. The 5xx numerator uses zero only when requests exist but no 5xx series has been observed. Optional-source failures appear in **SQL sources** even when the scrape remains healthy.

All panels use existing aggregate metrics and bounded route/status/source labels. Do not add Game Nicks, user/Game/Game Request/session IDs, provider identities, arbitrary content, query parameters, chat/session-information secrets, or credentials to the dashboard. Prometheus's `instance` label identifies the operator's scrape target, not a player.

This example contains no Grafana alert rules. Configure alerts separately for scrape failure, SQL collection failure, sustained 5xx/latency, and pool saturation/waits with thresholds and pending periods appropriate to the LAN.
