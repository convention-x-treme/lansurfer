# LANsurfer Roadmap

This roadmap carries LANsurfer from its completed web prototype to a focused 1.0 self-hosting release. The 1.0 work is organized as **parallel development tracks**, not a linear sequence of feature milestones. Track dependencies describe the points where work must converge; otherwise tracks may advance independently.

Product behavior and scope remain governed by [PRODUCT.md](PRODUCT.md). Technical decisions and module boundaries remain governed by [ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](../AGENTS.md).

Do not implement post-1.0 candidates for completeness. Within 1.0, work may proceed in any track whose stated dependencies are satisfied.

## 1.0 direction

LANsurfer 1.0 is optimized for a simple deployment model: **one LANsurfer installation serves one LAN party or gaming event context**.

For 1.0:

- Game Requests are instance-wide product objects. Creating, discovering, and joining a request must not require selecting or joining an Event, Clan, or other parent context.
- Do not introduce a generic `context`, polymorphic parent, or similar abstraction merely to prepare for future Event/Clan scoping.
- Multi-Event participation, Open Play, Event membership/access boundaries, Clans, and other persistent shared-service/community features are outside the 1.0 scope.
- Generic OIDC and Discord OAuth2 are independent supported login paths so an event can onboard players who do not already have an account with the operator's identity provider. Accounts may explicitly link both providers.
- Features that directly improve a self-hosted single-event installation remain in scope: player identity, local/rich Game catalog, instance identity, optional outbound notifications, installability, release artifacts, upgrades, backup, and operations.

Post-1.0 items are intentionally **not scheduled to a version**. They are collected below so they do not influence 1.0 implementation prematurely.

## Completed prototype baseline

The completed prototype established:

- a Go modular monolith with PostgreSQL, migrations, sqlc, templ, HTMX, Tailwind, and DaisyUI
- generic OIDC login, local users, revocable PostgreSQL sessions, opaque cookies, and global administrator promotion
- Event and local Game catalog administration, aliases, external-ID storage, and Game subscriptions
- Game Request creation, joining, leaving, cancellation, expiration, and transaction-safe capacity enforcement
- SSE-driven live updates with polling fallback
- embedded production assets, responsive desktop/mobile UX, security controls, integration coverage, and self-hosting foundations

The prototype schema and behavior are an input to the 1.0 migration path, not a promise that every prototype interface or relationship remains part of the 1.0 product model.

## Development model

Tracks A–F may run concurrently. A dependency blocks only the dependent integration work, not unrelated work inside the track.

| Track | Scope | Planning status | Integration dependencies |
| --- | --- | --- | --- |
| **A** | Single-Event Core | Prioritized | None for core work; Track B for final public-player-identity rendering |
| **B** | Player Identity & Authentication | Prioritized | None |
| **C** | Game Catalog & IGDB | Ready | None |
| **D** | Instance Identity & PWA | Ready | None |
| **E** | Durable Notifications | Ready with integration dependencies | Track A for GameRequest outbox hookup; Track D for browser Web Push integration; Track B only for any public-player-identity rendering |
| **F** | Distribution & Operations | Ready | Final backup/restore and upgrade validation depend on completed persistent migrations from the other tracks |

### Planning status and delivery evidence

**Prioritized** and **Ready** describe planning, not verified implementation progress. Record a responsible owner and a linked delivery issue/PR before marking work **In progress**. **Integration ready** requires demonstrated track-local outcomes, agreed contracts, and a linked list of cross-track checks still pending. **Done** additionally requires merged work and passing applicable integration checks. No owner, delivery date, or completion percentage is inferred from a track description.

