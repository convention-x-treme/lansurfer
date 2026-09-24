# Track E2 — Remaining Web Push Implementation Tasks

Status: Implementation tasks E2-D1–D8 and E2-C1–C11 complete; real-device validation blocked (see `docs/validation/track-e-web-push.md`). Historical execution record; E3 not started.
Date: 2026-09-24
Tracking issue: #28
Branch: `docs/track-e-notifications-sdd`
Depends on: `2026-09-24-track-e-branch-reconciliation-plan.md`

## Authority

This plan decomposes only work already required by:

- `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md`
- the approved E1 design;
- Track D's existing service-worker contract;
- current canonical PRODUCT/ARCHITECTURE/ROADMAP.

It does not add product scope. Exact type/function/file names below are implementation choices only where the approved design explicitly delegated them.

## Already implemented — do not repeat

The following work already exists on this branch and should be treated as input, not as tasks:

- E1 durable work/delivery persistence, claiming, worker, retries, diagnostics/manual retry;
- `suppressed` terminal delivery state;
- data-encryption primitives and Web Push/VAPID configuration;
- Push subscription/preference schema and persistence;
- encrypted subscription material, keyed endpoint fingerprint, browser-device hash;
- `SubscriptionService`;
- authenticated `/account/notifications` page and server-side preference/device actions;
- logout-side server deletion of the current device;
- Game follower read;
- viewer-independent Game Request summary read;
- subscription-by-ID and audience-wide delivery reads;
- `WebPushTransport.Targets` recipient materialization.

The current branch HEAD before this planning pass implements delivery-plan Tasks 1-4 through commit `3fc7cf37`.

## Findings this plan must correct

1. **Target reassignment hazard:** a materialized Web Push target currently stores only a subscription row ID. The E2 design says an old delivery must never be redirected to a newly eligible user/device. Because explicit enable can atomically reassign an existing endpoint row to another user while retaining the stable row ID, an old target keyed only by ID can point at a new owner.
2. **Instance context omission:** the planned payload implementation does not yet carry Track D's required resolved instance context.
3. **Actor Game Nick gap:** `joined`/`left` payloads may include the actor's public Game Nick, but the current delivery code has no narrow read dependency wired for it.
4. **Browser-side E2 is still absent:** the settings form exists, but there is no permission/subscription JavaScript, no push/click worker behavior, no current-device browser reconciliation, and no browser-side unsubscribe on explicit logout/current-device removal.

Complete the tasks in order. Keep each commit limited to one task unless a generated-output commit is mechanically required.

---

## E2-D1 — Make a materialized Web Push target ownership-stable

**Goal**

Ensure an old delivery cannot be redirected when the same Push endpoint/subscription row is later reassigned to another LANsurfer user.

**Affected files/components**

- `internal/notification/webpush.go`
- `internal/notification/webpush_test.go`
- any small target parsing helper added in `internal/notification`

**Concrete changes**

1. Change Web Push target identity so it snapshots both:
   - the subscription row ID;
   - the user ID that owned that subscription when the target was materialized.
2. Keep `Target.Transport == "webpush"`.
3. Encode the opaque `Target.Key` deterministically from those two values. Use one private encoder/decoder in the notification package; no generic target framework.
4. Update `Targets` so every target records the candidate user ID together with each subscription ID.
5. Add tests proving:
   - the key round-trips;
   - malformed keys are rejected by the decoder;
   - two different owners for the same subscription ID produce different target keys;
   - existing actor exclusion/deduplication behavior remains unchanged.

**Dependencies**

Reconciliation plan R1-R5.

**Acceptance / verification**

- `go test ./internal/notification/... -run 'TestWebPushTargets|TestWebPushTarget' -count=1` passes.
- No persistence schema change is needed: `notification_delivery.target_key` is already opaque text.
- No other transport's target format changes.

---

## E2-D2 — Add a narrow actor Game Nick read for push payloads

**Goal**

Give Web Push delivery the approved public actor identity for `joined`/`left` without importing user persistence types into the notification module.

**Affected files/components**

- `internal/notification/webpush.go`
- `internal/notification/webpush_test.go`
- `internal/app/notification.go`
- existing user service/store adapter code only as needed

**Concrete changes**

