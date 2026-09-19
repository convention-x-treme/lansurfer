# Track B — Game Nick & Authentication Implementation Plan

> For the coding agent: execute this plan task-by-task. The approved design is `docs/superpowers/specs/2026-09-19-game-nick-auth-design.md`. Do not reopen settled design choices. Prefer failing test -> minimal implementation -> passing test. Keep commits coherent and reviewable.

**Goal:** Implement issue #25 without expanding stable-1.0 scope.

**Architecture:** `user` owns Game Nick/User writes. `auth` owns provider identities, provider adapters, flow state, sessions. A focused application-layer coordinator shares one PostgreSQL transaction for new-account creation without cross-module persistence reach-in.

## Preflight

- [ ] Read `AGENTS.md`, issue #25, the design spec, relevant PRODUCT/ARCHITECTURE sections.
- [ ] Inspect `feat/game-nick-auth` and PR #36 before changing code.
- [ ] If Track C has merged, rebase only Track B commits from base `d5d51f109daac276a41fcce31445958af9750031` onto current `origin/main`.
- [ ] Re-check latest migration number after any rebase.
- [ ] Run baseline tests/build appropriate to the environment.

## Task 1 — schema and populated upgrade migration

Primary files:

- create `internal/platform/postgres/migrations/00009_game_nick_authentication.sql` while Track C remains the base;
- integration tests under `internal/platform/postgres`.

Implement:

- `users.game_nick NULL`;
- no backfill from old `display_name`;
- drop `users.display_name`;
- partial unique index on `lower(game_nick)`;
- migrate `user_identities.provider` to `provider_key`;
- add `provider_kind='oidc'` for existing rows;
- constraints:
  - unique(kind,key,subject),
  - unique(user_id,kind,key),
  - kind in oidc/discord;
- create `auth_flows` supporting OAuth + pending-resolution phases.

Tests must use populated prototype data and prove old sessions survive, users require onboarding, issuer values survive, identity constraints work, and flow rows support one-use semantics.

Suggested commit: `feat(auth): add Track B identity and flow schema`

## Task 2 — user domain becomes Game Nick

Files:

- `internal/user/user.go`
- user tests;
- user postgres queries/store;
- generated user sqlc;
- `internal/app/administration.go`;
- `cmd/lansurfer/main.go` and tests.

Implement reusable Game Nick validation:

- Unicode outer trim;
- 1–32 code points;
- printable Unicode;
- reject control/format;
- preserve spelling;
- internal spaces allowed.

Implement initial claim + rename and map DB unique violations to `ErrGameNickTaken`.

Update CLI terminology to GAME NICK; handle null nick safely.

Suggested commit: `feat(user): introduce Game Nick public identity`

## Task 3 — namespace-aware auth identities

Files:

- `internal/auth/auth.go` and tests;
- auth postgres queries/store;
- generated auth sqlc.

Identity fields:

- UserID;
- ProviderKind;
- ProviderKey;
- Subject;
- private DisplayName;
- private Email;
- CreatedAt.

Repository/service operations:

- lookup by kind/key/subject;
- list by user;
- create;
- refresh metadata;
- unlink with concurrency-safe last-identity protection.

Suggested commit: `refactor(auth): model provider identity namespaces`

## Task 4 — persistent flow repository

Add auth-flow queries/store/service operations for:

- create OAuth flow;
- atomically validate + consume OAuth flow;
- create verified pending identity-resolution state;
- validate pending state from bound browser;
- consume pending state atomically with successful account/link completion;
- opportunistic expiry cleanup.

Hash state/browser-binding before persistence. Persist nonce/verifier only while necessary.

Test expiry, replay, wrong browser/provider/operation/user, same-browser parallel flows, and callback one-use.

Suggested commit: `feat(auth): persist one-use browser-bound auth flows`

## Task 5 — provider configuration

Files:

- config implementation/tests;
- `.env.example`.

Add:

```text
LANSURFER_OIDC_DISPLAY_NAME
LANSURFER_OIDC_GAME_NICK_CLAIM
LANSURFER_DISCORD_CLIENT_ID
LANSURFER_DISCORD_CLIENT_SECRET
```

Preserve OIDC core semantics. Discord partial config is an error.

CXT example:

```text
LANSURFER_OIDC_DISPLAY_NAME=CXT account
LANSURFER_OIDC_GAME_NICK_CLAIM=nickname
```

Suggested commit: `feat(config): add Track B authentication provider settings`

## Task 6 — OIDC adapter refinement

Files:

- `internal/auth/oidc.go`;
- OIDC tests.

Preserve discovery, issuer, nonce, PKCE, ID-token verification and existing `/auth/callback`.

Read optional top-level custom Game Nick claim from verified ID token.

Suggestion order:

1. custom claim;
2. preferred_username;
3. name;
4. empty.

Only valid string candidates qualify. No UserInfo call solely for suggestion. Continue private name/email refresh.

Suggested commit: `feat(auth): support OIDC Game Nick suggestions`

## Task 7 — Discord adapter

Suggested files:

- `internal/auth/discord.go`;
- tests.

Implement official OAuth2 authorization-code flow, `identify` only, exact redirect `/auth/discord/callback`, profile via current-user endpoint, subject = Discord user ID, key = `discord`.

Suggestion: valid global_name -> valid username -> empty.

Do not request email or persist provider tokens.

Use official Discord docs and standard-library HTTP where practical; do not add a Discord SDK without approval.

Suggested commit: `feat(auth): add Discord OAuth2 login provider`

## Task 8 — atomic account registration coordinator

Suggested:

- focused code under `internal/app`;
- transaction-bound user/auth persistence adapters as needed.

Required transaction:

