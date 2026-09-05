# Contributing to LANsurfer

Thanks for helping make LANsurfer useful and maintainable.

Before changing code, read [AGENTS.md](AGENTS.md). Consult the current [roadmap milestone](docs/ROADMAP.md#progress) and the sections of [the architecture](docs/ARCHITECTURE.md) and [product definition](docs/PRODUCT.md) relevant to your task.

## Development setup

Install:

- Go 1.27.1 or later in the Go 1.27 release line
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

Run `task generate` after changing SQL queries or migrations and commit the generated sqlc files.

To run Go directly, start the development database with `task db:up`, then set the configuration shown in [.env.example](.env.example):

```sh
export LANSURFER_DATABASE_URL='postgres://lansurfer:lansurfer@127.0.0.1:5432/lansurfer?sslmode=disable'
export LANSURFER_LISTEN_ADDR=':8080'
task run
```

These credentials are for local development only. Run `task --list` for all available workflows.

## Changes

- Keep pull requests focused on one concrete problem.
- Add tests where regressions would matter.
- Do not add dependencies for hypothetical future flexibility.
- Update documentation when public behavior, configuration, or architecture changes.
- Explain new direct dependencies and significant tradeoffs in the pull request.

Use clear domain terminology such as `GameRequest` and `GameRequestMember`; surf-themed language is reserved for branding and marketing copy.