1. Add a notification-owned narrow interface that resolves one user ID to its public Game Nick.
2. Add that dependency to `WebPushTransport`.
3. In the app layer, adapt the existing user read/service to this interface.
4. Do not expose Discord/provider identity data.
5. Do not fetch an actor Game Nick for `created`, `closed`, or `cancelled`.
6. Unit-test that the dependency is only required for `joined`/`left`.

**Dependencies**

E2-D1.

**Acceptance / verification**

- `joined`/`left` can resolve the actor's LANsurfer Game Nick.
- actor lookup failures can be surfaced by `Send` as a delivery failure later; no silent provider-identity fallback exists.
- `go test ./internal/notification/... ./internal/app/...` passes.

---

## E2-D3 — Define the versioned Web Push payload and deterministic notification copy

**Goal**

Implement the approved privacy-minimized payload as a pure, testable builder before adding provider I/O.

**Affected files/components**

- new `internal/notification/webpush_payload.go`
- new `internal/notification/webpush_payload_test.go`

**Concrete changes**

1. Define one versioned JSON payload containing only:
   - schema version;
   - activity kind;
   - Game name;
   - planned start when present;
   - same-origin Game Request deep link;
   - resolved Track D instance name/context;
   - optional actor Game Nick only for `joined`/`left`;
   - deterministic notification title/copy required by the design.
2. Use the approved titles exactly:
   - `created`: `New <Game> Game Request`
   - `joined`: `<Game Nick> joined <Game>`
   - `left`: `<Game Nick> left <Game>`
   - `closed`: `<Game> Game Request closed`
   - `cancelled`: `<Game> Game Request cancelled`
3. Build the deep link from configured base URL plus the concrete request ID.
4. Use the same resolved instance identity as Track D: configured instance name when set, otherwise `LANsurfer`.
5. Do not include description, roster, provider identity, membership list, activity history, raw user IDs, subscription data, or delivery IDs.
6. Unit-test JSON keys explicitly so accidental payload expansion fails tests.

**Dependencies**

E2-D2.

**Acceptance / verification**

- Tests cover all five activity kinds.
- `joined`/`left` include actor Game Nick; the other three do not.
- zero/missing start time is omitted rather than invented.
- instance context is present in every payload.
- a test asserts forbidden fields are absent.

---

## E2-D4 — Add the narrow `go-webpush/v2` sender adapter

**Goal**

Wrap the approved Web Push library behind the existing private `webPushSender` interface and map provider results to E1 outcomes.

**Affected files/components**

- new `internal/notification/webpush_sender.go`
- new `internal/notification/webpush_sender_test.go`
- `go.mod`
- `go.sum`

**Concrete changes**

1. Import only the approved `github.com/marknefedov/go-webpush/v2` dependency.
2. Construct a sender from the configured VAPID key pair and required VAPID subject.
3. Always send with a maximum TTL of 300 seconds.
4. Map outcomes exactly as the E2 design requires:
   - provider acceptance -> succeeded;
   - expired/invalid subscription -> permanent + `Expired=true`;
   - 429 / provider-temporary / 5xx / transport-network failure -> retryable;
   - other non-expiry 4xx -> permanent, but not expired;
   - use `uncertain` only if the actual library/provider result exposes an outcome where safe retry cannot be determined.
5. Preserve an explicit provider Retry-After duration for E1 scheduling; do not cap it in the adapter.
6. Do not log endpoint, `p256dh`, `auth`, VAPID private key, or decrypted subscription material.
7. Use an injected/fake HTTP client or the library's supported seam so tests perform no real network requests.

**Dependencies**

E2-D3 and reconciliation Task R5.

**Acceptance / verification**

- focused sender tests cover success, expiration, retryable 429/5xx/network error, permanent non-expiry 4xx, and Retry-After propagation where the library exposes it.
- `go test ./internal/notification/... -run 'TestWebPushSender' -count=1` passes.
- `go mod tidy -diff` is clean.
- no real push is sent by automated tests.

---

## E2-D5 — Implement `WebPushTransport.Send`

**Goal**

Turn one already-materialized Web Push delivery into one provider send while enforcing current target ownership and current eligibility.

**Affected files/components**

