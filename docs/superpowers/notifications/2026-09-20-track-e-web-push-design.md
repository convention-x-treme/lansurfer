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
- **Server-side preferences:** persist an explicit `web_push_enabled` preference, default `false`, and a `new_request_audience` preference, default `followed` with optional `all`. Lifecycle notifications for requests the user currently participates in depend on `web_push_enabled`, not on the new-request audience preference.
- Browser permission, browser PushSubscription existence, LANsurfer enablement, and LANsurfer audience preference are separate states.
- **Multiple devices:** each active subscription endpoint is an independent delivery target; a user with several active subscriptions receives the same eligible personal notification on all of them.
- **Subscription labels:** labels are user-editable human-readable names such as `Gaming PC`, `iPhone`, or `Laptop`; LANsurfer may prefill a neutral suggestion such as `This device`. Do not infer labels from browser/OS fingerprinting.
- **Endpoint ownership:** one push endpoint belongs to at most one LANsurfer user at a time. If another authenticated user explicitly enables Push in that same browser, the endpoint is atomically reassigned to the newly consenting user.
- **Logout:** explicit logout removes/deactivates the current browser's server-side subscription for the current user and best-effort calls browser-side `unsubscribe()`. Other devices remain active.
- **Manual device removal:** remove the active server-side subscription record. If the removed device is the current browser, best-effort unsubscribe browser-side as well. Historical delivery rows remain separate and are not deleted with the subscription.
- **Browser-device identity:** the browser receives a random opaque Push-device identifier when Push is first enabled. Store the browser token in a secure HttpOnly cookie and persist only the corresponding hashed/opaque identifier with the subscription. This identity is independent of the authentication-session lifetime and lets server-side logout remove exactly the current browser's subscription.
- **Enable flow:** one explicit user action runs the complete enable sequence: reuse Track D's service-worker registration -> inspect/request browser notification permission -> create the PushManager subscription with the configured VAPID public key -> persist/reassign the subscription to the authenticated user -> set `web_push_enabled=true`. Never request permission automatically on page load.
- **Permission states:** `denied` is shown as browser-blocked and LANsurfer does not repeatedly prompt; unsupported/insecure contexts are shown as unavailable. Permission state remains distinct from LANsurfer enablement.
- **VAPID availability:** if both VAPID keys are absent, Web Push is an optional disabled feature and LANsurfer starts normally. If only one key is supplied or configured keys are invalid, startup fails as invalid operator configuration. With a valid key pair, Web Push is available.
- **Open-browser suppression:** before showing an OS notification, the service worker checks same-origin window clients. If at least one LANsurfer window is currently visible, no OS notification is shown; SSE remains the active in-browser update path.
- **Lifecycle actor naming:** `joined` and `left` may name the actor's public Game Nick. `closed` and `cancelled` remain actor-neutral.
- **Notification multiplicity:** each activity produces its own OS notification; notifications from the same Game Request are not replaced/coalesced by a shared tag.
- **Notification click:** the notification deep link targets the concrete Game Request. If a LANsurfer window already exists, focus it and navigate it to the deep link; otherwise open a new same-origin window/tab for the deep link.
- **Session expiry:** normal authentication-session expiry does not disable Web Push. Explicit logout, device removal, Push disablement, endpoint expiry, or later explicit reassignment are the subscription-ending events. A click after auth expiry may require normal reauthentication before the request can be viewed.
- **Global disable:** `web_push_enabled=false` pauses all personal Push delivery but retains the user's active device subscriptions so re-enable can reuse them. Device removal remains a separate action; explicit logout still removes the current device.
- **VAPID subject:** when Web Push is configured, require an explicit `LANSURFER_WEBPUSH_SUBJECT` operator setting containing a valid contact-style subject (for example `mailto:` or HTTPS URL). Missing both VAPID keys disables Web Push; partial/invalid VAPID configuration is a startup error.
- **Push TTL:** use a maximum Push-service TTL of 5 minutes for LANsurfer notifications.
- **Provider result mapping:** provider acceptance maps to success; expired/invalid subscriptions map to permanent failure and remove that active subscription; 429, temporary/5xx, and network failures map to retryable; other non-expiry 4xx failures map to permanent without deleting the endpoint solely because of status class; uncertain remains reserved for adapter outcomes where retry cannot be considered safe.
- **Payload contract:** use versioned structured minimal JSON containing schema version, activity kind, Game, planned start, request deep link, minimum instance context, and optional actor Game Nick only for `joined`/`left`. Do not include description, roster, provider identity data, or activity history.
- **Recipient set semantics:** materialize the positive recipient/delivery set when a work item is first processed. Later eligibility checks may suppress an originally eligible delivery, but must never add a newly eligible user/device to old work.
- **Suppression:** if an existing delivery target becomes ineligible before send/retry, persist a terminal `suppressed` state with an internal reason. Do not delete it or misclassify it as a provider failure.
- **PushSubscription encryption at rest:** encrypt endpoint, `p256dh`, and `auth` using the Track E operator-provided data-encryption key and AES-GCM. Do not log or expose decrypted subscription material in diagnostics/UI.
- Keep non-sensitive ownership/operational metadata separately queryable.
- **Endpoint fingerprint:** persist a keyed HMAC-SHA256 fingerprint of the normalized endpoint, using key material derived from the Track E data-encryption key. Enforce global uniqueness on that fingerprint so one Push endpoint belongs to at most one LANsurfer user. Never persist the raw endpoint alongside the ciphertext.
- **Preference/device separation:** removing the last device does not change `web_push_enabled`; the UI may show enabled-with-no-devices and the user's audience preference remains intact.
- **Data-key requirement:** if VAPID is not configured, Web Push is disabled and no data-encryption key is required for Web Push. If VAPID is configured but `LANSURFER_DATA_ENCRYPTION_KEY` is missing or invalid, application startup fails rather than storing subscriptions in plaintext or silently downgrading.
- The data-encryption key is therefore required when Web Push is enabled, not only for Discord. Losing the key makes existing encrypted PushSubscriptions unrecoverable and requires users to register devices again; later Discord credentials encrypted with the same key likewise require reconfiguration.
- **VAPID public key exposure:** render the public VAPID key into the authenticated server-rendered notification/account UI (for example as a `data-*` value). Do not add a JSON API solely to fetch the public key.
- **Notification copy:** use short deterministic wording from the structured payload:
  - `created`: “New <Game> Game Request”
  - `joined`: “<Game Nick> joined <Game>”
  - `left`: “<Game Nick> left <Game>”
  - `closed`: “<Game> Game Request closed”
  - `cancelled`: “<Game> Game Request cancelled”
  The body primarily carries planned start time and minimum instance context; richer details stay behind the deep link.
- This is an intentional extension beyond the current roadmap's creation-focused Web Push and identity-minimized wording and requires canonical documentation reconciliation before implementation merge.

### Open decision branches

- exact subscription persistence fields and key constraints;
- subscribe/unsubscribe HTTP contract and CSRF behavior;
- browser permission/enablement state transitions;
- exact wording/payload schema for each activity kind;
- delivery-time eligibility query;
- payload shape;
- notification replacement/tag behavior;
- notification click/focus behavior;
- invalid endpoint cleanup;
- automated and real-device validation details.

These remain unresolved until E1 prerequisites and later Web Push grilling rounds are complete.
