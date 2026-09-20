# Track E1 — Durable Notification Foundation Design

Status: Historical — E1 implemented; see docs/ARCHITECTURE.md §7 (notification) for current architecture
Date: 2026-09-20
Tracking issue: #28
Branch: docs/track-e-notifications-sdd
Base: e46afb39f0ce680aa81212eea4e98f6b84ba72a5

## Authority and intent

This document is a supporting SDD artifact for Track E. It does not override `AGENTS.md`, `docs/PRODUCT.md`, `docs/ARCHITECTURE.md`, or `docs/ROADMAP.md`.

The approved Track E planning cut is:

1. E1 — Durable Notification Foundation
2. E2 — Web Push
3. E3 — Discord Delivery

E1 exists only to provide durable delivery mechanics concretely shared by E2 and E3. It must not become a generic notification, event-bus, plugin, or job framework.

## Goal

Provide restart-safe PostgreSQL-backed notification work consumption and delivery state for the approved Web Push and Discord transports without coupling external transport failure to Game Request writes.

## Settled repository constraints

- `notification` is the approved Track E business module.
- `game_request_activity` is Track A's current transactional A -> E handoff.
- `origin=backfill` rows are never deliverable.
- Pre-existing `origin=live` history needs a safe first-consumer baseline so an upgrade does not deliver an archive.
- `sequence` identifies activity rows and orders one Game Request's history; it is not a global replay cursor.
- Delivery-specific state belongs on the Track E consumer side unless a concrete design need justifies changing the canonical handoff.
- External delivery failure must never fail or roll back a Game Request action.
- PostgreSQL remains the only required persistence/runtime coordination service.
- Redis, message brokers, event sourcing, full CQRS, and a persistent in-app notification inbox are outside scope.
- E2 and E3 depend on E1 but must not depend on each other.
- The current durable activity kinds are exactly `created`, `joined`, `left`, `closed`, and `cancelled`.
- Track E consumes live activity rows; transport-specific rules decide whether a consumed work item produces a concrete delivery.

## Scope boundary

E1 covers only shared durable mechanics such as work discovery, durable per-work/per-delivery state, claiming, retries, restart recovery, stale-update protection where shared, failure classification where shared, diagnostics/manual retry foundations, and worker lifecycle.

E1 does not own Web Push permission/subscription UX, VAPID, service-worker push behavior, Discord OAuth, Discord rendering, or transport-specific payload rules.

## Current design tree

The interactive design tree is being resolved with the user in rounds. Only decisions whose prerequisites are settled are recorded as final decisions below.

### Settled decisions

- Top-level cut: E1 foundation -> E2 Web Push / E3 Discord.
- E1 should abstract only behavior proven common to the two approved transports.
- E1 is implemented before E2; E3 follows independently later.
- Track D service-worker integration is an E2 concern, not E1.
- **Persistence model:** use two durable levels:
  - one notification work record for each consumed deliverable `game_request_activity` row;
  - zero or more concrete delivery records beneath that work item, one per external target that is actually eligible for that activity.
  The work record is the consumer-side processing/accounting record; the delivery record owns transport/target status, attempts, retry schedule, and transport-specific external references where needed.
