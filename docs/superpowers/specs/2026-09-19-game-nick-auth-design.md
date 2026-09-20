# Track B — Game Nick & Authentication Design

Status: Approved for implementation
Date: 2026-09-19
Branch: `feat/game-nick-auth`
Tracking issue: #25

## Authority and intent

This design records the approved Track B decisions from the interactive design review. Implement it together with `AGENTS.md`, `docs/ROADMAP.md`, `docs/PRODUCT.md`, and `docs/ARCHITECTURE.md`. Do not reopen settled product/security decisions during implementation.

If code reality requires a change to the approved technology, module boundaries, database/authentication/security model, deployment model, public domain model, or stable-1.0 scope, stop and request approval instead of silently redesigning Track B.

## Branch context

Track B was intentionally created from Track C PR #36 at commit:

`d5d51f109daac276a41fcce31445958af9750031`

Track C owns migrations `00007_igdb_discovery.sql` and `00008_igdb_enrichment.sql`, so Track B starts at `00009` while this stack remains.

Track B stays one coherent PR. If Track C merges before Track B is opened, rebase only Track B commits onto the new `origin/main`; re-check migration numbering after the rebase.

## Scope

Track B delivers the complete stable-1.0 identity/authentication scope from issue #25:

- unique case-insensitive public Game Nick;
- upgrade onboarding for prototype users;
- provider-neutral generic OIDC;
- independent Discord OAuth2 authorization-code login;
- OIDC-only, Discord-only, and explicitly linked accounts;
- explicit signed-in link/unlink;
- short-lived persistent one-use auth flows;
- local UUID users and revocable PostgreSQL sessions;
- public identity/privacy sweep across existing product surfaces;
- verification for provider configurations, concurrency, replay/binding failures, upgrade sessions, linking conflicts, and privacy.

Not in scope:

- full account merge;
- automatic correlation by email/name/Game Nick;
- additional auth adapters beyond generic OIDC + Discord;
- rich profiles, avatars, biographies, per-game handles;
- local passwords, JWT browser sessions, passkeys, guest accounts;
- Track E notification infrastructure;
- Game Nick history/reservation;
- provider-side grant revocation;
- session-model redesign.

## Game Nick

`users.display_name` is removed. Add nullable `users.game_nick`.

Upgrade behavior:

- existing users are migrated with `game_nick = NULL`;
- do not backfill from old `display_name`;
- old `display_name` is dropped in the same forward migration;
- existing sessions remain valid and lead to onboarding.

Validation after outer trim:

- 1–32 Unicode code points;
- printable Unicode only;
- reject control/invisible format characters;
- normal internal spaces allowed;
- preserve submitted spelling/case;
- no aggressive Unicode normalization;
- no internal whitespace normalization;
- no reserved words.

Persistence owns the uniqueness invariant:

```sql
CREATE UNIQUE INDEX ... ON users (lower(game_nick))
WHERE game_nick IS NOT NULL;
```

`Foo` and `foo` collide. `Foo Bar` and `Foo  Bar` are distinct.

Rename is allowed from `/account`, uses the same validation/unique claim, and requires only the current session + CSRF. The old nick becomes immediately reusable. Activity and web read models render the current Game Nick via user ID rather than storing a historical nick snapshot.

A pure nick rename does not fan out updates to already-sent Discord messages. Future Track E renders/edits use the current Game Nick when a normal request update occurs.

## Provider identity model

Separate adapter kind from provider security namespace:

```text
provider_kind   oidc | discord
provider_key    OIDC: verified issuer
                Discord: discord
subject         provider-local user subject/id
```

Required persistence invariants:

```text
UNIQUE(provider_kind, provider_key, subject)
UNIQUE(user_id, provider_kind, provider_key)
```

`provider_kind` is constrained to `oidc` or `discord` in 1.0.

Existing OIDC identities currently store the verified issuer in `user_identities.provider`. Migrate that value losslessly into `provider_key` and set `provider_kind = oidc`.

