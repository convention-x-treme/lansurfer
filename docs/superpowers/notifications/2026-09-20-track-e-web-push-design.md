# Track E2 — Web Push Design

Status: Draft — waiting on E1 shared-contract decisions
Date: 2026-09-20
Tracking issue: #28
Branch: docs/track-e-notifications-sdd
Base: e46afb39f0ce680aa81212eea4e98f6b84ba72a5

## Authority and intent

This is the bounded Web Push SDD artifact for Track E. It depends on the approved E1 Durable Notification Foundation design and the Track D service-worker contract already merged to `main`.

It does not override `AGENTS.md`, `docs/PRODUCT.md`, `docs/ARCHITECTURE.md`, or `docs/ROADMAP.md`.

## Goal

Deliver personal Web Push notifications for eligible Game Requests with explicit browser/user consent, durable delivery through E1, privacy-minimized payloads, and reuse of Track D's single service-worker registration path.

## Settled repository constraints

- VAPID is required.
- Use the approved narrow adapter around `github.com/marknefedov/go-webpush/v2`.
- Multiple labeled Push subscriptions per user are required.
- Browser capability detection and an explicit user gesture are required before permission is requested.
- Web Push requires explicit LANsurfer enablement.
- Audience modes are:
  - followed / My Games only by default;
  - all new Game Requests only through explicit opt-in.
- A creator must never receive the creation push for their own Game Request.
- Eligibility must be re-checked before delayed/retried sends.
- Payload is limited to Game, planned time, deep link, and minimum instance context.
- Creator identity, description, roster, and activity history are excluded from Web Push payloads.
- Track D owns service-worker registration/lifecycle.
- E2 extends the existing worker with `push` and `notificationclick`; it must not add a second service worker or registration path.
- Track G owns visual presentation conventions for notification settings/status states.

## Current design tree

Detailed E2 questions are intentionally blocked until E1 settles the shared durable contracts.

### Settled decisions

- E2 is a complete vertical Web Push slice on top of E1.
- E2 and E3 remain independent.
- Browser permission, subscription existence, LANsurfer enablement, and audience preference must be treated as distinct states.
- **Actor exclusion is general for personal Web Push:** the actor who caused an activity never receives a push for that same activity.
- **Created:** eligible followers/My Games users receive a push for a newly created request; users in the explicit all-new-requests audience may receive it regardless of following. The creator is excluded.
- **Joined:** Game Request participants receive a push that someone joined, excluding the joining actor.
- **Left:** current Game Request participants receive a push that someone left, excluding the leaving actor.
- **Closed:** current Game Request participants receive a push that the request closed, excluding the actor.
- **Cancelled:** current Game Request participants receive a push that the request was cancelled, excluding the actor.
- **Delivery-time membership:** lifecycle recipients are resolved from current membership at send/retry time; users who have since left are not sent stale lifecycle pushes.
- **Audience scope:** followed/My Games versus all-new-requests applies to `created` discovery only. Lifecycle notifications are based on current participation.
- **Join/leave actor identity:** the push may include the activity actor's public Game Nick. Provider identity/name/email remain forbidden.
- This is an intentional extension beyond the current roadmap's creation-focused Web Push and identity-minimized wording and requires canonical documentation reconciliation before implementation merge.

### Open decision branches

- exact preference schema;
- subscription schema and labels;
- duplicate endpoint ownership;
- subscribe/unsubscribe HTTP contract;
- enable/disable semantics;
- exact wording/payload schema for each activity kind;
- delivery-time eligibility query;
- payload shape;
- notification replacement/tag behavior;
- notification click/focus behavior;
- invalid endpoint cleanup;
- automated and real-device validation details.

These remain unresolved until E1 prerequisites and later Web Push grilling rounds are complete.
