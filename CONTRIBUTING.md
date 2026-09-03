# Contributing to LANsurfer

Thanks for helping make LANsurfer useful and maintainable.

Before changing code, read [AGENTS.md](AGENTS.md), [the architecture](docs/ARCHITECTURE.md), [the product definition](docs/PRODUCT.md), and [the roadmap](docs/ROADMAP.md). These documents define the project scope and architectural boundaries.

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

## Changes

- Keep pull requests focused on one concrete problem.
- Add tests where regressions would matter.
- Do not add dependencies for hypothetical future flexibility.
- Update documentation when public behavior, configuration, or architecture changes.
- Explain new direct dependencies and significant tradeoffs in the pull request.

Use clear domain terminology such as `GameRequest` and `GameRequestMember`; surf-themed language is reserved for branding and marketing copy.