Review checkpoint: **2026-09-15**, against `main` at [`e356791`](https://github.com/convention-x-treme/lansurfer/commit/e356791affdef6a2eb4968c1256c17be870e8407). For example, the [Game Request service at this revision](https://github.com/convention-x-treme/lansurfer/blob/e356791affdef6a2eb4968c1256c17be870e8407/internal/gamerequest/gamerequest.go) still requires a current Event during creation. The single-event target is therefore not evidence that Track A is delivered.

Prefer early risk reduction in Track A's migration and interaction reliability, Track B's onboarding/privacy boundary, and Track F's deployment/restore rehearsals. This is a prioritization recommendation, not a new serial dependency: Tracks C, D, and E can still advance independently as described below.

Keep delivery slices small enough to review separately. Each implementation issue/PR should identify its owner, scope, required contracts, acceptance evidence, and remaining risks. Coordinate migration numbering, shared templates, and composition-root changes before parallel branches alter the same areas. Final release scope and staffing decisions are recorded under [Review decisions and backlog alignment](#review-decisions-and-backlog-alignment).

### Cross-track contracts

Keep cross-track contracts deliberately small so parallel work does not collapse back into one shared implementation branch.

1. **Track A → Track E: Game Request notification contract**
   - Track A owns Game Request writes and the transactional outbox integration point.
   - Track E may build workers, retry behavior, transport models, and adapters in parallel.
   - Final outbox persistence/hookup must use Track A's stabilized Game Request write paths and must commit notification intent in the same PostgreSQL transaction as the originating request change.
   - Agree on stable work/request identifiers, change kinds, and ordering/replay behavior in the contract PR. Failed or rolled-back domain changes must not leave notification intent behind; the in-memory SSE publisher is not a durable handoff.

2. **Track B internal contract: authentication → onboarding**
   - OIDC and Discord both resolve to the same local LANsurfer User model.
   - Game Nick onboarding is provider-independent and runs after successful authentication when required.
   - Discord login, OIDC login, linking, and profile work may be developed in parallel as long as they preserve that boundary.

3. **Track D → Track E: service worker contract**
   - Track D owns the installable web shell, manifest, service-worker registration, lifecycle, and update behavior.
   - Track E owns Web Push subscriptions, permission UX, payloads, and delivery.
   - Browser Web Push integration waits for the Track D service-worker contract; backend Web Push work does not.

4. **Track F consumes all tracks only at release validation**
   - Build/release automation, binaries, images, checksums, SBOMs, provenance, health/version commands, and baseline operations documentation may be developed early.
   - Final prototype-to-1.0 upgrade, backup/restore, and rollback-limit validation wait until the persistent schema changes from the other tracks have settled.

5. **Track B → Tracks A/E: public player identity**
   - Track B owns the local-user/Game Nick contract. Request views, and any delivery presentation that includes player identity, consume public fields rather than authentication-provider names or emails.
   - Only that final rendering integration depends on Track B; core request writes, notification workers, and Discord destination setup do not wait for Discord login.
   - Web Push remains identity-free under its existing payload rules. This contract does not authorize adding player data to push payloads or expanding Discord message content.

# Track A — Single-Event Core

## Goal

Align the prototype with the focused 1.0 deployment model so the installation itself is the event context and Game Requests no longer depend on Event membership or selection.

## Can proceed in parallel with

Tracks B, C, D, F and most of Track E.

Final public-player-identity rendering consumes Track B’s contract; core migration and Game Request behavior do not wait for it.

## Expected outcomes

- Treat the LANsurfer installation itself as the 1.0 event context.
- Let signed-in players discover, create, join, leave, close, and cancel Game Requests without selecting or joining an Event, Open Play, Clan, or other parent context.
- Treat Game Requests as instance-wide product objects in 1.0 behavior and UI.
- Preserve existing prototype Game Requests during migration while removing any user-visible requirement to select an Event or satisfy Event membership/lifecycle rules.
- Keep migration and persistence details explicit in the architecture; do not add a generic context abstraction for future flexibility.
- Do not add Event switching, Event invitations, seats, Event rosters, Event-scoped access control, Open Play, or Event lifecycle/history workflows in 1.0.
- Stabilize the Game Request write paths and transactional boundaries that Track E will use for durable notification outbox records.

## Acceptance

The complete Game Request flow works without Event/Clan/context selection or membership, existing prototype requests survive the migration path, capacity invariants remain concurrency-safe, and Track E has a stable transactional integration point for notification intent.

### Verification evidence

- Exercise a fresh installation with no Event setup and populated prototype fixtures with absent/current/historical Event states. Preserve request IDs, creators, memberships, status, and timestamps; document the treatment of legacy Event data instead of silently resetting it.
- Cover duplicate and overlapping joins/leaves, competing joins for the last spot, and close/cancel races. Existing creator and capacity rules must hold independently of browser-side guards.
- Investigate [#15](https://github.com/convention-x-treme/lansurfer/issues/15) and retain an evidence-backed reproduction and regression test for the discovered hang. Verify that failed, aborted, timed-out, and conflicting mutations release pending UI state and that SSE/HTMX overlap does not leave the interface stuck. A debounce alone is not proof of a fix.
- At the A/E integration boundary, prove that a rollback leaves neither the domain change nor its outbox work, while a committed change retains notification intent across a restart before delivery.

# Track B — Player Identity & Authentication

## Goal

Give players a stable public identity and make onboarding practical even when attendees do not have an account with the event operator's identity provider.

## Can proceed in parallel with

All other tracks. Profile/Game Nick work and Discord-provider work may also proceed in parallel inside this track if they share the provider-independent onboarding contract.

## Expected outcomes

### Player profile

- Require a unique, case-insensitive public **Game Nick** after first login; upgraded prototype users choose one once.
- Allow a short biography.
- Allow multiple labeled player handles per Game.
- Show Game Nick, biography, followed Games, and Game handles on a profile.
- Let any signed-in user view player profiles.
- Generate initials for the profile image; do not add avatar uploads.
- Keep provider identity name and email visible only to the account owner. Global administrators do not receive provider identity details through normal product UI.

### Authentication providers

- Keep generic OIDC authentication provider-neutral.
- Add Discord OAuth2 authorization-code authentication as a separate provider with its own callback.
- Support OIDC-only, Discord-only, and accounts explicitly linked to both.
- Link and unlink providers only through an explicit signed-in account flow.
- Require at least one linked identity at all times.
- Never merge accounts based only on email.
- Persist short-lived, one-use authentication/link flow state that binds the initiating browser, provider, operation, and signed-in user where applicable.
- Keep local user IDs, revocable PostgreSQL sessions, opaque cookies, and the existing no-local-password model.
- Route both providers through the same provider-independent Game Nick onboarding decision after authentication.

## Acceptance

A new or upgraded user can authenticate through either OIDC or Discord, explicitly link both when desired, is never implicitly merged by email, and can complete Game Nick onboarding and use profiles without exposing provider data.

### Verification evidence

- Exercise OIDC-only, Discord-only, and both-provider configurations, plus upgraded users reaching onboarding through an existing session. Preserve the local user and its existing Game subscriptions and request relationships.
- Reject expired/replayed flow state, wrong-browser/provider/operation callbacks, attempts to link an identity already owned by another account, and concurrent unlink attempts that would remove the last identity.
- Test case-insensitive Game Nick collisions, including concurrent claims, and the profile's owner/non-owner/administrator visibility boundaries.
- Check rendered pages, fragments, and live-update/public read models for provider-name/email leakage; changing the profile page alone is not sufficient.

# Track C — Game Catalog & IGDB

## Goal

Make catalog administration faster and Game presentation richer while keeping LANsurfer's local catalog authoritative and optional-provider behavior resilient.

## Dependencies

No hard dependency on another 1.0 track. Showing new metadata inside Game Request read models/cards may be integrated after the relevant Track A UI/read-model changes settle.

## Expected outcomes

- Add an optional IGDB catalog provider using operator-supplied Twitch credentials.
- Restrict search, preview, import, attachment, and refresh to global administrators.
- Import summary, first release date, platform names, and one cover reference.
- Preview search results before creating a local Game or explicitly attaching the external ID to an existing Game.
- Never merge automatically by name.
- Preview refresh changes and require field-level selection before overwriting local data; local edits remain authoritative.
- Show covers and metadata in the Game catalog and Game Request cards/details.
- Keep core catalog search, local edits, and Game Request behavior usable when IGDB is unavailable or unconfigured.
- Display required IGDB attribution and document the provider terms and credential setup.

### Approved remote-cover exception

IGDB covers may load directly from `images.igdb.com`. This is a narrow exception to the no-runtime-CDN/external-assets rule and must include:

- a fixed image-host allowlist and matching Content Security Policy
- `Referrer-Policy: no-referrer`
- a local placeholder when a cover is unavailable
- operator/user documentation that a viewer's IP address is disclosed to IGDB when a remote cover loads
- graceful degradation limited to cover presentation

Do not add another catalog provider or a local cover proxy/cache in this track.

## Acceptance

An administrator can safely preview and control imports and refreshes. Locally managed data is never silently overwritten or merged, and disabling or losing IGDB does not break core workflows.

### Verification evidence

- Deny provider-management actions to non-administrators. Test conflicting external-ID attachments, repeated imports, field-level refresh selection, and a local edit made between preview and confirmation.
- Exercise unconfigured credentials, timeouts, rate limits, and malformed/provider-error responses while local catalog and Game Request flows remain usable.
- Verify the fixed cover host/CSP boundary, no-referrer behavior, attribution, and local placeholder for missing or blocked covers. IGDB is not a prerequisite for local Game discovery or following.

# Track D — Instance Identity & PWA

## Goal

Let an operator identify the installation for the LAN party or event it serves while retaining the fixed, accessible visual system and making the site installable as a web app.

## Dependencies

No hard dependency to start. The service-worker registration/lifecycle contract produced here is a prerequisite for Track E's browser-side Web Push integration.

## Expected outcomes

- Store a per-instance display name, welcome text, contact link, logo, and application icon in PostgreSQL.
- Limit configuration to global administrators.
- Retain LANsurfer colors, typography, accessibility rules, and a visible “Powered by LANsurfer” attribution.
- Accept a PNG logo up to 2 MiB and 2048×1024 pixels.
- Accept a square PNG icon from 192 to 512 pixels and up to 512 KiB.
- Store uploaded identity assets in PostgreSQL; do not depend on remote asset hosts.
- Serve a dynamic web app manifest with a stable application ID, stable start URL, and standalone display mode.
- Omit manifest icons when none are configured and use a generated monogram fallback in the normal UI.
- Add the minimal service-worker shell needed for installation and Web Push; do not promise offline application behavior.
- Define service-worker registration, lifecycle, and update behavior so Track E can attach push handling without owning the shell.

## Acceptance

An administrator can safely configure identity fields and valid images, browsers can install the site with stable manifest identity, customization cannot replace the fixed design system or obscure LANsurfer attribution, and Track E has a stable browser service-worker integration point.

### Verification evidence

- Test unauthorized updates, malformed PNGs, and byte/dimension limits; confirm that valid identity assets survive backup/restore without remote storage.
- Record installation and update behavior on the agreed browser/device matrix, including absent icons and identity changes. The manifest's application identity must remain stable.
- Verify service-worker update/logout behavior so private content is not exposed through stale cached responses. Unsupported installation or push capabilities must leave ordinary browser workflows usable; no offline application support is added.

# Track E — Durable Notifications

## Goal

Deliver relevant Game Request changes beyond the open browser without coupling external transport failures to core user actions and without requiring multi-Event concepts.

## Parallel work and dependencies

The notification module, worker model, delivery/retry persistence, Web Push backend adapter, and Discord delivery adapter may start immediately.

The following integration work waits for its specific contract:

- **GameRequest outbox hookup depends on Track A** stabilizing the Game Request write/persistence paths.
- **Browser Web Push integration depends on Track D** stabilizing service-worker registration/lifecycle behavior.
- **Public-player-identity rendering depends on Track B** only where such identity is included; identity-free Web Push and transport work do not.

Discord notification delivery is independent from Discord login in Track B.

## Expected outcomes

### Notification architecture

- Add a focused `notification` module because two concrete durable delivery transports exist.
- Record Game Request notification work in a transactional outbox owned by the Game Request module.
- Process delivery asynchronously with durable workers and persisted delivery/message references.
- Never fail or roll back a Game Request action because an external delivery fails.
- Keep SSE as the ephemeral in-browser update path.
- Retry HTTP 429 responses according to `Retry-After`; retry network and 5xx failures with capped exponential backoff and jitter; record permanent failures for manual retry.
- Do not add a persistent in-app notification inbox.

### Web Push

- Use VAPID and a small adapter around `github.com/marknefedov/go-webpush/v2`; adding this direct dependency is approved for the concrete Web Push need.
- Support multiple labeled browser/device subscriptions per user.
- Require feature detection and an explicit user gesture before requesting permission.
- Provide a global push toggle. Do not add per-Event settings in 1.0.
- Send a new-request push only when its Game is followed, push is enabled for the recipient, and the recipient is not the creator.
- Limit push content to Game, time, and a deep link. The configured instance display name may be used as presentation context. Do not include creator identity, description, roster, or other member information.

### Discord delivery

- Keep Discord notification delivery separate from Discord login even though both are supported in 1.0.
- Configure at most one Discord destination for the LANsurfer instance through Discord's `webhook.incoming` OAuth flow; do not accept pasted webhooks or add a bot/gateway connection.
- Let global administrators configure the destination.
- Encrypt webhook IDs and tokens at rest with AES-GCM using an operator-provided 32-byte data-encryption key; never reveal a stored secret.
- Create one Discord message per Game Request with `wait=true`, store its message ID, and edit it when membership or status changes.
- Provide outbound links only; Discord commands and Discord-side writes are out of scope.
- Sanitize user content, send no allowed mentions, and honor Discord rate-limit headers.
- Provide authorized integration status, test, and manual-retry controls.

## Acceptance

Outbox persistence survives restarts, retries are bounded and observable, push targeting and payload privacy are tested, Discord messages remain synchronized, external outages never prevent Game Request actions, and the applicable cross-track contracts are integrated without crossing module ownership boundaries.

### Verification evidence

- Exercise worker interruption, abandoned work recovery, duplicate work, and out-of-order updates. Retried Discord edits must not overwrite a newer request state with an older one.
- Cover the case where a provider accepts a send but its response is lost. Preserve the one-message-per-request objective, document/test the recovery path, and expose uncertain delivery rather than claiming success or assuming exactly-once external delivery.
- Test 429 handling, capped retries, exhausted/permanent failures, and authorized manual retry. Document retry budgets, stale-work handling, and delivery-record retention; diagnostics must not disclose webhook credentials or push subscription secrets.
- Recheck push eligibility before delayed sends/retries, including unfollow, toggle-off, and creator exclusion. Remove permanently expired subscriptions as specified in the architecture; test payload privacy on every send path.
- Demonstrate that disabled or failing outbound transports do not prevent Game Request actions. Keep the Game Request outbox distinct from any separately approved request activity history.

# Track F — Distribution & Operations

## Goal

Make LANsurfer reproducibly installable, upgradable, backed up, and diagnosable by self-hosting operators running a single-event instance.

## Parallel work and dependencies

Release automation and most operator tooling can begin immediately. Final upgrade/backup validation depends on the persistent schema from Tracks A–E being stable enough to represent the intended 1.0 state.

## Expected outcomes

- Publish official Linux amd64 and arm64 binaries and a matching multi-architecture GHCR image.
- Add `lansurfer version`, healthcheck, encryption-key generation, and Web Push key-generation commands.
- Tag stable artifacts by exact version, major.minor, major, and `latest`; tag prereleases by exact version only.
- Publish checksums, software bills of materials, and provenance attestations.
- Use native Go tooling plus pinned official Docker and GitHub Actions; do not add GoReleaser.
- Provide a production Compose example with a pinned LANsurfer image, PostgreSQL not exposed on the host, and only the application and database as required services.
- Keep TLS and reverse-proxy operation as operator responsibilities and document safe example configuration.
- Document every configuration value, administrator bootstrap, backup/restore, encryption and VAPID key backup, upgrades, and rollback limitations.
- Keep migrations forward-only and test upgrading a populated prototype schema to the 1.0 single-event model.
- Document the supported upgrade path for each release. Do not promise arbitrary direct upgrades across all 1.x releases.

## Acceptance

Release artifacts are reproducible and verifiable, the documented Compose deployment starts from an empty host, backup/restore is exercised, and a populated prototype installation upgrades without data loss after the integration schema is finalized.

### Verification evidence

- Record the source revision, pinned build inputs, repeat-build comparison, binary/image architectures, checksums, SBOMs, attestations, and artifact-verification steps. Confirm that prereleases do not move stable image tags.
- Run the documented clean-host installation and first-admin flow without relying on a developer checkout, unlisted tools, or an exposed PostgreSQL port.
- Restore a populated database and required encryption/VAPID keys into a clean deployment. Check profiles, Game subscriptions, requests/memberships, identity assets, and notification references, not merely that PostgreSQL starts.
- Exercise the documented prototype upgrade and migration-failure/restart path. A failed migration must prevent HTTP startup; recovery and rollback documentation must explain restoring a compatible backup rather than automatically running DOWN migrations or assuming an older binary accepts the new schema.

# 1.0 Integration Gate

Run cross-track integration checks as soon as their contracts land, rather than postponing integration testing until feature work ends. The formal Integration Gate passes only when the required track outcomes and evidence are complete. It is not a separate feature track; it proves the independently developed work together.

## Depends on

Tracks A–F reaching their required integration-ready state.

## Required integration checks

- OIDC-only, Discord-only, and explicitly linked-account authentication all reach the same provider-independent Game Nick onboarding and local User model.
- Player profiles and Game handles work with both authentication paths without exposing provider-private data.
- Game Requests are fully instance-wide and preserve concurrency/capacity behavior after the prototype migration.
- Game Request writes produce durable notification intent atomically where required, without transport failures affecting the originating action.
- Web Push works through the Track D service worker and respects follow/toggle/privacy rules.
- Discord delivery remains separate from Discord authentication and keeps one instance-level message synchronized per Game Request.
- IGDB failure/unconfiguration does not break the local Game catalog or Game Request workflows.
- Instance identity, manifest, service worker, CSP, and remote-cover exception coexist correctly.
- A populated prototype installation upgrades through all 1.0 migrations without data loss.
- Production build, Compose deployment, backup/restore, and documented operator flows are exercised against the integrated application.

## Acceptance

All cross-track contracts are exercised by relevant domain, PostgreSQL, HTTP, browser-facing, and release/operations tests. No integration blocker requires redesigning the product back into a shared multi-Event/community service.

### Gate evidence and blocker policy

- Link the tested revision/artifact, test commands/results, migration fixture, configuration variants, and browser/device matrix in the integration tracking issue or release PR. Document repeatable manual checks where provider/browser automation is impractical; this does not authorize a new testing framework.
- Exercise both a locally usable installation with optional integrations disabled and the enabled-provider combinations. Optional operator configuration does not make the corresponding approved 1.0 capability optional to deliver.
- Agree the pilot/load envelope and acceptance targets before running the final rehearsal: concurrent sessions/SSE connections, active requests, request bursts, response/update latency, and recovery after provider outages. [README.md](../README.md) describes approximately 400 CXT attendees; that is context, not a measured concurrency or throughput target.
- Treat data loss, authentication/authorization bypass, provider-private-data exposure, capacity violations, and reproducible persistent hangs in core workflows as release blockers. Record unresolved failures explicitly; a feature-complete checklist or a documentation PR cannot close them.
- Record the release decision owner and results of the agreed pilot/rehearsal. Dates and numerical thresholds remain open under [Review decisions and backlog alignment](#review-decisions-and-backlog-alignment); do not invent a successful pilot or a release commitment.

# 1.0 Release Candidate and Stable Release Gate

## Depends on

The 1.0 Integration Gate being green.

## Expected outcomes

- Publish `v1.0.0-rc.1` with complete migration, operations, security, privacy, authentication-provider, catalog-provider, and notification-provider documentation.
- Before cutting the candidate, reconcile the accepted/deferred scope decisions and link the release evidence and decision owner. Unapproved proposals remain unscheduled; including an issue in the backlog does not silently expand 1.0.
- After the release candidate, accept only release blockers, security fixes, migration fixes, and documentation corrections.
- Re-run the integrated OIDC/Discord onboarding, player profile, instance-wide Game Request, provider-failure, notification-retry, backup/restore, and prototype-to-1.0 upgrade paths.
- Verify that no 1.0 workflow requires Event selection/membership, Open Play, Clans, or another Game Request parent context.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Publish `v1.0.0` when release-candidate blockers are resolved.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of the release, not during feature development.

## Acceptance

All track and Integration Gate acceptance criteria remain green in a release build, documentation matches observable behavior, signed artifacts are available for both supported architectures, and no release blockers remain.

# Review decisions and backlog alignment

Review snapshot: **2026-09-15**. The following issues need explicit placement or delivery tracking; their existence is not proof of implementation or approval for 1.0. This review preserves the approved Tracks A–F and does not schedule additional product features.

| Issue | Relationship to the roadmap | Disposition in this review |
| --- | --- | --- |
| [#15 — interaction hangs](https://github.com/convention-x-treme/lansurfer/issues/15) | Track A and Integration Gate reliability | Prioritize investigation/regression evidence within existing core-quality work; not resolved by this document. |
| [#12 — request search and filters](https://github.com/convention-x-treme/lansurfer/issues/12) | Track A discovery; tag filters only if #10 is approved | Explicit 1.0 inclusion/deferral decision needed for the issue's proposed feature set. Baseline discovery remains required. |
| [#11 — English/German i18n](https://github.com/convention-x-treme/lansurfer/issues/11) | Cross-track presentation, validation, accessibility copy, and contributor guidance | Scope decision needed; if included, agree the shared message/locale contract early without introducing localization into domain values. |
| [#10 — curated request tags](https://github.com/convention-x-treme/lansurfer/issues/10) | Track A request vocabulary and optional #12 filters | Scope decision needed, including final vocabulary and selection limit; neither is fixed by this review. |
| [#13 — trending/current interest](https://github.com/convention-x-treme/lansurfer/issues/13) | Focused read models; #12 navigation/search integration; optional Track C covers | Scope decision needed. Request-based ranking and persisted search-interest tracking are distinct choices, not an automatic analytics commitment. |
| [#14 — request activity history](https://github.com/convention-x-treme/lansurfer/issues/14) | Track A transactional lifecycle writes and Track B public identity | Scope decision needed. If included, coordinate with A/E; durable activity retention is not interchangeable with delivery outbox retention and is not a notification inbox. |
| [#7 — optional dotenv](https://github.com/convention-x-treme/lansurfer/issues/7), [#8 — development database modes](https://github.com/convention-x-treme/lansurfer/issues/8) | Contributor workflow adjacent to Track F | Track separately from production-release acceptance; these do not change the production configuration model. |

## Decisions needed

| ID | Question for the product/release owner | Until answered |
| --- | --- | --- |
| D1 — scope | Which of #10–#14 belong in 1.0, which are deferred, and which are only pilot experiments? For #13, decide request-based trends separately from search-interest persistence. | No additions are authorized by this review. Keep the existing approved 1.0 scope; prioritize #15 as core-quality work. |
| D2 — pilot and timing | Is there a target LAN/pilot date, and what must be available for that pilot? Is that a pilot subset or the full 1.0 release? | No invented deadline or automatic scope reduction. A reduced pilot does not redefine the stable-release gate. |
| D3 — operating envelope | What peak concurrent usage, hardware/network conditions, and desktop/mobile browser combinations should be supported, and what measurable response/recovery targets determine acceptance? | The approximately 400-attendee use case is not treated as a load-test result. Agree the envelope before the final rehearsal. |
| D4 — ownership and capacity | Who owns each active delivery slice, how much implementation/review capacity is available, and who makes the final release decision? | No assumed assignments or dates. Parallel tracks permit concurrency; they do not imply six staffed teams. |

Record decisions with an owner, date, and linked issue/PR. When a candidate is approved, update the relevant track, acceptance criteria, and `PRODUCT.md` (and `ARCHITECTURE.md` when needed) together before implementation. Unresolved candidate proposals do not block unrelated work in already approved tracks.

# Post-1.0 candidates — not scheduled

These items are useful possible directions, but they are **not assigned to a release and must not shape 1.0 abstractions or implementation unless the roadmap is explicitly changed**.

## Multi-Event participation and contextual Game Requests

Possible future work includes:

- multiple scheduled Events in one LANsurfer installation
- a persistent Open Play context
- Event membership with participant/manager roles
- Event invitations, rosters, seats, notification preferences, and access boundaries
- upcoming/active/ended lifecycle and read-only history
- Event selection and overlap behavior
- context-scoped Game Requests
- per-context Discord destinations and push settings
- durable removal/cleanup workflows across Event and Game Request ownership boundaries

If this work is scheduled later, design the concrete Event/Request relationship for that need. Do not prebuild a generic parent/context framework in 1.0.

## Clans and persistent social discovery

Possible future work includes:

- Clan ownership and membership
- Clan invitations and rosters
- a primary Clan/tag on player profiles
- administrator safety controls
- Clan-aware discovery or Game Request scoping when a concrete use case is approved

Clan posts, chat, private Clan LFG, and custom roles remain out of scope unless separately approved.

# Explicitly not planned

The following have no planned release and should not influence current implementation unless the roadmap is explicitly changed:

- a generic polymorphic `context`/parent abstraction for Game Requests
- a persistent in-app notification inbox
- generic reports, suspensions, or a complex moderation system
- Clan posts, chat, private Clan LFG, or custom roles
- Game Request waitlists, ready checks, or comments
- guest accounts, passkeys, or account self-deletion
- full theme customization
- catalog providers other than IGDB
- local caching or proxying of remote covers
- multi-instance or high-availability application architecture
- WebSockets, native applications, tournaments, ratings, teams, or federation
