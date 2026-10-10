# LANsurfer local load-test harness (in development)

Part of #139. The test harness has three implemented pieces:

1. Seed independent **normal LANsurfer users and browser sessions** directly into an isolated database without creating Keycloak, Discord, OIDC, or guest-login accounts.
2. Run authenticated **read-only HTTP/HTMX browsing** with k6 in Docker.
3. Open sustained **authenticated SSE streams** with a lightweight Go runner.

**Not yet implemented:** realistic test Games/Game Requests, create/join/leave/close/chat traffic, SSE mutation fan-out and reconnect correctness, database/system monitoring integration, recovery scenarios, and pilot-level capacity evidence. These are tracked in #139 and #93. The current tools do not yet prove that the first LAN will support 400 players.

## Requirements (WSL2)

WSL2/Linux with Docker Desktop integration, Docker Compose, Go 1.27.1 (as required by go.mod), and Task 3.x. Store the repository on the WSL Linux filesystem where possible.

All commands below run from the repository root on branch test/load-testing.

## 1. Start isolated LANsurfer

```sh
task loadtest:up
curl -fsS http://127.0.0.1:18080/readyz
```

This uses the dedicated Compose project "lansurfer-loadtest", a disposable PostgreSQL database named "lansurfer_loadtest", localhost HTTP port 18080, and localhost PostgreSQL port 55433. It does not use your regular development volumes.

## 2. Generate test players (once per fresh database)

```sh
export LANSURFER_LOADTEST_DATABASE_URL='postgres://lansurfer_loadtest:local-loadtest-only@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable'
umask 077
task loadtest:seed USERS=400
```

The default output is "$HOME/lansurfer-loadtest-sessions.jsonl". Each record contains the local player ID, unique Game Nick, and raw session token for the existing "lansurfer_session" cookie. The server stores only the session token hash. The file is created mode 0600 and may **not** be committed, copied to an issue, or logged. Its presence means the seed has already happened; **do not run seed twice** on the same database with the same nick prefix. The seeder refuses any database not named exactly "lansurfer_loadtest", and it never uses the normal "LANSURFER_DATABASE_URL".

If the credentials file is elsewhere, set "LANSURFER_LOADTEST_SESSIONS_FILE" to that full path before running HTTP or SSE.

## 3. HTTP load in Docker (no local k6 installation)

```sh
task loadtest:run USERS=20 DURATION=30s
task loadtest:run USERS=100 DURATION=2m
task loadtest:run USERS=200 DURATION=2m
task loadtest:run USERS=400 DURATION=3m
task loadtest:report
```

Try 20 users first. Start with a short duration, then increase. Each virtual player reads pages using one of the seeded sessions; k6 keeps each VU's own identity, disables redirect-following, and reports any authentication redirects as failures.

The current HTTP profile randomly navigates Games, filtered and searched Games, Game Request lists, filtered list fragments, refreshes, and the request composer. It uses real Cookie + optional HX-Request headers and user think times. **It performs no mutations and does not exercise CSRF or concurrency invariants yet.**

The script can only contact "http://lansurfer:8080" on the private Docker Compose network. It reads the owner-only credentials by bind mount, as the current WSL user. Reports with HTTP duration p95/p99 and check/failure rates go to "$HOME/lansurfer-loadtest-reports" (override with "LANSURFER_LOADTEST_REPORT_DIR"). The exploratory unexpected-response threshold is below 1%; that is not an agreed release SLO.

## 4. SSE connections (same seeded users)

```sh
task loadtest:sse USERS=20 DURATION=45s
task loadtest:sse USERS=400 DURATION=1m
```

The Go runner uses localhost port 18080 only. It opens one real SSE stream per seeded user, counts successfully connected/peak simultaneous streams, unexpected disconnects, connection failures, and "game-request" events. A read-only environment is expected to produce **zero events**. To measure write-to-SSE propagation and resulting browser refresh amplification, we still need the planned mutation workload.

For a combined read-only workload, run a k6 HTTP test and an SSE test in two WSL terminals at the same time. Both may reuse a seeded user token concurrently.

## 5. Stop and dispose of data

```sh
task loadtest:down                        # keep test DB volume
# DESTRUCTIVE: delete only the dedicated isolated test DB!
docker compose -f tools/loadtest/compose.yaml down -v
rm -f "$HOME/lansurfer-loadtest-sessions.jsonl"
unset LANSURFER_LOADTEST_DATABASE_URL
```

"down -v" destroys everything in the dedicated load-test stack, not the ordinary development stack. Use the exact Compose file and double-check the project before deleting.

## Interpretation and pilot validation

A local WSL result measures your workstation *and* the load generator, so it is a development rehearsal, not reliable pilot capacity evidence. For the actual #93 validation run, repeat against pilot-equivalent hardware, proxy, and network conditions, ideally with the load generator on a different machine. Record exact git revision, hardware/resources, workload, observed p95/p99, server/database metrics, SSE behavior, failures and recovery. No #93 acceptance checkbox is automatically passed by a working local test.
