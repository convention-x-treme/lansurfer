# LANsurfer local load-test harness

Part of #139. Everything here targets only the isolated Compose project `lansurfer-loadtest` (database `lansurfer_loadtest`, `127.0.0.1:18080`, PostgreSQL on `127.0.0.1:55433`). It never reads `LANSURFER_DATABASE_URL`, never touches the development stack, and needs no Keycloak/OIDC/Discord account.

What it provides:

1. **Seed** independent normal LANsurfer users and real browser sessions (`loadtest:seed`).
2. **Fixtures**: local Games, follows, Game Requests and memberships through the normal domain services (`loadtest:fixtures`, `loadtest:fixtures:large`).
3. **HTTP/HTMX browsing and writes** with k6 in Docker (`loadtest:run`, `loadtest:writes`), CSRF-correct, with strict outcome classification.
4. **Join race** against a nearly-full request, verifying capacity and membership in the database (`loadtest:race`).
5. **SSE** streams, fan-out, `browser-like` and `worst-case` refresh models, and reconnect recovery (`loadtest:sse`).
6. **Profiling**: authenticated Prometheus scrape, PostgreSQL activity/locks/WAL/table/statement-family deltas, and container CPU/RAM (`loadtest:profile`).
7. **Browser calibration** with 5–10 separate real Chromium processes against 120/400 simulated streams (`loadtest:calibrate`).
8. **Lost-signal proof**: a proxy drops one SSE frame while PostgreSQL keeps the change (`loadtest:loss`).
9. **Chaos**: stream cut, offline window, `tc netem` delay, and 2 % packet loss inside the test proxy container only (`loadtest:chaos`).
10. **Soak**: 120 participants (115 streams/writers + 5 real browsers), 10-minute profile windows, cooldown and database audit (`loadtest:soak`).
11. **Private run bundles**: every scenario task writes a `0700` directory with a `0600` manifest, console log and artifacts; `loadtest:scan` counts secrets in them.

Out of scope: notification delivery, Grafana dashboards, pilot-equivalent hardware, and 600-client stress.

## Requirements (WSL2)

WSL2/Linux with Docker Desktop integration, Docker Compose, Go 1.27.1 (go.mod), and Task 3.x. Keep the repository on the WSL Linux filesystem. All commands run from the repository root on branch `test/load-testing`.

## 1. Start isolated LANsurfer

```sh
task loadtest:up
curl -fsS http://127.0.0.1:18080/readyz
```

## 2. Seed players and fixtures (once per fresh database)

```sh
export LANSURFER_LOADTEST_DATABASE_URL='postgres://lansurfer_loadtest:local-loadtest-only@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable'
umask 077
task loadtest:seed USERS=400
task loadtest:fixtures GAMES=20 REQUESTS=60
```

`loadtest:seed` writes `$HOME/lansurfer-loadtest-sessions.jsonl` (mode 0600): user ID, Game Nick, and the raw `lansurfer_session` token per player. The server stores only token hashes. **Never commit, paste, or log this file.** Its presence means the seed already happened; do not seed twice with the same nick prefix. Override the path with `LANSURFER_LOADTEST_SESSIONS_FILE`.

`loadtest:fixtures` reads only user IDs from that file and writes request IDs (no tokens) to `$HOME/lansurfer-loadtest-fixtures.json` (override: `LANSURFER_LOADTEST_FIXTURES_FILE`). It refuses to run again while that file exists. Both commands refuse any database not named exactly `lansurfer_loadtest`.

## 3. HTTP/HTMX browsing

```sh
task loadtest:run USERS=20 DURATION=30s
task loadtest:run USERS=100 DURATION=2m
task loadtest:run USERS=400 DURATION=3m
task loadtest:report
```

Each virtual player uses its own seeded session and randomly browses Games, request search/filter/list, request details, refresh fragments, and the composer with think times. Detail views also update recent-view history. Reports go to `$HOME/lansurfer-loadtest-reports` (`LANSURFER_LOADTEST_REPORT_DIR`). The exploratory threshold (unexpected responses < 1%) is not a release SLO.

