# LANsurfer Product

This document is the durable product contract: what LANsurfer is, how it behaves for players, administrators, and operators, and which boundaries are part of the product. It is written in the present tense and carries no schedule, priority, implementation status, or release-gate evidence. Software-development planning lives in GitHub Issues and milestones; a proposal there becomes part of this contract only when an approved change updates this document.

Related authorities: [ARCHITECTURE.md](ARCHITECTURE.md) for technical decisions, [TERMINOLOGY.md](TERMINOLOGY.md) for vocabulary, and [DESIGN.md](../DESIGN.md) for the design system.

## 1. Product Summary

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties and gaming events.

LANsurfer is deliberately focused on a simple operating model: **one LANsurfer installation serves one LAN party or gaming event context**. The current product is not a shared multi-community online service.

At a LAN event, the problem is usually not that there are too few players in the building. The problem is discovering:

> Who wants to play the same game as me right now?

LANsurfer should answer that question with as little friction as possible. It is not intended to be a complex matchmaking engine. It should feel closer to “Who wants to play this?” than “Find me the mathematically perfect teammate.”

## 2. Core Product Flow

1. A player signs in through a configured provider (generic OIDC, Discord, or Twitch) and chooses a public Game Nick.
2. They find or follow Games they are interested in.
3. They create a Game Request.
4. Other relevant players see the request in the browser or through an enabled notification transport.
5. Players join and then start playing together.

Following remains an explicit action on the Games surface. The Game Request detail page does not expose Follow Game. Viewing, creating, or joining a Game Request never follows its Game implicitly.

The LANsurfer installation itself provides the event context. Players do not need to select or join an Event, Clan, Open Play space, or other parent context before using Game Requests.

## 3. Product Principles

### Fast

Creating a request should take only a few seconds. Joining should require very little interaction.

### Obvious

Players should understand the UI without learning LANsurfer-specific terminology.

### Live filtering

Every filter in the UI applies as soon as its value changes. Do not require or provide a separate **Filter** or **Apply filters** button. Search fields are live filters too, but debounce their input before applying it.

A **Clear filters** control is optional rather than a default requirement. Add one only when the implementation request explicitly includes it or after confirming with the requester that the screen should provide it.

### Event focused

LANsurfer should work especially well when the people using an installation already share the same physical LAN party or gaming event.

### Low-friction onboarding

An event must not require every attendee to already have an account with the operator's identity provider. Generic OIDC, Discord, and Twitch are independent login choices.

### Self-hosted and open

LAN parties and clubs should be able to operate their own open-source instance with few required services.

### Private by default

Expose only the identity information needed for players to find one another. Authentication-provider data is account data, not public identity.

### Maintainable

Avoid features and abstractions that require disproportionate technical complexity before they are proven useful. Do not prebuild generic context, rich-profile, branding-editor, localization, or tag-taxonomy infrastructure for hypothetical later needs.

## 4. Terminology

Product and domain language stays plain: Game, Game Request, Player, Game Nick, Join and Leave, Follow Game and My Games. Surf metaphors are reserved for branding and marketing copy such as the tagline; players never need a surf vocabulary to operate LANsurfer.

