# Track E3 — Discord Delivery Implementation Tasks

Status: Current implementation plan
Date: 2026-09-24
Tracking issue: #28
Branch: `docs/track-e-notifications-sdd`
Depends on:
- `2026-09-24-track-e-branch-reconciliation-plan.md`
- completed E1 foundation
- E2 is independent; E3 must not depend on E2 implementation details except shared E1/data-encryption infrastructure already approved

## Authority

This plan decomposes only requirements already approved in:

- `docs/superpowers/notifications/2026-09-20-track-e-discord-delivery-design.md`
- the approved E1 foundation design;
- canonical PRODUCT/ARCHITECTURE/ROADMAP.

Discord authentication and Discord notification delivery remain separate integrations. Do not reuse Discord login identity as public Game Request identity, do not add a bot/gateway, and do not request extra OAuth scopes.

## Global execution rules

- One task = one narrow concern.
- Add tests in the same task as the behavior they prove.
- Do not introduce a generic provider/plugin/event framework.
- PostgreSQL remains the only durable coordination store.
- Never commit/log/render webhook ID, webhook token, OAuth access token, OAuth refresh token, or reconstructed webhook URL.
- Keep `allowed_mentions` empty on every create/edit.
- Existing E1 retry/manual-retry semantics remain authoritative.
- Provider `Retry-After` must flow through unchanged to E1.
- Do not replay historical work on connect/reconnect.
- Do not add a “test message” feature.

---

## E3-01 — Add Discord-delivery configuration, independent from Discord login

**Goal**

Represent optional Discord-delivery OAuth credentials without coupling them to existing Discord authentication configuration.

**Affected files/components**

- `internal/platform/config/config.go`
- `internal/platform/config/config_test.go`
- `.env.example`
- operator configuration docs/README only as necessary

**Concrete changes**

1. Add a separate optional Discord-delivery config block using dedicated environment values, e.g.:
   - `LANSURFER_DISCORD_WEBHOOK_CLIENT_ID`
   - `LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET`
2. Keep existing `LANSURFER_DISCORD_CLIENT_ID/SECRET` unchanged for authentication.
3. Both delivery credentials absent => E3 disabled normally.
4. Exactly one delivery credential present => startup config error.
5. Delivery configured => existing Track E data-encryption key must be present/valid; fail closed if not.
6. Do not implicitly enable Discord authentication when delivery is configured, or vice versa.

**Dependencies**

Reconciliation R1-R5.

**Acceptance / verification**

- config tests cover disabled, valid, partial, and missing/invalid data-key cases.
- existing Discord-login-only configuration tests remain green.
- no shared “Discord enabled” boolean conflates the two integrations.

---

## E3-02 — Add Discord destination and setup-state persistence schema

**Goal**

Create the minimum E3-owned persistence needed for one instance destination and one-use administrator OAuth setup state.

**Affected files/components**

- new next-numbered migration under `internal/platform/postgres/migrations/`
- new `internal/notification/postgres/queries/discord.sql`
- `sqlc.yaml` only if required by existing notification sqlc package layout
- generated `internal/notification/postgres/sqlcgen/*`
- integration migration tests

**Concrete changes**

Create persistence for exactly:

1. **one instance Discord destination** containing safe/queryable metadata:
   - connection state (`connected` / `broken`; absence means not configured);
   - encrypted versioned webhook credential blob;
   - guild ID;
   - channel ID;
   - webhook display name when returned;
   - connected/updated timestamps;
   - broken timestamp/reason when applicable.
2. **short-lived one-use OAuth setup state** containing only what is required to bind the flow to:
   - random state identifier/hash;
   - initiating administrator user ID;
   - initiating authenticated session/browser binding available in the existing web stack;
   - operation type “configure Discord destination”;
   - expiry/consumed state.
3. Enforce singleton destination semantics in the database.
4. Do not store webhook ID plaintext.
5. Do not store OAuth access/refresh tokens.
6. Do not create per-user Discord destinations.

**Dependencies**

E3-01.

**Acceptance / verification**

