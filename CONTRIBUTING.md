# Contributing to LANsurfer

Thanks for helping make LANsurfer useful and maintainable.

This guide is the starting point for a new developer. Before changing code, read [AGENTS.md](AGENTS.md). Consult the relevant GitHub issue/milestone entry for current work and dependencies, plus the sections of [the architecture](docs/ARCHITECTURE.md), [product definition](docs/PRODUCT.md), and [terminology guide](docs/TERMINOLOGY.md) relevant to your task.

## Development prerequisites

Install these tools before running the development workflows:

- [Git](https://git-scm.com/downloads) for cloning and branches
- [Go](https://go.dev/dl/) 1.27.1 or later in the Go 1.27 release line
- [Node.js](https://nodejs.org/en/download) 24 with npm for frontend generation and checks
- [Docker](https://docs.docker.com/get-started/get-docker/) with Docker Compose v2; the repository uses the `docker compose` command
- [Task](https://taskfile.dev/docs/installation) for the documented development commands
- a C compiler such as GCC or Clang when running the race-enabled Go tests locally

Task supports several installation methods. Because Go is already a project prerequisite, one simple option is:

```sh
go install github.com/go-task/task/v3/cmd/task@latest
```

Make sure the Go binary directory is on your `PATH`, then verify the local toolchain:

```sh
git --version
go version
node --version
npm --version
docker --version
docker compose version
task --version
```

The project does not pin a Task executable version. `Taskfile.yml` is the authoritative list of project commands; run `task --list` whenever you are unsure which workflows are available.

## Clone the repository

Clone the repository with your normal GitHub credentials and enter the working tree:

```sh
git clone https://github.com/convention-x-treme/lansurfer.git
cd lansurfer
task --list
```

The first generator or build run may download Go modules, npm packages, templ, and sqlc tooling, so it requires network access.

## Choose a local development mode

There are two supported development patterns. The full Compose stack is the quickest way to get the application running. Running the Go process directly is useful when iterating on backend code.

### Option A: full stack with Compose

Run:

```sh
task dev
```

`task dev` first runs the project generators and then builds and starts LANsurfer plus PostgreSQL through Docker Compose. Because generation happens on the host, **Go, Node.js/npm, and Task are still required locally even though the application itself runs in Docker**.

The default local endpoints are:

- LANsurfer: <http://localhost:8080>
- PostgreSQL: `127.0.0.1:5432`
- health check: <http://localhost:8080/healthz>
- readiness check: <http://localhost:8080/readyz>

The application should return `ok` from `/healthz` and `ready` from `/readyz`.

Stop and remove the development services with:

```sh
task db:down
```

`compose.yaml` also supports `LANSURFER_HTTP_PORT` and `LANSURFER_POSTGRES_PORT` when the default host ports are occupied. If you change the HTTP port and need OIDC login, also use a matching `LANSURFER_BASE_URL` and ensure the identity provider accepts that callback origin.

### Option B: Go locally, PostgreSQL in Docker

Start only PostgreSQL:

```sh
task db:up
```

Create a local environment file from the checked-in example:

```sh
cp .env.example .env
```

`.env` is ignored by Git. `task run` does **not** load it automatically, so export the variables into your shell before starting the Go process. For POSIX shells such as Bash or Zsh:

```sh
set -a
. ./.env
set +a
task run
```

On shells that do not support that syntax, set the variables from `.env.example` using the shell's normal environment-variable mechanism before running `task run`.

`LANSURFER_DATABASE_URL` is required. `LANSURFER_LISTEN_ADDR` and `LANSURFER_BASE_URL` have localhost defaults. OIDC configuration is optional for starting the process, but authenticated workflows require a configured provider.

When you are finished, stop PostgreSQL with:

```sh
task db:down
```

## Local authentication and administrator access

The Compose development stack defaults to the CXT development OIDC realm and the `lansurfer` public client documented in [README.md](README.md#configure-oidc-login). The callback URL is `http://localhost:8080/auth/callback` by default.

You need an account accepted by the configured identity provider to exercise authenticated workflows. After your first successful login, LANsurfer creates your local user with the `user` role.

When running the full Compose stack, list users and promote your local account to administrator with:

```sh
task admin:users
task admin:promote USER_ID=<your-user-id>
```

When running the Go process directly instead of the `lansurfer` Compose service, keep the environment loaded and use the equivalent CLI commands:

```sh
go run ./cmd/lansurfer admin users
go run ./cmd/lansurfer admin promote --user-id <your-user-id>
```

Administrator access is useful for exercising catalog and other administration workflows during development.

To set up Web Push or Discord delivery locally, generate the VAPID keys and the data-encryption key. The command needs no configuration or database. Run it from the repository root and copy the printed lines into your `.env`; set `LANSURFER_WEBPUSH_SUBJECT` yourself (`mailto:` or `https://`):

```sh
go run ./cmd/lansurfer admin notifications generate-secrets
```

Use the package path `./cmd/lansurfer`, not `cmd/lansurfer/main.go`; the command spans several files. Never commit the output.

## Project orientation

The repository is a modular Go monolith. These are the most useful entry points for a new contributor:

- `cmd/lansurfer/`: executable entry point and CLI commands
- `internal/app/`: application composition and wiring
- `internal/auth/`, `internal/user/`, `internal/game/`, `internal/gamerequest/`: business modules
- `internal/web/`: HTTP handlers, middleware, templ pages/components, branding, and embedded assets
- `internal/query/`: read models for UI queries that cross module boundaries
- `internal/platform/`: PostgreSQL, configuration, SSE, and other technical infrastructure
- `web/`: frontend source files used to generate embedded production assets
- `docs/PRODUCT.md`: product behavior and approved scope
- `docs/TERMINOLOGY.md`: canonical domain/project terminology
- `docs/ROADMAP.md`: planning landing page; current status lives in GitHub Issues and the active release milestone
- `docs/ARCHITECTURE.md`: technical decisions and module boundaries
- `AGENTS.md`: repository-wide development rules and approval boundaries
- `Taskfile.yml`: authoritative development commands

Do not infer that a feature is implemented merely because it is described in product or architecture documentation. Check the current GitHub issue/milestone status and the existing code before designing a change.

## Generate code and frontend assets

Run:

```sh
task generate
```

after changing templ templates, frontend sources, branding, SQL queries, or migrations. The task:

1. installs the locked npm dependencies with `npm ci`
2. regenerates branding CSS
3. regenerates templ Go sources
4. builds the frontend assets and runs the TypeScript type check
5. regenerates sqlc database code

Generated Go code and production assets are committed to the repository. Review generated diffs before committing them.

`task dev` runs `task generate` before starting Compose. `task run` does not, so remember to generate explicitly when working directly on sources that require it.

## Tests, lint, and build

The normal local verification set is:

```sh
task lint
task test
task test:integration
task test:browser
task build
```

What these commands cover:

- `task lint`: Go formatting/module/vet checks plus npm install and TypeScript type checking
- `task test`: race-enabled Go unit tests
- `task test:integration`: an isolated PostgreSQL container plus race-enabled integration tests
- `task test:browser`: Chromium regression tests for interaction hardening
- `task build`: builds `bin/lansurfer`

The race detector requires a working C toolchain on platforms where Go uses CGO for `-race`.

The integration-test Compose stack uses host port `55432` and tears itself down after the task completes. If that port is already in use, free it before running `task test:integration`.

`task test:browser` downloads Chromium on first run and serves `internal/web/browsertest` on host port `8123`. That fixture runs the real router, templates, assets, and SSE handler against in-memory stores, so it needs no database. Its scope is deliberately narrow: the rapid-click, realtime stream lifecycle, abort-recovery, and SSE-versus-swap behavior that lives in `web/src/app.ts` and that no Go test can reach. Product behavior belongs in the Go unit and PostgreSQL integration tests, which are faster and more precise — do not grow this into a general end-to-end suite.

Run `task generate` before final verification when your change affects generated files. Continuous integration also checks formatting, Go modules, `go vet`, generated sqlc code, unit tests, PostgreSQL integration tests, browser interaction tests, the Go build, and a Compose startup smoke test.

## Useful development commands

| Command | Purpose |
| --- | --- |
| `task --list` | Show the authoritative command list |
| `task dev` | Generate, build, and run LANsurfer plus PostgreSQL |
| `task run` | Run the Go application from the current environment |
| `task db:up` | Start only the development PostgreSQL service |
| `task db:down` | Stop the development Compose services |
| `task generate` | Regenerate templ, frontend assets, branding, and sqlc output |
| `task frontend:build` | Regenerate templ and production frontend assets |
| `task lint` | Run formatting/module/vet and TypeScript checks |
| `task test` | Run race-enabled unit tests |
| `task test:integration` | Run isolated PostgreSQL integration tests |
| `task test:browser` | Run Chromium interaction-hardening regression tests |
| `task build` | Build `bin/lansurfer` |
| `task admin:users` | List users in the running Compose application |
| `task admin:promote USER_ID=<UUID>` | Promote a local Compose user to administrator |

## Troubleshooting the local stack

If the Compose application does not become ready, inspect its logs:

```sh
docker compose logs lansurfer
docker compose logs postgres
```

Check for port conflicts on `8080`, `5432`, or `55432`, and verify the OIDC issuer/client settings if login fails.

To remove the local development stack while keeping the PostgreSQL data volume, use:

```sh
task db:down
```

To completely reset the local PostgreSQL data, use the destructive command below only when you intentionally want a fresh database:

```sh
docker compose down --volumes
```

Database migrations run automatically before HTTP startup. A migration failure prevents the application from starting, so startup failures after schema changes should be investigated in the application logs rather than bypassing migrations.

## Making changes

- Keep pull requests focused on one concrete problem.
- Work only on scheduled capability work whose declared dependencies are satisfied; do not implement blocked integration work or unscheduled future candidates for completeness.
- Inspect existing code and the relevant product/architecture sections before designing new structures.
- Add tests where regressions would matter.
- Do not add dependencies for hypothetical future flexibility.
- Update documentation when public behavior, configuration, or architecture changes.
- Explain new direct dependencies, significant tradeoffs, verification limits, and affected cross-capability contracts in the pull request.
- Feature-branch specs/plans/tasks are allowed when they help collaboration. Before merge, promote durable decisions to canonical docs, move remaining work to GitHub Issues, remove temporary working artifacts, and prefer a squash merge for branches that contained them.

Use clear domain terminology such as `GameRequest` and `GameRequestMember`; surf-themed language is reserved for branding and marketing copy.
