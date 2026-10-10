# LANsurfer local load-test harness (work in progress)

This directory is a test-only workstream for #139. It does **not** add guest access or an authentication bypass. The final HTTP/SSE load runners and measured acceptance report are not yet implemented.

## Supported development environment

WSL2/Linux with working Docker Desktop integration, Docker Compose, Go 1.27.1 (as required by `go.mod`), and a clone on the WSL ext4 filesystem is recommended.

## Start the isolated stack

From the repository root:

```sh
docker compose -f tools/loadtest/compose.yaml up -d --build --wait
curl -fsS http://localhost:18080/readyz
```

This uses a dedicated Compose project, database named `lansurfer_loadtest`, and loopback ports `18080` (HTTP) and `55433` (PostgreSQL). It does not use the root `compose.yaml` volumes.

## Create independent users and sessions

Run only after the application has completed its database migrations and `/readyz` is healthy:

```sh
export LANSURFER_LOADTEST_DATABASE_URL='postgres://lansurfer_loadtest:local-loadtest-only@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable'
umask 077
go run ./tools/loadtest/seed -users 400 -output "$HOME/lansurfer-loadtest-sessions.jsonl"
```

Each JSONL record includes a user ID, unique Game Nick, and opaque cookie token (the `lansurfer_session` cookie). The seeder uses normal user registration, the official session service and server-side session hashes; no Keycloak accounts are involved. **Treat the JSONL file as live authentication credentials.** Never commit it, paste it into an issue, or include it in diagnostic logs. The command will refuse to overwrite a preexisting file. Use a fresh test database before re-running with the same prefix.

The seeder refuses any database not named exactly `lansurfer_loadtest` and deliberately ignores the normal `LANSURFER_DATABASE_URL`. It cannot be used to log in to a different installation.

## Stop or dispose

```sh
docker compose -f tools/loadtest/compose.yaml down
# Destructive! Only the isolated load-test project:
docker compose -f tools/loadtest/compose.yaml down -v
rm -f "$HOME/lansurfer-loadtest-sessions.jsonl"
unset LANSURFER_LOADTEST_DATABASE_URL
```

The `down -v` operation deletes **all data in the disposable load-test stack**, not the regular developer Compose project. Do not run it against any other Compose file/project.

## Not yet done

- A k6/HTTP workload including CSRF/cookie handling and realistic browse/create/join/leave flows
- A sustained SSE load and fan-out/reconnection client
- Seeded Games/Game Requests and configurable activity mixes
- Performance dashboards, latency thresholds, failure/recovery scenarios and pilot hardware evidence

The pilot validation evidence belongs in #93 and must identify the exact revision, server hardware, generator location and measurements.
