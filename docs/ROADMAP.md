# LANsurfer Roadmap

This roadmap carries LANsurfer from its completed web prototype to the 1.0 self-hosting release. Milestones describe product outcomes and architectural boundaries; implementation details remain governed by [PRODUCT.md](PRODUCT.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](../AGENTS.md).

Do not implement later milestones for completeness. Finish and verify the current milestone before advancing the progress marker.

## Progress

- [x] Prototype milestones 0–8 — foundation, authentication, catalog, Events, Game Requests, realtime updates, UX, and hardening
- [ ] **Current:** Milestone 9 — Player Profiles and Multi-Provider Identity
- [ ] Milestone 10 — Event Participation and Open Play
- [ ] Milestone 11 — Clans and Social Discovery
- [ ] Milestone 12 — IGDB Catalog Import and Rich Game Metadata
- [ ] Milestone 13 — Instance Identity and Installable Web Shell
- [ ] Milestone 14 — Durable Notifications, Web Push, and Discord Delivery
- [ ] Milestone 15 — Production Distribution and Operations
- [ ] Milestone 16 — 1.0 RC and Stable Release

There are no date or real-event pilot gates. Each milestone advances when its acceptance criteria and relevant tests are complete.

## Completed prototype (milestones 0–8)

The prototype established:

- a Go modular monolith with PostgreSQL, migrations, sqlc, templ, HTMX, Tailwind, and DaisyUI
- generic OIDC login, local users, revocable PostgreSQL sessions, opaque cookies, and global administrator promotion
- Event and local Game catalog administration, aliases, external-ID storage, and game subscriptions
- Game Request creation, joining, leaving, cancellation, expiration, and transaction-safe capacity enforcement
- SSE-driven live updates with polling fallback
- embedded production assets, responsive desktop/mobile UX, security controls, integration coverage, and self-hosting foundations

The prototype schema and behavior are an input to the 1.0 migration path, not a promise that every prototype interface remains unchanged.

# Milestone 9 — Player Profiles and Multi-Provider Identity

## Goal

Give every player a stable LANsurfer identity while retaining provider-neutral authentication and allowing Discord as an independent login provider.

## Expected outcomes

### Player profile

- Require a unique, case-insensitive public **Game Nick** after first login; upgraded prototype users choose one once.
- Allow a short biography.
- Allow multiple labeled player handles per Game.
- Show Game Nick, biography, primary and other Clans, followed Games, and Game handles on a profile.
- Let any signed-in user view player profiles.
- Generate initials for the profile image; do not add avatar uploads.
- Keep provider identity name and email visible only to the account owner. Event Managers, Clan owners, and global administrators do not receive provider identity details through normal product UI.

### Authentication providers

- Keep generic OIDC authentication.
- Add Discord OAuth2 authorization-code authentication as a separate provider with its own callback.
- Support OIDC-only, Discord-only, and accounts linked to both.
- Link and unlink providers only through an explicit signed-in account flow.
- Require at least one linked identity at all times.
- Never merge accounts based only on email.
- Persist short-lived, one-use authentication/link flow state that binds the initiating browser, provider, operation, and signed-in user where applicable.

## Acceptance

A new or upgraded user cannot enter the application until they have chosen a valid Game Nick. Profiles and per-Game handles are usable without exposing provider data. OIDC and Discord accounts can be used independently or explicitly linked without implicit email merging.

# Milestone 10 — Event Participation and Open Play

## Goal

Make Events explicit participation and access boundaries while retaining an always-available place for informal play.

## Expected outcomes

### Event model and lifecycle

- Add explicit Event kinds: `scheduled` and the singleton `open` Event.
- Call the open Event **Open Play** in the UI. “Space” is not product or domain terminology.
- Derive scheduled Event state from time as upcoming, active, or ended; do not store an `is_current` flag.
- Keep `GameRequest.EventID` required.
- Allow participants to plan requests for an upcoming Event only within its time window.
- Make ended Events read-only history.
- When scheduled Events overlap, select the remembered Event first; otherwise select the active Event ending soonest; otherwise select Open Play.
- Provide an Event switcher.

### Membership and access

- Store Event membership with participant/manager role, notification setting, selection preference, and optional seat reference.
- Join new users to Open Play with notifications muted by default; allow them to leave, rejoin, mute, and unmute.
- Allow participants to leave scheduled Events themselves.
- Allow any signed-in user to view only calendar information for Events they have not joined: name, schedule, and public description. Hide rosters and Game Requests.
- Give Event Managers control of their scheduled Event, its participants, managers, and invitations.
- Give global administrators access to every Event and sole management of Open Play.

### Invitations and seats

- Use named, reusable, revocable invitation links stored as hashes.
- Support an earlier expiry and use limit; an invitation never remains valid after its scheduled Event ends.
- Let participants or Event Managers set an optional free-text seat reference.
- Enforce case-insensitive seat-reference uniqueness within a scheduled Event.
- Show seats only to Event members. Do not add venue maps or ticketing.

### Removal workflow

- Removing a member immediately denies Event access and starts a durable `removal_pending` workflow.
- The Event module durably asks the Game Request module to cancel open requests owned by the removed user and remove their joined memberships.
- Retry failures, record an audit trail, and finalize removal only when cleanup completes.
- Preserve module ownership: no cross-module table writes and no shared cross-module transaction.

### Prototype migration

- Preserve all existing Game Requests and their Event associations.
- Add every existing user to every existing Event so existing global access is not silently lost; global administrators become Event Managers and other users become participants.
- Create Open Play and join every existing user with notifications muted.

## Acceptance

