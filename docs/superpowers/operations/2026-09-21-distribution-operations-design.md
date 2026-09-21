# Distribution & Operations Design

**Date:** 2026-09-21  
**Tracking issue:** #29 — Distribution & Operations  
**Status:** approved-scope design; implementation-ready for slices F1-F3; final validation slice F4 is dependency-blocked

## Authority and planning model

This design is based on current `main`, issue #29, PRODUCT, ARCHITECTURE, AGENTS, the existing CI/Docker/Compose implementation, and the approved notification designs on `docs/track-e-notifications-sdd`.

It follows the planning-to-implementation model proposed in PR #54: durable behavior/architecture belongs in canonical documents, while this feature design resolves bounded distribution/operations decisions and the accompanying implementation plan carries temporary execution detail.

This branch is based on `main`, not PR #54, so that Track F is not technically stacked on the documentation-governance PR. Rebase/reconcile terminology after #54 merges.

## Context

Current `main` already provides useful foundations:

- Go 1.27.1 modular monolith;
- scratch-based non-root application image;
- PostgreSQL 18.6 development Compose service;
- automatic forward migrations before HTTP startup;
- `/healthz` and `/readyz`;
- CI with formatting, module/vet, unit, PostgreSQL integration, browser tests, binary build, and development-Compose smoke test;
- committed/generated frontend assets;
- administrator CLI commands.

Track F must turn those development foundations into a reproducible, verifiable release and supported self-hosting path.

Current gaps include:

- no release workflow or GitHub Release artifacts;
- no official Linux amd64/arm64 binaries;
- no multi-architecture GHCR publication;
- no embedded release version/commit command;
- no binary healthcheck command suitable for the scratch image;
- no operator key-generation commands;
- no production Compose file that consumes a pinned released image and keeps PostgreSQL private;
- no formal backup/restore/upgrade runbook or final evidence artifact;
- build-tool drift: contributor docs require Node 24 while CI currently uses Node 22;
- final notification persistence/configuration has not yet landed, so final populated backup/restore/upgrade evidence cannot honestly be completed yet.

## Goals

- Produce official Linux amd64 and arm64 release binaries.
- Publish one multi-architecture GHCR image for each release.
- Give every release artifact an unambiguous version and source revision.
- Publish checksums and supply-chain metadata using the smallest supported toolchain.
- Provide a clean-host production Compose path requiring only Docker/Compose plus operator configuration.
- Provide operator commands for version inspection, container healthchecking, the Track E data-encryption key, and Web Push VAPID keys.
- Document backup, restore, upgrade, rollback limits, configuration/secrets, administrator bootstrap, and reverse-proxy/TLS boundaries.
- Make release validation repeatable and evidence-driven.
- Preserve the existing simple two-service runtime model: LANsurfer + PostgreSQL.

## Non-goals

- No GoReleaser or general release framework.
- No Kubernetes/Helm/systemd installer.
- No bundled reverse proxy, certificate automation, Redis, queue, or observability stack.
- No automatic database DOWN migration or schema rollback.
- No in-application backup scheduler or backup storage service.
- No package repositories beyond GitHub Releases and GHCR.
- No Windows/macOS binaries for stable 1.0.
- No generic secret-management framework.
- No new signing/provenance product selected silently if the repository's GitHub plan cannot provide the approved native attestation capability.
- No claim that final restore/upgrade validation is complete before remaining stable-1.0 persistent migrations have landed.

## Slice model

Track F is an epic, not one safe implementation PR.

Use these implementation slices:

1. **F1 — Release identity and operator CLI**
2. **F2 — Release artifacts and supply chain**
3. **F3 — Production deployment and operator runbook**
4. **F4 — Final restore / upgrade / release evidence**

F1-F3 can be implemented before notification delivery is complete. F4 remains blocked until the stable-1.0 persistent schema/configuration required for the candidate is settled.

## Decision 1: semantic release identity

Git release tags are immutable exact semantic versions:

- stable: `vX.Y.Z`
- prerelease: `vX.Y.Z-<prerelease>`

Do not create moving Git tags for major/minor/latest.

The application embeds exactly two release identity values:

- version;
- source commit SHA.

Do not embed build wall-clock time. Release output must not become non-reproducible merely because two builders ran at different times.

Local/unversioned builds use a clearly non-release default such as `dev` / `unknown`.

`lansurfer version` reports the embedded version and source revision without loading application configuration or connecting to PostgreSQL.

## Decision 2: stable and prerelease image tags

The canonical image is:

`ghcr.io/convention-x-treme/lansurfer`

For a stable `v1.2.3` release publish:

- `v1.2.3` — exact release tag, never intentionally reused;
- `v1.2` — moving stable-minor tag;
- `v1` — moving stable-major tag;
- `latest` — moving latest-stable tag.

For a prerelease such as `v1.2.3-rc.1`, publish only the exact prerelease tag.

Do not move `latest`, major, or minor tags for prereleases.

The GitHub Release itself is always attached to the exact Git tag.

## Decision 3: one build identity for binaries and image

Release builds use:

- exact Go version from `go.mod`;
- `CGO_ENABLED=0`;
- `-trimpath`;
- `-buildvcs=false`;
- identical ldflags for embedded version and commit.

The Dockerfile becomes the canonical release build definition and uses explicit `TARGETOS` / `TARGETARCH` build arguments so Buildx can produce both release-image platforms and binary exports from the same Go build stage.

The Go builder image must be pinned to the project Go version and a committed image digest. Pinning by version tag alone is insufficient for a release build input.

Generated frontend/template/sqlc output remains committed. Release verification regenerates it and fails on drift before publishing; the runtime build does not install Node/npm.

## Decision 4: align the frontend build toolchain

Node 24 is the project LTS line already documented for contributors. CI/release must stop using Node 22.

Pin one exact Node 24 patch version in one repository-level version file and have CI/release consume it. At planning time the current LTS patch is 24.21.0; implementation should commit that exact version unless a newer approved 24.x patch is deliberately selected while implementing.

`package-lock.json` remains the npm dependency lock.

The release PR should not opportunistically move to Node 26/current; Track F is eliminating drift, not changing the frontend platform line.

## Decision 5: operator CLI

Add these commands without changing normal no-argument server startup:

### `lansurfer version`

- no configuration required;
- no database access;
- prints release version and source commit;
- local builds clearly report non-release values.

### `lansurfer healthcheck`

- no database configuration load;
- checks the running application's readiness endpoint;
- default target is the local listener derived from `LANSURFER_LISTEN_ADDR`, falling back to loopback port 8080;
- wildcard listen hosts are checked through loopback;
- optional `--url` overrides the target for operators/tests;
- short finite timeout (3 seconds);
- exits non-zero on connection failure, non-2xx response, or unexpected readiness body.

The final scratch image uses this command in Docker `HEALTHCHECK`, so no curl/wget shell dependency is introduced.

### `lansurfer generate data-encryption-key`

- uses `crypto/rand`;
- generates exactly 32 random bytes;
- prints raw unpadded base64url suitable for `LANSURFER_DATA_ENCRYPTION_KEY`;
- writes no secret to logs;
- no configuration/database required.

This matches the approved notification encryption contract.

### `lansurfer generate web-push-keys`

- generates one P-256 VAPID keypair compatible with the approved Track E Web Push configuration;
- outputs shell-friendly assignments for:
  - `LANSURFER_WEBPUSH_PUBLIC_KEY`
  - `LANSURFER_WEBPUSH_PRIVATE_KEY`
- does not invent a Web Push subject; the operator must explicitly configure `LANSURFER_WEBPUSH_SUBJECT`;
- no configuration/database required.

Use standard-library crypto for generation/encoding where practical; do not add a release-only framework.

## Decision 6: release workflow and permissions

Add a dedicated tag-triggered release workflow for `v*.*.*`.

Use only:

- native shell/Go/Docker tooling;
- GitHub-maintained actions;
- Docker-maintained actions.

All actions are pinned by full commit SHA with a version comment, matching the existing CI style.

Grant permissions per job, not globally. Publication jobs receive only the permissions they need, such as:

- `contents: write` for GitHub Release publication;
- `packages: write` for GHCR;
- `id-token: write` and `attestations: write` only where native artifact attestations are created.

Before publishing, validate:

- tag is accepted SemVer;
- release commit is on the intended mainline history;
- generated source/assets are clean;
- applicable project verification passes.

Do not publish on ordinary branch pushes or pull requests.

## Decision 7: binary release artifacts

Publish deterministic archives for:

- linux/amd64;
- linux/arm64.

Recommended names:

- `lansurfer_<version>_linux_amd64.tar.gz`
- `lansurfer_<version>_linux_arm64.tar.gz`

Each archive contains:

- `lansurfer`;
- `LICENSE`;
- concise release/install readme if useful.

Normalize tar ownership, ordering, timestamps, and gzip metadata so rebuilding the same release input with the same toolchain does not vary solely because of archive metadata.

Publish one `SHA256SUMS` covering downloadable release assets whose bytes are expected to be stable.

## Decision 8: SBOM and provenance

### Container image

Build/push with Docker Buildx using:

- multi-platform `linux/amd64,linux/arm64`;
- OCI metadata/annotations including source, revision, version, description, and AGPL license;
- BuildKit provenance;
- BuildKit SBOM attestation.

Current Docker documentation supports provenance and SBOM attestations directly from `docker/build-push-action`; use those capabilities rather than introducing a separate image-scanning stack.

### Binary artifacts

Prefer GitHub's native artifact attestation for released binary archives/checksums because it binds released bytes to the GitHub workflow/source identity without long-lived signing keys.

Important external constraint, as of 2026-09-21: the repository is private, and GitHub's native artifact attestations for private/internal repositories require GitHub Enterprise Cloud.

Therefore:

- implement the native attestation path when the connected organization supports it;
- do not silently introduce Cosign/SLSA-generator/long-lived signing keys as a fallback;
- if native private-repository attestations are unavailable, the implementation may still build/test release artifacts, but Track F's provenance acceptance remains blocked until the repository plan/visibility changes or maintainers explicitly approve an alternative trust mechanism.