- new `internal/notification/webpush_send.go`
- new `internal/notification/webpush_send_test.go`
- `internal/notification/webpush.go`

**Concrete changes**

1. Decode the ownership-stable target key from E2-D1.
2. Load the subscription by ID.
3. Before decrypting/sending, require that the current subscription owner still equals the snapshotted target owner.
   - owner mismatch or missing subscription => do not redirect the old delivery; return an outcome that lets the worker terminate/suppress it according to the existing eligibility path.
4. Reuse current preference/subscription/membership checks; do not create a second audience-policy implementation inside `Send`.
5. Decrypt endpoint/`p256dh`/`auth` only immediately before provider use.
6. Read the fresh Game Request summary.
7. Read actor Game Nick only for `joined`/`left`.
8. Build the E2-D3 payload and call the E2-D4 sender.
9. If the provider reports an expired/invalid subscription, best-effort delete exactly that currently-owned subscription.
10. Never delete a subscription solely for an unrelated permanent 4xx.
11. Never persist or log plaintext subscription material.

**Dependencies**

E2-D1 through E2-D4.

**Acceptance / verification**

Unit tests cover:

- successful send;
- owner mismatch after endpoint reassignment does not send;
- missing/deleted subscription does not send;
- decryption failure does not call provider;
- fresh summary is used at send time;
- actor Game Nick is present only for join/leave;
- expired subscription triggers deletion;
- non-expiry permanent failure does not delete;
- sender Retry-After is returned unchanged.

Run `go test ./internal/notification/... -count=1`.

---

## E2-D6 — Wire real Web Push transport dependencies in the application runtime

**Goal**

Register the real transport in the single E1 transport-registration path when Web Push is configured.

**Affected files/components**

- `internal/app/notification.go`
- `internal/app/app.go`
- corresponding app tests

**Concrete changes**

1. Replace the zero-argument placeholder `notificationTransports()` with explicit construction dependencies needed by Web Push:
   - notification store;
   - Game discovery/follower reader;
   - Game Request query reader/participants;
   - user/Game Nick reader;
   - config/base URL;
   - resolved instance name;
   - data-encryption key;
   - VAPID configuration.
2. When `cfg.WebPush == nil`, return no Web Push transport and preserve optional-feature behavior.
3. When Web Push is configured, construct exactly one `WebPushTransport`.
4. Fail startup if the transport cannot be constructed from already-validated configuration.
5. Pass the same transport slice to both:
   - worker;
   - notification service/manual retry paths within the running application.
6. Do not add a registry/plugin abstraction.

**Dependencies**

E2-D5.

**Acceptance / verification**

- app unit tests prove zero transports when Web Push is disabled and one `webpush` transport when enabled.
- application startup with valid config constructs the worker without panic/error.
- `go test ./internal/app/... -count=1` passes.

---

## E2-D7 — Make standalone notification CLI retries construct the same Web Push transport

**Goal**

Keep `admin notifications retry` and `retry-uncertain` able to re-check Web Push eligibility now that a real transport exists.

**Affected files/components**

- `internal/app/notification.go`
- app/CLI tests as appropriate

**Concrete changes**

1. In the one-shot CLI database path, construct the same narrow read dependencies used by runtime Web Push transport construction:
   - notification store;
   - game store/discovery follower reader;
   - Game Request query store;
   - user reader;
   - config, base URL, resolved instance name, VAPID/data key.
2. Reuse one helper for Web Push transport construction so runtime and CLI cannot silently drift.
3. Listing problems must not require provider network access.
4. Manual retry only reopens a delivery after existing `Service` eligibility validation; it must not itself send a push.
5. Web Push disabled => existing `ErrTransportNotConfigured` behavior remains correct for historical Web Push deliveries.

**Dependencies**

E2-D6.

**Acceptance / verification**

- tests cover configured and unconfigured transport construction for CLI retry.
- no duplicate Web Push construction logic remains.
- `go test ./internal/app/... ./cmd/lansurfer/... -count=1` passes.

---

## E2-D8 — Add worker-level Web Push integration coverage with a fake sender

**Goal**

Prove the complete server-side E2 flow without requiring a real browser or push service.

**Affected files/components**

- `internal/platform/postgres/notification_integration_test.go` or a focused new integration test file under the same package
- test-only fake sender/adapters