An OIDC issuer change is a new provider namespace. Never reinterpret matching `sub`, email, or name values across issuers as the same identity.

The namespace-aware constraint deliberately avoids a future schema dead-end if a later release supports more than one OIDC issuer, while stable 1.0 still implements only one configured generic OIDC provider plus Discord.

## Provider configuration

### OIDC

Keep existing variables and semantics:

```text
LANSURFER_OIDC_ISSUER
LANSURFER_OIDC_CLIENT_ID
LANSURFER_OIDC_CLIENT_SECRET
```

Issuer + client ID are required together; the secret remains optional for a public PKCE client. All core OIDC fields empty means OIDC disabled.

Add:

```text
LANSURFER_OIDC_DISPLAY_NAME
LANSURFER_OIDC_GAME_NICK_CLAIM
```

`DISPLAY_NAME` is presentation only and defaults to `OpenID Connect`.

`GAME_NICK_CLAIM` is an optional top-level ID-token claim name used only to suggest an onboarding value. It never becomes identity authority and never changes an already-claimed Game Nick.

CXT example:

```text
LANSURFER_OIDC_DISPLAY_NAME=CXT account
LANSURFER_OIDC_GAME_NICK_CLAIM=nickname
```

The current CXT Keycloak maps `nickname` as a string.

OIDC suggestion order:

1. configured custom claim when it is a valid JSON string and valid Game Nick;
2. `preferred_username` when valid;
3. `name` when valid;
4. empty.

Read the custom claim from the verified ID token. Do not add a UserInfo request only for this suggestion. Missing/non-string/invalid suggestion data never fails authentication.

OIDC name/email remain private identity metadata and may be refreshed on successful login.

### Discord

Add:

```text
LANSURFER_DISCORD_CLIENT_ID
LANSURFER_DISCORD_CLIENT_SECRET
```

Both empty means Discord disabled; partial configuration is an error.

Callback:

```text
/auth/discord/callback
```

Use Discord OAuth2 authorization-code login with `identify` only. Do not request `email`. Fetch the current Discord user after exchange, use Discord user ID as `subject`, and do not persist OAuth access/refresh tokens after the profile request.

Discord suggestion order:

1. `global_name` when valid;
2. `username` when valid;
3. empty.

Authentication and Track E Discord delivery are separate integrations.

## Auth capability

Use one auth capability with two explicit adapters: `oidc` and `discord`. Do not introduce a generic provider plugin framework.

The auth module owns:

- external identities;
- provider adapters;
- persistent auth/link/resolution flows;
- browser sessions.

The user module owns:

- local User;
- role;
- Game Nick validation/claim/rename.

Modules do not write each other's tables. Keep narrow interfaces in consuming packages.

Preserve OIDC compatibility:

- `GET /auth/oidc`;
- `GET /auth/callback`;
- existing OIDC environment variables.

Discord gets a separate login entry and callback.

## Persistent auth flows

Replace cookie-only state/nonce/PKCE authority with PostgreSQL `auth_flows`.

Flows are:

- 10 minutes;
- one-use at the OAuth callback boundary;
- bound to browser;
- bound to provider;
- bound to operation;
- bound to initiating user for link operations.

Use one short-lived host-only HttpOnly browser-binding cookie shared across concurrent tabs. Store only its hash in flow rows. Store state only as a hash.

Persist nonce/PKCE verifier only while needed for protocol completion. Never log raw state, browser-binding, session, access, or refresh tokens.

The flow persistence must support:

- OAuth phase;
- login/link operation;
- provider kind/key;
- optional initiating user;
- state hash;
- browser-binding hash;
- nonce/verifier where required;
- expiry/consumption;
- a short-lived verified unknown-identity resolution phase;
- optional parent resolution reference;
- private pending provider metadata;
- Game Nick suggestion.

Do not create a separate durable `pending_identities` table.

