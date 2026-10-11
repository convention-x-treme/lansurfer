# Contributing to LANsurfer

Thanks for helping make LANsurfer useful and maintainable.

This guide is the starting point for a new developer. Before changing code, read [AGENTS.md](AGENTS.md). Consult the relevant GitHub issue/milestone entry for current work and dependencies, plus the sections of [the architecture](docs/ARCHITECTURE.md), [product definition](PRODUCT.md), [terminology guide](docs/TERMINOLOGY.md), and [design system](DESIGN.md) relevant to your task.

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

`.env` is ignored by Git and optional. Task loads it automatically for every `task` command, so `task run` needs no manual exports. Variables already set in your shell or CI environment take precedence over `.env` values:

```sh
task run
LANSURFER_LISTEN_ADDR=:9090 task run # overrides .env
```

The loading is a Task convenience only. Running `go run ./cmd/lansurfer` or the built binary directly still reads the process environment only, and production has no implicit `.env` loading. Docker Compose reads the same `.env` itself for variable substitution.

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
- `PRODUCT.md`: durable product behavior, approved scope, and product boundaries
- `docs/TERMINOLOGY.md`: canonical domain/project terminology
- `DESIGN.md`: design system; `docs/ui-kit/` for specific screen compositions and interactions
- `docs/ARCHITECTURE.md`: technical decisions and module boundaries
- `AGENTS.md`: repository-wide development rules and approval boundaries
- `Taskfile.yml`: authoritative development commands

Planning lives in GitHub Issues and milestones, not in a repository roadmap file. Use them for software development only: proposals, bugs, tests, build/release tooling, and technical documentation. Live deployment, event preparation, and release approvals are not tracked there. An open proposal is not approved product scope until `PRODUCT.md` changes.

Check the current GitHub issue/milestone status and the existing code before designing a change.

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

## Local load and reliability testing

The optional load-test harness uses **only** the isolated Compose project `lansurfer-loadtest` (database `lansurfer_loadtest`, HTTP `127.0.0.1:18080`, PostgreSQL `127.0.0.1:55433`). It does not use the development/production database, Keycloak, or real notification accounts. Run it on WSL2/Linux with Docker Compose, Go, Task, Node.js/npm and Playwright Chromium. Read the [complete load-test guide](tools/loadtest/README.md) for the workloads, safeguards, report handling, and reset procedure.

From the repository root, bootstrap the browser tools, start the isolated stack, and initialize its test data:

```sh
task loadtest:deps             # npm ci and Playwright Chromium (first time)
task loadtest:up
export LANSURFER_LOADTEST_DATABASE_URL='postgres://lansurfer_loadtest:local-loadtest-only@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable'
task loadtest:seed USERS=400    # first time on a fresh test DB only
task loadtest:fixtures GAMES=20 REQUESTS=60  # first time only
task loadtest:prepare          # control request and private metrics key
```

Run a small smoke test and then stop the isolated stack without deleting its database:

```sh
task loadtest:browser
task loadtest:run USERS=20 DURATION=30s
task loadtest:soak USERS=20 DURATION=2m BROWSERS=5
task loadtest:down
```

A soak also performs a five-minute cooldown before the final audit. Seed session tokens and the metrics key are secrets: **never commit or share them**. Keep fixture manifests and reports private; only share reviewed, sanitized report excerpts after checking for token/key leakage with `task loadtest:scan`. Do not repeat seed/fixture creation against an existing database.

Additional Task entry points cover writes, join races, SSE join/leave fan-out, chat lost-signal checks, profiling, browser calibration, and proxy/network chaos; some require a prepared Game Request ID. See `task --list` and the [load-test guide](tools/loadtest/README.md) for exact parameters. The load-test `task loadtest:browser` is **not** the product's `task test:browser`. Known product failures reproduced by chaos tests are tracked separately and do not indicate that the isolated harness is unsafe.

## Useful development commands

