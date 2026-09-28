# LANsurfer Product

## 1. Product Summary

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties and gaming events.

The 1.0 product is deliberately focused on a simple operating model: **one LANsurfer installation serves one LAN party or gaming event context**. LANsurfer is not trying to be a shared multi-community online service in 1.0.

At a LAN event, the problem is usually not that there are too few players in the building. The problem is discovering:

> Who wants to play the same game as me right now?

LANsurfer should answer that question with as little friction as possible. It is not intended to be a complex matchmaking engine. It should feel closer to “Who wants to play this?” than “Find me the mathematically perfect teammate.”

## 2. Core Product Flow

1. A player signs in through generic OIDC or Discord and chooses a public Game Nick.
2. They find or follow Games they are interested in.
3. They create a Game Request.
4. Other relevant players see the request in the browser or through an enabled notification transport.
5. Players join and then start playing together.

The LANsurfer installation itself provides the event context in 1.0. Players do not need to select or join an Event, Clan, Open Play space, or other parent context before using Game Requests.

## 3. Product Principles

### Fast

Creating a request should take only a few seconds. Joining should require very little interaction.

### Obvious

Players should understand the UI without learning LANsurfer-specific terminology.

### Live filtering

Every filter in the UI applies as soon as its value changes. Do not require or provide a separate **Filter** or **Apply filters** button. Search fields are live filters too, but debounce their input before applying it.

A **Clear filters** control is optional rather than a default requirement. Add one only when the implementation request explicitly includes it or after confirming with the requester that the screen should provide it.

### Event focused

LANsurfer 1.0 should work especially well when the people using an installation already share the same physical LAN party or gaming event.

### Low-friction onboarding

An event must not require every attendee to already have an account with the operator's identity provider. Generic OIDC and Discord are independent login choices in stable 1.0.

### Self-hosted and open

LAN parties and clubs should be able to operate their own open-source instance with few required services.

### Private by default

Expose only the identity information needed for players to find one another. Authentication-provider data is account data, not public identity.

### Maintainable

Avoid features and abstractions that require disproportionate technical complexity before they are proven useful. Do not prebuild generic context, rich-profile, branding-editor, localization, or tag-taxonomy infrastructure for hypothetical later needs.

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

### Planned stable 1.0 scope

The stable 1.0 release delivers these capabilities through parallel roadmap tracks:

- a focused single-event operating model in which Game Requests are instance-wide rather than scoped through Event membership
- a unique public Game Nick as the 1.0 player identity
- independent generic OIDC and Discord authentication with explicit account linking
- an authoritative local Game catalog plus an optional synchronized IGDB discovery provider
- request discovery by Game name/aliases, followed/My Games and open capacity, a compact request-based Trending view, and durable request-local activity history
- fixed LANsurfer branding with only the minimum instance identity needed for a recognizable deployment, plus an installable PWA/service-worker shell
- durable Web Push and one instance-level Discord delivery integration without a persistent inbox
- production release artifacts, operational documentation, and a tested upgrade path from a populated earlier `main` revision to 1.0

The 1.0 product UI is English-only. Internationalization/localization is not a stable-1.0 prerequisite.