OAuth callback rule: validate all bindings, atomically consume the matching OAuth row, then exchange/verify provider response. A correctly bound callback is not replayable even if provider exchange/profile handling subsequently fails.

Wrong browser/provider/operation/user, expired flows, and replay are rejected.

Pending onboarding/resolution is different: a Game Nick 422 validation/taken error does not consume it. Successful account creation or successful identity resolution consumes it once.

Expired/consumed rows are cleaned opportunistically; no cleanup worker.

## Unknown identity resolution

### Known identity

Successful provider authentication resolves to an existing User, refreshes private provider metadata, creates a normal local session, then:

- Game Nick present -> `/game-requests`;
- Game Nick missing -> onboarding.

### Unknown identity, no alternative proof path

If only the provider just used is currently available, do not ask whether the player already has an account.

- do not create a local User at callback time;
- create verified short-lived pending resolution/onboarding state;
- go directly to Game Nick onboarding;
- create User + Identity only after explicit valid Game Nick submission.

### Unknown identity, second configured provider available

Show:

- Create new LANsurfer account;
- I already have a LANsurfer account.

The existing-account path starts a fresh OAuth proof with the other provider and references the pending identity. The second identity must already resolve to a local User. Then attach the original pending identity to that User.

Never search or merge by email/name.

If the second proof identity is unknown too, do not create two accounts or guess a match; return a recoverable start-over/error path.

No same-provider identity replacement in 1.0.

## Explicit link/unlink

Link starts only from signed-in `/account` via CSRF-protected POST. A normal login entry reached by an already signed-in user redirects to account guidance instead of becoming an implicit link.

Link flow is bound to the initiating User.

If the external identity belongs to another User, reject with an account-level conflict; no takeover or merge. Concurrent same-user completion may be treated idempotently.

Unlink:

- signed-in POST + CSRF;
- no step-up auth;
- at least one identity must remain;
- UI disables/omits unlink for the last identity and explains why;
- persistence/domain still protects against races;
- existing sessions remain valid;
- no provider-side token revocation call;
- disabled-provider identities remain visible and may be unlinked if another identity remains.

## Atomic new-account creation

New users are not persisted with `game_nick = NULL`.

Use a focused application-layer transaction coordinator:

```text
BEGIN
  user-owned User + Game Nick write
  auth-owned Identity write
  consume pending resolution
COMMIT
create normal auth session
```

Do not let auth SQL write `users` or user SQL write `user_identities`. Transaction-bound repositories/services may share one pgx transaction. Do not add a generic Unit of Work framework or new account business module just for orchestration.

Only upgraded existing users may temporarily have null Game Nick.

## Sessions

Keep the current model unchanged:

- opaque random browser token;
- only token hash persisted;
- PostgreSQL revocation;
- existing ~7 day lifetime;
- HttpOnly/SameSite;
- Secure in production.

Link/unlink does not revoke current or other sessions. Logout revokes current session only.

## Onboarding hard gate

Authenticated existing Users with `game_nick = NULL` keep a valid session but cannot use normal product/admin surfaces.

Centrally redirect normal signed-in routes to Game Nick onboarding, including Games, Game Requests, realtime request stream, and admin routes.

Allow the onboarding flow, account/auth routes needed to complete it, logout, and normal static/health infrastructure.

For an upgraded session, private stored identity `display_name` may prefill Game Nick when it passes current validation; never require provider re-login just for a suggestion.

## Account page

Add minimal `/account`, reached by making the existing viewer panel an account link instead of adding a main-navigation item.

Show owner-only:

- Game Nick + rename form;
- linked providers;
- provider availability;
- private provider display name;
- OIDC email when present;
- link/unlink actions.

Do not show technical provider subject IDs, tokens, flow IDs, or issuer details.

Disabled configured provider: linked identity remains visible as currently unavailable. No data is deleted.

## Login page

Always render LANsurfer's login page.