- fresh migration succeeds;
- singleton invariant is database-enforced;
- OAuth state can be atomically consumed once and expires;
- schema contains no plaintext webhook URL/token/access-token/refresh-token column;
- `task generate` and focused integration tests pass.

---

## E3-03 — Define E3 domain types and narrow repositories

**Goal**

Give Discord delivery explicit notification-module vocabulary without leaking sqlc/database types upward.

**Affected files/components**

- new `internal/notification/discord.go`
- new `internal/notification/discord_test.go` for pure helpers if needed

**Concrete changes**

Define minimal domain types/interfaces for:

- destination state and safe metadata;
- versioned encrypted webhook credentials in memory before/after crypto operations;
- setup-state repository;
- destination repository;
- Discord message synchronization repository introduced later;
- provider client interface used for OAuth/webhook operations.

Do not implement HTTP/provider behavior in this task.

**Dependencies**

E3-02.

**Acceptance / verification**

- notification package compiles without importing Discord auth package or sqlc-generated types.
- no generic “external integration” abstraction is introduced.

---

## E3-04 — Implement encrypted Discord webhook credential persistence

**Goal**

Store webhook ID + token together as one versioned AES-GCM blob using the existing Track E data-encryption key.

**Affected files/components**

- new `internal/notification/discord_credentials.go`
- unit tests
- `internal/notification/postgres/store.go`
- generated/query code from E3-02 as needed

**Concrete changes**

1. Define a versioned plaintext credential shape containing webhook ID + token only.
2. Serialize, encrypt with existing `internal/platform/crypto`, and persist only ciphertext.
3. Decrypt only in the notification service/transport immediately before Discord API use.
4. Safe metadata (guild/channel/display name/state/timestamps) remains separate/queryable.
5. Broken-state transition must be able to delete/clear ciphertext while preserving safe metadata.
6. Tests must inspect persisted rows and prove plaintext webhook ID/token are not stored.

**Dependencies**

E3-03.

**Acceptance / verification**

- round-trip encrypt/decrypt unit tests pass;
- wrong key/corrupt ciphertext fails closed;
- integration test confirms plaintext secret substrings do not appear in destination columns.

---

## E3-05 — Add the dedicated `webhook.incoming` OAuth provider client

**Goal**

Implement the narrow Discord-delivery OAuth exchange without reusing the Discord login provider.

**Affected files/components**

- new `internal/notification/discord_oauth.go`
- new `internal/notification/discord_oauth_test.go`

**Concrete changes**

1. Build authorization URL using only:
   - Discord OAuth authorize endpoint;
   - delivery client ID;
   - dedicated delivery redirect URI;
   - `webhook.incoming` scope;
   - generated state.
2. Do not request `identify`, `guilds`, bot, commands, or Gateway permissions.
3. Exchange callback code at Discord token endpoint.
4. Parse the webhook object returned by `webhook.incoming` response:
   - webhook ID/token;
   - guild ID;
   - channel ID;
   - webhook name when present.
5. Discard OAuth access token and refresh token immediately after extracting the webhook result.
6. Do not expose those tokens through returned domain types, logs, or errors.
7. Use an injected HTTP client for tests.

**Dependencies**

E3-04.

**Acceptance / verification**

- authorization URL test asserts exact scope set.
- token-response test proves only webhook credential + safe metadata leave the adapter.
- response fixtures containing access/refresh tokens never surface them in returned/error strings.

---

## E3-06 — Implement one-use, administrator-bound Discord setup state

**Goal**

Protect the destination OAuth flow with the exact binding required by the design.

**Affected files/components**

- notification setup-state service in `internal/notification/discord_setup.go`
- postgres store/query methods
- unit/integration tests

**Concrete changes**

1. On setup start, create cryptographically random state.
2. Persist only the safe/hashed form consistent with existing OAuth-flow conventions.
3. Bind state to:
   - initiating admin user ID;
   - current authenticated session/browser binding available to the server;
   - explicit Discord-destination operation;
   - short expiry.
4. Callback atomically consumes state once.
5. Reject wrong user/session/browser/operation, expired state, and replay.
6. State validation must happen before accepting/storing webhook credentials.

**Dependencies**

E3-05.

**Acceptance / verification**

