# Contributing to LANsurfer

Thanks for helping make LANsurfer useful and maintainable.

Before changing code, read [AGENTS.md](AGENTS.md). Consult the relevant [roadmap track and declared dependencies](docs/ROADMAP.md), plus the sections of [the architecture](docs/ARCHITECTURE.md) and [product definition](docs/PRODUCT.md) relevant to your task.

## Development setup

Install:

- Go 1.27.1 or later in the Go 1.27 release line
- Node.js 24 and npm for frontend generation
- Docker with Docker Compose
- [Task](https://taskfile.dev/) for the documented development commands

Start the complete development stack:

```sh
task dev
```

Run the checks used by continuous integration:

```sh
task lint
task test
task test:integration
task build
```

Run `task generate` after changing templ templates, frontend sources, branding,
SQL queries, or migrations. Commit the generated Go code and production assets;
the deployed binary does not require Node.js.

To run Go directly, start the development database with `task db:up`, then set the configuration shown in [.env.example](.env.example):

```sh
export LANSURFER_DATABASE_URL='postgres://lansurfer:lansurfer@127.0.0.1:5432/lansurfer?sslmode=disable'
export LANSURFER_LISTEN_ADDR=':8080'
task run
```

OIDC login is enabled by default in the local Compose environment. When running
Go directly, export `LANSURFER_BASE_URL`, `LANSURFER_OIDC_ISSUER`, and
`LANSURFER_OIDC_CLIENT_ID` as described in the README and `.env.example`.
`LANSURFER_OIDC_CLIENT_SECRET` is optional for a public PKCE client.
These credentials are for local development only. Run `task --list` for all
available workflows.

## Changes

- Keep pull requests focused on one concrete problem.
- Work only in 1.0 tracks whose declared dependencies are satisfied; do not implement blocked integration work or unscheduled post-1.0 candidates for completeness.
- Add tests where regressions would matter.
- Do not add dependencies for hypothetical future flexibility.
- Update documentation when public behavior, configuration, or architecture changes.
- Explain new direct dependencies and significant tradeoffs in the pull request.

Use clear domain terminology such as `GameRequest` and `GameRequestMember`; surf-themed language is reserved for branding and marketing copy.
