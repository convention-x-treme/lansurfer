# Distribution & Operations Implementation Plan

**Date:** 2026-09-21  
**Tracking issue:** #29  
**Design:** [2026-09-21-distribution-operations-design.md](2026-09-21-distribution-operations-design.md)  
**Status:** F1-F3 implementation-ready; F4 final execution blocked on release-candidate persistence/configuration

## How to use this plan

Track F is intentionally split into multiple implementation PRs. Do not turn this into one large code/release PR.

For each slice:

> Read AGENTS.md, the Track F design, the relevant canonical docs/code, and this slice. Implement only this slice, run its verification, and update the PR with exact results. Do not broaden release/runtime scope.

If PR #54 has merged, also read `docs/DOMAIN_LANGUAGE.md` and `docs/DEVELOPMENT_WORKFLOW.md`.

## F1 — release identity and operator CLI

**Can start:** now  
**Suggested branch:** `feat/29-release-cli`

### Files/areas

- `cmd/lansurfer/main.go` and tests;
- focused build/version package under `internal/platform` or similarly technical package;
- focused healthcheck helper/tests;
- `Dockerfile`;
- `Taskfile.yml`;
- operator/config documentation where command names are listed.

### Tasks

1. Add build identity variables with safe local defaults:
   - version = `dev`;
   - commit = `unknown`.
2. Add `lansurfer version`:
   - no config/database load;
   - deterministic human-readable output containing version + commit.
3. Add `lansurfer healthcheck`:
   - default local readiness target derived from `LANSURFER_LISTEN_ADDR`;
   - wildcard/empty bind host maps to loopback;
   - default 3-second timeout;
   - optional `--url`;
   - require successful HTTP response and expected `ready` body;
   - no application config/database load.
4. Add Dockerfile `HEALTHCHECK` using the binary command.
5. Add `lansurfer generate data-encryption-key`:
   - 32 bytes from `crypto/rand`;
   - raw unpadded base64url;
   - no logging/config/database.
6. Add `lansurfer generate web-push-keys`:
   - P-256 keypair;
   - encoding compatible with approved `LANSURFER_WEBPUSH_PUBLIC_KEY` / `LANSURFER_WEBPUSH_PRIVATE_KEY`;
   - output two shell-friendly env assignments;
   - no generated subject.
7. Refactor CLI parsing enough to keep command usage clear; do not add Cobra/urfave or another CLI dependency.
8. Update usage text and focused README/operator docs.

### Tests

- version command works with injected release and default local values;
- non-server commands prove they do not require `LANSURFER_DATABASE_URL`;
- healthcheck success, wrong body, HTTP failure, timeout, URL override, wildcard-listen translation;
- generated data key decodes to exactly 32 bytes and repeated runs differ;
- generated Web Push public/private keys form a valid matching P-256 pair and match the approved Track E encoding;
- invalid command/flags return usage errors without starting the app.

### Verification

```sh
task lint
task test
task build
./bin/lansurfer version
./bin/lansurfer generate data-encryption-key
./bin/lansurfer generate web-push-keys
```

Start a local test server/application and exercise `healthcheck` success/failure paths.

### Done

One small PR containing only release identity + operator CLI behavior, with no release workflow yet.

---

## F2 — reproducible release artifacts and supply chain

**Can start:** after F1  
**Suggested branch:** `feat/29-release-pipeline`

### Files/areas

- `.github/workflows/ci.yml`;
- new `.github/workflows/release.yml`;
- `Dockerfile`;
- repository-level exact Node version file;
- `Taskfile.yml` and/or small `scripts/release/` helpers;
- `.gitignore`;
- release documentation.

### Tasks

1. Align Node:
   - commit exact Node 24 LTS patch version;
   - make CI/release consume the same file;
   - update contributor docs;
   - do not change Node major line.
2. Extend CI so the currently supported PWA/browser verification is not absent from release confidence.
3. Refactor Dockerfile release build:
   - exact Go version;
   - builder image pinned by digest;
   - explicit `TARGETOS` / `TARGETARCH`;
   - `CGO_ENABLED=0`;
   - `-trimpath -buildvcs=false`;
   - version/commit ldflags from build args;
   - preserve scratch + non-root runtime.