```text
BEGIN
  create User with claimed Game Nick through user-owned path
  create Identity through auth-owned path
  consume pending resolution
COMMIT
create browser session
```

No auth SQL writes users, no user SQL writes identities, no generic Unit of Work, no new account business module.

Test rollback and concurrent identity/nick conflicts.

Suggested commit: `feat(app): create accounts atomically after onboarding`

## Task 9 — auth service resolution/linking behavior

Implement:

Known identity:
- refresh private metadata;
- create session;
- return account state.

Unknown identity:
- never create User immediately;
- if no other provider is available: pending state -> direct Game Nick onboarding;
- if other provider exists: resolution choice.

Existing-account proof:
- second OAuth flow through the other provider;
- second identity must resolve to existing User;
- attach original pending identity;
- never match by email/name.

Explicit link:
- signed-in user-bound flow;
- conflict if identity belongs to other User;
- no same-provider replacement.

Suggested commit: `feat(auth): resolve and link provider identities explicitly`

## Task 10 — web auth routes use persisted flows

Files:

- authentication handler/tests;
- router/tests;
- middleware/security tests as needed.

Preserve:

- `GET /auth/login`;
- `GET /auth/oidc`;
- `GET /auth/callback`;
- `POST /auth/logout`.

Add Discord entry/callback and explicit account link starts.

Login starts may be GET. Link/unlink are CSRF-protected POST.

Replace old state/nonce/pkce cookie authority with one short-lived browser-binding cookie plus PostgreSQL flow state.

Already-signed-in normal login start redirects to account guidance.

Map flow errors neutrally; link conflict goes back to account.

Suggested commit: `feat(web): use persistent provider auth flows`

## Task 11 — account page and onboarding hard gate

Suggested files:

- `internal/web/handler/account.go` + tests;
- `internal/web/pages/account.templ`;
- layout/router/middleware tests.

`/account` shows:

- current Game Nick + rename;
- linked providers;
- private owner-only provider metadata;
- provider availability;
- link/unlink actions.

Make existing viewer panel link to `/account`; no new main-nav item.

Login page:
- always rendered;
- OIDC first;
- Discord second;
- configured OIDC label;
- only configured providers visible;
- no-provider healthy message.

Onboarding:
- new account uses pending verified identity, no local session yet;
- upgraded existing User uses existing session;
- private stored identity display name may prefill if valid.

Hard gate null-Game-Nick Users away from Games, Requests, realtime, and admin.

Game Nick validation/taken returns HTTP 422 inline and preserves input.

Suggested commit: `feat(web): add account and Game Nick onboarding`

## Task 12 — public identity sweep

Files include:

- `internal/query/gamerequest/queries/game_requests.sql`;
- query model/tests;
- Game Request templates/tests;
- layout/components;
- affected handlers/tests.

Rename public fields explicitly to Game Nick, e.g.:

- `Viewer.GameNick`;
- `CreatorGameNick`;
- `Member.GameNick`;
- `ActorGameNick`.

Search repo-wide for `DisplayName`/`display_name`. Remaining uses should be provider-private metadata, migration compatibility, or generated code.

Privacy fixture:

```text
Game Nick      NightOtter
Provider name  Alice Example
Provider email alice@example.test
```

Public pages/fragments/read models must contain only the Game Nick marker.

Suggested commit: `refactor(identity): render Game Nick on public surfaces`

## Task 13 — acceptance/security/concurrency tests

Ensure coverage for:

- no-provider/OIDC-only/Discord-only/both;
- populated upgrade with existing session;
- concurrent case-insensitive nick claim;
- rename/case-only rename/old nick reuse;
- custom OIDC claim fallback;
- issuer namespace safety;
- Discord identify/no email/no token persistence;
- flow expiry/replay/wrong browser/provider/operation/user;
- same-browser parallel flows;
- unknown-identity direct onboarding;
- existing-account proof through other provider;
- no email merge;
- link conflict;
- last-identity race;
- disabled-provider identity retention;
- sessions survive link/unlink;
- owner-only provider metadata;
- no public provider leakage.

Prefer Go/HTTP/PostgreSQL tests. Do not grow Playwright into product E2E coverage.

Suggested commit: `test(auth): cover Track B security and upgrade paths`

## Task 14 — docs

Update:

- `.env.example`;
- `README.md`;
- `docs/ARCHITECTURE.md`;
- `docs/PRODUCT.md` only where exact approved behavior needs clarification.

Document Discord app setup/callback/scope, OIDC label/custom claim, CXT `nickname` example, issuer-change semantics, Game Nick onboarding, linking/unlinking, privacy, and no email merge.

Do not modify ROADMAP merely for progress.

Suggested commit: `docs(auth): document Game Nick and provider login setup`

## Task 15 — generate and verify

Generated files are never hand-edited.

Run:

```sh
task generate
task lint
go test -race ./...
task test:integration
task test:browser
task build
```

Audit remaining `DisplayName`, `display_name`, provider names/emails, and generated diffs.

Final checklist:

- OIDC-only works;
- Discord-only works;
- both-provider works;
- no-provider healthy;
- `/auth/callback` preserved;
- Discord callback separate;
- no automatic email merge;
- explicit link/unlink;
- at least one identity remains;
- onboarding gates product/admin;
- upgrade session reaches onboarding;
- public identity is Game Nick only;
- concurrent claim safe;
- replay/binding tests pass;
- raw secrets not logged/persisted recoverably;
- provider OAuth tokens not persisted;
- no Track E implementation added;
- docs/config match code;
- all generators/checks pass.

Prepare one Track B PR linked to issue #25. If PR #36 is still in branch history, state that Track B is stacked on Track C. Do not mark #25 complete until verification evidence exists.
