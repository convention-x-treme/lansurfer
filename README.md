# 🌊 LANsurfer

**Find players. Start a game.**

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties, gaming events, and gaming communities.

At a large LAN event, the hard part is often not finding people. It is discovering who wants to play the same game at the same time. LANsurfer gives participants a simple web interface to follow games, create a Game Request, join other players, and start playing.

> LANsurfer is in early development. The architecture, schema, and user interface may change before the first release.

## Product direction

The first prototype is web-only and targets the CXT LAN party with roughly 400 participants. Its core flow is:

1. A user signs in through OpenID Connect.
2. The user follows games they are interested in.
3. A user creates a Game Request.
4. Other interested users see it immediately.
5. Players join the request and start playing.

The v0.1 scope includes server-rendered pages, HTMX interactions, Server-Sent Events, a local game catalog, basic event support, PostgreSQL-backed users and sessions, and Docker Compose self-hosting.

Discord, Web Push, chat, teams, tournaments, external game catalogs, and configurable instance branding are deliberately deferred. See [the product definition](docs/PRODUCT.md) and [roadmap](docs/ROADMAP.md) for details.

## Current status

The repository currently provides the backend and PostgreSQL foundation:

- Go application with chi routing
- PostgreSQL connection retry and readiness checks
- embedded goose migrations protected by a PostgreSQL advisory lock
- pgx and sqlc setup
- structured logging and graceful shutdown
- health and readiness endpoints
- Docker and Docker Compose workflows
- unit, integration, and CI checks

Product features and the frontend are not implemented yet.

## Quick start

Requirements:

- Docker with Docker Compose
- Go 1.27.1 for local Go development
- [Task](https://taskfile.dev/) for the documented task shortcuts

Start LANsurfer and PostgreSQL:

```sh
docker compose up --build
```

Then verify the service:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Both endpoints should return a successful response. Stop the stack with:

```sh
docker compose down
```

The Compose credentials are development defaults and must not be reused for an exposed deployment.

## Local development

Common commands:

```sh
task dev               # build and run the complete Compose stack
task run               # run Go locally using environment configuration
task build             # build bin/lansurfer
task lint              # check formatting, modules, and Go source
task test              # run race-enabled unit tests
task test:integration  # start an isolated PostgreSQL and run integration tests
task generate          # regenerate sqlc code
```

Running the Go process directly requires:

```sh
export LANSURFER_DATABASE_URL='postgres://lansurfer:lansurfer@127.0.0.1:5432/lansurfer?sslmode=disable'
export LANSURFER_LISTEN_ADDR=':8080' # optional; this is the default
go run ./cmd/lansurfer
```

## Architecture

LANsurfer is a modular Go monolith using server-side rendering and progressive enhancement. PostgreSQL is the only required infrastructure dependency.

Read these documents before contributing:

- [Development rules](AGENTS.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Product definition](docs/PRODUCT.md)
- [Roadmap](docs/ROADMAP.md)
- [Contributing](CONTRIBUTING.md)

## License

LANsurfer is licensed under the [GNU Affero General Public License v3.0](LICENSE).