4. Add a release build helper/task producing normalized deterministic linux/amd64 and linux/arm64 archives.
5. Add `SHA256SUMS`.
6. Add tag parser/validation:
   - stable `vX.Y.Z`;
   - prerelease `vX.Y.Z-...`;
   - reject unsupported tag forms.
7. Add tag-triggered `release.yml`.
8. Release verification job:
   - checkout exact tag with sufficient history to validate mainline ancestry;
   - set up pinned Go/Node;
   - run generation and fail if tracked output changes;
   - run required lint/test/integration/browser/PWA/build checks.
9. Binary job:
   - build both architectures using canonical release build definition;
   - package normalized archives;
   - produce checksums;
   - upload workflow artifacts for publication.
10. Image job:
    - authenticate to GHCR with `GITHUB_TOKEN`;
    - set up QEMU/Buildx;
    - generate OCI labels/annotations;
    - build `linux/amd64,linux/arm64`;
    - enable BuildKit SBOM + provenance;
    - apply tags exactly according to stable/prerelease policy;
    - capture immutable image digest.
11. GitHub Release job:
    - waits for verification/binaries/image;
    - creates release for exact Git tag using GitHub CLI;
    - marks prerelease tags as prerelease;
    - uploads binary archives/checksums/deployment assets;
    - records image digest in release notes or attached metadata.
12. Artifact provenance:
    - use `actions/attest` pinned to full SHA for binary artifacts when supported by the organization's GitHub plan;
    - do not add an unapproved fallback signer;
    - if private-repo attestations are unavailable, document the blocked acceptance criterion and do not claim Track F complete.
13. Pin every action by full SHA + human-readable version comment.

### Release-output contract

Stable `v1.2.3` image tags:

```text
ghcr.io/convention-x-treme/lansurfer:v1.2.3
ghcr.io/convention-x-treme/lansurfer:v1.2
ghcr.io/convention-x-treme/lansurfer:v1
ghcr.io/convention-x-treme/lansurfer:latest
```

Prerelease `v1.2.3-rc.1`:

```text
ghcr.io/convention-x-treme/lansurfer:v1.2.3-rc.1
```

No moving prerelease aliases.

### Verification

Before using a real release tag, exercise build/tag logic with non-publishing helpers and a temporary workflow/manual test path where safe.

For a test prerelease, verify:

- both binary archives extract/run `version`;
- archive checksums match;
- image manifest contains amd64 + arm64;
- image `version` matches binary version;
- prerelease did not move `latest`/major/minor;
- image has SBOM/provenance attestations;
- GitHub binary attestation verifies when platform support is available.

### Done

Official release machinery exists and can create a verifiable prerelease without a developer-local release tool.

---

## F3 — production deployment and operator runbook

**Can start:** in parallel with late F2 once CLI/image contract is stable  
**Suggested branch:** `feat/29-production-operations`

### Files/areas

- new `deploy/compose.yaml`;
- production environment template under `deploy/`;
- new operator documentation, recommended `docs/OPERATIONS.md`;
- README links/quick start;
- SECURITY release-policy text where appropriate.

### Tasks

1. Add production Compose:
   - `image:` only, never `build:`;
   - require exact release image value;
   - PostgreSQL exact approved version line;
   - no PostgreSQL host port;
   - persistent database volume;
   - only app + postgres required services;
   - app port only;
   - default host bind loopback;
   - health/dependency/restart/graceful-stop behavior.
2. Add release-consumable environment template:
   - no real secrets;
   - exact app-image placeholder/version mechanism;
   - database URL;
   - base URL/listen/trusted proxy;
   - current auth/catalog/instance settings;
   - approved notification encryption/Web Push settings when those features are present;
   - comments point to operator docs rather than embedding a second configuration specification.
3. Write `docs/OPERATIONS.md`:
   - supported topology;
   - clean-host prerequisites;
   - install from GitHub Release/GHCR without checkout;
   - exact-version pinning;
   - configuration reference/links;
   - administrator bootstrap;
   - reverse proxy/TLS responsibility;
   - health/readiness/version inspection;
   - logs/diagnostics;
   - backup;
   - secret/config backup;
   - restore;
   - upgrade;
   - rollback limits;
   - image/binary checksum/attestation verification.
4. Backup runbook:
   - use PostgreSQL `pg_dump -Fc` from the deployed PostgreSQL toolchain;
   - preserve database dump plus operator config/secrets;
   - explicitly name data-encryption/VAPID key importance.