- OIDC shown when configured;
- Discord shown when configured;
- if both are configured, OIDC first, Discord second;
- OIDC button uses configured display name;
- Discord label is fixed;
- if neither is configured, startup remains healthy and login says not configured.

## Errors and redirects

Game Nick invalid/taken:

- same page;
- HTTP 422;
- preserve entered value;
- concrete message;
- never reveal which user owns a nick.

Flow security errors:

- internal tests/logs distinguish expiry/replay/wrong browser/provider/operation/user;
- browser message remains neutral;
- never log raw flow secrets.

Provider cancellation gets a simple user-facing provider-declined message.

Identity-link conflict returns to `/account`, not a generic auth failure page.

No general `return_to` is added in Track B. Fixed destinations:

- complete login -> `/game-requests`;
- missing Game Nick -> onboarding;
- link/unlink -> `/account`;
- identity resolution -> onboarding or `/game-requests`.

## Public identity/privacy sweep

All public player fields become explicit Game Nick terminology:

- `user.User.GameNick`;
- `components.Viewer.GameNick`;
- request creator Game Nick;
- member Game Nick;
- activity actor Game Nick;
- SQL aliases/read models likewise.

The only remaining `DisplayName` concept should describe private provider metadata where appropriate.

Update Game Request list/detail/member/activity/fragments/navigation and operator CLI wording. `admin users` must say GAME NICK rather than DISPLAY NAME and safely render upgraded users without a nick.

Privacy regression fixtures should use intentionally different values:

```text
Game Nick: NightOtter
Provider name: Alice Example
Provider email: alice@example.test
```

Public output contains `NightOtter` and not the private markers. Owner `/account` may show the private values. Normal global-admin UI may not expose another player's provider metadata.

## Track B -> Track E contract

Track B does not implement notification workers/webhook setup/message fanout.

Track E must use Game Nick for public creator identity and never provider name/email. A normal future request-message render/edit reads current Game Nick. A pure nick rename does not enqueue updates for all existing messages.

## Verification matrix

Provider configurations:

- no provider;
- OIDC only;
- Discord only;
- both;
- partial OIDC rejected;
- partial Discord rejected;
- OIDC display-name/custom-claim config parsed.

Game Nick:

- trim, Unicode length, printability;
- invalid control/format rejection;
- concurrent case-insensitive claim exactly one winner;
- rename + case-only rename;
- old nick reusable;
- invalid provider suggestion fallback;
- upgraded existing session reaches onboarding.

OIDC:

- existing callback compatibility;
- issuer + subject namespace;
- private metadata refresh;
- configured string custom claim;
- missing/non-string/invalid fallback;
- issuer change does not reuse by sub/email.

Discord:

- authorization code;
- own callback;
- identify only;
- current-user profile mapping;
- global_name -> username suggestion;
- no email;
- no persistent provider tokens.

Flow security:

- expired;
- replayed;
- wrong browser;
- wrong provider;
- wrong operation;
- wrong signed-in link user;
- multiple same-browser flows;
- OAuth one-use after valid callback;
- Game Nick 422 preserves pending resolution.

Resolution/linking:

- known identity;
- unknown identity/no alternative skips resolution choice;
- unknown identity/alternative can create new;
- unknown identity proves existing User through other provider;
- never merge by email;
- other-user identity conflict;
- no same-provider replacement;
- last identity cannot be removed;
- disabled provider retained;
- sessions survive link/unlink.

Privacy:

- navigation viewer;
- Game Request creator;
- participant list;
- durable activity;
- fragments/read models;
- owner account private metadata only.

## Generation and final verification

Generated templ/sqlc files are produced by generators and never hand-edited.

Run:

```sh
task generate
task lint
go test -race ./...
task test:integration
task test:browser
task build
```

Browser tests remain interaction-hardening coverage per AGENTS.md; Track B product behavior belongs primarily in Go/HTTP/PostgreSQL tests.