- tests cover correct consume, replay, expiry, wrong admin, wrong browser/session binding, wrong operation.
- no destination changes on invalid callback.

---

## E3-07 — Implement destination connect/replace transaction semantics

**Goal**

Make a successful OAuth callback atomically install/replace the one local destination while preserving the old destination until the new one is durably stored.

**Affected files/components**

- `internal/notification/discord_setup.go`
- postgres destination store/query
- tests

**Concrete changes**

1. Complete/validate OAuth callback and encrypt new webhook credentials.
2. In one local database operation/transaction:
   - retain old destination until new destination row/state is valid;
   - atomically switch singleton destination to the new connected destination.
3. Return old decrypted credential only to the immediate cleanup path, not UI.
4. After successful commit, best-effort delete the old remote webhook through provider client.
5. Old-webhook cleanup failure:
   - does not roll back the new destination;
   - records safe diagnostic failure metadata/logging without secret material.
6. Connecting/replacing does not scan/replay historical `notification_work`.

**Dependencies**

E3-06.

**Acceptance / verification**

- failed new OAuth/storage leaves old destination active;
- successful replacement makes exactly new destination active;
- remote old-webhook delete is attempted only after local commit;
- cleanup failure leaves new destination connected.

---

## E3-08 — Implement destination disconnect and broken-state transitions

**Goal**

Centralize lifecycle transitions required by disconnect and definitive deleted-webhook failures.

**Affected files/components**

- `internal/notification/discord_destination.go`
- postgres queries/store
- integration tests

**Concrete changes**

1. **Disconnect:**
   - immediately remove/deactivate the local destination;
   - suppress still-pending/claimed Discord deliveries targeting that destination;
   - commit local change first;
   - best-effort delete remote webhook afterward;
   - leave existing Discord channel messages untouched.
2. **Broken:**
   - retain safe destination metadata;
   - clear encrypted webhook credential blob;
   - store safe broken timestamp/reason;
   - stop future target materialization for that destination;
   - suppress still-pending deliveries for that destination.
3. Reconnect later replaces broken state with a new OAuth-authorized destination.
4. No historical replay on either reconnect path.

**Dependencies**

E3-07.

**Acceptance / verification**

- integration tests prove pending deliveries become suppressed on disconnect/broken transition;
- credential ciphertext is gone in broken state;
- explicit disconnect removes remaining local destination metadata;
- no remote cleanup failure can restore local connection state.

---

## E3-09 — Add admin connect/reconnect/callback/disconnect routes

**Goal**

Expose E3 lifecycle through existing global-admin authorization and CSRF conventions.

**Affected files/components**

- `internal/web/router.go`
- existing/new admin handler file under `internal/web/handler/`
- handler/router tests

**Concrete changes**

1. Add admin-only routes for:
   - start connect/reconnect via CSRF-protected POST;
   - OAuth callback;
   - disconnect via CSRF-protected POST.
2. Start action creates E3-06 state and redirects to E3-05 authorization URL.
3. Callback validates one-use state and performs E3-07 connect/replace.
4. Disconnect invokes E3-08.
5. Existing destination stays active while connect/reconnect authorization is in progress.
6. Callback must never link/create a Discord authentication identity.
7. Reject non-admin starts/disconnects through existing `RequireAdmin`.

**Dependencies**

E3-08.

**Acceptance / verification**

- router/handler tests cover admin/non-admin, CSRF, callback success/failure/replay.
- no Discord auth/linking service is called by E3 routes.

---

## E3-10 — Render the admin Discord delivery state surface

**Goal**

Show only the approved safe destination states/actions under the existing admin UI.

**Affected files/components**

- existing admin page/template or one focused partial/page under `internal/web/pages/`
- admin handler view model/tests
- generated templ output

**Concrete changes**

Display at least:

- Not configured;
- Connected;
- Broken.

For Connected/Broken show only safe metadata such as guild/channel IDs, display name, connected/broken timestamps/reason as appropriate.

Provide:

- Connect;
- Reconnect;
- Disconnect;
- link/access path to existing E1 notification delivery diagnostics where repository UI conventions permit.

Do not display:

- webhook ID;
- webhook token;
- webhook URL;
- OAuth token;
- test-message control.

**Dependencies**

E3-09.

**Acceptance / verification**

- template tests assert secret material cannot be rendered.
- broken state clearly requires reconnect.
- existing Discord authentication UI remains separate.

---

## E3-11 — Add a viewer-independent current Game Request snapshot for Discord rendering

**Goal**

Provide exactly the current request fields required by the Discord message without letting notification own Game Request tables.

**Affected files/components**

- `internal/query/gamerequest/queries/game_requests.sql`
- `internal/query/gamerequest/query.go`
- generated sqlc output
- query tests/integration tests
- small adapter in `internal/app/notification.go`

**Concrete changes**

Add one system read returning:

- request ID;
- Game name;
- planned start time;
- current status;
- current player count;
- max capacity / enough data for free-place summary;
- creator's LANsurfer Game Nick;
- request description.

No viewer personalization is needed.

Do not return:

- full roster;
- durable activity history;
- provider identity data.

**Dependencies**

E3-10.

**Acceptance / verification**

- tests cover open/full/unlimited/closed/cancelled request snapshots as existing domain fixtures allow.
- notification module consumes a narrow adapter/type, not sqlc rows directly.

---

## E3-12 — Add E3 message synchronization persistence

**Goal**

Persist one external Discord message identity and stale-update guard per destination + Game Request without making E1 generic.

**Affected files/components**

- next-numbered migration
- `internal/notification/postgres/queries/discord.sql`
- store/domain types
- integration tests

**Concrete changes**

Create an E3-specific synchronization record keyed by destination + Game Request containing only what the approved design needs:

- external Discord message ID;
- highest applied/request activity/version marker needed to reject stale updates;
- timestamps.

Rules:

1. At most one current Discord message reference per destination + Game Request.
2. Do not store message content/history here.
3. Do not move Discord-specific message reference/version fields into generic E1 tables unless already required by another transport.
4. Provide atomic compare/update operations needed by later send tasks.
5. Destination replacement naturally creates a new destination identity, so old message references are not reused.

**Dependencies**

E3-11.

**Acceptance / verification**

- uniqueness is database-enforced.
- an older version cannot overwrite a newer stored applied version.
- deleted-message recovery can clear/replace one message ID safely.

---

## E3-13 — Add `superseded` terminal delivery state for Discord-obsolete work

**Goal**

Represent E3's approved coalescing outcome explicitly without misclassifying it as success/failure.

**Affected files/components**

- next-numbered migration (may be combined with E3-12 only if both are one tightly-scoped E3 persistence migration)
- `internal/notification/notification.go`
- E1 postgres queries/store
- worker/repository interfaces/tests
- diagnostics behavior only if needed to exclude non-problem state

**Concrete changes**

1. Add `superseded` as a terminal delivery state.
2. Add a repository operation to record superseded with a safe internal reason/version reference.
3. Worker must not auto-retry superseded deliveries.
4. Superseded is not an E1 “problem” alongside exhausted/permanent/uncertain.
5. Do not apply supersession to Web Push.

**Dependencies**

E3-12.

**Acceptance / verification**

- migration constraint accepts `superseded`;
- claimed/pending delivery can become superseded;
- claim query never returns superseded;
- diagnostics problem listing remains exhausted/permanent/uncertain only unless canonical docs explicitly require otherwise.

---

## E3-14 — Implement pure Discord message rendering and sanitization

**Goal**

Build the approved compact current-state embed independently from HTTP/provider behavior.

**Affected files/components**

- new `internal/notification/discord_message.go`
- new `internal/notification/discord_message_test.go`

**Concrete changes**

Render exactly one compact embed containing:

- Game;
- planned start;
- current status;
- players/capacity or free-place summary;
- creator Game Nick;
- short sanitized/plain-content description;
- LANsurfer deep link;
- minimum resolved instance context.

Rules:

1. Never include full roster/activity history/join-leave actor identities/provider-private identity.
2. Treat description as plain content:
   - neutralize Discord formatting/control content as required by design;
   - obey LANsurfer's existing description bound;
   - user text cannot set embed URLs/fields/structure.