| Command | Purpose |
| --- | --- |
| `task --list` | Show the authoritative command list |
| `task dev` | Generate, build, and run LANsurfer plus PostgreSQL |
| `task run` | Run the Go application from the current environment plus optional `.env` |
| `task db:up` | Start only the development PostgreSQL service |
| `task db:down` | Stop the development Compose services |
| `task generate` | Regenerate templ, frontend assets, branding, and sqlc output |
| `task frontend:build` | Regenerate templ and production frontend assets |
| `task lint` | Run formatting/module/vet and TypeScript checks |
| `task test` | Run race-enabled unit tests |
| `task test:integration` | Run isolated PostgreSQL integration tests |
| `task test:browser` | Run Chromium interaction-hardening regression tests |
| `task build` | Build `bin/lansurfer` |
| `task loadtest:deps` | Install Node dependencies and Chromium for load-test browser scenarios |
| `task loadtest:up` / `task loadtest:down` | Start/stop the separate load-test Compose stack (retain its DB volume) |
| `task loadtest:run` / `task loadtest:writes` | Run isolated HTTP read/write profiles |
| `task loadtest:browser` | Run the isolated Playwright smoke test (not the product browser suite) |
| `task loadtest:soak` | Run mixed HTTP/SSE/browser load, profiling, cooldown, and audit |
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

## Commit messages and pull request titles

Use **Conventional Commits for new work only**. The same format applies to individual commit subjects (including AI-generated commits) and to pull request titles:

```text
type(scope): short imperative summary
type(scope)!: short imperative summary
```

- **Type:** `feat` (feature), `fix` (bug), `perf` (performance), `refactor`, `docs`, `test`, `build`, `ci`, `chore`, or `revert`. Do not invent new types without updating the validator and the documentation together.
- **Scope is required.** Use one lowercase product/module or maintenance area; lowercase letters, digits, and internal hyphens only. Typical scopes: `auth`, `user`, `game`, `gamerequest`, `notification`, `web`, `db`, `release`, `docs`, `deps`, `docker`; use `repo` only for genuinely cross-cutting changes. Scopes are examples, not a fixed allowlist.
- **Summary:** keep it short, descriptive, and preferably in imperative English (for example, "handle expired sessions"). Start a human-written summary in lowercase, keep the title on one line, do not add a trailing period. Bot-generated dependency summaries may retain their own casing.
- **Breaking change:** insert `!` before the colon, and explain the incompatible behavior under a `BREAKING CHANGE:` paragraph in the PR description / final squash commit body. The maintainer still selects the SemVer version explicitly.
- **Squash merges:** a validated PR title becomes the final squash commit subject. Check the proposed subject in the GitHub merge dialog instead of accepting an automatically generated or unrelated one. Intermediate branch commits should use the same convention but are not individually gated by CI.
- **Backports, reverts, and housekeeping:** follow the same title syntax (for example, `fix(auth): backport session expiry fix` or `revert(web): restore previous layout`). GitHub's default `Revert ...` title may need to be renamed before merging.
- **History:** do not amend old commits, rewrite tags, or rename old PRs to make history uniform. The first automated release notes must gracefully handle legacy messages; see #84.

Examples:

```text
feat(gamerequest): add capacity filter
fix(auth): handle expired sessions
perf(db): optimize game discovery queries
docs(repo): clarify contribution guidelines
ci(release): validate PR titles
chore(deps): update Go modules
feat(auth)!: remove legacy callback
```

The lightweight **PR title** workflow checks title structure on opening, editing, updating, and reopening a PR. It runs against the trusted target branch, with read-only permissions, without executing PR code. Its status is named **Conventional PR title**. Configure that check as required in branch protection/rulesets *when repository permissions and plan support it*; until then, a failed check is visible but may not technically block a squash merge. The existing CI also runs the validator's positive/negative test suite. Locally:

```sh
bash .github/scripts/validate-pr-title.sh "fix(auth): handle expired sessions"
bash .github/scripts/test-validate-pr-title.sh
```

Release notes themselves will be generated by the GoReleaser work in #84. This PR-title convention makes merged subjects predictable; it does not retroactively change releases or existing commits.

## Validating release artifacts locally

