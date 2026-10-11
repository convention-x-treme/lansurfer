# Load-test report

Copy this file, fill every field from real output, and attach it to the relevant issue or PR. Aggregate numbers only: never paste session tokens, cookies, or the credentials file.

## Classification

- [ ] Local rehearsal (workstation, load generator on the same machine)
- [ ] Representative run (pilot-equivalent hardware, proxy, network; generator elsewhere)

A local rehearsal is not capacity evidence.

## Environment (`bash tools/loadtest/collect-env.sh`)

```text
<paste output>
```

Reverse proxy / network path: <none (direct loopback) | describe>
Docker resource limits: <from collect-env output>

## Workload

| Item | Value |
| --- | --- |
| Data | `task loadtest:seed USERS=…`, `task loadtest:fixtures GAMES=… REQUESTS=…` |
| HTTP read profile | `task loadtest:run USERS=… DURATION=…` |
| Write profile | `task loadtest:writes USERS=… DURATION=…` (create/join/leave/chat, CSRF enforced) |
| SSE | `task loadtest:sse USERS=… [REQUEST= TRIGGER= MODE=off|browser-like|worst-case RECONNECT=1]` |
| Join race | `task loadtest:race REQUEST=… JOINERS=… ` (repeat `-repeat` for double submit) |
| Browser calibration | `task loadtest:browser-calibration REQUEST=… SIMULATED_USERS=… BROWSER_CLIENTS=… DURATION=…` |
| Fault proxy | `task loadtest:proxy DELAY=… DROP_EVERY=… CUT_AFTER=… OFFLINE_FOR=… SSE_ONLY=1 RESET_SSE=1` |
| Scale fixtures | `task loadtest:scale-fixtures GAMES=… REQUESTS=… CHAT_MESSAGES=…` |
| Soak | `task loadtest:soak USERS=… DURATION=…` (HTTP writes + SSE + DB samples) |
| Profile | `task loadtest:profile DURATION=… INTERVAL=…` |

## Results

### HTTP latency and errors

| Profile | VUs | Duration | Requests | Response bytes | p95 | p99 | Max | Unexpected rate |
| --- | --- | --- | --- | --- | --- | --- | --- |

Error classification: `ok` / `conflict` (expected business refusal) / `unexpected` (403, 5xx, redirect to login, anything else). `http_req_failed` also counts the deliberate CSRF-negative 403s; use `loadtest_unexpected_response_rate` for server errors.

### Write actions

| Action | ok | conflict | unexpected | p95 | p99 |
| --- | --- | --- | --- | --- | --- |

### SSE and fan-out

| Streams | Connected / peak | Unexpected disconnects | Deliveries (expected / observed) | Delay p50 / p95 / max | Refresh GETs (ok / failed) | Refresh p95 |
| --- | --- | --- | --- | --- | --- | --- |

### Browser calibration

| Real pages | Simulated streams | Duration | Events expected / observed | Reconnects | Converged pages | DOM mismatches | Refresh p95 |
| --- | --- | --- | --- | --- | --- | --- | --- |

### Network fault injection

| Scenario | Delay | HTTP 503 rate | SSE offline window | Streams recovered | Events missing | Browser result |
| --- | --- | --- | --- | --- | --- | --- |

### Concurrency invariants

| Request capacity / members before | Joiners (× repeat) | Joined | Members after | Duplicates | Invariants hold |
| --- | --- | --- | --- | --- | --- |

### Recovery

| Scenario | Outage | Time to recover | Errors during outage | Errors after recovery |
| --- | --- | --- | --- | --- |
| App restart (`docker compose restart lansurfer`) | | | | |
| Database stop, then start (`docker compose stop postgres`, `start postgres`) | | | | |

### System and database pool

CPU, memory, goroutines, and pgx pool wait from `/metrics` or Grafana when available: <values or "not collected">.
Profile sample errors: <`sample_errors` from JSONL summary; explain any `sample_error` rows>.

### Scale fixtures and soak

| Games | Requests | Chat messages | Soak duration | HTTP VUs | SSE streams | Profile interval | Unexpected responses |
| --- | --- | --- | --- | --- | --- | --- | --- |

## Blockers and observations

- <anything that failed, is slower than expected, or limits the conclusion>

## Not covered

- Grafana dashboards, pilot-equivalent infrastructure, notification delivery, and 600-client stress: <not run / describe>
- Packet loss/delay used `tc netem` inside the proxy container only (egress, not RTT): <executed / not executed with reason>
- Run bundle path and `manifest.json` exit code/duration: <path>; `loadtest:scan` hits: <0>