**Concrete changes**

Exercise through real PostgreSQL:

1. create users, follows/membership, preferences, and encrypted subscriptions;
2. materialize a live Game Request activity;
3. run one worker tick with real `WebPushTransport` and fake sender;
4. assert one delivery per eligible subscription;
5. assert actor exclusion;
6. assert a disabled user gets no delivery;
7. after materialization, remove/disable/reassign a target and assert the old delivery is suppressed/not redirected;
8. exercise one retryable provider result and subsequent success;
9. assert payload contains only approved fields.

**Dependencies**

E2-D7.

**Acceptance / verification**

- focused integration test passes under the repository's integration test harness.
- no external network access.
- `task test:integration` passes.

---

# Browser/client slice

## E2-C1 — Add current-browser device identity to the settings view

**Goal**

Let progressive browser code know whether a listed server-side device is the current browser, without exposing the raw HttpOnly device token.

**Affected files/components**

- `internal/notification/subscription.go`
- subscription unit tests
- `internal/web/handler/notification_settings.go`
- `internal/web/pages/notification_settings.templ`
- generated templ output

**Concrete changes**

1. Add a small `SubscriptionService` method that, given user ID + raw device cookie value, computes the existing device hash and returns the matching active subscription ID if present.
2. The handler reads the HttpOnly device cookie server-side and resolves the current subscription ID.
3. Render only the numeric current subscription ID / boolean current-device marker into the page; never render the raw cookie or hash.
4. Mark the matching device row with a data attribute for browser code.
5. Preserve valid `enabled with zero devices` state.

**Dependencies**

E2-D8.

**Acceptance / verification**

- service tests prove current-device matching and no-match behavior.
- handler/page tests prove no raw device token/hash is rendered.
- the current row is distinguishable from other devices.

---

## E2-C2 — Add a server action to reconcile/remove the current browser device

**Goal**

Support the design rule that browser-detected revocation/no-subscription removes only the current device server-side while preserving the user's global preference.

**Affected files/components**

- `internal/web/handler/notification_settings.go`
- `internal/web/router.go`
- handler/router tests

**Concrete changes**

1. Add one authenticated, CSRF-protected POST action under `/account/notifications` that removes the current browser device using the existing HttpOnly device cookie and `SubscriptionService.RemoveDeviceByHash`.
2. If the cookie is absent/unknown, treat it as an idempotent no-op.
3. Expire the device cookie after a successful/no-op reconciliation.
4. Do not change `web_push_enabled` or audience preference.
5. Do not accept a raw device hash/token in request parameters.

**Dependencies**

E2-C1.

**Acceptance / verification**

- tests cover known device, unknown/missing cookie, authentication, CSRF, and preference preservation.

---

## E2-C3 — Add a focused browser Web Push settings module

**Goal**

Implement capability detection and one explicit enable action without adding another service-worker registration path.

**Affected files/components**

- new `web/src/notifications.ts`
- `web/src/app.ts`
- `web/src/pwa.ts` only if a narrow helper exposure is required
- `internal/web/pages/notification_settings.templ`
- frontend build output

**Concrete changes**

1. Import and reuse `registerServiceWorker()` from `web/src/pwa.ts`.
2. Only initialize on the notification settings page.
3. Detect:
   - secure context;
   - Notification API support;
   - PushManager support;
   - service-worker registration result.
4. Never request permission on page load.
5. On the explicit Enable button:
   - request permission if state is `default`;
   - stop with a browser-blocked UI state if `denied`;
   - obtain/reuse the single Track D registration;
   - call `registration.pushManager.subscribe` with `userVisibleOnly: true` and the rendered VAPID public key;
   - extract endpoint/`p256dh`/`auth`;
   - populate the existing same-origin form and submit it through existing CSRF-protected server flow.
6. Do not create a JSON API or second worker registration.
7. Use neutral `This device` unless the user explicitly supplies a label; do not fingerprint browser/OS.

**Dependencies**

E2-C2.

**Acceptance / verification**

- TypeScript typecheck passes.
- no call to `Notification.requestPermission()` occurs without the explicit click path.
- no direct `navigator.serviceWorker.register()` is added outside `pwa.ts`.

---