Release build configuration lives in `.goreleaser.yaml`. We use **GoReleaser OSS v2.18.3** for the release-build baseline (see #84); the existing `Dockerfile` continues to build development/Compose images, while `Dockerfile.release` packages the **same GoReleaser-built Linux amd64 and arm64 binaries** in `scratch` images.

Development and Compose images remain **local builds** (`task dev` or `docker compose up --build`). GHCR has no automatic `dev` / `dev-<sha>` publisher; only deliberate SemVer tag releases publish official images.

After installing GoReleaser OSS and ensuring your Go version matches `go.mod`, the safe development commands are:

```sh
task release:check
task release:notes:test
task release:snapshot
```

- `release:check` verifies the checked-in configuration without publishing.
- `release:notes:test` constructs a disposable Git history and **runs the actual GoReleaser changelog logic**, testing historical commits, breaking/features/fixes/performance groupings, prereleases, maintenance-branch backports, and release ranges with only internal changes. It does not push Git tags.
- `release:snapshot` builds Linux archives, checksums and **local-only per-platform Docker images**. It requires a running Docker daemon with Docker Buildx and arm64/QEMU support where applicable. Snapshot builds intentionally skip changelog generation, so the separate notes test is essential.

Inspect `dist/` for archives and checksums after a successful snapshot; the directory is ignored by Git. The PR snapshot workflow uses a pinned GoReleaser version, read-only repository permissions and no GHCR credentials, uploads only reviewable archives, and never publishes a version. Do not use plain `goreleaser release` against a real version tag; official publication must go through the reviewed `.github/workflows/release.yml` workflow after the tag's full CI gate.

GoReleaser OSS generates and groups changelog entries from reachable Git history. **Prereleases** compare against the preceding reachable version tag. **Stable releases** include all changes since the preceding *reachable stable* tag, even when one or more release candidates were published in between. For the **first stable release**, with no previous stable tag, the entire reachable history is included. Before publishing, `scripts/release/notes_config.py` creates a temporary GoReleaser configuration with exact prerelease-tag exclusions and sets `GORELEASER_PREVIOUS_TAG` when a prior stable exists. No tags are moved/deleted and no GoReleaser Pro feature is required. The live publishing script and disposable `task release:notes:test` fixtures exercise the same logic. New squash-merge titles drive predictable groups; nonconforming legacy subjects remain in **Other changes** rather than being rewritten. Alias promotion is a separate policy in #84.

## Creating and recovering official releases

Official tag-driven releases are implemented in #84. The first private pilot RC (`v0.1.0-rc.1`) and stable version (`v0.1.0`) were published in October 2026; never reuse or move either tag. A public repository, publicly pullable GHCR image and release-tag protection are separate GitHub settings tracked in #150.

### Maintainer-controlled release

Only an intentional **pushed version tag** activates `.github/workflows/release.yml` (not PRs, merges to `main`, workflow dispatch or Dependabot). Accepted tags include:

```text
v0.1.0-rc.1
v0.1.0
v0.1.1
v1.0.0
v1.4.2
v2.1.0-rc.1
```

No leading-zero version components, build metadata (`+...`), arbitrary prerelease names or retagging. **The maintainer chooses the SemVer number**, not Conventional Commits or automated version bumping.

**Pre-1.0 pilot releases:** `v0.1.0-rc.1` and `v0.1.0` were published as the first private pilot. Subsequent `v0.x.y` releases still publish only the exact GHCR version tag: no floating GHCR `v0`, `v0.1`, or `latest` aliases. The publisher requests `make_latest=false` for all 0.x releases, but GitHub's `GET /releases/latest` may still resolve the newest published stable 0.x release; that API behavior is **not** evidence of a GHCR `latest` tag. GitHub labels `-rc.N` as a prerelease; an unqualified `v0.x.y` is a regular SemVer release. Public 1.0 remains a separate milestone.

A tag must point to a commit reachable from `main` or an existing `release/vN` maintenance branch. The exact tagged revision runs the existing quality/unit, PostgreSQL, browser and build/Compose CI **before** the publisher receives write permissions. The publisher is serialized across all versions using GitHub Actions' `queue: max` (multiple pending release jobs are queued rather than canceled). Release tags should still be pushed deliberately and verified one at a time.

Once the right reviewed commit is on the appropriate branch, the maintainer can use:

```sh
git fetch origin --tags
git switch main
git pull --ff-only
git tag -a v0.1.0-rc.1 -m "LANsurfer v0.1.0-rc.1"
git push origin refs/tags/v0.1.0-rc.1
```

Use an **unused** approved version number; the example above uses an already-published tag and must not be executed verbatim. Never push a tag from a temporary feature branch, move or delete an existing published tag, or create/rewrite a release by rerunning a previously published version.

**What the automation does:** first reject invalid/previously used tags; confirm the GitHub tag object still matches the checkout; confirm no GitHub Release (including a draft) and no GHCR exact-version image exists. GoReleaser OSS then creates an **unpublished GitHub draft**, Linux amd64/arm64 archives and checksums, and an exact-version multi-architecture GHCR image. Before the GitHub draft goes public, the workflow checks the archives/checksums, release assets, image architectures and source tag again. It updates stable aliases only when they would not regress, then publishes the GitHub Release. Prereleases publish only their exact version tag, stay flagged as prereleases, and never change stable aliases or the GitHub "latest" badge.

**Aliases:** `v0.1.0` and all other pre-1.0 releases promote **none**. Beginning with `v1.0.0`, stable versions such as `v1.4.2` may promote `v1.4`, `v1` and `latest`. If `v2.0.0` is already published, a later `v1.4.3` backport only promotes `v1.4` and (if no newer `v1.x`) `v1`; it never changes `latest`. If a newer patch already exists on the same line, the older version cannot move that line's aliases. A newer **draft** release also conservatively prevents lower versions from promoting those aliases: its GHCR image/aliases may already have been created before a failed release. Reconcile failed drafts before proceeding with older versions. The publisher also requests the GitHub latest-release setting according to this numerical policy; GitHub's latest-release API may still select a published regular 0.x release even with `make_latest=false`. Only release automation should manage mutable GHCR aliases.

For a local review without publishing:

```sh
task release:check
task release:policy:test
task release:notes:test
task release:snapshot
```

The tag and alias validation logic can be exercised with `python3 scripts/release/release_policy.py` against locally saved GitHub release-list fixtures; it does not write to GitHub/GHCR. The PR snapshot CI runs these policy tests and cannot publish.

### Maintaining an older major version

When developing a new major on `main`, create a `release/vN` branch **only if that older major needs maintenance** (usually from its latest stable release). Cherry-pick applicable fixes onto the maintenance branch, open and squash-merge a properly titled PR there, run that branch's tests, then tag the reviewed maintenance-branch revision:

```sh
git fetch origin --tags
git switch -c release/v1 v1.4.2    # Only when first opening v1 maintenance
git push -u origin release/v1
# For each backport: cherry-pick on a short-lived branch, open a PR to release/v1.
# Once the reviewed backport is merged:
git switch release/v1
git pull --ff-only
git tag -a v1.4.3 -m "LANsurfer v1.4.3"
git push origin refs/tags/v1.4.3
```

Do not create a permanent branch per minor version. Avoid bypassing review just because the target is a maintenance branch.

### Failure or partial publication

There is **no atomic transaction** across GitHub draft creation, release-asset upload, the GHCR exact-version image and mutable aliases. A failed run may leave a **draft**, **exact-version image**, or a partially promoted alias.

- **Before a version exists:** fix the problem and create a *new* version tag if the earlier tag was published/partially used. Do not move an existing version tag.
- **Draft or registry image already exists:** the automatic workflow intentionally **fails closed** rather than overwriting it. Inspect the exact source revision, archive checksums, GHCR manifest architectures and alias state. Only a maintainer should decide whether to finish a fully verified draft manually or to issue a new version after correcting the problem. GitHub's `GET /releases/tags/{tag}` may return 404 for an unpublished **draft**; identify the draft via the paginated Releases collection and inspect it using `GET /releases/{id}` instead. Do not interpret a 404 from the tag lookup as permission to recreate/reuse a release. Do not delete or replace a published exact-version image to make a rerun pass.
- **Alias promotion fails:** avoid marking a release successful. Verify or repair the mutable aliases with the semver policy; don't point `latest` or `vN` to an older release.
- **After the GitHub Release is public:** preserve the tag/artifacts; fixes get a new SemVer version.

During the private-repository phase, GitHub branch/tag rulesets may be unavailable; #150 tracks enforcing them after going public. Until then, maintainers must avoid manual tag movement or bypassing these conventions. The workflow checks for moved refs but cannot guarantee protection from out-of-band administrative changes.

## Making changes

- Keep pull requests focused on one concrete problem.
- Work on a scheduled issue whose declared dependencies are satisfied; do not implement blocked work or open proposals for completeness.
- Inspect existing code and the relevant product/architecture sections before designing new structures.
- Add tests where regressions would matter.
- Do not add dependencies for hypothetical future flexibility.
- Update documentation when public behavior, configuration, or architecture changes.
- Explain new direct dependencies, significant tradeoffs, verification limits, and affected cross-capability contracts in the pull request.
- Feature-branch specs/plans/tasks are allowed when they help collaboration. Before merge, promote durable decisions to canonical docs, move remaining work to GitHub Issues, remove temporary working artifacts, and prefer a squash merge for branches that contained them.

Use clear domain terminology such as `GameRequest` and `GameRequestMember`; surf-themed language is reserved for branding and marketing copy.