5. Restore runbook:
   - fresh database/volume;
   - restore before app startup;
   - restore config/secrets;
   - app runs forward migrations;
   - verification checklist.
6. Upgrade runbook:
   - backup first;
   - exact current/target version;
   - explicit image pull/version change;
   - migration-before-HTTP behavior;
   - verify version/readiness/logs/data.
7. Rollback text:
   - no automatic DOWN;
   - previous binary after schema migration is not generally supported;
   - rollback uses pre-upgrade backup + matching old version unless release notes explicitly state compatibility.
8. Update `SECURITY.md` only to the level justified before stable release; final supported-version policy is a release-candidate task.
9. Ensure release workflow attaches/copies the production Compose/env template so clean-host install requires no source checkout.

### Verification

On a clean machine/VM with only Docker + Compose:

1. obtain release deployment assets without cloning;
2. configure required values;
3. start PostgreSQL + LANsurfer;
4. verify `version`, `healthcheck`, `/healthz`, `/readyz`;
5. bootstrap admin;
6. verify PostgreSQL is not host-exposed;
7. stop/start and verify persistence.

Record exact commands/environment in the PR, but do not call this final F4 release evidence yet.

### Done

An operator can install and operate an exact released version from documented artifacts without a developer checkout.

---

## F4 — final backup/restore/upgrade/release evidence

**Can prepare template now; do not execute as final acceptance yet.**  
**Blocked by:** stable release candidate persistence/configuration, especially remaining notification delivery state and final production assets.

**Suggested branch at RC time:** `release/1.0-validation`

### Files/areas

- `docs/validation/` release evidence;
- release notes / SECURITY supported-version policy;
- only fixes required by discovered validation blockers.

### Precondition

Before starting the final run:

- all stable-1.0 schema migrations intended for RC are on the candidate;
- notification encryption/persistence contracts are final;
- production branding/assets are final;
- release pipeline has produced the exact candidate artifacts.

### Evidence run 1 — clean host

Record:

- host OS/kernel/architecture;
- Docker/Compose versions;
- exact release tag/commit;
- exact image digest;
- installation inputs;
- health/readiness results;
- administrator bootstrap;
- public/reverse-proxy path as applicable.

### Evidence run 2 — populated backup + restore

Populate representative data that covers at least:

- Users and Game Nicks;
- authentication identities/sessions as appropriate for safe test fixtures;
- Games, follows/subscriptions, aliases/provider mappings;
- Game Requests, membership, activity;
- IGDB/provider generation/enrichment state as applicable;
- notification work/delivery/preferences/subscriptions/references as implemented.

Back up database + required secret/config material.

Restore onto a clean target and verify the expected durable state. Do not put real production credentials or personal data in repository evidence.

### Evidence run 3 — supported populated upgrade

Start from the documented supported predecessor/prototype fixture, including historical data affected by migrations such as the former Event relationship.

Upgrade to the candidate using the documented operator procedure.

Verify:

- migrations complete;
- HTTP starts only after migration success;
- no required persistent data is lost;
- encrypted notification/provider state remains usable with restored keys;
- representative application flows continue to work.

### Evidence run 4 — failure/recovery

Exercise at least:

- invalid/missing required secret/config prevents unsafe startup where expected;
- migration failure prevents HTTP startup;
- restore/retry procedure is documented and works;
- no DOWN migration is invoked automatically.

### Supply-chain evidence

Record/verify:

- SHA256SUMS;
- binary provenance attestation when available;
- multi-arch image digest/platforms;
- image SBOM/provenance;
- source revision and pinned build inputs.

### Final documentation reconciliation

Before declaring Track F complete:

- update `SECURITY.md` from pre-stable wording to the actual supported 1.0 policy;
- update README operator entry points;
- update issue #29 checklist based on evidence, not assumption;
- ensure PRODUCT/ARCHITECTURE describe durable release/operations behavior without copying the validation log;
- keep the validation artifact as release evidence.

## What “leg los” means

For F1:

> Check out the approved F1 implementation branch. Read AGENTS.md, the Track F design and F1 section of the plan, then implement F1 end-to-end, run its verification, and update the PR. Do not start F2.

For F2 and F3, use the same pattern with the respective slice.

Do **not** tell a coding agent “implement Track F” as one task. The planning result has established that this tracker contains multiple PR-sized changes plus a dependency-blocked release-validation gate.