## 4. HTTP writes (CSRF enforced)

```sh
task loadtest:writes USERS=20 DURATION=1m
task loadtest:writes USERS=100 DURATION=2m
task loadtest:writes USERS=200 DURATION=2m
task loadtest:writes USERS=400 DURATION=2m SESSION_OFFSET=0

```

Each player keeps its session and CSRF cookies across iterations (`noCookiesReset: true`); LANsurfer issues `lansurfer_csrf` on a normal page GET, and the script submits it as `csrf_token`. Per iteration: create 5%, join 35%, leave 15%, chat 42%, and a 3% negative check that a POST without a token gets `403`. Outcomes are `ok`, `conflict` (expected refusal such as full, already a member, not a member, chat rate limit), or `unexpected`. The first unexpected responses per player log status, redirect, HTMX retarget, and a CSRF-rejection boolean; never log credentials.

`http_req_failed` also counts deliberate missing-CSRF `403` responses; use `loadtest_unexpected_response_rate` for unexpected server errors.

## 5. Concurrent joins

Pick an open request with free places that none of the racing players hosts, then:

```sh
task loadtest:race REQUEST=<request-id> JOINERS=100
```

All joiners get a CSRF token first, wait on a barrier, and POST `/join` at once. Sessions stay distinct; existing members may submit duplicate joins and count as expected conflicts. The command then checks PostgreSQL: members never exceed capacity, no memberships duplicate, every success is stored, and every free place is filled when joiners suffice. Each run consumes free places of its request; pick another request for the next run.

```sh
go run ./tools/loadtest/race -request <id> -joiners 100 -repeat 3 -sessions "$HOME/lansurfer-loadtest-sessions.jsonl"
```

Each run consumes free places of its request; pick another request for the next run.

## 6. SSE, fan-out, and recovery

```sh
task loadtest:sse USERS=20 DURATION=45s
task loadtest:sse USERS=400 DURATION=1m
# mutation-to-stream delay and browser refresh amplification:
task loadtest:sse USERS=400 DURATION=2m REQUEST=<request-id> TRIGGER=20 MODE=browser-like
# restart/reconnect (restart the app from another terminal while this runs):
task loadtest:sse USERS=400 DURATION=75s RECONNECT=1
```

`TRIGGER=N` alternates join and leave; an odd N leaves the trigger player joined, while an even N restores prior membership. Use an open request with free capacity that the last seeded player neither hosts nor has joined. `TRIGGER_DELAY` waits after every stream connects. Events are matched against POST completions; delay can be negative when SSE arrives first. `MODE=browser-like` models client refresh coalescing; `MODE=worst-case` refreshes once per event. `ALLOW_MISSING_EVENTS=1` records expected losses instead of failing the harness.

Recovery scenarios against the isolated stack only:

```sh
docker compose -f tools/loadtest/compose.yaml restart lansurfer          # app restart
docker compose -f tools/loadtest/compose.yaml stop postgres              # database outage
docker compose -f tools/loadtest/compose.yaml start postgres
```

Run `loadtest:writes` or the SSE command during the outage and again afterwards; `/readyz` answers 503 while PostgreSQL is down.

## 7. Browser calibration, lost signals, and chaos

Use `task loadtest:prepare` first; it creates (or safely reuses) one control Game Request that random writers never touch, and resets the trigger player through the normal `Leave` service. Its ID is in `$HOME/lansurfer-loadtest-control.json`. Never reuse a control request that is full; archive its file and prepare a new one.

```sh
REQ=$(node -p "require('$HOME/lansurfer-loadtest-control.json').request_id")
task loadtest:calibrate REQUEST=$REQ USERS=120      # 4 cells, idle and 100 writers, plus 115+5, bursts, refresh faults
task loadtest:calibrate REQUEST=$REQ USERS=400
task loadtest:loss REQUEST=$REQ                     # final SSE frame dropped; PostgreSQL keeps the change
task loadtest:chaos REQUEST=$REQ SCENARIO=cut|offline|delay|loss USERS=120|400 [DELAY_MS=200|500]
```