3. Always construct an explicit empty `allowed_mentions` object for both create and edit payloads.
4. Rendering is deterministic from current snapshot + instance/base URL.

**Dependencies**

E3-11.

**Acceptance / verification**

- table tests cover representative statuses/capacity states.
- malicious mention/markdown-like description cannot create mentions or payload structure.
- forbidden data fields are absent.

---

## E3-15 — Implement narrow Discord webhook HTTP client primitives

**Goal**

Add provider operations required by E3 without yet deciding create-vs-edit orchestration.

**Affected files/components**

- new `internal/notification/discord_webhook_client.go`
- new `internal/notification/discord_webhook_client_test.go`

**Concrete changes**

Implement only:

- create message via webhook execute with `wait=true`;
- edit known webhook message;
- delete webhook for best-effort cleanup;
- optionally provider operation needed to distinguish/read webhook only if the approved flow actually requires it.

Return structured results that expose:

- HTTP status;
- Discord JSON error code when present;
- returned message ID on successful create;
- Retry-After duration when present;
- whether failure happened before request transmission versus after an ambiguous network timeout where the client can know.

Never include secrets in error strings.

**Dependencies**

E3-14 and E3-05 provider HTTP conventions.

**Acceptance / verification**

- create request includes `wait=true`;
- create/edit payload includes empty `allowed_mentions`;
- Retry-After parsing tests;
- Discord codes 10008 and 10015 are preserved distinctly;
- no real network calls.

---

## E3-16 — Implement create/edit provider result classification

**Goal**

Encode the approved intentional difference between potentially duplicating create and replay-safe edit.

**Affected files/components**

- `internal/notification/discord_delivery.go`
- unit tests

**Concrete changes**

For **create**:

- known pre-transmission failure -> retryable;
- explicit 429 -> retryable + Retry-After;
- explicit 5xx -> retryable;
- timeout/connection loss after request may have reached Discord -> uncertain;
- 10015 Unknown Webhook -> destination-broken signal;
- other non-retryable 4xx -> permanent.

For **edit of known message ID**:

- network/5xx/429 -> retryable;
- 10008 Unknown Message -> deleted-message-recovery signal;
- 10015 Unknown Webhook -> destination-broken signal;
- other non-retryable 4xx -> permanent;
- no uncertain classification merely because an idempotent/current-snapshot edit response was lost.

**Dependencies**

E3-15.

**Acceptance / verification**

- exhaustive table tests cover create/edit difference and exact 10008/10015 handling.
- Retry-After is never capped here.

---

## E3-17 — Implement Discord transport target materialization

**Goal**

Create at most one Discord target for new live work when a connected destination exists, and no target while not configured/broken.

**Affected files/components**

- `internal/notification/discord.go`
- transport tests
- destination repository adapter

**Concrete changes**

1. Add `DiscordTransport.Name() == "discord"`.
2. `Targets` returns:
   - no target when destination absent/broken;
   - exactly one opaque target tied to the currently connected destination identity.
3. Old work already materialized with zero Discord deliveries remains untouched after connect.
4. Reconnect creates a new destination identity; old target keys must not redirect to the new destination.
5. A later new live activity for an existing Game Request may target the newly connected destination.

**Dependencies**

E3-08 and E3-12.

**Acceptance / verification**

- tests cover absent, connected, broken, replacement destination.
- old destination target key never resolves as new destination.

---

## E3-18 — Implement serialized current-state Discord send orchestration

**Goal**

Create/edit one synchronized Discord message per destination + request from the latest current Game Request snapshot while preventing stale overwrites.

**Affected files/components**

- `internal/notification/discord_delivery.go`
- postgres sync store from E3-12
- unit/integration tests

**Concrete changes**

For one claimed Discord delivery:

1. Resolve current destination and require target destination identity still matches.
2. Resolve the latest Game Request snapshot from E3-11.
3. Acquire/perform PostgreSQL-backed serialization for that destination + Game Request; do not use only an in-process mutex.
4. Read synchronization record:
   - no message ID => create with current snapshot;
   - message ID => edit same message with current snapshot.
