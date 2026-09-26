# Track E final review remediation spec

**Date:** 2026-09-26  
**Branch:** `docs/track-e-notifications-sdd`  
**Reviewed HEAD:** `47407e74063e9f75d105a800a5d9f11665a40093`  
**Status:** review remediation required before merge  
**Related:** [Track E issue #28](https://github.com/convention-x-treme/lansurfer/issues/28), [E2 design](2026-09-20-track-e-web-push-design.md), [E3 design](2026-09-20-track-e-discord-delivery-design.md), [E3 historical plan](2026-09-24-track-e3-discord-delivery-plan.md)

## Purpose

Resolve the substantive findings from the post-E3 review of the current Track E branch, reconcile the branch with current `main`, and rerun final verification before merge.

Do not broaden this into a new notification architecture. Preserve the approved Track E invariants:

- PostgreSQL remains the durable coordination boundary.
- No broker, generic provider/plugin framework, second service worker, bot, or Discord Gateway.
- Discord delivery remains separate from Discord login.
- Automatic retries must not replay ambiguous Discord CREATEs.
- No historical Discord replay on connect/reconnect.
- Web Push subscriptions and Discord webhook credentials remain encrypted at rest.
- No secret-bearing logs, diagnostics, docs, or fixtures.
- Existing E2/E3 behavior that already passed review must remain intact.

## Finding 1 — HIGH — Discord connect/reconnect OAuth is blocked by CSP

### Evidence

`internal/web/handler/discord_delivery.go` currently handles the CSRF-protected POST for Discord connect/reconnect by returning a direct HTTP redirect to Discord:

```go
http.Redirect(w, r, h.authorize.AuthorizationURL(state), http.StatusSeeOther)
```

LANsurfer also sets:

```text
Content-Security-Policy: ... form-action 'self' ...
```

Browsers apply `form-action` to redirect hops originating from a form submission, so the external Discord authorization redirect is blocked.

This is already understood elsewhere in the repository: account linking in `internal/web/handler/authentication.go` deliberately renders `pages.Redirecting(...)` instead of redirecting directly for exactly this reason.

The failure has been reproduced manually in the browser:

```text
Sending form data to 'http://localhost:8080/admin/discord/connect'
violates the following Content Security Policy directive:
"form-action 'self'".
```

### Required fix

Reuse the existing same-origin OAuth bridge pattern.

For successful Discord connect/reconnect start:

1. Keep the route as the existing CSRF-protected POST.
2. Create and persist the setup state exactly as today.
3. Return a same-origin `200` page using the existing `pages.Redirecting` component with the Discord authorization URL.
4. Let the meta refresh/fallback link perform the fresh top-level navigation to Discord.
5. Do not weaken `form-action 'self'`.
6. Do not change the setup-state/admin/browser-binding security model.

### Tests

Update/add HTTP tests proving:

- admin + valid CSRF starts setup;
- response no longer relies on a 3xx external redirect;
- response contains/uses the expected Discord authorization destination through the existing redirect bridge;
- non-admin, unauthenticated, invalid-CSRF, invalid-state, replay, and callback protections remain unchanged;
- reconnect still leaves the existing destination active until successful replacement.

A focused browser/manual smoke test should verify that clicking Connect from `/admin/discord` reaches Discord without a CSP console error.

---

## Finding 2 — HIGH — target-resolution failure can permanently lose a notification transport

### Evidence

In `internal/notification/worker.go`, materialization currently loops over configured transports. If one transport's `Targets()` returns an error, the worker logs the error and continues. It then still calls `MaterializeWork` with whatever targets were successfully resolved.

`MaterializeWork` creates the unique `notification_work` row for the activity. Future discovery uses a `NOT EXISTS` anti-join on that row, so once the work row exists the activity is never materialized again.

Failure scenario:

```text
activity discovered
WebPush.Targets() -> temporary DB/read error
Discord.Targets() -> succeeds
MaterializeWork(activity, discord-only)

next worker tick:
notification_work already exists
=> Web Push target resolution is never retried
=> the Web Push delivery is permanently lost
```

With only one configured transport, a one-off `Targets()` error can produce a work row with zero deliveries and permanently lose the notification entirely.

### Required invariant

An activity must not become durably materialized until target resolution has succeeded for **every currently configured transport** for that materialization attempt.

### Required fix

In `Worker.materialize`:

- if any configured transport returns an error from `Targets()`, log the failure and skip `MaterializeWork` for that activity;
- retry the whole activity on a later tick;
- do not persist a partial set of deliveries;
- preserve zero-target materialization when every configured transport resolved successfully and legitimately returned no targets.

Do not add a second durable cursor or per-transport materialization framework unless the simple fail-closed behavior proves insufficient.

### Tests

Add a deterministic unit test with at least two transports:

1. transport A fails `Targets()` on first materialization attempt;
2. transport B resolves successfully;
3. first tick must not call/persist `MaterializeWork`;
4. second tick has both transports resolve;
5. work is materialized once with the full expected target set.

Also cover the one-transport case where the first resolution fails and the second succeeds.

Where useful, add PostgreSQL-backed evidence that a failed target-resolution attempt leaves no `notification_work` row, so discovery can retry the activity.

---

## Finding 3 — HIGH — Track E migration numbers collide with current main

### Evidence

Current `main` has advanced six commits beyond the Track E merge base and now contains:

```text
internal/platform/postgres/migrations/00014_identity_avatars.sql
```

The Track E branch independently contains:

```text
00014_notification_foundation.sql
00015_notification_suppressed_delivery.sql
00016_notification_push_subscriptions.sql
00017_notification_discord_delivery.sql
00018_notification_discord_message_sync.sql
00019_notification_superseded_delivery.sql
00020_notification_discord_create_reservation.sql
00021_notification_discord_create_owner.sql
```

PR #47 is currently not mergeable cleanly against `main` and the migration version collision must be resolved deliberately.

### Required precondition

Before changing migration numbers, determine whether **any non-disposable database** has already applied the Track E branch's current migration versions `00014..00021`.

Examples that count as non-disposable:

- a persistent `test.lansurfer.de` database;
- a shared staging database;
- any environment whose migration history/data must survive.

### Reconciliation rule

**If no persistent database has ever applied Track E `00014..00021`:**

- merge current `main` into the branch using normal history;
- renumber the Track E migrations so they follow the migrations now on `main` without duplicate versions;
- update migration-count/version assertions and generated/test expectations;
- run fresh-database and upgrade-path integration tests.

**If a persistent database has already applied Track E `00014..00021`:**

- do **not** merely rename the files and hope migration history follows;
- stop and design an explicit forward-only migration-history reconciliation compatible with that database's recorded Goose versions;
- never automatically run DOWN migrations;
- preserve existing Track E data and encrypted credentials;
- document the exact upgrade path before changing migration files.

Do not rewrite already-merged `main` migrations.

### Main reconciliation

Bring the six commits currently on `main` into the Track E branch before final merge verification. Prefer a normal merge/reconciliation commit rather than rewriting the already-pushed Track E history.

Pay particular attention to overlapping files:

- `internal/app/app.go`
- `internal/web/router.go`
- `internal/web/components/layout.templ` and generated output
- `internal/web/pages/admin.templ` and generated output
- `internal/query/gamerequest/*`
- canonical docs
- frontend generated assets

After conflict resolution, regenerate committed generated files rather than hand-editing generated output.

---

## Finding 4 — MEDIUM — Web Push endpoint validation does not enforce its claimed SSRF boundary

### Evidence

`validPushEndpoint` in `internal/web/handler/notification_settings.go` accepts any absolute HTTPS URL with a host and no userinfo.

Its comment states that this prevents the delivery worker from being pointed at a private-network URL, but the implementation does not reject:

- loopback addresses;
- RFC1918/private addresses;
- link-local addresses;
- other non-public IP literals;
- DNS names resolving to non-public addresses;
- redirect chains to non-public destinations.

The stored endpoint is later used as the outbound Web Push request destination.

The endpoint is browser-submitted form data and must not be trusted merely because normal browsers obtain it from `PushManager`.

### Required security property

A forged subscription must not turn LANsurfer into a generic HTTPS request primitive to loopback, link-local, private, or otherwise prohibited internal destinations.

### Required fix

Implement outbound Web Push endpoint hardening at the network boundary, not only as string validation.

The solution must cover:

- direct IP-literal private/loopback/link-local destinations;
- DNS resolution to prohibited addresses;
- redirect handling so a public URL cannot redirect to a prohibited destination;
- IPv4 and IPv6;
- continued HTTPS-only delivery;
- normal browser Push service endpoints used by supported browsers.

Prefer a narrow Web Push-specific HTTP transport/client policy. Do not introduce a generic repository-wide proxy/HTTP framework.

Do not log full subscription endpoints in errors.

### Tests

Add focused tests for:

- accepted normal public HTTPS Push endpoint;
- rejected loopback IPv4/IPv6;
- rejected private and link-local targets;
- rejected redirect to private destination;
- hostname resolution to prohibited IP is blocked;
- provider-facing error remains non-secret.

If DNS/network-policy testing requires a custom resolver/dialer, keep it injectable and local to Web Push sending.

---

## Finding 5 — MEDIUM — send-time eligibility read error currently falls through to provider send

### Evidence

Before each provider send, `Worker.processOne` re-runs `transport.Targets()`.

Current behavior:

- if `Targets()` succeeds and target disappeared: suppress;
- if `Targets()` succeeds and target remains: send;
- if `Targets()` **fails**: log the error and still proceed to send.

That makes "eligibility unknown" behave like "eligible".

### Required fix

Treat a send-time eligibility resolution error as a retryable internal failure without calling the provider.

Required behavior:

- do not suppress, because ineligibility was not proven;
- do not send, because eligibility was not proven;
- record retry/backoff through the existing worker outcome path;
- preserve provider `Retry-After` semantics for actual provider calls.

### Tests

Add a worker test proving:

1. delivery is claimed;
2. eligibility `Targets()` errors;
3. transport `Send` / `SendClaimed` is not called;
4. delivery is recorded retryable/pending according to existing retry policy.

---

## PR/documentation cleanup

PR #47's description is stale: it still says the PR ships E1 only and that E2/E3 are not implemented.

After the code fixes and `main` reconciliation are complete:

- update PR #47 to describe the actual Track E branch contents;
- state E1, E2, and E3 implementation status accurately;
- preserve the distinction that Track E remains open until the remaining E2 manual validation items are completed;
- list the final verification commands actually run;
- do not claim unsupported manual browser/device evidence.

Canonical docs must remain consistent with final merged behavior. Historical design/plan records should stay marked historical and must not override PRODUCT/ARCHITECTURE/ROADMAP.

---

## Required execution order

1. Confirm clean branch and current HEAD.
2. Fix Discord CSP OAuth bridge and tests.
3. Fix materialization fail-closed behavior and tests.
4. Fix send-time eligibility-error behavior and tests.
5. Implement Web Push outbound SSRF hardening and tests.
6. Determine whether any persistent DB has already applied Track E migrations `00014..00021`.
7. Merge current `main` into the branch using normal history.
8. Reconcile migration numbering/history according to the result of step 6.
9. Resolve code/generated/docs conflicts.
10. Run targeted tests for every finding.
11. Run the complete repository verification set.
12. Update PR #47 description/status only after verification is green.

If step 6 reveals a persistent database with the old Track E migration versions, stop before renumbering and report the deployed migration state plus a proposed forward-only reconciliation plan.

---

## Verification

Run targeted tests first, then at minimum:

```sh
task generate
task test
task test:integration
task test:pwa
task lint
task build
go vet ./...
go vet -tags integration ./...
git diff --check
task generate
```

The second `task generate` must produce no diff.

Also run the focused browser/manual Discord setup smoke test:

```text
/admin/discord
-> Connect
-> no CSP form-action error
-> Discord authorization page opens
-> choose server/channel
-> callback succeeds
-> destination shows Connected
```

Then perform a Discord delivery smoke test:

```text
new live Game Request activity
-> first Discord CREATE exactly once
later Join/Leave/Close/Cancel
-> same Discord message is edited
```

Do not treat live Discord access as a replacement for automated integration tests.

---

## Secret audit

Before final merge:

- no Discord webhook ID/token in plaintext;
- no reconstructed webhook URL in logs/docs/tests;
- no Discord OAuth access/refresh token persisted or logged;
- no real Web Push endpoint/auth/p256dh fixture values;
- no VAPID private key or data-encryption key committed;
- target keys remain non-secret identifiers;
- errors/diagnostics remain safe.

Synthetic fixtures should be unmistakably fake.

---

## Completion criteria

This remediation is complete only when:

- Discord Connect/Reconnect works under the existing CSP without weakening it;
- no configured transport can be silently omitted from a materialized activity because of a temporary `Targets()` failure;
- send-time eligibility errors do not call external providers;
- Web Push cannot be forged into a private/internal HTTPS request primitive;
- Track E migrations coexist safely with current `main`;
- current `main` is reconciled into the branch;
- targeted and full verification are green;
- generated output is clean;
- PR #47 accurately describes the final branch;
- no secret-safety regression is present.

Do not mark Track E complete solely from this remediation. Track E's remaining E2 manual browser/device validation remains governed by `docs/validation/track-e-web-push.md`.