Both start `compose.chaos.yaml` (a Go reverse proxy with `NET_ADMIN`, bound to `127.0.0.1:18081`/`18082` only). `POST :18082/control` accepts only fixed modes with bounded fields: `normal`, `hold-refresh`, `fail-refresh`, `cut-streams`, `offline`, `drop-sse`, `delay`, `loss`. `tc netem` runs only inside the proxy container and is removed afterwards. If `NET_ADMIN` or netem is unavailable the packet scenarios exit 2 as "not executed".

Calibration launches 5–10 separate Chromium processes, observes EventSource `open`/`error` and named events, HTMX before/after/swap, busy and disabled state, and compares visible DOM with an authoritative server fragment. The final diagnostic fetch is excluded from refresh counts. Synthetic clients refresh the same filtered list as the pages (`-refresh-query`). `browser-like` coalesces to one in-flight refresh plus one trailing refresh per client; `worst-case` refreshes once per event. Synthetic streams reuse the browser identities in the 400-stream runs, so 400 streams plus five browsers are 400 identities, not 405.

Every scenario task writes `$HOME/lansurfer-loadtest-reports/<UTC>-<scenario>/` (mode `0700`) with `manifest.json` (SHA, dirty flag, image IDs/creation, container limits, versions, parameters, exit code, actual duration), `console.log`, and artifacts (`0600`). Run `task loadtest:scan REPORT_PATH=<dir>` before publishing; it prints only the number of session-token or metrics-key hits.

### Measured local results (WSL2, not capacity evidence)

Commit `411d094` plus uncommitted harness changes; app image built before this PR's harness changes (product code unchanged).

| 120 identities, 90 s, 20 mutations / 500 ms | Browser refresh p50 / p95 | Refresh per event | Deliveries | Result |
| --- | --- | --- | --- | --- |
| browser-like, idle | 2.0 s / 4.1 s | 0.79 | 2400/2400 | pass |
| browser-like, +100 writers | 1.7 s / 3.8 s | 0.09 | 2400/2400 | pass; writers p95 1.8 s |
| worst-case, idle | 3.2 s / 6.6 s | 0.75 | 2400/2400 | pass |
| worst-case, +100 writers | 15.0 s / 15.0 s (HTTP 500 timeouts) | 0.09 | 0/2400 | **fail**, writers p95 15 s, 5–6 % unexpected, reproduced twice |
| 115 streams + 5 browsers | 0.27 s / 0.32 s | 1.00 | 2300/2300 | pass |
| burst 40 mutations / 20 ms | 2.0 s / 4.3 s | 0.67 | 3480/4800 | **fail**, 1,320 lost signals |

At 400 identities every cell failed or degraded: idle browser-like p95 11.7 s with 5,600/8,000 deliveries, worst-case with writers 15 s timeouts and 0/8,000 deliveries, burst 3,200/16,000 deliveries. The mutation POST itself timed out under the worst-case writer load, so missing deliveries there include a stalled trigger.

Chaos at 120 / 400 streams (cut 30 s, offline 30–90 s, delay 30–90 s, 2 % loss 30–40 s; 5 browsers; serial control mutations on the direct port): sessions stayed valid before and after every run (0 invalid), the database audit passed, and the proxy returned to `normal`. Streams returned 1.7 s / 2.3 s after a cut and about 60 s after the offline window. Delay and loss produced no disconnects. After the 60 s offline window two of five 120-stream browsers (detail pages) remained stale without a manual reload; the 400-stream offline run exited nonzero because refresh GETs through the proxy returned 503 during the window, which the runner counts as failed refreshes. `tc -s qdisc` shows the netem qdisc during the fault and `noqueue` afterwards.