## E2-C4 — Reconcile browser permission/subscription state on the settings page

**Goal**

Keep the active server-side current-device row consistent with browser revocation while keeping permission, device registration, and global enablement visibly distinct.

**Affected files/components**

- `web/src/notifications.ts`
- `internal/web/pages/notification_settings.templ`

**Concrete changes**

1. On settings-page load, inspect `Notification.permission` and current `PushManager.getSubscription()` without prompting.
2. If permission is `denied` or the browser has no subscription and the page identifies a known current server device:
   - POST the E2-C2 current-device reconciliation action;
   - do not change global `web_push_enabled`.
3. Present separate UI states for:
   - unsupported/insecure/unavailable;
   - permission denied/browser blocked;
   - Push globally disabled;
   - Push enabled with zero registered devices;
   - current browser registered;
   - other devices registered.
4. Do not claim `web_push_enabled=true` means “enabled on at least one device”; the current server copy must reflect the separate-state model.
5. Do not automatically request permission or automatically create a subscription.

**Dependencies**

E2-C3.

**Acceptance / verification**

- browser tests can distinguish all required states.
- server preference remains unchanged when browser revocation removes a device.

---

## E2-C5 — Best-effort unsubscribe the current browser on manual current-device removal

**Goal**

Match the design rule that removing the current device removes server state and best-effort unsubscribes the browser subscription; removing another device must not affect the current browser.

**Affected files/components**

- `web/src/notifications.ts`
- notification settings template data markers/forms

**Concrete changes**

1. Intercept only the remove form marked as the current browser's device.
2. Before allowing the normal server POST/navigation, best-effort call current `PushSubscription.unsubscribe()`.
3. Browser-side unsubscribe failure must not block the server-side removal.
4. Removal of any non-current device performs no browser unsubscribe call.

**Dependencies**

E2-C4.

**Acceptance / verification**

- browser test proves current-device removal invokes unsubscribe.
- browser test proves other-device removal does not.

---

## E2-C6 — Best-effort unsubscribe the current browser on explicit logout

**Goal**

Complete the E2 logout contract: server-side current-device removal already exists; add the browser-side best-effort unsubscribe.

**Affected files/components**

- `web/src/notifications.ts` or a minimal always-loaded hook in `web/src/app.ts`
- logout form marker in `internal/web/components/layout.templ` if required
- browser tests

**Concrete changes**

1. Mark the existing logout form semantically; do not create a second logout endpoint.
2. On explicit logout submission, if Push APIs are available, best-effort:
   - obtain the existing registration/subscription;
   - call `unsubscribe()`.
3. Always continue the original logout POST even when browser APIs are unsupported or unsubscribe fails.
4. Do not unregister the service worker.
5. Do not change session/logout server semantics.

**Dependencies**

E2-C5.

**Acceptance / verification**

- browser test proves logout still succeeds when unsubscribe succeeds, fails, or Push APIs are absent.
- service worker remains registered after logout.

---

## E2-C7 — Add the service-worker `push` handler with visible-window suppression

**Goal**

Render real OS notifications from the E2 payload while suppressing duplicates when LANsurfer is visibly open.

**Affected files/components**

- `web/src/service-worker.ts`
- `web/tests-pwa/service-worker.spec.ts` or a focused E2 push test file using the same PWA config

**Concrete changes**

1. Add a `push` event listener to the existing Track D worker only.
2. Parse only the versioned E2 payload contract.
3. Before `showNotification`, inspect same-origin window clients with `clients.matchAll({type: "window", includeUncontrolled: true})`.
4. If at least one matching LANsurfer window is visible, do not show an OS notification.
5. Otherwise show one notification for that activity using the payload's deterministic title/body/context/deep link.
6. Do not add `fetch`, CacheStorage, IndexedDB, message bus, background sync, or a second worker.
7. Do not use a shared tag that coalesces multiple activities from one request.

**Dependencies**

E2-C6 and E2-D3 payload contract.

**Acceptance / verification**

- tests prove visible window => no `showNotification`;
- no visible window => one notification;
- malformed/unsupported payload does not crash the worker;
- Track D tests still prove zero cache/fetch interception.

---

## E2-C8 — Add the service-worker `notificationclick` deep-link behavior

**Goal**