- The work layer is not a second transactional outbox and does not replace `game_request_activity`; it is materialized only on the Track E consumer side.
- **Uncertain external outcome:** persist a distinct `uncertain` delivery state when the adapter cannot determine whether the provider accepted the operation and safely retrying could duplicate an externally visible effect.
- `uncertain` is not retried automatically. It is exposed for authorized diagnosis and explicit manual retry.
- The transport adapter decides when an ambiguous result is safe/idempotent enough to remain normally retryable versus when it must be classified as `uncertain`.
- **Initial baseline:** on first E1 initialization, all already-visible committed `origin=live` activity rows are durably materialized as baseline/ignored work with no deliveries. Rows that become visible after that initialization are normal work. Do not use a timestamp or sequence cutoff.
- **Work materialization:** `notification_work` is created asynchronously by E1 from `game_request_activity`; Track A does not write notification tables inside Game Request transactions.
- The durable uniqueness between activity and work must make repeated discovery harmless and prevent duplicate work rows for the same activity.
- **Transport-independent processing:** E1 keeps consuming/materializing work even when no transport is configured or no eligible targets exist. Such work completes with zero deliveries; enabling a transport later does not replay old activity.
- **Claiming/recovery:** due deliveries are claimed through PostgreSQL with a durable lease (using row locking such as `FOR UPDATE SKIP LOCKED` or equivalent). A crashed/abandoned claim becomes eligible again after the lease expires.
- **Wake-up:** E1 uses simple periodic PostgreSQL polling for new/future-due work. `LISTEN/NOTIFY` is not required for 1.0.
- **Retry exhaustion model:** automatic retry uses at most 6 total attempts per retry round. After the cap the delivery becomes `exhausted`; an authorized manual retry explicitly re-opens it for a new retry round.
- **Retry schedule:** use capped exponential backoff with jitter, starting approximately `5s -> 30s -> 2m -> 5m -> 5m` between the six total attempts. No individual automatic wait exceeds 5 minutes. HTTP 429 honors `Retry-After`, but the automatic wait remains capped at 5 minutes for the 1.0 LAN use case.
- **Polling/lease defaults:** poll PostgreSQL approximately every 2 seconds; claims use a 60-second durable lease. Provider/network requests must have a shorter bounded timeout so the lease is crash recovery, not a transport timeout.
- **Diagnostics/authorization:** delivery diagnostics and manual retry are administrator-only and reuse the existing global admin authorization path (`RequireAdmin`); no new role model is introduced. E1's actual implementation substitutes an `lansurfer admin notifications ...` CLI surface for this HTTP page — see `docs/ARCHITECTURE.md` §7 — since shell/database access is a strictly stronger authorization boundary than the web `RequireAdmin` role check; a future HTTP page reusing `RequireAdmin`, if one is ever built, remains consistent with "no new role model."
- **Manual retry:** manual retry re-checks present-day transport eligibility before reopening delivery and grants a fresh automatic retry budget.
- **Uncertain retry UX:** `uncertain` requires an explicit administrator action that warns of possible duplicate external effect (semantically “retry despite possible duplicate”); it is not presented as the same action as an ordinary exhausted/permanent retry.
- **Retention:** 1.0 performs no automatic cleanup of notification work/delivery history. This technical diagnostic history is not a user-facing notification inbox.
- **Stale Discord boundary:** E1 persists the activity/work identity and state needed to reason about ordering, but E3 owns Discord-specific stale-update/supersession policy. Web Push deliveries are not generically coalesced or ordered through a transport-neutral stream abstraction.
- **Suppressed delivery state:** `suppressed` is a general terminal E1 delivery state meaning an originally materialized delivery is intentionally not sent because current eligibility no longer permits it. It is not a provider failure, is not automatically retried, and is retained for diagnostics. Transports may use this state when a target/user/destination becomes ineligible after materialization.

### Notification coverage

The canonical sources originally described Web Push mainly around new Game Requests. The interactive SDD intentionally extends the personal Web Push behavior for Track E and must therefore be reconciled back into the canonical product/roadmap documentation before merge.

Settled personal-notification rules so far:

- **Actor exclusion:** the user whose action produced the activity never receives a personal notification for that same activity.
- **`created`:** notify eligible users for the Game according to their Web Push audience preference; never notify the creator.
- **`joined`:** notify the Game Request participants, excluding the joining actor.
- **`left`:** notify current Game Request participants, excluding the leaving actor.
- **`closed`:** notify current Game Request participants, excluding the actor.
- **`cancelled`:** notify current Game Request participants, excluding the actor.
- **Lifecycle snapshot:** resolve current participant eligibility at delivery time; delayed/retried sends may suppress recipients who are no longer eligible, but old work never gains newly eligible recipients.
- **Discord:** one message is created per Game Request and the same message is edited as request state changes; its current status and capacity/free-place summary are synchronized from later activity.
- E1 consumes the durable live activity stream independently of whether every transport emits a delivery for every activity kind.

### Implementation details intentionally delegated

The remaining choices are implementation-level naming and mechanics that do not change the settled contract, such as exact Go method/type names and exact SQL column/index names. They are not open product/architecture decisions.

## Acceptance direction

The final design must make it possible to prove:

- no historical backfill/archive delivery;
- no global `sequence` cursor;
- committed notification intent survives restart-before-delivery;
- external failures cannot affect originating Game Request writes;
- retry state survives restart;
- duplicate/crash scenarios do not silently lose work;
- ambiguous provider acceptance is not blindly duplicated;
- E2 and E3 can consume E1 without transport-specific leakage into the foundation.
