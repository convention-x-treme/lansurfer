# Load-test report

Copy this file, fill every field from real output, and attach it to the relevant issue or PR (#139 for harness rehearsals, #140/#93 for the pilot-equivalent run). Aggregate numbers only: never paste session tokens, cookies, or the credentials file.

## Classification

- [ ] Local rehearsal (workstation, load generator on the same machine)
- [ ] Representative run (pilot-equivalent hardware, proxy, network; generator elsewhere)

A local rehearsal is not capacity evidence and passes no #93 gate.

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
| SSE | `task loadtest:sse USERS=… [REQUEST= TRIGGER= REFRESH=1 RECONNECT=1]` |
| Join race | `task loadtest:race REQUEST=… JOINERS=… ` (repeat `-repeat` for double submit) |

## Results

### HTTP latency and errors

| Profile | VUs | Duration | Requests | p95 | p99 | Max | Unexpected rate |
| --- | --- | --- | --- | --- | --- | --- | --- |

Error classification: `ok` / `conflict` (expected business refusal) / `unexpected` (403, 5xx, redirect to login, anything else). `http_req_failed` also counts the deliberate CSRF-negative 403s; use `loadtest_unexpected_response_rate` for server errors.

### Write actions

| Action | ok | conflict | unexpected | p95 | p99 |
| --- | --- | --- | --- | --- | --- |

### SSE and fan-out

| Streams | Connected / peak | Unexpected disconnects | Deliveries (expected / observed) | Delay p50 / p95 / max | Refresh GETs (ok / failed) | Refresh p95 |
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

## Blockers and observations

- <anything that failed, is slower than expected, or limits the conclusion>

## Not covered

- <scenarios not run, e.g. 600-client stress, notification delivery with fakes>