5. Before external mutation, detect whether a newer activity/version is already represented or waiting such that this delivery is obsolete; mark obsolete intermediate delivery `superseded`.
6. After success, atomically persist message ID/highest applied version.
7. An older delivery must never overwrite state recorded by a newer one.
8. Render from current snapshot, not original activity contents.
9. Classify a CREATE outcome as: known pre-transmission failure => retryable
   (release reservation); explicit 429 => retryable (release reservation,
   preserve `Retry-After`); explicit 5xx => retryable (release reservation,
   preserve `Retry-After`); ambiguous timeout/connection loss that may have
   reached Discord => uncertain (retain reservation). Only Discord's absence
   of a definitive HTTP response is ambiguous; any HTTP status Discord
   returns is definitive.

**Dependencies**

E3-13, E3-14, E3-16, E3-17.

**Acceptance / verification**

- concurrency integration test proves two deliveries for same destination+request cannot externally apply in stale order;
- rapid activities may result in one create/latest edit and superseded intermediate deliveries;
- no process-local mutex is the durability mechanism;
- an explicit Discord 5xx response to CREATE releases the reservation, reports
  retryable, and permits an immediate automatic retry;
- an ambiguous CREATE outcome without an HTTP response retains the
  reservation and reports uncertain.

---

## E3-19 — Implement deleted-message recovery

**Goal**

Recover one request when Discord says the stored message no longer exists but the webhook remains valid.

**Affected files/components**

- `internal/notification/discord_delivery.go`
- sync repository
- tests

**Concrete changes**

1. On edit result `10008 Unknown Message`:
   - keep destination connected;
   - clear/replace stale message reference under the same serialization boundary;
   - create exactly one replacement message from the latest current snapshot with `wait=true`;
   - persist new message ID.
2. Do not replay intermediate historical activity.
3. Apply normal create-result classification to the replacement create, including possible uncertain outcome.

**Dependencies**

E3-18.

**Acceptance / verification**

- one missing message yields one replacement create;
- later edits use replacement message ID;
- destination remains connected.

---

## E3-20 — Implement deleted-webhook/broken-destination handling from delivery

**Goal**

Stop delivery safely when Discord definitively reports the configured webhook is gone.

**Affected files/components**

- `internal/notification/discord_delivery.go`
- E3-08 destination lifecycle service
- integration tests

**Concrete changes**

1. On `10015 Unknown Webhook` from create/edit:
   - transition destination to Broken using E3-08;
   - delete encrypted credentials;
   - retain safe diagnostic metadata/reason/timestamp;
   - suppress pending deliveries for that destination.
2. Do not keep retrying the invalid webhook.
3. Do not auto-reconnect.
4. Current delivery terminates consistently with the broken transition; no duplicate retry loop.

**Dependencies**

E3-19.

**Acceptance / verification**

- after 10015, no further target materialization occurs;
- credential ciphertext is cleared;
- pending deliveries are suppressed;
- admin state renders Broken.

---

## E3-21 — Register Discord transport in runtime and standalone retry paths

**Goal**

Make E3 available to the same worker/manual-retry infrastructure as Web Push without duplicating transport registration logic.

**Affected files/components**

- `internal/app/notification.go`
- `internal/app/app.go`
- app/CLI tests

**Concrete changes**

1. Extend the single notification transport-construction helper.
2. Add Discord transport only when delivery configuration is enabled and a destination may be read from DB.
3. Pass required narrow dependencies:
   - destination/sync stores;
   - current Game Request snapshot reader;
   - base URL/resolved instance identity;
   - data-encryption key;
   - webhook client.
4. Reuse same construction path for:
   - runtime worker;
   - `admin notifications retry`;
   - `retry-uncertain`.
5. No provider call occurs merely from listing diagnostics.
6. Wire `retry-uncertain` for an uncertain Discord delivery with an
   unresolved CREATE reservation (recorded by E3-18): before re-invoking
   `SendClaimed`,
   - verify the exact delivery is still `uncertain`;
   - verify target/destination generation still matches and is eligible;
   - verify the reservation belongs to that exact destination + request
     (the CREATE attempt this delivery represents);
   - atomically clear that reservation and reopen the uncertain delivery in
     the same transaction that verifies the above;
   - only this explicit administrator action may override the normal
     at-most-one-automatic-create guard from E3-18; automatic lease
     expiry/reclaim must never clear the reservation;
   - if destination generation changed, fail as no-longer-eligible rather
     than clearing another generation's reservation;
   - reconnect is not required merely to retry an uncertain delivery.