[TERMINOLOGY.md](TERMINOLOGY.md) owns the canonical vocabulary, including the terms that must not be used. Event and Clan are not current product concepts (see [Product Boundaries](#5-product-boundaries)).

## 5. Product Boundaries

LANsurfer is deliberately small. The following are not part of the product. They are current product behavior, not a backlog: none of them is scheduled or promised by being named here.

Instance and community model:

- multiple Events in one installation, Event membership, invitations, managers, seats, venue maps, ticket claims, Event lifecycle/history, and Open Play
- Game Requests scoped to an Event, Clan, or any other parent, and a generic polymorphic Game Request context/parent framework
- Clans, Clan ownership, rosters, invitations, primary-Clan identity, Clan posts, Clan chat, private Clan LFG, and custom roles
- tournaments, ratings, teams, and federation
- multi-instance or high-availability operation

Players and accounts:

- guest accounts, passkeys, and local passwords
- authentication providers beyond generic OIDC, Discord, and Twitch
- player biographies, per-Game player handles, richer public profile pages, and avatar uploads
- generic reports, suspensions, and complex moderation

Game Requests and notifications:

- waitlists, ready checks, a separate ready-to-play state, and comments apart from the request-local Lobby chat
- curated quick-info tags and tag-based filtering
- a persistent in-app notification inbox or history
- per-Event notification settings or more than one Discord destination

Presentation and platform:

- localization: the product UI is English-only and has no localization framework
- an administrator-managed branding editor, uploaded instance logos or icons, white-labeling, or custom theming
- external catalog providers other than IGDB
- WebSockets and native applications

Do not create generic abstractions merely to make any of these easier later. A proposal to change a boundary is raised as a GitHub Issue and changes the product only through an approved update to this document.

## 6. Players and Authentication

LANsurfer owns a local User record. External identities authenticate that user but are not the LANsurfer user ID.

### Landing page

A logged-out `GET /` shows a public landing page instead of the bare login screen: one promise ("Find your crew. Catch the next game."), up to four Games from the same six-hour Trending ranking as the Game Requests rail, a dashed "Any game" tile, and one **Log in** action. Each Game tile states how many Game Requests are `open` for it and how many distinct players are members of those requests. Signed-in visitors are redirected to onboarding or Game Requests as before; the login screen stays at `/auth/login`.

This makes Game names and aggregate counts visible without an account. It exposes no player identity: no Game Nicks, avatars, request details, or rosters. Tiles lead through login and then to the matching Game Request search, or to the composer for "Any game". The copy is fixed product copy, not operator configuration.

A visitor who is sent to login from a page, or who clicks a landing tile, returns to that page afterwards. The return target is a validated local path only; anything else falls back to Game Requests.

### First-time guidance

- A brand-new account that has just chosen its Game Nick sees one **Welcome** screen explaining the core flow in three steps (follow your games, find players, play together), with **Choose my games** and **Skip and browse Game Requests**. It is not a wizard and stores no state. A player who signed in from a specific page returns there instead, and an existing user who claims a missing Game Nick skips it.
- A public **How it works** page (`/help`) repeats the three steps and briefly explains Game Requests, following a Game, and Join/Leave. Signed-in players reach it from the navigation.
- Games shows a one-line hint about following while the player follows no Game. The composer explains that other players can join until the player has created a first Game Request, and the description field suggests an example. These hints never block anything; there is no tutorial or tooltip framework.

### Public player identity

- Every player must choose a unique, case-insensitive public **Game Nick** after first login. An existing account without a Game Nick chooses one at its next sign-in.
- The Game Nick is the public identity used in Game Request participant lists, request activity, Discord request messages, and other public player references.
- A Discord display name may be offered as an initial suggestion, but LANsurfer owns Game Nick validation and uniqueness.
- Provider name and email are visible only to the account owner, not to other players or global administrators through normal product UI.
- Biographies, per-Game handles, richer public profile pages, and avatar uploads are outside the current product model.

### Avatars

- A player may show one avatar next to the Game Nick. The source is an explicit account choice: one of the player's own linked identities that provides a profile picture (Discord avatar, Twitch or OIDC `picture`), or **Default**.
- Default, and any missing or failed image, renders the local Game Nick initial. No external avatar service is used.
- A new account initially selects the creating identity if it has a picture. Later logins, links, and metadata refreshes never change the choice; unlinking the selected identity falls back to Default.
- Provider avatars are served same-origin through LANsurfer; player browsers never contact Discord, Twitch, or OIDC image hosts. On a cache miss LANsurfer fetches only the selected identity's stored picture server-side, which exposes the server's egress IP address and request timing to that host. Avatar URLs reveal no provider name, subject, username, or email.
- There are no avatar uploads, cropping, or galleries.

### Authentication

- Generic OpenID Connect remains provider-neutral.
- Discord OAuth2 authorization-code login is a separate supported provider with its own callback.
- An account may use a single provider identity or be explicitly linked to several.
- Linking and unlinking require an explicit signed-in account flow.
- At least one identity must remain linked.
- Never merge identities or accounts based only on email.
- Authentication and linking flows are short-lived, one-use, and bound to their initiating browser, provider, operation, and signed-in user where applicable.
- LANsurfer stores no local passwords and does not use browser JWT sessions.
- Local user IDs, explicit revocable PostgreSQL sessions, and opaque cookies remain the account/session model.

Discord authentication and Discord notification delivery are separate integrations.

Twitch is an optional third authentication provider. Twitch login reuses the same local identity, explicit linking, onboarding, and session model as OIDC and Discord. It requests only a stable Twitch subject and display username, never Twitch email; a Twitch account may be explicitly linked to the same local User as the other providers. Twitch login and IGDB discovery share optional Twitch application credentials but remain independently enabled features.

### Account deletion

- A signed-in player can permanently delete their own account from the **Danger zone** on the account page. No re-authentication is required; the player must retype their current Game Nick exactly before the final action is available.
- Deletion removes the local account and everything attributable to it: linked identities, sessions, notification devices, followed games, memberships, Game Requests the player created (for every player), and the activity the player caused. No "Deleted user" placeholder is kept.
- The Game Nick is released immediately.
- The only administrator cannot delete their account; another administrator must exist first.
- Discord messages already posted for deleted Game Requests are not edited or removed.

## 7. Roles and Management Boundaries

Global application roles remain intentionally small:

- `user`
- `admin`

A global administrator manages the local Game catalog, optional provider integration, instance-level integrations, and application-wide operations.

The current product model does not include Event-scoped participant/manager roles, Clan roles, or a generic permission framework.

### Operator metrics

Administrators may generate or rotate exactly one active, metrics-only Metrics API Key
at **Administration → Metrics**. The full key is shown only in its generation response;
only a secure hash is stored. `GET /metrics` requires `Authorization: Bearer ...`,
remains inaccessible before generation, and does not accept browser-session access.
Rotation invalidates the old credential for new requests immediately.

The Prometheus-compatible endpoint exposes aggregate HTTP/runtime/process/database
health, current user/session/follow/request counts, and durable create/join/leave/
cancel/close/start totals. The explicit first `open → playing` transition is the
games-started signal. Reporting totals survive restarts and account deletion without
retaining any attributable activity or identity; upgrades can only reconstruct
retained history and therefore provide historical lower bounds. No PII, arbitrary
content, session-information secrets, or identifiers are exported. Collection
failures must not disrupt player workflows. There is no built-in metrics dashboard,
generic API-key subsystem, or additional required runtime service.

## 8. Single-Event Instance Model

A LANsurfer installation represents one LAN party or gaming event context.

- Players do not select an Event before using the application.
- Players do not join an Event membership before seeing or creating Game Requests.
- There is no Open Play fallback context.
- There is no Event switcher, Event invitation model, Event roster, seat assignment, Event lifecycle/history access model, or Event-scoped authorization.
- A minimum instance display identity may distinguish the deployment, but LANsurfer does not provide a branding-management surface.

Game Requests have no Event association. Legacy Event rows from earlier versions may remain in the database but are never read. Administrators do not manage Events, and no user needs to understand or satisfy a hidden Event model.

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

Per-Game player handles are outside the current product model.

## 11. Game Requests, Discovery, Trending, and Activity

A Game Request means a player is looking for people to play a particular Game on the LANsurfer instance.

The installation itself provides the event context. A Game Request is not presented as belonging to a selectable Event, Clan, Open Play area, or generic parent context.

Core properties include Game, creator/Host, optional maximum players, planned start time, actual start time, description, status, and timestamps. Lifecycle statuses are `open`, `playing`, `closed`, and `cancelled`; fullness is derived from member count and maximum capacity.

### Behavior

- The creator automatically becomes the first member.
- Maximum players is optional for games without a useful hard limit.
- Starts-at supports now, a near-future choice, or a specific timezone-aware time.
- The creator is the Host. They may cancel an unstarted `open` request, explicitly start it into `playing`, and close a started `playing` request when the session finishes; ownership transfer is not included.
- A player cannot join the same request twice.
- Concurrent joins must never overfill a limited request.
- One player may hold several open requests for the same Game at the same start time, because organizing two separate groups is legitimate. The application therefore does not treat a repeated creation as the same request; accidental double submission is prevented in the interface instead.
- Any signed-in player may discover and use current Game Requests according to the normal application rules; Event membership is not an access prerequisite.

### Coordination and playing

The explicit Host action **Start game** is the authoritative signal that a session actually started. It transitions `open -> playing` and records a durable actual start time. The planned start time remains planning information and does not gate this action. A Host may start before the planned time or when no other player has joined.

A `playing` request remains active and visible in normal discovery and shared navigation until it is closed. The lifecycle is intentionally strict:

- `open -> playing -> closed`
- `open -> cancelled`

A started request is not cancelled; it is closed when finished. Existing historical `closed` requests that predate the explicit start signal do not gain an invented actual start time.

Time presentation follows lifecycle state. Open requests show planned time and may use relative countdown and urgency. Playing requests show a valid actual start time when available; otherwise, they show no time. Playing requests never show planned-time countdown or urgency. Closed requests show actual start when recorded; legacy closed requests and cancelled requests show planned time as history. Historical time uses explicit labels so it cannot read as a future start.

Coordination continues while a request is `playing`:

- players may join or leave while capacity permits; the Host remains a member and cannot leave;
- the Host may change the maximum player count while `open` or `playing`, but never below current participation (see [Capacity](#capacity));
- the Host maintains generic key/value session information; only the Host and currently joined players read it while the request is `open` or `playing`;
- terminal requests do not expose session information; leaving removes future read access;
- session-information CRUD is not activity or notification behavior;
- secret entries are hidden by default, clearly marked, copyable while masked with generic success or failure feedback, and temporarily revealed for no more than 20 seconds; copying never reveals a value or resets its reveal timer;
- editing a secret pre-fills a password-style field; the Host may toggle secret presentation while editing;
- secret values use the same Host/current-participant audience and server-side authorization as other session information; leaving removes future access, rejoining an active request restores it, and terminal requests expose no session information;
- authorized server-rendered HTML may contain plaintext; reveal state stays client-only and resets on fragment or full-detail refresh;
- secret values remain plaintext in PostgreSQL with no application-level encryption or separate secrets store; they must not appear in public/discovery views, logs, metrics, notification payloads, Discord synchronization, or unrelated diagnostic surfaces;
- session-information realtime delivery rechecks current participant authorization and carries no entry content; secret values come only from the independently authorized read;
- secret sharing outside a Game Request, per-entry access lists, secret history/versioning, and key-management UI are out of scope;
- the Host and currently joined participants may use a lightweight request-local private chat while the request is active.

The Game Request detail page presents participant chat and durable Game Request activity together in the **Lobby**. Current members may read Lobby history in every lifecycle state; leaving removes read access immediately. Nonmembers cannot read it. Chat posting remains limited to current members while the request is `open` or `playing`; terminal Lobbies are read-only. The Lobby shows the latest 200 combined entries in chronological order. Historical backfill appears as history but is never delivered as notification.

Secret presentation protects against shoulder-surfing, not database access, server compromise, or an authorized participant inspecting browser tools.
Request-local chat is not a general messaging subsystem: there are no direct messages, global rooms, threads, reactions, attachments, read receipts, typing indicators, or presence.

The primary UI should show Game, player count/capacity, start time, short description, and the Join action without forcing the player through a detail page. While a request is `playing`, the list and detail page keep the same Join, Leave, and Full states as an `open` one.

### Editing a Game Request

The Host may edit mutable coordination details from the Game Request detail page without recreating the request; discovery/list rows remain read-only. Enhanced mode uses a native responsive dialog: a centered modal on desktop and a bottom sheet on mobile. Without JavaScript, the existing inline `?edit=1` fallback remains.

- While `open`, the Host may edit **Max players**, the planned start time, and description.
- While `playing`, the Host may edit **Max players** and description, but the planned start time is no longer editable.
- `closed` and `cancelled` requests are not editable.
- The Game, Host, lifecycle state, and authoritative actual start time (`started_at`) are never editable through this flow.
- One Save applies all changed details atomically. Saving a semantically identical state is a no-op: no request write, activity entry, live update, or Discord synchronization is produced.
- A successful edit creates one durable request-update activity regardless of how many editable fields changed. Capacity and planned-start changes retain typed before/after values; a description change records only that the description changed, not copies of the old/new text.
- Request edits refresh the synchronized Discord message. A planned-start change also sends a personal Web Push to currently eligible request participants except the Host; capacity- and description-only edits do not. Edits do not influence Trending.

### Capacity

A Game Request's maximum players is optional. A request without one is **unlimited** while it is active.

- A request is joinable by a player who is not a member while it is `open` or `playing` and, if it has a maximum, has fewer members than that maximum. Nothing else controls joinability: there is no separate "accepting players" setting. Join and Leave are rejected on `closed` and `cancelled` requests.
- When the maximum is reached, further joins are refused. When a member leaves, the place is free again; raising or removing the maximum frees places immediately.
- Only the Host changes the maximum, through the detail page's **Edit** flow, while the request is `open` or `playing`. The discovery list shows capacity but offers no editor.
- The Host may set a finite maximum, raise or lower it, or remove it to return to unlimited. A finite maximum can never be lower than the current number of members.
- Entering `0` or leaving the field blank means unlimited. A finite maximum is a whole number from `2` to `999`, because the Host is the first member. A request created before this rule that holds a maximum of `1` keeps it, but any later edit must be unlimited or `2` or more.
- Capacity participates in the shared request-edit no-op rule: leaving it unchanged does not by itself create activity, a live update, or a Discord update.

### Discovery

Discovery includes practical filtering without requiring a tag taxonomy:

- search by Game name and aliases
- one **audience scope**, chosen from **All**, **My Games** (Games the player follows), **Joined**, and **Created by me**
- an open-capacity/free-places filter

The scope is a single exclusive choice rather than four combinable toggles, because asking for requests a player created *and* requests for Games they follow at the same time has no useful answer. Free places is a separate toggle, since it narrows capacity inside any scope.

Additional filters are not part of the product merely because they appear in an issue proposal.

### Trending

LANsurfer includes a compact request-based Trending view over a rolling **6-hour** window. The UI states the window explicitly, for example `Trending · last 6 hours`.

Trending is derived from local Game Request activity. Search queries are not persisted. Neither search queries nor page views are ranking inputs: the private per-player reading history behind "Recently viewed" (see [Shared navigation](#shared-navigation)) is never aggregated across players and never influences Trending. A Host's request edit is configuration, not popularity, and does not count toward Trending. Quiet periods may therefore produce a sparse or empty Trending view rather than silently expanding the window.

### Durable request-local activity

Each Game Request keeps chronological durable activity for successful:

- create
- join
- leave
- start
- close
- cancel
- request update, recording typed previous/new capacity when capacity changed, typed previous/new planned start when it changed, and only a description-changed marker rather than old/new description text

Activity is separate from current membership and separate from notification delivery history. Failed, rejected, or duplicate mutations do not create activity entries. A player leaving removes current membership but does not erase the historical activity entry. Public actor identity uses Game Nick where relevant.

This does not turn activity into generic audit logging, event sourcing, comments/chat, or a global social feed.

### Shared navigation

Every signed-in screen renders the same navigation, so its numbers and lists mean the same thing wherever the player is:

- **Game Requests**, with how many Game Requests are active (`open` or `playing`) on the installation. This count is never narrowed by the player's current discovery filter.
- **Games**, with how many Games the local catalog holds, and **Followed** beneath it with how many Games this player follows. Both lead to the same Games screen; Followed applies its "Followed only" filter. A count of zero is omitted rather than rendered.
- **How it works**, the public help page.
- **Administration**, for administrators only.
- **Your active games** — up to three active Game Requests this player has joined, soonest planned start first. Open requests show planned-start urgency. Playing requests show actual start time without countdown or urgency.
- **Recently viewed** — up to five Game Requests this player most recently opened, newest first.

"Recently viewed" is private reading history. It is per-player and never visible to anyone else, it records only that a player opened a Game Request's detail page, and it states what the player read rather than what is still open: a Game Request that closed or was cancelled after they read it stays listed and says so. Re-opening a Game Request moves it back to the top instead of adding a second entry, and a deleted Game Request leaves the list entirely. Opening a Game Request is not an activity entry, is never delivered as a notification, and never reaches the live-update stream.

## 12. Live and Durable Notifications

SSE remains the ephemeral in-browser live-update path, with polling fallback where necessary. It can surface new requests, joins, leaves, request-detail updates, and status changes.

The durable notification system has Web Push and Discord transports. External delivery is asynchronous and must never fail or roll back a Game Request action. LANsurfer does not provide a persistent in-app inbox.

### Web Push

- A player may register multiple labeled browser/device subscriptions and manage them from the authenticated Account/Profile area.
- Permission is requested only after feature detection and an explicit user gesture. Browser permission, registered devices, LANsurfer enablement, and audience preference remain distinct states.
- Personal Web Push is explicitly disabled by default. A player may pause it without deleting registered devices.
- New Game Requests use two audience modes:
  - **followed/My Games only** — default;
  - **all new Game Requests** — explicit opt-in.
- For a newly created request, notify currently eligible players in the selected audience except the creator.
- Join, leave, start, close, and cancel activity may also notify current request participants independently of the new-request audience setting. The actor never receives a personal push for their own activity.
- Join/leave notifications may name the actor by public Game Nick. Start/close/cancel notifications remain actor-neutral.
- A planned-start edit sends a minimal personal push only when the request remains open at the newly saved time; the Host actor is excluded. Capacity- and description-only edits send no personal push.
- For an open request, LANsurfer creates at most one reminder per request/planned-time pair when that time enters the next 15 minutes. Only currently eligible participants receive it. Returning to a previously reminded time does not repeat it; obsolete reminders are suppressed.
- Recipient/device sets are not expanded later for old work. Delayed/retried delivery re-checks current eligibility and suppresses a delivery that is no longer permitted rather than sending stale personal notification work.
- When LANsurfer is already visibly open in the browser, the service worker suppresses the duplicate OS notification and SSE remains the in-browser live-update path.
- Version 4 activity payloads contain only the minimum data needed to render a notification: activity kind, Game, lifecycle status, planned time, optional actual start time, same-origin deep link, minimum instance context, current player count/capacity, and optional creator or join/leave actor Game Nick. Separate time-only payloads identify a planned-start change or reminder and contain no roster, description, or identity data. The worker labels times as planned or actual from lifecycle state, omits unavailable times, and drops reminders received after their planned time. A `playing` notification remains valid without an actual start time and then shows status without a time.

### Discord delivery

- Discord notification delivery is separate from Discord authentication and uses independently configurable OAuth credentials.
- One Discord destination may be configured for the LANsurfer instance through Discord's `webhook.incoming` OAuth flow; users do not paste webhooks and LANsurfer does not add a bot/gateway.
- Global administrators connect, reconnect, disconnect, and diagnose the destination.
- LANsurfer maintains one synchronized Discord message per Game Request in the current destination. It creates the message with `wait=true`, stores the returned message ID, and edits that same message as request state changes.
- Discord reflects the current Game Request snapshot rather than replaying request activity. Rapid changes may collapse into one newer edit, and a new live change after reconnect may create the current request's first message in the new destination without replaying older work.
- The message uses the Game Requests list title with creator Game Nick and Game. It includes `Planned start` for open/cancelled and legacy closed requests, or `Started at` when an actual start exists. Playing without valid actual start omits the field. It includes status with a color-coded embed accent, current capacity/free-place summary, a short sanitized request description, an explicit `Open in browser ↗` link below the metadata, and minimal instance context. The optional provider Game cover appears as a Discord thumbnail linked directly from IGDB. The web UI keeps covers same-origin; a local base URL still prevents Discord users from opening the deep link.
- The full participant roster and durable request activity remain in LANsurfer and are not mirrored into Discord. A Host request edit updates the same synchronized message with the current capacity, planned time, and description snapshot and posts no separate message.
- Provider identity data such as Discord usernames or email addresses is never used as public identity in the message.
- User-authored descriptions are treated as plain content and Discord mentions are disabled on every create/edit.
- Stored webhook identifiers/tokens are encrypted and never displayed back to an operator. OAuth access/refresh tokens are not retained after successful destination setup.
- Disconnecting or replacing a destination does not delete already-posted Discord messages and never replays historical notification work.

Per-Event Discord destinations and Event-scoped notification preferences are not part of the product.

## 13. Instance Identity, Branding, and PWA

The product is called **LANsurfer**. Its tagline is:

> Find your crew. Catch the next game.

The fixed visual system uses the canonical LANsurfer design and bundled application assets. [DESIGN.md](../DESIGN.md) owns that design system; this section owns what a deployment may and may not change.

LANsurfer does **not** provide a full administrator-managed branding surface. In particular, the current product model does not include configurable welcome text, contact link, uploaded logo, uploaded app icon, custom colors, fonts, CSS, white-labeling, or theme editing.

Operator legal identity is the one configurable exception: legal entity name, postal address, contact email, and an optional external imprint URL are operator configuration, because each deployment is its own data controller. LANsurfer serves a public Terms of Service (`/terms`) and Privacy Policy (`/privacy`) filled from these values, links them and the imprint from the footer of every screen (the contact/reporting details live in the Terms), and states on the login screen that logging in accepts the Terms. Support, abuse, Discord, and data requests go to the contact email; software bugs go to the project's issue tracker. This does not reintroduce welcome text, logos, colors, or free-form legal text as configuration.

A deployment may expose the minimum configurable instance identity needed to distinguish a LANsurfer installation. The application otherwise uses fixed/default LANsurfer branding and bundled assets. Minimum instance identity is not Event-specific and does not grow into a second branding system.

The **Powered by Convention-X-Treme** operator attribution is fixed and always visible. It is not an instance-identity field and is not administrator-editable.

Dark and light presentations are equally supported, and neither loses information or actions. Theme choice is a per-browser presentation preference, not instance branding: it is remembered in a preference cookie and rendered into the document, so a navigation never paints the other theme first.

The web app manifest uses a stable application identity and start URL, and the minimal service-worker shell supports installation plus Web Push integration. Installation does not imply offline application support.

The IGDB cover and provider-avatar proxies concern catalog and player content and do not permit remote instance branding or broader theme customization.

## 14. Self-Hosting and Release Experience

The supported production deployment requires only LANsurfer and PostgreSQL as local runtime services. Migrations run before HTTP startup and a migration failure prevents startup. DOWN migrations never run automatically.

A release distribution includes:

- Linux amd64 and arm64 binaries
- a matching multi-architecture GHCR image
- exact-version release artifacts, stable moving tags, checksums, SBOMs, and provenance attestations
- a production Compose example using a pinned application image and a PostgreSQL service not exposed on the host
- version, healthcheck, encryption-key-generation, and Web Push key-generation commands
- configuration, reverse-proxy/TLS, administrator bootstrap, backup/restore, key backup, and upgrade documentation

TLS and the reverse proxy remain operator responsibilities. The project documents each release's supported upgrade path rather than guaranteeing arbitrary direct upgrades across all 1.x versions. Migrations are forward-only. Feature-branch databases are disposable prototype state, not supported upgrade sources.

## 15. Product Decisions Left to Implementation

Layout, copy, pagination, route shape, migration mechanics, exact browser versions used as validation evidence, exact provider-index schema, stale-state thresholds, and validation helpers are implementation details unless an approved product-scope change makes them product requirements. GitHub Issues may schedule development work or validation but do not silently redefine product scope. Follow the [approval rules](../AGENTS.md#changes-requiring-approval) for changes to technology, security, module boundaries, deployment, domain terminology, or approved product scope.