Do not make the coding agent choose a fallback signing architecture.

## Decision 9: production Compose

Keep the existing `compose.yaml` as the developer stack.

Add a separate production Compose definition under a clear deployment path (for example `deploy/compose.yaml`).

Production Compose:

- uses the released GHCR image, never `build:`;
- requires an exact `LANSURFER_IMAGE` value/tag or release-generated environment value;
- runs PostgreSQL as the only other required service;
- does not publish PostgreSQL to the host;
- persists PostgreSQL data in a named volume;
- preserves health-based application startup dependency;
- publishes the application port only;
- defaults application host publication to loopback so a host reverse proxy can terminate TLS;
- allows an operator to choose an explicit host bind when direct LAN exposure is intended;
- uses `restart: unless-stopped`;
- keeps TLS and reverse-proxy configuration out of LANsurfer's required runtime services.

Provide a production environment template without real secrets.

The clean-host path must not require a Git checkout: release assets include the production Compose file/environment template or provide direct release-download instructions.

## Decision 10: configuration and secret ownership

Operator documentation must distinguish:

### Database data

Back up with PostgreSQL-native tools.

### Required secret/config material

Back up separately from the database, including all operator-supplied configuration needed to reconstruct the deployment.

When notification features are enabled this includes at minimum:

- `LANSURFER_DATA_ENCRYPTION_KEY`;
- `LANSURFER_WEBPUSH_PUBLIC_KEY`;
- `LANSURFER_WEBPUSH_PRIVATE_KEY`;
- `LANSURFER_WEBPUSH_SUBJECT`;
- authentication/catalog/delivery provider credentials as configured.

The data-encryption key is especially critical: encrypted persisted notification credentials/subscriptions are not recoverable without it.

Never print stored/decrypted production secrets through diagnostic commands.

## Decision 11: backup and restore

The supported database backup is PostgreSQL `pg_dump` custom format produced with tools from the deployment's PostgreSQL image/version line.

A database dump alone is not a complete LANsurfer backup; the operator must also preserve required configuration/secrets.

Restore is documented into a clean/fresh PostgreSQL database or volume, not on top of an unknown partially initialized schema.

Recommended restore order:

1. provision PostgreSQL without starting LANsurfer;
2. restore the database dump;
3. restore operator configuration/secrets;
4. start the target LANsurfer version;
5. let LANsurfer run forward migrations before HTTP startup;
6. verify readiness and representative data/flows.

A backup/restore guide must say what is expected to survive and what optional provider state may be rebuilt.

## Decision 12: upgrade and rollback limits

Stable upgrades are explicit image/version changes, never implicit `latest` pulls in production instructions.

Before an upgrade:

- take and verify a current database + secret/config backup;
- record current and target versions.

Upgrade:

- pull/select the exact target image;
- start LANsurfer;
- forward migrations run before HTTP startup;
- verify `version`, readiness, logs, and representative application data.

Because migrations are forward-only:

- downgrading the application after a schema-changing upgrade is not generally supported;
- rollback means restoring the pre-upgrade backup with the matching older application version, unless a specific release explicitly documents binary compatibility with the migrated schema.

Do not run DOWN migrations automatically or advertise them as a rollback mechanism.

## Decision 13: verification evidence is a release artifact

Create a durable validation document/template under `docs/validation/` for the candidate.

It records:

- exact Git tag and commit;
- binary checksums;
- image tags and immutable digest;
- Go/Node/PostgreSQL/build inputs;
- CI/release workflow run;
- SBOM/provenance/attestation verification commands/results;
- clean-host environment;
- backup source and restore target;
- populated upgrade source/target;
- restored/verified domain data;
- failures/blockers and recovery observations;
- responsible GitHub user and dated go/no-go decision.

Do not mark a gate green from implementation checkboxes alone.

## Dependency boundary for F4

F4 final evidence may be prepared as a template now, but the actual stable-1.0 populated restore/upgrade run waits until the release candidate's persistent migrations and secret/config contracts have settled.

In particular, Web Push / Discord delivery persistence may add encrypted state that must be proven restorable together with `LANSURFER_DATA_ENCRYPTION_KEY`.

This is an intentional dependency, not unfinished Track F design.

## Acceptance criteria

Track F is complete when:

1. exact release tags produce Linux amd64/arm64 binaries and one matching multi-architecture GHCR image;
2. binaries/image report the exact release version + commit;
3. stable/prerelease tag policy behaves exactly as defined;
4. released bytes have SHA256 verification and the approved SBOM/provenance path;
5. the production Compose path uses a released image, keeps PostgreSQL private, and requires no runtime services beyond app + database;
6. a clean host can install from release artifacts/documentation without a developer checkout;
7. operator docs cover configuration, admin bootstrap, TLS/reverse proxy boundary, backup/restore, key backup, upgrades, and rollback limits;
8. an exercised database + secret/config restore proves required persistent data survives;
9. an exercised populated supported upgrade proves forward migration behavior;
10. final evidence records exact revision/build inputs/artifacts/environment/results and blockers;
11. no release claim depends on stale roadmap status or an unfinished downstream persistence contract.
