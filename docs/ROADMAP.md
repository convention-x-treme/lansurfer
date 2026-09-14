# LANsurfer Roadmap

This roadmap carries LANsurfer from its completed web prototype to a focused 1.0 self-hosting release. Milestones describe product outcomes and architectural boundaries; implementation details remain governed by [PRODUCT.md](PRODUCT.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](../AGENTS.md).

Do not implement later milestones for completeness. Finish and verify the current milestone before advancing the progress marker.

## 1.0 direction

LANsurfer 1.0 is optimized for a simple deployment model: **one LANsurfer installation serves one LAN party or gaming event context**.

For 1.0:

- Game Requests are instance-wide product objects. Creating, discovering, and joining a request must not require selecting or joining an Event, Clan, or other parent context.
- Do not introduce a generic `context`, polymorphic parent, or similar abstraction merely to prepare for future Event/Clan scoping.
- Multi-Event participation, Open Play, Event membership/access boundaries, Clans, and other persistent shared-service/community features are outside the 1.0 scope.
- Generic OIDC remains the supported external authentication path for 1.0. Additional login providers and account linking are deferred until there is a concrete post-1.0 need.
- Features that directly improve a self-hosted single-event installation remain in scope: player identity, local/rich Game catalog, instance identity, optional outbound notifications, installability, release artifacts, upgrades, backup, and operations.

Post-1.0 items are intentionally **not scheduled to a version**. They are collected below so they do not influence 1.0 implementation prematurely.

## Progress

- [x] Prototype milestones 0–8 — foundation, authentication, catalog, Events, Game Requests, realtime updates, UX, and hardening
- [ ] **Current:** Milestone 9 — Single-Event Model and Player Identity
- [ ] Milestone 10 — IGDB Catalog Import and Rich Game Metadata
- [ ] Milestone 11 — Instance Identity and Installable Web Shell
- [ ] Milestone 12 — Durable Notifications, Web Push, and Discord Delivery
- [ ] Milestone 13 — Production Distribution and Operations
- [ ] Milestone 14 — 1.0 RC and Stable Release

There are no date or real-event pilot gates. Each milestone advances when its acceptance criteria and relevant tests are complete.

## Completed prototype (milestones 0–8)

The prototype established:

- a Go modular monolith with PostgreSQL, migrations, sqlc, templ, HTMX, Tailwind, and DaisyUI
- generic OIDC login, local users, revocable PostgreSQL sessions, opaque cookies, and global administrator promotion
- Event and local Game catalog administration, aliases, external-ID storage, and game subscriptions
- Game Request creation, joining, leaving, cancellation, expiration, and transaction-safe capacity enforcement
- SSE-driven live updates with polling fallback
- embedded production assets, responsive desktop/mobile UX, security controls, integration coverage, and self-hosting foundations

The prototype schema and behavior are an input to the 1.0 migration path, not a promise that every prototype interface or relationship remains part of the 1.0 product model.

# Milestone 9 — Single-Event Model and Player Identity

## Goal

Align the prototype with the focused 1.0 deployment model and give players a stable public identity that is useful during one LAN party or gaming event.

## Expected outcomes

### Single-event request model

- Treat the LANsurfer installation itself as the 1.0 event context.
- Let signed-in players discover, create, join, leave, close, and cancel Game Requests without selecting or joining an Event, Open Play, Clan, or other parent context.
- Treat Game Requests as instance-wide product objects in 1.0 behavior and UI.
- Preserve existing prototype Game Requests during migration while removing any user-visible requirement to select an Event or satisfy Event membership/lifecycle rules.
- Keep migration and persistence details explicit in the architecture; do not add a generic context abstraction for future flexibility.
- Do not add Event switching, Event invitations, seats, Event rosters, Event-scoped access control, Open Play, or Event lifecycle/history workflows in 1.0.

### Player profile

- Require a unique, case-insensitive public **Game Nick** after first login; upgraded prototype users choose one once.
- Allow a short biography.
- Allow multiple labeled player handles per Game.
- Show Game Nick, biography, followed Games, and Game handles on a profile.
- Let any signed-in user view player profiles.
- Generate initials for the profile image; do not add avatar uploads.
- Keep provider identity name and email visible only to the account owner. Global administrators do not receive provider identity details through normal product UI.

### Authentication scope

- Keep generic OIDC authentication provider-neutral and supported for 1.0.
- Keep local user IDs, revocable PostgreSQL sessions, opaque cookies, and the existing no-local-password model.
- Do not add Discord login, multiple linked providers, provider linking/unlinking flows, or email-based account merging in 1.0.

## Acceptance

