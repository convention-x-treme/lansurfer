# LANsurfer Product

## 1. Product Summary

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties and gaming events.

The 1.0 product is deliberately focused on a simple operating model: **one LANsurfer installation serves one LAN party or gaming event context**. LANsurfer is not trying to be a shared multi-community online service in 1.0.

At a LAN event, the problem is usually not that there are too few players in the building. The problem is discovering:

> Who wants to play the same game as me right now?

LANsurfer should answer that question with as little friction as possible. It is not intended to be a complex matchmaking engine. It should feel closer to “Who wants to play this?” than “Find me the mathematically perfect teammate.”

## 2. Core Product Flow

1. A player signs in through generic OIDC or Discord and chooses a public Game Nick.
2. They follow Games they are interested in.
3. They create a Game Request.
4. Other relevant players see the request in the browser or through an enabled notification transport.
5. Players join and then start playing together.

The LANsurfer installation itself provides the event context in 1.0. Players do not need to select or join an Event, Clan, Open Play space, or other parent context before using Game Requests.

## 3. Product Principles

### Fast

Creating a request should take only a few seconds. Joining should require very little interaction.

### Obvious

Players should understand the UI without learning LANsurfer-specific terminology.

### Event focused

LANsurfer 1.0 should work especially well when the people using an installation already share the same physical LAN party or gaming event.

### Low-friction onboarding

An event must not require every attendee to already have an account with the operator's identity provider. Generic OIDC and Discord are independent login choices in 1.0.

### Self-hosted and open

LAN parties and clubs should be able to operate their own open-source instance with few required services.

### Private by default

Expose only the identity information needed for players to find one another. Authentication-provider data is account data, not a public profile.

### Maintainable

Avoid features and abstractions that require disproportionate technical complexity before they are proven useful. Do not prebuild generic context or shared-service infrastructure for hypothetical post-1.0 features.

## 4. Terminology

LANsurfer branding may use surf-related visuals and marketing language, but product and domain terminology remains simple.

Use in 1.0:

- Game
- Game Request
- Player
- Game Nick
- Join and Leave
- Follow Game and My Games

Do not use Wave, Crew, Spot, Space, or a generic Context as required product/domain concepts. “Crew” may remain in marketing copy such as the tagline.

Use explicit domain names in code and the database. Surf metaphors are reserved for branding and marketing copy.

Event and Clan terminology may be introduced later only when concrete post-1.0 product work is scheduled.

## 5. Release Scope

### Completed prototype baseline

The web-only prototype implemented:

- generic OIDC login and local users
- global user/administrator roles and revocable PostgreSQL sessions
- Event and local Game administration
- Game aliases, external-ID storage, search, and subscriptions
- Game Requests with concurrency-safe joining, leaving, cancellation, and expiration
- server-side rendered templ/HTMX pages and SSE live updates
- responsive UI feedback, embedded production assets, migrations, and Compose-based development deployment

The prototype is an input to the 1.0 migration path. Prototype relationships and interfaces are not automatically part of the final 1.0 product model.

### Planned 1.0 scope

The 1.0 release delivers these capabilities through parallel roadmap tracks:

- a focused single-event operating model in which Game Requests are instance-wide rather than scoped through Event membership
- public player profiles, per-Game handles, and a unique Game Nick
- independent generic OIDC and Discord authentication with explicit account linking
- optional administrator-controlled IGDB import and rich Game metadata
- basic per-instance identity and an installable web shell
- durable Web Push and one instance-level Discord delivery integration without a persistent inbox
- production release artifacts, operational documentation, and a tested prototype-to-1.0 upgrade path

