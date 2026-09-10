# LANsurfer Product

## 1. Product Summary

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties, gaming events, and gaming communities.

At a large LAN event, the problem is usually not that there are too few players in the building. The problem is discovering:

> Who wants to play the same game as me right now?

LANsurfer should answer that question with as little friction as possible. It is not intended to be a complex matchmaking engine. It should feel closer to “Who wants to play this?” than “Find me the mathematically perfect teammate.”

## 2. Core Product Flow

1. A player signs in and chooses a public Game Nick.
2. They join an Event or use Open Play.
3. They follow Games they are interested in.
4. They create a Game Request.
5. Other relevant players see the request in the browser or through an enabled notification transport.
6. Players join and then start playing together.

## 3. Product Principles

### Fast

Creating a request should take only a few seconds. Joining should require very little interaction.

### Obvious

Players should understand the UI without learning LANsurfer-specific terminology.

### Local and community focused

LANsurfer should work especially well when players already share a physical Event or gaming community.

### Self-hosted and open

LAN parties and clubs should be able to operate their own open-source instance with few required services.

### Private by default

Expose only the identity and Event information needed for players to find one another. Authentication-provider data is account data, not a public profile.

### Maintainable

Avoid features that require disproportionate technical complexity before they are proven useful.

## 4. Terminology

LANsurfer branding may use surf-related visuals and marketing language, but product and domain terminology remains simple.

Use:

- Game
- Event
- Open Play
- Game Request
- Player
- Game Nick
- Clan
- Join and Leave
- Follow Game and My Games

Do not use Wave, Crew, Spot, or Space as required product/domain concepts. “Crew” may remain in marketing copy such as the tagline.

Use explicit domain names in code and the database, including `User`, `Identity`, `AuthSession`, `Game`, `GameAlias`, `GameExternalID`, `GameSubscription`, `Event`, `EventMembership`, `GameRequest`, `GameRequestMember`, `Clan`, `ClanMembership`, and notification delivery concepts. Surf metaphors are reserved for branding and marketing copy.

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

### Planned 1.0 scope

The 1.0 release adds, in roadmap order:

- public player profiles, per-Game handles, and a unique Game Nick
- independent generic OIDC and Discord login identities with explicit account linking
- Event membership, invitations, Event management, seats, history, and Open Play
- Clans with ownership, invitations, rosters, and primary-Clan display
- optional administrator-controlled IGDB import and rich Game metadata
- basic per-instance identity and an installable web shell
- durable Web Push and per-Event Discord delivery without a persistent inbox
- production release artifacts, operational documentation, and a tested prototype-to-1.0 upgrade path