A new or upgraded user can enter the application after choosing a valid Game Nick, use profiles and per-Game handles without exposing provider data, and use the full Game Request flow without selecting or joining any Event/Clan/context. Existing prototype requests survive the migration path.

# Milestone 10 — IGDB Catalog Import and Rich Game Metadata

## Goal

Make catalog administration faster and Game presentation richer while keeping LANsurfer's local catalog authoritative and optional-provider behavior resilient.

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

Do not add another catalog provider or a local cover proxy/cache in this milestone.

## Acceptance

An administrator can safely preview and control imports and refreshes. Locally managed data is never silently overwritten or merged, and disabling or losing IGDB does not break core workflows.

# Milestone 11 — Instance Identity and Installable Web Shell

## Goal

Let an operator identify their LANsurfer installation for the specific LAN party or event it serves, while retaining the product's fixed, accessible visual system and making the site installable as a web app.

## Expected outcomes

- Store a per-instance display name, welcome text, contact link, logo, and application icon in PostgreSQL.
- Limit configuration to global administrators.
- Retain LANsurfer colors, typography, accessibility rules, and a visible “Powered by LANsurfer” attribution.
- Accept a PNG logo up to 2 MiB and 2048×1024 pixels.
- Accept a square PNG icon from 192 to 512 pixels and up to 512 KiB.
- Store uploaded identity assets in PostgreSQL; do not depend on remote asset hosts.
- Serve a dynamic web app manifest with a stable application ID, stable start URL, and standalone display mode.
- Omit manifest icons when none are configured and use a generated monogram fallback in the normal UI.
- Add the minimal service-worker shell needed for installation and the later Web Push milestone; do not promise offline application behavior.

## Acceptance

An administrator can safely configure identity fields and valid images, browsers can install the site with stable manifest identity, and customization cannot replace the fixed design system or obscure LANsurfer attribution.

# Milestone 12 — Durable Notifications, Web Push, and Discord Delivery

## Goal

Deliver relevant Game Request changes beyond the open browser without coupling external transport failures to core user actions and without requiring multi-Event concepts.

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

- Keep Discord delivery independent from authentication; Discord login is not required for this milestone.
- Configure at most one Discord destination for the LANsurfer instance through Discord's `webhook.incoming` OAuth flow; do not accept pasted webhooks or add a bot/gateway connection.
- Let global administrators configure the destination.
- Encrypt webhook IDs and tokens at rest with AES-GCM using an operator-provided 32-byte data-encryption key; never reveal a stored secret.
- Create one Discord message per Game Request with `wait=true`, store its message ID, and edit it when membership or status changes.
- Provide outbound links only; Discord commands and Discord-side writes are out of scope.
- Sanitize user content, send no allowed mentions, and honor Discord rate-limit headers.
- Provide authorized integration status, test, and manual-retry controls.

## Acceptance

Outbox persistence survives restarts, retries are bounded and observable, push targeting and payload privacy are tested, Discord messages remain synchronized, and external outages never prevent Game Request actions.

# Milestone 13 — Production Distribution and Operations

## Goal

Make LANsurfer reproducibly installable, upgradable, backed up, and diagnosable by self-hosting operators running a single-event instance.

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

Release artifacts are reproducible and verifiable, the documented Compose deployment starts from an empty host, backup/restore is exercised, and a populated prototype installation upgrades without data loss.

# Milestone 14 — 1.0 RC and Stable Release

## Goal

Freeze the focused 1.0 feature set, validate the complete supported deployment and upgrade paths, and publish a stable release.

## Expected outcomes

- Publish `v1.0.0-rc.1` with complete migration, operations, security, privacy, catalog-provider, and notification-provider documentation.
- After the release candidate, accept only release blockers, security fixes, migration fixes, and documentation corrections.
- Exercise generic OIDC authentication, Game Nick onboarding, player profiles, the instance-wide Game Request flow, catalog-provider failure, notification retries, backup/restore, and prototype-to-1.0 upgrade paths.
- Verify that no 1.0 workflow requires Event selection/membership, Open Play, Clans, or another Game Request parent context.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Publish `v1.0.0` when release-candidate blockers are resolved.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of the release, not during planning.

## Acceptance

All earlier milestone acceptance criteria remain green in a release build, documentation matches observable behavior, signed artifacts are available for both supported architectures, and no release blockers remain.

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

## Multi-provider identity

Possible future work includes:

- Discord OAuth2 login as an independent provider
- OIDC-only, Discord-only, and linked accounts
- explicit provider linking and unlinking
- one-use, browser-bound authentication/link flow state

Never merge accounts based only on email. Discord notification delivery does not imply Discord login.

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
