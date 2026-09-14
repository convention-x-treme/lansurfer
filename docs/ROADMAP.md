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

| Track | Scope | Status | Hard dependencies |
| --- | --- | --- | --- |
| **A** | Single-Event Core | Active | None |
| **B** | Player Identity & Authentication | Active | None |
| **C** | Game Catalog & IGDB | Ready | None |
| **D** | Instance Identity & PWA | Ready | None |
| **E** | Durable Notifications | Ready with integration dependencies | Track A for GameRequest outbox hookup; Track D for browser Web Push integration |
| **F** | Distribution & Operations | Ready | Final backup/restore and upgrade validation depend on completed persistent migrations from the other tracks |

### Cross-track contracts

Keep cross-track contracts deliberately small so parallel work does not collapse back into one shared implementation branch.

1. **Track A → Track E: Game Request notification contract**
   - Track A owns Game Request writes and the transactional outbox integration point.
   - Track E may build workers, retry behavior, transport models, and adapters in parallel.
   - Final outbox persistence/hookup must use Track A's stabilized Game Request write paths and must commit notification intent in the same PostgreSQL transaction as the originating request change.

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

# Track A — Single-Event Core

## Goal

Align the prototype with the focused 1.0 deployment model so the installation itself is the event context and Game Requests no longer depend on Event membership or selection.

## Can proceed in parallel with

Tracks B, C, D, F and most of Track E.

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

# Track E — Durable Notifications

## Goal

Deliver relevant Game Request changes beyond the open browser without coupling external transport failures to core user actions and without requiring multi-Event concepts.

## Parallel work and dependencies

The notification module, worker model, delivery/retry persistence, Web Push backend adapter, and Discord delivery adapter may start immediately.

Two integration points are intentionally delayed:

- **GameRequest outbox hookup depends on Track A** stabilizing the Game Request write/persistence paths.
- **Browser Web Push integration depends on Track D** stabilizing service-worker registration/lifecycle behavior.

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

Outbox persistence survives restarts, retries are bounded and observable, push targeting and payload privacy are tested, Discord messages remain synchronized, external outages never prevent Game Request actions, and the two stated cross-track contracts are integrated without crossing module ownership boundaries.

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

# 1.0 Integration Gate

The Integration Gate begins when the individual track acceptance criteria are substantially complete. It is not a separate feature track; it is where the independently developed work is proven together.

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

# 1.0 Release Candidate and Stable Release Gate

## Depends on

The 1.0 Integration Gate being green.

## Expected outcomes

- Publish `v1.0.0-rc.1` with complete migration, operations, security, privacy, authentication-provider, catalog-provider, and notification-provider documentation.
- After the release candidate, accept only release blockers, security fixes, migration fixes, and documentation corrections.
- Re-run the integrated OIDC/Discord onboarding, player profile, instance-wide Game Request, provider-failure, notification-retry, backup/restore, and prototype-to-1.0 upgrade paths.
- Verify that no 1.0 workflow requires Event selection/membership, Open Play, Clans, or another Game Request parent context.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Publish `v1.0.0` when release-candidate blockers are resolved.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of the release, not during feature development.

## Acceptance

All track and Integration Gate acceptance criteria remain green in a release build, documentation matches observable behavior, signed artifacts are available for both supported architectures, and no release blockers remain.

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
- Clan-aware discovery or Game Request scoping if a concrete use case is approved

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