7. Do not add an automatic timeout/expiry for reservations. The existing
   `retry-uncertain` command is itself the explicit administrator
   acknowledgement of duplicate risk required by the E1 contract
   (`ManualRetryUncertain` explicitly retries despite possible
   externally-visible duplication).

**Dependencies**

E3-20.

**Acceptance / verification**

- configured runtime registers `discord`;
- disabled runtime does not;
- CLI retry can re-check current destination eligibility;
- `retry-uncertain` against an uncertain Discord delivery with an unresolved
  CREATE reservation atomically clears that exact reservation and reopens
  the delivery, but only for a still-matching destination generation;
  automatic lease expiry never clears it;
- `go test ./internal/app/... ./cmd/lansurfer/... -count=1` passes.

---

## E3-22 — Add end-to-end PostgreSQL-backed Discord delivery integration tests

**Goal**

Prove the durable E3 lifecycle with fake Discord HTTP responses and no real provider dependency.

**Affected files/components**

- focused integration test file under `internal/platform/postgres/`
- test fake HTTP server/client

**Concrete changes**

Cover at least:

1. connect stores encrypted credential and no OAuth token;
2. new live activity creates one Discord delivery;
3. first send creates one message and persists message ID;
4. later activity edits same message;
5. delayed initial create uses latest current snapshot;
6. rapid activities cannot produce stale final state;
7. intermediate obsolete delivery becomes superseded;
8. 429 schedules retry using provider Retry-After;
9. ambiguous create becomes uncertain and is not auto-retried;
10. edit network failure remains retryable;
11. 10008 recreates one message;
12. 10015 breaks destination and suppresses pending work;
13. disconnect/reconnect does not replay old work;
14. later new live activity after reconnect can create current request's first message in new destination.

**Dependencies**

E3-21.

**Acceptance / verification**

- `task test:integration` passes.
- no real Discord network use.
- tests assert no secret appears in logged/persisted diagnostics where test harness permits.

---

## E3-23 — Reconcile canonical documentation and Track E issue status

**Goal**

Document only verified E3 behavior after implementation, and retire stale execution checklists.

**Affected files/components**

- `docs/ARCHITECTURE.md`
- `docs/ROADMAP.md`
- `docs/superpowers/README.md`
- this plan's status
- GitHub issue #28

**Concrete changes**

1. Describe the actual connected/broken lifecycle, one-message-per-request synchronization, retry/uncertain/deleted-message behavior, and encrypted credential storage.
2. Keep Discord authentication/delivery separation explicit.
3. Mark E3 complete only after verification.
4. Update issue #28 E3 checkboxes/status to match code.
5. Do not copy secret/provider implementation trivia into canonical product docs.

**Dependencies**

E3-22.

**Acceptance / verification**

- docs match verified implementation.
- no claim of bot/gateway/test-message support.
- no stale “design in progress” wording remains for approved/implemented E3.

---

## E3-24 — Full E3 verification

**Goal**

Close Track E with the repository's complete verification set.

**Affected files/components**

No planned behavior changes; fix only defects found by verification.

**Concrete changes**

Run in order:

1. `task generate`
2. `task test`
3. `task test:integration`
4. `task test:pwa` (proves E3 did not regress Track D/E2 worker behavior)
5. `task lint`
6. `task build`

If live Discord validation is available, record a separate manual validation note using a disposable/test destination; never commit webhook credentials. Do not make live-provider access a hidden prerequisite for automated tests.

**Dependencies**

E3-23.

**Acceptance / verification**

- every automated command passes;
- second `task generate` has no diff;
- no secrets in git diff/logged test fixtures;
- Track E issue can be considered complete only when E1, E2, and E3 verified behavior matches canonical acceptance criteria.