The authoritative track scope, dependencies, acceptance criteria, Integration Gate, and release gates are in the [roadmap](ROADMAP.md#development-model). A planned feature is not implemented merely because it is described here.

### Post-1.0 candidates — not scheduled

These are possible directions, not commitments to a particular release:

- multiple Events in one installation, Event membership, invitations, managers, seats, lifecycle/history, and Open Play
- Game Requests scoped to an Event, Clan, or another concrete parent when a real use case justifies it
- Clans, Clan ownership, rosters, invitations, and primary-Clan identity
- additional authentication providers beyond OIDC and Discord
- per-Event notification settings or multiple Discord destinations

Do not create generic abstractions in 1.0 merely to make these possibilities easier later.

### Explicitly not planned

- a generic polymorphic Game Request context/parent framework
- persistent in-app notification inbox/history
- generic reports, suspensions, and complex moderation
- Clan posts, chat, private Clan LFG, and custom roles
- Game Request waitlists, ready checks, and comments
- guest accounts, passkeys, and account self-deletion
- full theme customization
- external catalog providers other than IGDB
- local caching or proxying of remote covers
- multi-instance/high-availability architecture
- WebSockets, native applications, tournaments, ratings, teams, and federation

## 6. Players, Profiles, and Authentication

LANsurfer owns a local User record. External identities authenticate that user but are not the LANsurfer user ID.

### Public player identity

- Every player must choose a unique, case-insensitive Game Nick after first login. Existing prototype users complete this once after upgrade.
- A profile can contain a short biography and multiple labeled handles per Game.
- Any signed-in player can view another player's Game Nick, biography, followed Games, and Game handles.
- LANsurfer generates initials for profile imagery. Avatar uploads are not included.
- Provider name and email are visible only to the account owner, not to other players or global administrators through normal product UI.

### Authentication

- Generic OpenID Connect remains provider-neutral.
- Discord OAuth2 authorization-code login is a separate supported provider with its own callback.
- An account may be OIDC-only, Discord-only, or explicitly linked to both.
- Linking and unlinking require an explicit signed-in account flow.
- At least one identity must remain linked.
- Never merge identities or accounts based only on email.
- Authentication and linking flows are short-lived, one-use, and bound to their initiating browser, provider, operation, and signed-in user where applicable.
- LANsurfer stores no local passwords and does not use browser JWT sessions.
- Local user IDs, explicit revocable PostgreSQL sessions, and opaque cookies remain the account/session model.

Discord authentication and Discord notification delivery are separate integrations.

## 7. Roles and Management Boundaries

Global application roles remain intentionally small:

- `user`
- `admin`

A global administrator manages the Game catalog, instance identity, integrations, and application-wide operations.

1.0 does not add Event-scoped participant/manager roles, Clan roles, or a generic permission framework.

## 8. Single-Event Instance Model

A 1.0 LANsurfer installation represents one LAN party or gaming event context.

- Players do not select an Event before using the application.
- Players do not join an Event membership before seeing or creating Game Requests.
- There is no Open Play fallback context in 1.0.
- There is no Event switcher, Event invitation model, Event roster, seat assignment, Event lifecycle/history access model, or Event-scoped authorization in 1.0.
- The configured instance name and welcome text may identify the LAN party or event being served.

The prototype contains Event data and Game Request/Event relationships. The 1.0 migration must preserve existing Game Requests while making Event selection/membership irrelevant to normal Game Request behavior. The exact persistence migration is an architectural decision; the product requirement is that a user never needs to understand or satisfy a hidden Event model to use 1.0.

Do not introduce a generic `context`, polymorphic parent, or similar abstraction solely to preserve future flexibility.

## 9. Game Catalog

LANsurfer owns a local authoritative Game catalog. Separate Games may represent different editions when they have different practical player pools.

Core local fields include name, slug, active state, aliases, and timestamps. The 1.0 catalog can also hold a summary, first release date, platform names, and one cover reference.

Aliases improve search, for example allowing `WC3` to find Warcraft III. External IDs use a generic provider/external-ID relation but do not make the provider authoritative.

### Administration

Global administrators can list, add, edit, activate/deactivate, and manage aliases for Games.

With optional IGDB/Twitch credentials configured, they can:

- search IGDB and preview a result
- create a local Game from the result or explicitly attach its external ID to an existing Game
- preview later provider changes and choose fields individually before refresh

LANsurfer never auto-merges Games by name or silently overwrites local edits. If IGDB is unavailable or unconfigured, local catalog search/editing and Game Requests continue to work.

IGDB attribution and operator setup/terms must be visible in the appropriate UI and documentation. Remote covers may load only from the approved `images.igdb.com` host with a fixed Content Security Policy allowlist, `Referrer-Policy: no-referrer`, and a local placeholder. Documentation must disclose that loading a remote cover exposes the viewer's IP address to IGDB. Local cover caching/proxying is not planned for 1.0.

## 10. Game Following and Handles

A GameSubscription means a player follows a Game. It drives personalized lists, in-browser relevance, and Web Push targeting.

Game handles are separate profile data: a player may record multiple labeled handles for a Game. Following a Game does not require a handle, and publishing a handle does not automatically opt a player into notifications.

## 11. Game Requests

A Game Request means a player is looking for people to play a particular Game on the LANsurfer instance.

In 1.0, the installation itself provides the event context. A Game Request is not presented as belonging to a selectable Event, Clan, Open Play area, or generic parent context.

Core properties include Game, creator, optional maximum players, start time, description, status, and timestamps. Initial statuses remain `open`, `closed`, and `cancelled`; fullness is derived from member count and maximum capacity.

### Behavior

- The creator automatically becomes the first member.
- Maximum players is optional for games without a useful hard limit.
- Starts-at supports now, a near-future choice, or a specific timezone-aware time.
- The creator closes or cancels their request instead of leaving as an ordinary member; ownership transfer is not included.
- A player cannot join the same request twice.
- Concurrent joins must never overfill a limited request.
- Any signed-in player may discover and use current Game Requests according to the normal application rules; Event membership is not an access prerequisite.

The primary UI should show Game, player count, start time, short description, and the Join action without forcing the player through a detail page.

## 12. Live and Durable Notifications

SSE remains the ephemeral in-browser live-update path, with polling fallback where necessary. It can surface new requests, joins, leaves, and status changes.

The 1.0 durable notification system has Web Push and Discord transports. External delivery is asynchronous and must never fail or roll back a Game Request action. LANsurfer does not provide a persistent in-app inbox.

### Web Push

- A player may register multiple labeled browser/device subscriptions.
- Permission is requested only after feature detection and an explicit user gesture.
- A global toggle controls delivery; 1.0 does not add per-Event notification settings.
- A new-request push is sent only when the recipient follows the Game, push is enabled, and the recipient is not the creator.
- Payloads contain only the Game, time, and a deep link. The configured instance display name may provide event context. They omit creator identity, description, roster, and member data.

### Discord delivery

- Discord notification delivery is separate from Discord authentication.
- One Discord destination may be configured for the LANsurfer instance through Discord's `webhook.incoming` OAuth flow; users do not paste webhooks.
- Global administrators configure the destination.
- LANsurfer creates one message per Game Request and edits it as membership or status changes.
- Messages contain outbound links only. Discord commands, bot/gateway connections, and Discord-side writes are excluded.
- User content is sanitized and mentions are disabled.
- Stored webhook credentials are encrypted and never displayed back to an operator.

Per-Event Discord destinations and Event-scoped notification preferences are post-1.0 candidates.

## 13. Post-1.0 Social and Multi-Event Features

Multi-Event operation and persistent social/community structures are intentionally outside the 1.0 release.

Possible future work may include:

- scheduled Events and Open Play
- Event membership, managers, invitations, rosters, seats, lifecycle/history, and access boundaries
- Event-scoped Game Requests
- Clans with ownership, membership, invitations, rosters, and primary-Clan display
- Clan-aware Game Request discovery or scoping when a concrete need exists
- authentication providers beyond OIDC and Discord

These features must not shape 1.0 through speculative generic abstractions. If they become scheduled, their concrete domain model should be designed against the actual use case at that time.

## 14. Instance Identity and Branding

The product is called **LANsurfer**. Its tagline is:

> Find your crew. Catch the next game.

The fixed visual system uses an ocean-blue base, restrained warm semantic accents, Montserrat typography, JetBrains Mono for compact data, equal dark/light presentations, and a guide-like otter mascot. See the [Branding guide](BRANDING.md).

For 1.0, a global administrator may configure a per-instance display name, welcome text, contact link, logo, and application icon. These fields can identify the specific LAN party or gaming event served by the installation. “Powered by LANsurfer” remains visible.

This is instance identity, not a theme editor: colors, typography, accessibility behavior, and component semantics remain fixed.

Uploaded identity assets are validated PNG files stored in PostgreSQL:

- logo: at most 2 MiB and 2048×1024 pixels
- icon: square, 192–512 pixels, at most 512 KiB

If an icon is absent, omit it from the manifest and use a generated monogram in the UI. The dynamic web app manifest uses a stable application ID and start URL with standalone display. Installation does not imply offline application support.

The remote IGDB-cover exception concerns catalog content and does not permit remote instance branding or broader theme customization.

## 15. Self-Hosting and Release Experience

The supported production deployment requires only LANsurfer and PostgreSQL as local runtime services. Migrations run before HTTP startup and a migration failure prevents startup. DOWN migrations never run automatically.

The 1.0 distribution includes:

- Linux amd64 and arm64 binaries
- a matching multi-architecture GHCR image
- exact-version release artifacts, stable moving tags, checksums, SBOMs, and provenance attestations
- a production Compose example using a pinned application image and a PostgreSQL service not exposed on the host
- version, healthcheck, encryption-key-generation, and Web Push key-generation commands
- configuration, reverse-proxy/TLS, administrator bootstrap, backup/restore, key backup, and upgrade documentation

TLS and the reverse proxy remain operator responsibilities. The project documents each release's supported upgrade path rather than guaranteeing arbitrary direct upgrades across all 1.x versions. Migrations are forward-only, and the populated prototype-to-1.0 path is tested.

## 16. 1.0 Success Criteria

LANsurfer 1.0 succeeds when an operator can reliably run one self-hosted LAN/event instance and players can use it without any shared-service or multi-Event concepts.

Operators can reliably install, configure, back up, restore, and upgrade LANsurfer, and signed-in players can:

1. maintain a public Game Nick and useful profile without exposing provider data
2. authenticate through either generic OIDC or Discord, with explicit account linking when desired
3. discover Games and instance-wide Game Requests without joining or selecting an Event/Clan/context
4. create and join capacity-safe Game Requests from desktop and mobile browsers
5. receive only the durable notifications they have enabled and are eligible to receive
6. continue core workflows when optional external providers are unavailable

A 1.0 release must not require Event switching, Open Play, Clans, or a generic Game Request context framework.

## 17. Product Decisions Left to Implementation

Layout, copy, pagination, route shape, migration mechanics, and validation helpers are implementation details. Follow the [approval rules](../AGENTS.md#constraints-requiring-approval) for changes to technology, security, module boundaries, deployment, domain terminology, or approved 1.0 scope.