Open/focus LANsurfer at the concrete Game Request deep link.

**Affected files/components**

- `web/src/service-worker.ts`
- PWA browser tests

**Concrete changes**

1. Add `notificationclick`.
2. Close the clicked notification.
3. Accept only same-origin deep links from the validated E2 payload.
4. If a LANsurfer window exists:
   - focus it;
   - navigate it to the concrete deep link.
5. Otherwise open a new same-origin window/tab at the deep link.
6. Do not bypass normal authentication; session-expired navigation may go through normal login.

**Dependencies**

E2-C7.

**Acceptance / verification**

- existing client is focused/navigated;
- no client => new window opens;
- cross-origin/invalid URL is not opened;
- worker remains cache/fetch-neutral.

---

## E2-C9 — Add focused browser tests for the settings enable/revocation/device lifecycle

**Goal**

Cover browser behavior that Go unit tests cannot prove, without broadening the general browser regression suite.

**Affected files/components**

- new or extended tests under `web/tests-pwa/`
- `playwright.pwa.config.ts` only if test fixture support is needed

**Concrete changes**

Using browser API stubs/test controls where real PushManager is unavailable in headless automation, cover:

1. no permission prompt on page load;
2. explicit enable requests permission and reuses Track D registration;
3. unsupported/insecure/denied states;
4. existing subscription form serialization;
5. browser revocation/no subscription invokes current-device reconciliation;
6. current-device removal best-effort unsubscribes;
7. other-device removal does not unsubscribe current browser;
8. explicit logout best-effort unsubscribes and still logs out;
9. service-worker push visible-window suppression;
10. notification click focus/navigate/open behavior.

**Dependencies**

E2-C8.

**Acceptance / verification**

- `task test:pwa` passes.
- tests do not require a real third-party push provider.

---

## E2-C10 — Record real Web Push validation evidence

**Goal**

Satisfy the Track D/E requirement that real-device permission/subscription/push behavior is validated separately from automation.

**Affected files/components**

- add/update `docs/validation/track-e-web-push.md`

**Concrete changes**

Record actual validation evidence, not hypothetical steps, for the platform categories required by the existing PWA validation obligations where Web Push is supported by the tested browser/platform. For each tested environment record:

- exact Git revision;
- instance name/base URL;
- browser/OS/device version;
- permission flow;
- subscription creation;
- receipt of a real push;
- visible-window suppression;
- click/deep-link behavior;
- logout/current-device removal;
- session-expiry behavior;
- any platform limitations/blockers.

Do not mark untested platforms as passing.

**Dependencies**

E2-C9 and a reachable HTTPS test deployment with valid VAPID configuration.

**Acceptance / verification**

- validation file contains actual evidence or explicitly documented blockers.
- no secrets, endpoints, subscription keys, or private VAPID key are committed.

---

## E2-C11 — Reconcile canonical docs, task status, and Track E issue for completed E2

**Goal**

Make durable documentation describe implemented E2 behavior and stop old unchecked tasks from being mistaken for current status.

**Affected files/components**

- `docs/ARCHITECTURE.md`
- `docs/ROADMAP.md`
- `docs/superpowers/README.md`
- this plan / old E2 execution plans' status headers
- GitHub issue #28 checklist/status

**Concrete changes**

1. Update canonical docs only to behavior actually implemented and verified.
2. Mark the old E2 foundation/delivery execution plans historical/completed rather than leaving stale unchecked items.
3. Record that E2 extends Track D's single service worker; no offline/fetch caching was added.
4. Update issue #28 E2 checkboxes to the verified state.
5. Keep E3 explicitly separate and not implemented unless its own plan has run.

**Dependencies**

E2-C10.

**Acceptance / verification**

- documentation matches code and validation evidence.
- no E3 completion claim.
- `task lint` and `task build` pass after doc/generation changes.

---

## Final E2 verification

After E2-C11 run, in order:

1. `task generate`
2. `task test`
3. `task test:integration`
4. `task test:pwa`
5. `task lint`
6. `task build`

A second `task generate` must produce no diff.

E2 is complete only when server-side delivery, browser permission/subscription flow, service-worker push/click behavior, current-device lifecycle, documentation, and real validation evidence all satisfy the approved E2 design.