Event access, request visibility, invitation use, seats, lifecycle rules, overlap selection, and concurrent removal cleanup behave consistently and are covered by domain and PostgreSQL tests.

# Milestone 11 — Clans and Social Discovery

## Goal

Let players represent persistent social groups without turning LANsurfer into a chat or content platform.

## Expected outcomes

- Add a separate `clan` business module.
- Allow multiple Clan memberships but only one Clan owned by a player.
- Use only owner and member roles.
- Let the owner edit the Clan, manage members and invitations, and transfer ownership.
- Require ownership transfer before an owner can leave.
- Use named, reusable, revocable, hashed invitations with use limits and a maximum 30-day expiry.
- Let a player select one primary Clan and show its tag beside their Game Nick; list all memberships on the player profile.
- Let all signed-in users browse Clan pages and rosters.
- Give global administrators audited safety controls to edit or archive a Clan, remove members, revoke invites, or transfer ownership.
- Do not add Clan posts, chat, private Clan LFG, or custom roles.

## Acceptance

Clan ownership and primary-Clan invariants hold under concurrent changes, invitations cannot be recovered from storage, and ordinary players can discover Clans without receiving moderation powers or private content features.

# Milestone 12 — IGDB Catalog Import and Rich Game Metadata

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

# Milestone 13 — Instance Identity and Installable Web Shell

## Goal

Let an operator identify their LANsurfer installation while retaining the product's fixed, accessible visual system and making the site installable as a web app.

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

# Milestone 14 — Durable Notifications, Web Push, and Discord Delivery

## Goal

Deliver relevant Game Request changes beyond the open browser without coupling external transport failures to core user actions.

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
- Provide global and per-Event push toggles.
- Send a new-request push only when its Game is followed, the recipient joined the unmuted Event, and the recipient is not the creator.
- Limit push content to Game, Event/Open Play, time, and a deep link. Do not include creator identity, description, roster, or other member information.

### Discord delivery

- Keep Discord delivery separate from Discord login.
- Configure one Discord destination per Event through Discord's `webhook.incoming` OAuth flow; do not accept pasted webhooks or add a bot/gateway connection.
- Let Event Managers configure their scheduled Events and global administrators configure Open Play.
- Encrypt webhook IDs and tokens at rest with AES-GCM using an operator-provided 32-byte data-encryption key; never reveal a stored secret.
- Create one Discord message per Game Request with `wait=true`, store its message ID, and edit it when membership or status changes.
- Provide outbound links only; Discord commands and Discord-side writes are out of scope.
- Sanitize user content, send no allowed mentions, and honor Discord rate-limit headers.
- Provide authorized integration status, test, and manual-retry controls.

## Acceptance

Outbox persistence survives restarts, retries are bounded and observable, push targeting and payload privacy are tested, Discord messages remain synchronized, and external outages never prevent Game Request actions.

# Milestone 15 — Production Distribution and Operations

## Goal

Make LANsurfer reproducibly installable, upgradable, backed up, and diagnosable by self-hosting operators.

## Expected outcomes

- Publish official Linux amd64 and arm64 binaries and a matching multi-architecture GHCR image.
- Add `lansurfer version`, healthcheck, encryption-key generation, and Web Push key-generation commands.
- Tag stable artifacts by exact version, major.minor, major, and `latest`; tag prereleases by exact version only.
- Publish checksums, software bills of materials, and provenance attestations.
- Use native Go tooling plus pinned official Docker and GitHub Actions; do not add GoReleaser.
- Provide a production Compose example with a pinned LANsurfer image, PostgreSQL not exposed on the host, and only the application and database as required services.
- Keep TLS and reverse-proxy operation as operator responsibilities and document safe example configuration.
- Document every configuration value, administrator bootstrap, backup/restore, encryption and VAPID key backup, upgrades, and rollback limitations.
- Keep migrations forward-only and test upgrading a populated prototype schema to the 1.0 schema.
- Document the supported upgrade path for each release. Do not promise arbitrary direct upgrades across all 1.x releases.

## Acceptance

Release artifacts are reproducible and verifiable, the documented Compose deployment starts from an empty host, backup/restore is exercised, and a populated prototype installation upgrades without data loss.

# Milestone 16 — 1.0 RC and Stable Release

## Goal

Freeze the approved 1.0 feature set, validate the complete supported deployment and upgrade paths, and publish a stable release.

## Expected outcomes

- Publish `v1.0.0-rc.1` with complete migration, operations, security, privacy, and provider documentation.
- After the release candidate, accept only release blockers, security fixes, migration fixes, and documentation corrections.
- Exercise OIDC-only, Discord-only, and linked-account authentication.
- Exercise Event membership/removal, Clan ownership, catalog-provider failure, notification retries, backup/restore, and prototype-to-1.0 upgrade paths.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Publish `v1.0.0` when release-candidate blockers are resolved.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of the release, not during planning.

## Acceptance

All earlier milestone acceptance criteria remain green in a release build, documentation matches observable behavior, signed artifacts are available for both supported architectures, and no release blockers remain.

# Explicitly deferred beyond 1.0

The following must not influence current implementation unless the roadmap is explicitly changed:

- a persistent in-app notification inbox
- generic reports, suspensions, or a complex moderation system beyond the narrow audited Clan administrator override
- Clan posts, chat, private Clan LFG, or custom roles
- Game Request waitlists, ready checks, or comments
- guest accounts, passkeys, or account self-deletion
- full theme customization
- catalog providers other than IGDB
- local caching or proxying of remote covers
- multi-instance or high-availability application architecture
- WebSockets, native applications, tournaments, ratings, teams, or federation