Lost signal (`loadtest:loss`): dropping one final SSE frame left list, detail and lobby pages stale after the stream stayed up (`events=0`), while PostgreSQL held the change (members 1→2, chat 0→1). A simulated visibility return (`hidden`→`visible`) restored every page. Reconnect-only recovery remains a product gap tracked in #147.

### Memory and unbounded list finding

A long soak leaves ~31,000 open requests from random `create` actions. Unfiltered `GET /game-requests/refresh` then returns 49 MB in 0.65 s; 120 concurrent unfiltered refreshes OOM-killed the app container (`OOMKilled=true`, exit 137) twice. The same request filtered by Game returns 328 KB. The harness therefore refreshes and browses filtered lists and primes CSRF on `/game-requests/new`. The unbounded list is a product scalability finding, not a harness fix; it is not a 120-player pilot blocker unless pilot data grows that large.

## 8. Large fixtures and soak

`task loadtest:scale-fixtures` was replaced by `task loadtest:fixtures:large` (`large-v1`: marked names/descriptions/chat markers, separate private manifest, duplicate-safe resume, domain services only). This workstation's database already held the older scale dataset (1,000 Games, 5,000 Game Requests, 5,000 chat messages), so the new command refused to mix manifests and was covered only by `go test ./tools/loadtest/fixtures`; run it on a fresh database.

```sh
task loadtest:prepare
task loadtest:profile DURATION=1m INTERVAL=5s
task loadtest:soak USERS=20 DURATION=2m BROWSERS=5
task loadtest:soak USERS=120 DURATION=10m BROWSERS=5
task loadtest:soak USERS=120 DURATION=2h BROWSERS=5
```

`loadtest:soak` runs 70 % browsing / 3 % create / 10 % join / 5 % leave / 12 % chat with 2–8 s think time through `runWriteAction`, `USERS-BROWSERS` SSE streams (browser-like refresh of a filtered list), `BROWSERS` real pages holding their identities, profiling in 10-minute window files, a 5-minute cooldown, and a final database audit. `BROWSERS` identities are rows 0..N-1 and are never writers.

Soak results: the 20-participant 2-minute soak and the 120-participant 10-minute soak exited 0. The 10-minute soak had 0 unexpected responses over 7,531 iterations, 5/5 browsers converged, and a clean audit. Its 115 browser-like refreshers (14,677 refreshes, 0 failed) saw p50 4.7 s / p95 5.2 s and the write requests p95 5.7 s, so refresh amplification dominates latency even with a filtered list. Earlier, before refresh was enabled in the soak, a two-hour run (120 writers + 120 streams) completed 465,766 requests with 0 unexpected outcomes, p95 6.2 ms, and 20.8 million events; peak RSS/heap 3.7/3.0 GB. These runs are not comparable and are not pilot-capacity evidence.

Local WSL results are workstation rehearsals, not pilot-capacity evidence.

## 9. Report

```sh
bash tools/loadtest/collect-env.sh   # revision, hardware, Docker limits; no secrets
```

Fill `tools/loadtest/report-template.md` from the real bundle and attach it to the issue/PR. Do not commit reports that contain environment-specific data unless the maintainers ask.

## 10. Stop and dispose

```sh
task loadtest:down                        # keep the test DB volume
# DESTRUCTIVE: deletes only the dedicated isolated test DB!
docker compose -f tools/loadtest/compose.yaml down -v
rm -f "$HOME/lansurfer-loadtest-sessions.jsonl" "$HOME/lansurfer-loadtest-fixtures.json"
unset LANSURFER_LOADTEST_DATABASE_URL
```

`down -v` destroys everything in the load-test stack only. Double-check the Compose file path before running it.

## Interpretation

A local WSL result measures your workstation and the load generator together, so it is a development rehearsal, not pilot capacity evidence. For the #140/#93 run, repeat on pilot-equivalent hardware, proxy, and network, ideally with the load generator on another machine, and record revision, resources, workload, p95/p99, server/database metrics, SSE behavior, failures, and recovery. A passing local run closes no #93 checkbox.