The authoritative order and acceptance criteria are in the [roadmap](ROADMAP.md#progress). A planned feature is not implemented merely because it is described here.

### Explicitly deferred beyond 1.0

- persistent in-app notification inbox/history
- generic reports, suspensions, and complex moderation beyond the narrow Clan administrator override
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
- Any signed-in player can view another player's Game Nick, biography, primary and other Clans, followed Games, and Game handles.
- LANsurfer generates initials for profile imagery. Avatar uploads are not included.
- Provider name and email are visible only to the account owner, not to other players, Event Managers, Clan owners, or global administrators through normal product UI.

### Authentication

- Generic OpenID Connect remains supported.
- Discord OAuth2 authorization-code login is an independent provider with a provider-specific callback.
- An account may be OIDC-only, Discord-only, or linked to both.
- Linking and unlinking require an explicit signed-in account flow.
- At least one identity must remain linked.
- Never merge identities or accounts based only on email.
- Authentication and linking flows are short-lived, one-use, and bound to their initiating browser, provider, operation, and signed-in user where applicable.
- LANsurfer stores no local passwords and does not use browser JWT sessions.

Discord authentication and Discord notification delivery are separate integrations.

## 7. Roles and Management Boundaries

Global application roles remain intentionally small:

- `user`
- `admin`

A global administrator manages the catalog, instance identity, Open Play, and application-wide operations. Scheduled Events add participant and manager membership roles. Clans add owner and member roles. These scoped roles do not form a generic permission framework.

## 8. Events and Open Play

An Event is an access, planning, and history boundary for Game Requests.

### Event kinds and lifecycle

- A `scheduled` Event has a name, public description, start, and end.
- The instance has one singleton `open` Event, presented as **Open Play**.
- Scheduled Event state is derived from time as upcoming, active, or ended; do not store an `is_current` switch.
- Upcoming participants may create Game Requests only inside the Event's time window.
- Ended Events are read-only history.
- `GameRequest.EventID` remains required.

When scheduled Events overlap, use the player's remembered selection first, otherwise the active Event ending soonest, otherwise Open Play. Provide an Event switcher.

### Membership and visibility

Event membership stores participant/manager role, notification setting, selection preference, and optional seat reference.

- New users join Open Play with notifications muted by default and may leave, rejoin, mute, or unmute.
- Participants may leave scheduled Events.
- Any signed-in user may see a non-joined scheduled Event's name, schedule, and public description, but not its roster or Game Requests.
- Event Managers edit their scheduled Event and manage its participants, managers, invitations, and notification integration.
- Global administrators can access all Events and are the only managers of Open Play.

### Invitations

Event invitation links are named, reusable, revocable, and stored only as hashes. They may have an earlier expiry and use limit, and never remain valid after a scheduled Event ends.

### Seats

A scheduled Event membership may have an optional free-text seat reference. The participant or an Event Manager may edit it. It is case-insensitively unique within that Event and visible only to Event members. Venue maps and ticketing are not included.

### Removal

Removing a participant immediately denies Event access and begins a durable `removal_pending` cleanup. The Event module asks the Game Request module to cancel that player's owned open requests and remove their joined memberships. Cleanup is retried and audited before membership removal is finalized. Modules do not write one another's tables or share a cross-module transaction.

### Prototype migration

The 1.0 migration preserves all existing Game Requests. Every existing user joins every existing Event to preserve prototype access: global administrators become Event Managers and other users become participants. Every existing user also joins the new Open Play Event with notifications muted.

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

IGDB attribution and operator setup/terms must be visible in the appropriate UI and documentation. Remote covers may load only from the approved `images.igdb.com` host with a fixed Content Security Policy allowlist, `Referrer-Policy: no-referrer`, and a local placeholder. Documentation must disclose that loading a remote cover exposes the viewer's IP address to IGDB. Local cover caching/proxying is deferred.

## 10. Game Following and Handles

A GameSubscription means a player follows a Game. It drives personalized lists, in-browser relevance, and Web Push targeting.

Game handles are separate profile data: a player may record multiple labeled handles for a Game. Following a Game does not require a handle, and publishing a handle does not automatically opt a player into notifications.

## 11. Game Requests

A Game Request means a player is looking for people to play a particular Game at an Event.

Core properties include Event, Game, creator, optional maximum players, start time, description, status, and timestamps. Initial statuses remain `open`, `closed`, and `cancelled`; fullness is derived from member count and maximum capacity.

### Behavior

- The creator automatically becomes the first member.
- Maximum players is optional for games without a useful hard limit.
- Starts-at supports now, a near-future choice, or a specific timezone-aware time.
- The creator closes or cancels their request instead of leaving as an ordinary member; ownership transfer is not included.
- A player cannot join the same request twice.
- Concurrent joins must never overfill a limited request.
- Access and creation obey Event membership and lifecycle rules.
- An ended Event exposes requests only as read-only history to its members.

The primary UI should show Game, player count, start time, short description, and the Join action without forcing the player through a detail page.

## 12. Live and Durable Notifications

SSE remains the ephemeral in-browser live-update path, with polling fallback where necessary. It can surface new requests, joins, leaves, and status changes.

The 1.0 durable notification system has Web Push and Discord transports. External delivery is asynchronous and must never fail or roll back a Game Request action. LANsurfer does not provide a persistent in-app inbox.

### Web Push

- A player may register multiple labeled browser/device subscriptions.
- Permission is requested only after feature detection and an explicit user gesture.
- Global and per-Event toggles control delivery.
- A new-request push is sent only when the recipient follows the Game, joined the unmuted Event, and is not the creator.
- Payloads contain only the Game, Event/Open Play, time, and a deep link. They omit creator identity, description, roster, and member data.

### Discord delivery

- One Discord destination may be configured per Event through Discord's `webhook.incoming` OAuth flow; users do not paste webhooks.
- Event Managers configure scheduled Events; global administrators configure Open Play.
- LANsurfer creates one message per Game Request and edits it as membership or status changes.
- Messages contain outbound links only. Discord commands, bot/gateway connections, and Discord-side writes are excluded.
- User content is sanitized and mentions are disabled.
- Stored webhook credentials are encrypted and never displayed back to an operator.

## 13. Clans

A Clan is a persistent group players can use for identity and discovery.

- A player may join multiple Clans but own at most one.
- Roles are owner and member only.
- Owners manage the Clan, members, and invitations and must transfer ownership before leaving.
- Invitations are named, reusable, revocable, hashed, use-limited, and expire within 30 days.
- A player can choose one primary Clan; its tag appears beside their Game Nick and all memberships appear on their profile.
- Any signed-in user can browse Clan pages and rosters.
- Global administrators have audited safety controls to edit/archive a Clan, remove members, revoke invites, and transfer ownership.

Clans do not include posts, chat, private Clan LFG, or custom roles.

## 14. Instance Identity and Branding

The product is called **LANsurfer**. Its tagline is:

> Find your crew. Catch the next game.

The fixed visual system uses an ocean-blue base, restrained warm semantic accents, Montserrat typography, JetBrains Mono for compact data, equal dark/light presentations, and a guide-like otter mascot. See the [Branding guide](BRANDING.md).

For 1.0, a global administrator may configure a per-instance display name, welcome text, contact link, logo, and application icon. “Powered by LANsurfer” remains visible. This is instance identity, not a theme editor: colors, typography, accessibility behavior, and component semantics remain fixed.

Uploaded identity assets are validated PNG files stored in PostgreSQL:

- logo: at most 2 MiB and 2048×1024 pixels
- icon: square, 192–512 pixels, at most 512 KiB

If an icon is absent, omit it from the manifest and use a generated monogram in the UI. The dynamic web app manifest uses a stable application ID and start URL with standalone display. Installation does not imply offline application support.

The remote IGDB-cover exception concerns catalog content and does not permit remote instance branding or broader theme customization.

## 15. Self-Hosting and Release Experience

The supported production deployment requires only LANsurfer and PostgreSQL. Migrations run before HTTP startup and a migration failure prevents startup. DOWN migrations never run automatically.

The 1.0 distribution includes:

- Linux amd64 and arm64 binaries
- a matching multi-architecture GHCR image
- exact-version release artifacts, stable moving tags, checksums, SBOMs, and provenance attestations
- a production Compose example using a pinned application image and a PostgreSQL service not exposed on the host
- version, healthcheck, encryption-key-generation, and Web Push key-generation commands
- configuration, reverse-proxy/TLS, administrator bootstrap, backup/restore, key backup, and upgrade documentation

TLS and the reverse proxy remain operator responsibilities. The project documents each release's supported upgrade path rather than guaranteeing arbitrary direct upgrades across all 1.x versions. Migrations are forward-only, and the populated prototype-to-1.0 path is tested.

## 16. 1.0 Success Criteria

LANsurfer 1.0 succeeds when operators can reliably install, configure, back up, restore, and upgrade it, and signed-in players can:

1. maintain a public Game Nick and useful profile without exposing provider data
2. authenticate through supported independent providers and explicitly link them
3. discover and join Events, Open Play, Clans, Games, and Game Requests within their access boundaries
4. create and join capacity-safe Game Requests from desktop and mobile browsers
5. receive only the durable notifications they have enabled and are eligible to receive
6. continue core workflows when optional external providers are unavailable

## 17. Product Decisions Left to Implementation

Layout, copy, pagination, route shape, and validation helpers are implementation details. Follow the [approval rules](../AGENTS.md#constraints-requiring-approval) for changes to technology, security, module boundaries, deployment, domain terminology, or approved 1.0 scope.
