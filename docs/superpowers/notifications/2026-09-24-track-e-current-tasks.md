# Track E — Current Task Index

Status: Current

Phase 1 E2 implementation tasks are complete on this branch. Real-device Web Push validation remains blocked; E2 is not fully validated. Phase 2 E3 has not started.
Date: 2026-09-24
Tracking issue: #28
Branch: `docs/track-e-notifications-sdd`

This file is the execution index for the remaining Track E work. It does not replace the approved designs or canonical product/architecture documentation.

## Source of truth

Behavior and scope come from:

- `docs/PRODUCT.md`
- `docs/ARCHITECTURE.md`
- `docs/ROADMAP.md`
- `2026-09-20-track-e-notification-foundation-design.md`
- `2026-09-20-track-e-web-push-design.md`
- `2026-09-20-track-e-discord-delivery-design.md`

The plans below only decompose that approved behavior into implementation tasks.

## Do not repeat already-completed work

Already implemented on this branch:

- E1 durable notification foundation;
- E1 `suppressed` state;
- E2 encryption/VAPID configuration;
- E2 subscription/preference persistence;
- E2 account notification settings/server actions;
- E2 browser-device server identity/logout cleanup;
- E2 server delivery prerequisites and recipient materialization corresponding to Tasks 1-4 of `2026-09-21-track-e2-web-push-delivery-plan.md`.

Historical/existing plans remain useful records, but their unchecked boxes are not the current task queue.

## Execute in this order

### Phase 0 — Reconcile branch and E1 follow-up

Use:

`2026-09-24-track-e-branch-reconciliation-plan.md`

Order:

- R1 merge current `main`;
- R2 renumber colliding Track E migrations;
- R3 regenerate derived output;
- R4 establish clean verification baseline;
- R5 fix provider `Retry-After` alignment.

Do not start new E2/E3 code before Phase 0 is green.

### Phase 1 — Finish E2 Web Push

Use:

`2026-09-24-track-e2-remaining-web-push-plan.md`

Server slice:

- E2-D1 ownership-stable materialized target;
- E2-D2 actor Game Nick read;
- E2-D3 versioned minimal payload + instance context;
- E2-D4 narrow `go-webpush/v2` sender;
- E2-D5 `WebPushTransport.Send`;
- E2-D6 runtime wiring;
- E2-D7 standalone CLI retry wiring;
- E2-D8 PostgreSQL-backed worker integration coverage.

Browser/client slice:

- E2-C1 current-browser device marker;
- E2-C2 current-device reconciliation endpoint;
- E2-C3 explicit enable/permission/subscription module;
- E2-C4 browser/server state reconciliation and distinct UI states;
- E2-C5 current-device removal browser unsubscribe;
- E2-C6 explicit logout browser unsubscribe;
- E2-C7 service-worker `push`;
- E2-C8 service-worker `notificationclick`;
- E2-C9 focused browser tests;
- E2-C10 real Web Push validation evidence;
- E2-C11 canonical docs/status reconciliation.

The old `2026-09-21-track-e2-web-push-delivery-plan.md` is an execution record for the already-started server slice. Do not execute its remaining Tasks 5-7 directly; the 2026-09-24 remaining-work plan supersedes those unexecuted steps because it incorporates the current branch state and discovered spec-alignment findings.

### Phase 2 — Implement E3 Discord delivery

Use:

`2026-09-24-track-e3-discord-delivery-plan.md`

Execute E3-01 through E3-24 in order. E3 remains independent from E2 transport behavior; only approved shared E1/data-encryption infrastructure is shared.

## Stop conditions for coding models

Stop and escalate instead of inventing a design if implementation would require changing any settled contract, including:

- adding another service worker/registration path;
- adding offline/fetch caching;
- changing Web Push audience or actor-exclusion semantics;
- changing the one-destination Discord model;
- adding bot/Gateway/slash-command behavior;
- storing Discord OAuth tokens;
- storing webhook/Push secrets in plaintext;
- replaying historical work after enabling a transport;
- replacing PostgreSQL durable coordination with an in-process-only mechanism;
- adding a generic notification/provider framework not required by E1/E2/E3.

Ordinary implementation details explicitly delegated by the designs — exact private helper/type names, SQL column names, route names consistent with existing structure, and concise UI wording — do not require a new architecture decision.

## Completion

Track E is complete only after:

1. Phase 0 is green;
2. E2 server + browser slices are implemented and validated;
3. E3 is implemented and verified;
4. canonical docs and issue #28 reflect actual verified behavior;
5. full repository generation/tests/integration/PWA/lint/build checks pass.