The authoritative track scope, dependencies, acceptance criteria, Pilot Gate, Integration Gate, and release gates are in the [roadmap](ROADMAP.md#development-model). A planned feature is not implemented merely because it is described here.

### Explicitly deferred beyond 1.0

The following may be useful later but are not stable-1.0 requirements:

- English/German localization or a general localization framework
- curated Game Request quick-info tags and tag-based filtering
- player biographies, per-Game player handles, and richer public profile pages
- a full administrator-managed branding editor, uploaded instance logos/icons, white-labeling, or custom theming

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
- full theme customization or white-label branding
- external catalog providers other than IGDB
- multi-instance/high-availability architecture
- WebSockets, native applications, tournaments, ratings, teams, and federation

## 6. Players and Authentication

LANsurfer owns a local User record. External identities authenticate that user but are not the LANsurfer user ID.

### Public player identity

- Every player must choose a unique, case-insensitive public **Game Nick** after first login. Existing prototype users complete this once after upgrade.
- The Game Nick is the public identity used in Game Request participant lists, request activity, Discord request messages, and other public player references.
- A Discord display name may be offered as an initial suggestion, but LANsurfer owns Game Nick validation and uniqueness.
- Provider name and email are visible only to the account owner, not to other players or global administrators through normal product UI.
- 1.0 does not require biographies, per-Game handles, richer public profile pages, or avatar uploads.

### Avatars

- A player may show one avatar next to the Game Nick. The source is an explicit account choice: one of the player's own linked identities that provides a profile picture (Discord avatar, Twitch or OIDC `picture`), or **Default**.
- Default, and any missing or failed image, renders the local Game Nick initial. No external avatar service is used.
- A new account initially selects the creating identity if it has a picture. Later logins, links, and metadata refreshes never change the choice; unlinking the selected identity falls back to Default.
- Provider avatars are served same-origin through LANsurfer; player browsers never contact Discord, Twitch, or OIDC image hosts. On a cache miss LANsurfer fetches only the selected identity's stored picture server-side, which exposes the server's egress IP address and request timing to that host. Avatar URLs reveal no provider name, subject, username, or email.
- There are no avatar uploads, cropping, or galleries.

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

Post-1.0, Twitch is an optional third authentication provider. Twitch login reuses the same local identity, explicit linking, onboarding, and session model as OIDC and Discord. It requests only a stable Twitch subject and display username, never Twitch email; a Twitch account may be explicitly linked to the same local User as the other providers. Twitch login and IGDB discovery share optional Twitch application credentials but remain independently enabled features.

## 7. Roles and Management Boundaries

Global application roles remain intentionally small:

- `user`
- `admin`

A global administrator manages the local Game catalog, optional provider integration, instance-level integrations, and application-wide operations.

1.0 does not add Event-scoped participant/manager roles, Clan roles, or a generic permission framework.

## 8. Single-Event Instance Model

A 1.0 LANsurfer installation represents one LAN party or gaming event context.

- Players do not select an Event before using the application.
- Players do not join an Event membership before seeing or creating Game Requests.
- There is no Open Play fallback context in 1.0.
- There is no Event switcher, Event invitation model, Event roster, seat assignment, Event lifecycle/history access model, or Event-scoped authorization in 1.0.
- A minimum instance display identity may distinguish the deployment, but 1.0 does not require a branding-management surface.

The prototype contained Event data and Game Request/Event relationships. The forward migration removed the association while preserving every existing Game Request, its creator, members, status, and timestamps; legacy Event rows are kept in the database but are no longer read. Administrators no longer manage Events, and no user needs to understand or satisfy a hidden Event model.

Do not introduce a generic `context`, polymorphic parent, or similar abstraction solely to preserve future flexibility.

## 9. Game Catalog and IGDB Discovery

LANsurfer owns the authoritative local Game catalog. Separate Games may represent different editions or variants when they have different practical player pools.

Core local fields include name, slug, active state, aliases, external identities, and timestamps. Materialized local Games may also carry provider-derived display metadata such as summary, release information, platform names, and a cover reference.

Aliases improve search, for example allowing `WC3` to find Warcraft III. External IDs use a generic provider/external-ID relation and are stable identity links; they do not make the provider authoritative.

### Discovery-first search

Games is a discovery-first surface. Players search one catalog without needing to know whether a result is already a local Game or comes from the synchronized IGDB index. Materialization is not a player-facing status.

Materialized IGDB-backed Games keep last-known-good presentation enrichment (cover, Game Type, platforms, and release date) while local name, slug, active state, and aliases remain authoritative.

Follow and Find Players are independent actions. Creating a Game Request does not automatically follow the Game.

### Local-only operation

IGDB is optional for a general LANsurfer deployment.

- LANsurfer starts and remains usable when IGDB is disabled or unconfigured.
- In local-only mode, player search uses authoritative local LANsurfer Games only.
- Only global administrators may create and manage local Games manually.
- Ordinary signed-in players do not receive free-form local Game creation.
- Disabling IGDB after it was used never deletes already materialized local Games.

A disabled or unconfigured provider is a normal `disabled` state, not an application health failure.

### Synchronized provider index

When configured, LANsurfer synchronizes lightweight IGDB metadata into a local searchable provider index. This index is discovery infrastructure, not the authoritative LANsurfer Game catalog.

- Synchronize lightweight search/classification metadata for all IGDB game records rather than filtering record types during ingestion.
- Keep enough provider data to search and disambiguate results, including provider ID, names/aliases, type/category information, useful relationship signals, platform/release context, and cover reference metadata where applicable.
- Do not mirror the full provider graph or bulk-download cover images; cover bytes are cached on demand only for cover IDs LANsurfer already knows.
- The initial sync starts asynchronously after LANsurfer is available and never blocks application startup.
- Player-facing IGDB results remain unavailable until one complete initial baseline sync succeeds; local Games remain usable while the provider is `not synced` or `syncing`.
- After the baseline, refresh the provider index automatically once per day and provide global administrators a manual **Sync now** action.
- Refresh failures retry independently of normal application traffic. The last successful provider index remains usable while the provider is `stale` or `failed`.
- Operator-visible provider states distinguish at least `disabled`, `not synced`, `syncing`, `current`, `stale`, and `failed`.
- While a synchronization is running, the administrator status page reports separately how many records the in-progress generation has staged so far, refreshing itself without a manual reload, and keeps reporting the last successful baseline's record count unchanged.

Ordinary player search shows by default the useful playable categories: main games, expansions, standalone expansions, remakes, remasters, ports, and expanded games. Noise-heavy categories such as DLC/add-ons, bundles, mods, episodes/seasons, packs, and updates remain synchronized but are hidden from ordinary player search by default. Provider records that IGDB marks as a version/edition of another game (for example a deluxe or collector edition) are hidden as well, while expansions that merely have a parent game stay discoverable. The same policy applies to direct selection of a provider record; already materialized local Games are unaffected.

### Materializing an authoritative Game

Searching or viewing a provider result alone does not create a LANsurfer Game.

Following a provider result or creating a Game Request for it automatically and atomically materializes an authoritative local Game. Any signed-in user may trigger that activation; administrator approval is not required when IGDB is enabled.

Concurrent activation of the same provider record must converge on one local Game through the provider/external-ID identity. LANsurfer never auto-merges by name.

Once materialized, the local Game remains authoritative and usable even if IGDB is later disabled, unavailable, or changes its metadata.

Global administrators may explicitly reconcile the authoritative local catalog:

- link an existing local Game to a record from the active synchronized IGDB catalog without replacing its local ID, name, slug, aliases, follows, or history
- unlink an incorrect IGDB identity and enrichment snapshot without deleting the local Game or its relationships
- merge an explicit source Game into an explicit target Game, keeping the target's local identity while moving Game Requests, follows, aliases, and a non-conflicting IGDB identity
- permanently delete only Games with no Game Requests or follows; deactivation remains the normal lifecycle action for Games with history

LANsurfer never chooses a duplicate automatically by name or silently discards one of two conflicting IGDB identities during a merge.

If an IGDB record disappears during synchronization:

- remove the provider-index record when no authoritative local Game references it
- when a local Game references it, preserve the provider identity as an archived/tombstoned record and exclude it from ordinary new-player provider discovery
- never delete or invalidate local Games, follows, Game Requests, or request activity merely because the provider record disappeared

### Covers and attribution

IGDB attribution and operator setup/terms must be visible in the appropriate UI and documentation. Covers are served same-origin through LANsurfer, so player browsers never contact IGDB directly. On a cache miss LANsurfer may fetch only a known cover ID from the fixed `images.igdb.com` origin server-side and cache the bytes on demand; missing or failed covers keep the local placeholder. Documentation must disclose that the server-side fetch exposes the LANsurfer server's egress IP address, request timing, and requested cover ID to IGDB. The bounded cover proxy is catalog infrastructure, not a generic remote-asset proxy.

## 10. Game Following

A GameSubscription means a player follows a Game. It drives followed lists, in-browser relevance, and the default Web Push audience.

Following remains explicit. Creating or joining a Game Request never implicitly follows a Game; a player follows a Game only through an explicit Follow action.

Followed Games are not a separate screen. Games is one catalog with a **Followed only** filter, so searching and narrowing to followed Games compose on the same page instead of splitting into two lists of the same cards. The retired `/games/mine` address resolves to that filter.

When IGDB is enabled, following a provider-only search result first materializes the corresponding authoritative local Game atomically and then records the subscription.

Per-Game player handles are not part of 1.0.

## 11. Game Requests, Discovery, Trending, and Activity

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
- One player may hold several open requests for the same Game at the same start time, because organizing two separate groups is legitimate. The application therefore does not treat a repeated creation as the same request; accidental double submission is prevented in the interface instead.
- Any signed-in player may discover and use current Game Requests according to the normal application rules; Event membership is not an access prerequisite.

The primary UI should show Game, player count/capacity, start time, short description, and the Join action without forcing the player through a detail page.

### Discovery

Stable 1.0 includes practical discovery without requiring a tag taxonomy:

- search by Game name and aliases
- one **audience scope**, chosen from **All**, **My Games** (Games the player follows), **Joined**, and **Created by me**
- an open-capacity/free-places filter

The scope is a single exclusive choice rather than four combinable toggles, because asking for requests a player created *and* requests for Games they follow at the same time has no useful answer. Free places is a separate toggle, since it narrows capacity inside any scope.

Additional filters beyond these are not automatically part of 1.0 merely because they appear in an issue proposal.

### Trending

1.0 includes a compact request-based Trending view over a rolling **6-hour** window. The UI states the window explicitly, for example `Trending · last 6 hours`.

Trending is derived from local Game Request activity. Search queries are not persisted. Neither search queries nor page views are ranking inputs: the private per-player reading history behind "Recently viewed" (see [Shared navigation](#shared-navigation)) is never aggregated across players and never influences Trending. Quiet periods may therefore produce a sparse or empty Trending view rather than silently expanding the window.

### Durable request-local activity

Each Game Request keeps chronological durable activity for successful:

- create
- join
- leave
- close
- cancel

Activity is separate from current membership and separate from notification delivery history. Failed, rejected, or duplicate mutations do not create activity entries. A player leaving removes current membership but does not erase the historical activity entry. Public actor identity uses Game Nick where relevant.

1.0 does not turn this into generic audit logging, event sourcing, comments/chat, or a global social feed.

### Shared navigation

Every signed-in screen renders the same navigation, so its numbers and lists mean the same thing wherever the player is:

- **Game Requests**, with how many Game Requests are open on the installation. This count is never narrowed by the player's current discovery filter.
- **Games**, with how many Games the local catalog holds, and **Followed** beneath it with how many Games this player follows. Both lead to the same Games screen; Followed applies its "Followed only" filter. A count of zero is omitted rather than rendered.
- **Administration**, for administrators only.
- **Your next games** — up to three Game Requests this player has joined, soonest first. A round whose start time has arrived is marked urgent with the same treatment the request list uses for "now".
- **Recently viewed** — up to five Game Requests this player most recently opened, newest first.

"Recently viewed" is private reading history. It is per-player and never visible to anyone else, it records only that a player opened a Game Request's detail page, and it states what the player read rather than what is still open: a Game Request that closed or was cancelled after they read it stays listed and says so. Re-opening a Game Request moves it back to the top instead of adding a second entry, and a deleted Game Request leaves the list entirely. Opening a Game Request is not an activity entry, is never delivered as a notification, and never reaches the live-update stream.

## 12. Live and Durable Notifications

SSE remains the ephemeral in-browser live-update path, with polling fallback where necessary. It can surface new requests, joins, leaves, and status changes.

The 1.0 durable notification system has Web Push and Discord transports. External delivery is asynchronous and must never fail or roll back a Game Request action. LANsurfer does not provide a persistent in-app inbox.

### Web Push

- A player may register multiple labeled browser/device subscriptions and manage them from the authenticated Account/Profile area.
- Permission is requested only after feature detection and an explicit user gesture. Browser permission, registered devices, LANsurfer enablement, and audience preference remain distinct states.
- Personal Web Push is explicitly disabled by default. A player may pause it without deleting registered devices.
- New Game Requests use two audience modes:
  - **followed/My Games only** — default;
  - **all new Game Requests** — explicit opt-in.
- For a newly created request, notify currently eligible players in the selected audience except the creator.
- Join, leave, close, and cancel activity may also notify current request participants independently of the new-request audience setting. The actor never receives a personal push for their own activity.
- Join/leave notifications may name the actor by public Game Nick. Close/cancel notifications remain actor-neutral.
- Recipient/device sets are not expanded later for old work. Delayed/retried delivery re-checks current eligibility and suppresses a delivery that is no longer permitted rather than sending stale personal notification work.
- When LANsurfer is already visibly open in the browser, the service worker suppresses the duplicate OS notification and SSE remains the in-browser live-update path.
- Push payloads contain only the minimum data needed to render the notification: activity kind, Game, planned time, deep link, minimum instance context, and optional join/leave actor Game Nick. They omit description, roster, provider identity data, and activity history.

### Discord delivery

- Discord notification delivery is separate from Discord authentication and uses independently configurable OAuth credentials.
- One Discord destination may be configured for the LANsurfer instance through Discord's `webhook.incoming` OAuth flow; users do not paste webhooks and LANsurfer does not add a bot/gateway.
- Global administrators connect, reconnect, disconnect, and diagnose the destination.
- LANsurfer maintains one synchronized Discord message per Game Request in the current destination. It creates the message with `wait=true`, stores the returned message ID, and edits that same message as request state changes.
- Discord reflects the current Game Request snapshot rather than replaying request activity. Rapid changes may collapse into one newer edit, and a new live change after reconnect may create the current request's first message in the new destination without replaying older work.
- The message contains the Game, planned start time, current request status, current capacity/free-place summary, the creator's public LANsurfer Game Nick, a short sanitized request description, a deep link back to LANsurfer, and minimal instance context.
- The full participant roster and durable request activity remain in LANsurfer and are not mirrored into Discord.
- Provider identity data such as Discord usernames or email addresses is never used as public identity in the message.
- User-authored descriptions are treated as plain content and Discord mentions are disabled on every create/edit.
- Stored webhook identifiers/tokens are encrypted and never displayed back to an operator. OAuth access/refresh tokens are not retained after successful destination setup.
- Disconnecting or replacing a destination does not delete already-posted Discord messages and never replays historical notification work.

Per-Event Discord destinations and Event-scoped notification preferences are post-1.0 candidates.

## 13. Post-1.0 Social and Multi-Event Features

Multi-Event operation and persistent social/community structures are intentionally outside the 1.0 release.

Possible future work may include:

- scheduled Events and Open Play
- Event membership, managers, invitations, rosters, seats, lifecycle/history, and access boundaries
- Event-scoped Game Requests
- Clans with ownership, membership, invitations, rosters, and primary-Clan display
- Clan-aware Game Request discovery or scoping when a concrete need exists
- authentication providers beyond OIDC, Discord, and Twitch

These features must not shape 1.0 through speculative generic abstractions. If they become scheduled, their concrete domain model should be designed against the actual use case at that time.

## 14. Instance Identity, Branding, and PWA

The product is called **LANsurfer**. Its tagline is:

> Find your crew. Catch the next game.

The fixed visual system uses the canonical LANsurfer design and bundled application assets. See the [Branding guide](BRANDING.md).

Stable 1.0 does **not** require a full administrator-managed branding surface. In particular, 1.0 does not require configurable welcome text, contact link, uploaded logo, uploaded app icon, custom colors, white-labeling, or theme editing.

Operator legal identity is the one configurable exception: legal entity name, postal address, contact email, and an optional external imprint URL are operator configuration, because each deployment is its own data controller. LANsurfer serves a public Terms of Service (`/terms`) and Privacy Policy (`/privacy`) filled from these values, links them and the imprint from the footer of every screen (the contact/reporting details live in the Terms), and states on the login screen that logging in accepts the Terms. Support, abuse, Discord, and data requests go to the contact email; software bugs go to the project's issue tracker. This does not reintroduce welcome text, logos, colors, or free-form legal text as configuration.

A deployment may expose the minimum configurable instance identity needed to distinguish a LANsurfer installation. The application otherwise uses fixed/default LANsurfer branding and bundled assets.

The web app manifest uses a stable application identity and start URL, and the minimal service-worker shell supports installation plus Web Push integration. Installation does not imply offline application support.

The IGDB cover and provider-avatar proxies concern catalog and player content and do not permit remote instance branding or broader theme customization.

## 15. Self-Hosting and Release Experience

The supported production deployment requires only LANsurfer and PostgreSQL as local runtime services. Migrations run before HTTP startup and a migration failure prevents startup. DOWN migrations never run automatically.

The stable 1.0 distribution includes:

- Linux amd64 and arm64 binaries
- a matching multi-architecture GHCR image
- exact-version release artifacts, stable moving tags, checksums, SBOMs, and provenance attestations
- a production Compose example using a pinned application image and a PostgreSQL service not exposed on the host
- version, healthcheck, encryption-key-generation, and Web Push key-generation commands
- configuration, reverse-proxy/TLS, administrator bootstrap, backup/restore, key backup, and upgrade documentation

TLS and the reverse proxy remain operator responsibilities. The project documents each release's supported upgrade path rather than guaranteeing arbitrary direct upgrades across all 1.x versions. Migrations are forward-only, and an upgrade from a populated earlier `main` revision to 1.0 is tested. Feature-branch databases are disposable prototype state, not supported upgrade sources.

The selected LAN pilot has a narrower release-artifact requirement than stable 1.0, but it still requires a production deployment path, clean-host installation, an exercised backup/restore, and at least one exercised upgrade path with existing data. The roadmap's Pilot Gate owns those validation requirements.

## 16. 1.0 Success Criteria

LANsurfer 1.0 succeeds when an operator can reliably run one self-hosted LAN/event instance and players can use it without any shared-service or multi-Event concepts.

Operators can reliably install, configure, back up, restore, and upgrade LANsurfer, and signed-in players can:

1. use a unique public Game Nick without exposing authentication-provider data
2. authenticate through either generic OIDC or Discord, with explicit account linking when desired
3. discover local/provider Games and instance-wide Game Requests without joining or selecting an Event/Clan/context
4. create and join capacity-safe Game Requests from desktop and mobile browsers
5. use My Games/open-capacity discovery, 6-hour request-based Trending, and durable request-local activity
6. receive only the durable notifications they have enabled and are eligible to receive
7. continue core workflows when optional external providers are unavailable

A 1.0 release must not require Event switching, Open Play, Clans, a generic Game Request context framework, rich profiles, a branding editor, localization, or curated request tags.

## 17. Product Decisions Left to Implementation

Layout, copy, pagination, route shape, migration mechanics, exact browser versions used as validation evidence, exact provider-index schema, stale-state thresholds, and validation helpers are implementation details unless the roadmap explicitly promotes them to release criteria. Follow the [approval rules](../AGENTS.md#changes-requiring-approval) for changes to technology, security, module boundaries, deployment, domain terminology, or approved 1.0 scope.
