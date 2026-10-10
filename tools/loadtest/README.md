# LANsurfer local load-test harness

Part of #139. Everything here targets only the isolated Compose project `lansurfer-loadtest` (database `lansurfer_loadtest`, `127.0.0.1:18080`, PostgreSQL on `127.0.0.1:55433`). It never reads `LANSURFER_DATABASE_URL`, never touches the development stack, and needs no Keycloak/OIDC/Discord account.

What it provides:

1. **Seed** independent normal LANsurfer users and real browser sessions (`loadtest:seed`).
2. **Fixtures**: local Games, follows, Game Requests (open / nearly full / playing / empty) and memberships through the normal domain services (`loadtest:fixtures`).
3. **HTTP/HTMX reads** with k6 in Docker (`loadtest:run`).
4. **HTTP/HTMX writes** with k6: create, join, leave, chat, with the real session and CSRF flow (`loadtest:writes`).
5. **Join race** against a nearly-full request, verifying capacity and membership in the database (`loadtest:race`).
6. **SSE** streams, mutation-to-stream delay, the list-refresh amplification a browser causes per event, and reconnect recovery (`loadtest:sse`).

Not covered: notification delivery (Web Push/Discord; test separately with fakes, never provider endpoints), `/metrics` scraping, Grafana, pilot-equivalent hardware, and any 600-client stress run. These stay in #139 / #140 / #93.

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

## 3. HTTP reads

```sh
task loadtest:run USERS=20 DURATION=30s
task loadtest:run USERS=100 DURATION=2m
task loadtest:run USERS=400 DURATION=3m
task loadtest:report
```

Each virtual player uses its own seeded session and randomly browses Games, searches, Game Request lists/fragments, and the composer with think times. Reports go to `$HOME/lansurfer-loadtest-reports` (`LANSURFER_LOADTEST_REPORT_DIR`). The exploratory threshold (unexpected responses < 1%) is not a release SLO.

## 4. HTTP writes (CSRF enforced)

```sh
task loadtest:writes USERS=20 DURATION=1m
task loadtest:writes USERS=120 DURATION=3m
```

Each player keeps a cookie jar with its session; LANsurfer issues `lansurfer_csrf` on a normal page GET and the script submits it as `csrf_token`, like the rendered forms. Per iteration: create 5%, join 35%, leave 15%, chat 42%, and a 3% negative check that a POST without a token gets `403`. Outcomes are `ok`, `conflict` (expected refusal such as full, already a member, not a member, chat rate limit), or `unexpected` (403, 5xx, redirect to login, anything else). `http_req_failed` also counts the deliberate 403s; use `loadtest_unexpected_response_rate` for real errors. The first unexpected responses per player are logged with status only.

## 5. Concurrent joins

Pick an open request with free places that none of the racing players hosts, then:

```sh
task loadtest:race REQUEST=<request-id> JOINERS=100
```

All joiners get a CSRF token first, wait on a barrier, and POST `/join` at once. The command then checks in PostgreSQL that members never exceed capacity, there are no duplicate memberships, every success is a stored membership, and every free place was filled when there were enough joiners. For double submits call the command directly with `-repeat 3`:

```sh
go run ./tools/loadtest/race -request <id> -joiners 100 -repeat 3 -sessions "$HOME/lansurfer-loadtest-sessions.jsonl"
```

Each run consumes free places of its request; pick another request for the next run.

## 6. SSE, fan-out, and recovery

```sh
task loadtest:sse USERS=20 DURATION=45s
task loadtest:sse USERS=400 DURATION=1m
# mutation-to-stream delay and browser refresh amplification:
task loadtest:sse USERS=400 DURATION=2m REQUEST=<request-id> TRIGGER=20 REFRESH=1
# restart/reconnect (restart the app from another terminal while this runs):
task loadtest:sse USERS=400 DURATION=75s RECONNECT=1
```

`TRIGGER=N` (even) makes the last seeded player join and leave `REQUEST` alternately N times, with a CSRF-correct POST every 500 ms. Every stream records when it sees each event; delay is measured from just before the POST. Choose an open request with a free place that the trigger player neither hosts nor has joined, and that no concurrent write workload joins or leaves (requests created by `loadtest:writes` are safe). `REFRESH=1` makes each stream send the `GET /game-requests/refresh` an open list page sends for every event; the web client does not debounce it. `RECONNECT=1` tolerates drops, requires every stream to come back, and prints outage and recovery times.

Recovery scenarios against the isolated stack only:

```sh
docker compose -f tools/loadtest/compose.yaml restart lansurfer          # app restart
docker compose -f tools/loadtest/compose.yaml stop postgres              # database outage
docker compose -f tools/loadtest/compose.yaml start postgres
```

Run `loadtest:writes` or the SSE command during the outage and again afterwards; `/readyz` answers 503 while PostgreSQL is down.

## 7. Report

```sh
bash tools/loadtest/collect-env.sh   # revision, hardware, Docker limits; no secrets
```

Fill `tools/loadtest/report-template.md` from the real output and attach it to the issue/PR. Do not commit reports that contain environment-specific data unless the maintainers ask.

## 8. Stop and dispose

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
