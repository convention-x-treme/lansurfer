# LANsurfer Architecture

## 1. Overview

LANsurfer is a self-hosted web application for finding people to play games with at LAN parties and gaming events.

The application is a **modular Go monolith** with server-side rendering, PostgreSQL, and minimal runtime infrastructure.

Primary goals:

- easy self-hosting
- low operational overhead
- long-term maintainability
- clear business-module boundaries
- strong PostgreSQL consistency
- progressive enhancement instead of SPA complexity
- a small number of runtime components

The implemented prototype and planned 1.0 release use the same primary runtime architecture:

```text
Browser
   |
   | HTTP / HTMX
   v
LANsurfer Go application
   |
   +-- chi HTTP routing
   +-- templ SSR
   +-- SSE
   +-- application modules
   |
   v
PostgreSQL
```

The normal deployment contains exactly two primary runtime services:

```text
LANsurfer
PostgreSQL
```

No Redis, queue, separate frontend runtime, or object storage is required.

This document records both the implemented prototype baseline and the approved 1.0 target. The [roadmap development model](ROADMAP.md#development-model) defines which 1.0 tracks may proceed in parallel, the Pilot Gate, and where integration dependencies apply. Target descriptions do not authorize post-1.0 work.

---

## 2. Technology Decisions

### Backend

- Go
- chi

### SSR / frontend

- templ
- HTMX
- Tailwind CSS
- DaisyUI
- Phosphor Icons (Regular + Fill webfonts)
- TypeScript for limited browser-specific behavior
- esbuild
- `go:embed`

### Realtime

- Server-Sent Events

### Persistence

- PostgreSQL
- pgx
- sqlc
- goose

### Tooling

- Taskfile
- Docker
- Docker Compose

---

## 3. Why Server-Side Rendering

LANsurfer does not require a SPA for its primary workflows.

Typical operations are:

- list/search games
- follow/unfollow a game
- create a game request
- join/leave a request
- close/cancel a request
- update account settings
- perform focused administrator operations

The preferred flow is:

```text
HTMX request
   -> Go handler
   -> service/query
   -> PostgreSQL
   -> templ component
   -> HTML fragment
```

This avoids maintaining duplicate frontend/backend models, a frontend router, client-side state architecture, an internal JSON API used only by the same application, and duplicated validation/business rules.

TypeScript is intentionally limited to progressive enhancement.

---

## 4. Realtime and Durable Change Delivery

SSE is used for realtime browser updates. Browser-to-server communication remains ordinary HTTP.

For the prototype and single-instance 1.0 release, the SSE broker may run in memory. Durable external notifications use PostgreSQL persistence and do not make SSE durable.

```text
Game Request transaction
  ├── domain write
  └── game_request_activity row when applicable
          |
          v
Track E delivery state / notification worker
  ├── Web Push adapter
  └── Discord webhook adapter
```

Game Request writes and the corresponding live `game_request_activity` row must be transactionally consistent. That row remains the single Track A → E notification-intent handoff as well as request-local history. A failed or rolled-back mutation leaves neither the domain change nor a deliverable activity row.

Track E consumes that handoff asynchronously and owns consumer-side durable state:

- one `notification_work` record per consumed live activity;
- zero or more `notification_delivery` records for concrete external targets;
- durable uniqueness so repeated discovery cannot duplicate work;
- a first-consumer baseline that marks already-visible live history as non-deliverable instead of using a timestamp or global sequence high-watermark;
- no delivery for `origin=backfill`.

`game_request_activity.sequence` identifies an activity row and orders activity within one Game Request; it is not a global replay cursor. PostgreSQL durable leases/row locking coordinate due delivery work. The 1.0 worker polls PostgreSQL rather than adding Redis, a broker, or LISTEN/NOTIFY.

Delivery state distinguishes retryable failure, permanent failure, exhaustion, ambiguous/uncertain provider acceptance, and intentional suppression when present-day eligibility no longer permits an already-materialized delivery. Uncertain delivery is never blindly retried because the external effect may already have occurred. Authorized manual retry opens a fresh retry round and re-checks current eligibility.

LANsurfer's own retry schedule uses capped exponential backoff with jitter and a six-attempt automatic round. Its computed backoff never exceeds five minutes. An explicit provider `Retry-After` is authoritative and is honored in full even when it exceeds that cap. External transport failure never fails or reverses the originating Game Request action.

The event publishing abstraction should remain replaceable so PostgreSQL `LISTEN/NOTIFY` can be considered later if multiple application instances are ever approved. Multi-instance/high-availability operation is not part of 1.0.

WebSockets are not part of the initial architecture. Introducing them requires approval and a meaningful bidirectional use case.

### Web Push

Web Push uses VAPID behind a narrow adapter around the approved `github.com/marknefedov/go-webpush/v2` dependency and extends Track D's single existing service worker with push/click behavior rather than registering another worker.

Store multiple labeled subscriptions per user. Browser permission, active subscription/device state, LANsurfer enablement, and new-request audience preference remain separate. New Game Requests use followed/My Games by default or all new requests through explicit opt-in. Join/leave/close/cancel notify current request participants independently of that creation-audience setting. The activity actor is excluded from their own personal push.

The positive recipient/device set is materialized once when work is first processed. Delayed/retried sends re-check current eligibility and may terminate an existing delivery as `suppressed`, but old work never gains newly eligible users or devices.

Push payloads are versioned, structured, and privacy-minimized: activity kind, Game, planned time, same-origin deep link, minimum instance context, and optional public Game Nick for join/leave. Do not include request description, roster, provider identity data, or activity history. The service worker suppresses the OS notification while a same-origin LANsurfer window is visible so SSE remains the in-browser path.

Treat PushSubscription endpoint/key material as secret-adjacent application data: encrypt endpoint, `p256dh`, and `auth` with AES-GCM using the Track E data-encryption key, never log or display decrypted material, and use a keyed endpoint fingerprint for equality/uniqueness lookup without storing the raw endpoint alongside ciphertext. A separate long-lived opaque browser-device cookie is stored only as a server-side hash and is independent of the authentication-session lifetime.

### Discord delivery

Discord delivery uses independently configurable OAuth credentials and the `webhook.incoming` scope rather than Discord authentication credentials, pasted URLs, or a bot/gateway. Setup is a short-lived one-use administrator OAuth flow; access/refresh tokens are discarded after the created webhook is durably stored.

Encrypt both webhook ID and token together as a versioned AES-GCM credential blob using the Track E data-encryption key. Safe destination metadata such as guild/channel IDs, display name, connection state, and diagnostic timestamps may remain queryable; never reconstruct or display the webhook URL or secret credentials.

Create one Discord message per Game Request with `wait=true`, retain the returned message ID, and edit that same message from the current Game Request snapshot as state changes. Mutations for one destination + Game Request are serialized so an older external mutation cannot overwrite a newer state. Rapid activity may be coalesced and obsolete intermediate deliveries may become `superseded`; Discord is not an activity feed.

The public message may contain:

- Game
- planned start time
- current request status
- current capacity/free-place summary
- creator's public LANsurfer Game Nick
- short sanitized/plain-content request description
- deep link to LANsurfer
- minimum instance context

Do not mirror the full participant roster or durable activity history. Provider identity names/emails are not public identity. Send an empty `allowed_mentions` policy on every create and edit.

A definitive missing/deleted webhook breaks the destination and requires administrator reconnect. A missing message with an otherwise valid webhook is recovered by creating a replacement message from the current request snapshot. Ambiguous initial message creation may become `uncertain` because automatic replay could create a duplicate; edits to a known message ID are replay-safe for the same current snapshot and use normal retry behavior.

---

## 5. Modular Monolith

Business modules through the prototype are:

```text
auth
user
game
gamerequest
```

The approved 1.0 target adds:

```text
notification
```

The prototype `event` module has been removed: Track A's forward migration dropped the Game Request/Event association, so no 1.0 behavior depends on Event selection, membership, lifecycle, or authorization. `clan` is post-1.0 and must not be introduced until that work is explicitly scheduled.

Supporting technical areas include:

```text
web
query
platform
app
```

A likely structure is:

```text
cmd/
└── lansurfer/
    └── main.go

internal/
├── app/
├── auth/
├── user/
├── game/
├── gamerequest/
├── notification/  # Track E
├── query/
├── web/
└── platform/
    ├── postgres/
    ├── sse/
    ├── config/
    └── logging/
```

This is guidance rather than a mandatory exact tree. Do not create empty architecture packages solely to match the diagram.

---

## 6. Dependency Direction

Business modules must not know concrete infrastructure implementations.

Conceptually:

```text
             web
              |
              v
        gamerequest
        /          \
       v            v
Repository      EventPublisher
   ^                ^
   |                |
postgres            sse
```

Interfaces should generally live where they are consumed. Concrete PostgreSQL, provider, and transport adapters stay behind narrow interfaces. Wire them explicitly in the composition root.

---

## 7. Module Ownership

Each module owns its writes.

### `user`

Owns:

- users
- internal LANsurfer roles
- unique public Game Nick

Rich biographies, per-Game player handles, and richer public profiles are not stable-1.0 requirements.

### `auth`

Owns:

- external identities
- authentication flows
- browser sessions
- one-use provider authentication/link flows

### `game`

Owns:

- authoritative local Games
- aliases
- provider/external identities attached to authoritative Games
- Game subscriptions
- administrator local-catalog behavior
- atomic materialization/activation of a provider result into one authoritative local Game

The synchronized provider index is discovery infrastructure. It must not become a second authoritative domain model or permit provider synchronization code to write Game Request state.

### Legacy Event data

Migration `00006_single_event_core` drops `game_requests.event_id` and its index; every prototype Game Request keeps its id, creator, members, status, description, and timestamps. The legacy `events` table is deliberately left in place with its rows: no application code reads it, no row references it, and destroying operator data is not something a 1.0 upgrade promised. A later release may remove the table once operators have had a chance to export it. Do not reintroduce an `event` module, Event selection, or Event membership.

### `gamerequest`

Owns:

- Game Requests
- request membership
- request lifecycle
- capacity rules
- durable request-local activity for successful create/join/leave/close/cancel changes
- the transactional notification-intent record Track E consumes
- per-player reading history of Game Requests

One table, `game_request_activity`, currently serves both of those responsibilities. Every successful change writes its entry inside the same transaction as the change itself, so a rolled-back mutation leaves neither the change nor a notification intent, and a committed one survives a restart before delivery. A second table would duplicate the same write for the same guarantee. Track E1's implementation confirmed this design: the `notification` module consumes this table asynchronously and keeps its own `notification_work`/`notification_delivery` state, with no second write into `gamerequest`'s tables or transactions. Splitting the notification record out remains the expected change only if a future track demonstrates delivery-specific state that does not belong here.

Two rules govern how a consumer reads it:

- **`sequence` identifies a row and orders one Game Request's history. It is not a global replay cursor.** The value is assigned when the row is inserted, not when its transaction commits, and creating a Game Request takes no shared lock, so two concurrent changes can commit in the opposite order to their sequence values. A consumer holding a single high-watermark would permanently skip the row that committed late. Ordering within one request is safe because join, leave, close, and cancel serialize on that request's row lock and `created` is always first — which is also the only ordering delivery needs, since stale-update protection is per request. A consumer therefore keeps durable per-row delivery state keyed by `sequence` as an opaque identifier, rather than advancing one cursor.
- **`origin` separates recorded activity from reconstructed activity.** Rows written while the application is running are `live`. Rows the `00006` migration reconstructed from prototype membership and status are `backfill`: they describe activity that predates the record and must never be delivered. A consumer starting for the first time on an installation that already has `live` history is responsible for its own baseline, so an upgrade does not deliver the archive.

Reading history lives in its own table, `game_request_views`, and is deliberately not `game_request_activity`. Activity is domain history and Track E's notification-intent handoff; opening a detail page is neither, so a view must never be delivered, must never reach the realtime stream, and must never enter the activity table. It is also not transactional with any Game Request change: the write happens while serving a successful read, and losing it costs a navigation entry rather than corrupting a Game Request. One row per (player, request) with the timestamp updated on re-reading keeps the table a set of distinct requests, so it needs no pruning to stay short, and the foreign key to `game_requests` removes the history with the request.

Request-local activity is not event sourcing, generic audit logging, chat, or notification delivery history.

### `notification` (Track E)

Owns:

- `notification_work`: one durable row per consumed live `game_request_activity` row, snapshotting the fields (`game_request_id`, `actor_id`, `kind`, `occurred_at`) delivery processing and diagnostics need, so neither has to join back into `gamerequest`'s tables afterward.
- `notification_delivery`: zero or more rows per work item, one per external target that was eligible for that activity at materialization time. Owns delivery status (`pending`, `claimed`, `succeeded`, `permanent`, `exhausted`, `uncertain`, `suppressed`), attempts, retry scheduling, and (in later tracks) transport-specific external references.
- Web Push subscriptions and audience preferences (`notification_push_subscription`, `notification_push_preference`), and the single instance-level Discord destination (Track E3, not yet implemented).

The notification module consumes durable Game Request outbox work asynchronously; it does not own or write Game Requests, and Track A never writes into `notification`'s tables. Discovery uses a `NOT EXISTS` anti-join against `game_request_activity.sequence` plus a unique constraint on `notification_work.activity_sequence`, not a high-watermark cursor, so a row committed late is never silently skipped. A one-time, idempotent baseline pass materializes all pre-existing `origin=live` activity as never-deliverable "baseline" work before the worker starts; `origin=backfill` rows are excluded from discovery entirely. Delivery claiming uses `FOR UPDATE SKIP LOCKED` with a 60-second lease so a crashed worker's claim becomes eligible again automatically, with no in-process mutex and no additional required runtime service. The worker re-checks target eligibility against the owning transport before every send, including retries, and marks a delivery `suppressed` — a terminal, never-automatically-retried state retained for diagnostics — rather than sending once a target that was eligible at materialization time no longer is.

E1 exposes authorized diagnostics and manual retry as `lansurfer admin notifications problems|retry|retry-uncertain` — a CLI surface, not an HTTP page, since Track G owns any future in-app presentation and no real transport exists yet to show anything transport-specific.

E2's foundation slice is implemented: `notification_push_subscription` persists one row per registered PushSubscription (endpoint, `p256dh`, `auth`) and `notification_push_preference` persists each user's new-request audience choice (followed/My Games vs. all). Subscription secrets are encrypted at rest with AES-256-GCM via `internal/platform/crypto`, keyed by a fingerprint used for uniqueness/ownership lookup rather than the plaintext endpoint. The `/account/notifications` page lets a signed-in user enable/disable push on the current device, set their audience preference, and remove other registered devices, wired through logout so the current browser's subscription is revoked on sign-out. This is Web Push *foundation* only: no code path in this slice has sent a push message. Recipient materialization (turning a `notification_work` row into eligible subscriptions) and the transport adapter that actually calls the Push service are a separate, not-yet-written follow-up. E2 delivery itself remains not yet implemented.

E1 (the durable foundation) and E2's subscription/preference foundation are implemented. E2's delivery (recipient materialization, transport, send) and E3 (Discord delivery) are not yet implemented; see `docs/ROADMAP.md`.

A module must not directly update another module's tables merely because they are in the same database.

---

## 8. Cross-Module and Cross-Track Communication

When a module needs another module's behavior, prefer a small explicit interface. Cross-module joins are allowed for read-only query models, not business writes.

The main 1.0 cross-track contracts are:

- **Track A → Track E:** Track A owns Game Request write transactions and outbox creation; Track E consumes durable outbox records. Final persistence belongs inside Track A's transaction boundary.
- **Track B internal:** OIDC and Discord identities resolve to the same local User. Game Nick onboarding is provider-independent and happens after authentication when required.
- **Track B → A/E presentation:** public identity consumers use Game Nick rather than provider names/emails. This dependency is for final rendering only, not for core request writes or notification worker construction.
- **Track D → Track E:** Track D owns service-worker registration/lifecycle. Track E owns push permission, subscriptions, push handling, and payload behavior. Backend Web Push work does not need to wait for Track D.
- **Track F:** release automation can proceed independently; final migration/backup/restore validation waits for persistent migrations from the feature tracks to settle.

Do not solve these contracts by sharing persistence implementations or creating generic cross-track frameworks.

---

## 9. Read Models / CQRS-light

Dedicated read models may join multiple module tables for UI purposes, for example Game Request + Game + creator Game Nick + member count + current-user state.

Rules:

- query/read models may read multiple module tables
- they do not perform business writes
- they return UI/read-specific models
- they are not domain entities

Trending is a focused read model derived from Game Request activity over the product-defined rolling six-hour window. It does not authorize raw search-query tracking or a general analytics event store.

The shared navigation is assembled once per request by a single builder in the web layer, so its counts and lists mean the same thing on every screen rather than being populated only by the screen that owns each number. Every read behind it is decoration around a page that has already succeeded: a failing count or list is logged and degrades to an absent number or an empty section, never to an error page replacing the screen the player asked for. `game_request_views` is read per viewer only; aggregating it across players would make it the analytics store this section rules out, and it is explicitly not a Trending ranking input.

Game Request list/detail read models may join the local `game_igdb_enrichment` last-known-good snapshot to render presentation (cover, Game Type, platforms, release date) alongside a request's Game. The `gamerequest` domain owns no IGDB state itself; it never reads or writes `game_igdb_enrichment`, and this join stays confined to the read model.

This is intentionally lightweight and does not imply event sourcing or full CQRS.

---

## 10. PostgreSQL and sqlc

PostgreSQL is the only supported database for the initial architecture.

Use explicit SQL, sqlc, pgx, and PostgreSQL invariants where appropriate. Generated sqlc types stay inside persistence/read infrastructure where practical.

Important invariants include foreign keys, unique constraints, transactions, row locks, and check constraints.

---

## 11. GameRequest and Identity Concurrency

Joining a Game Request is concurrency-sensitive. If `MaxPlayers` is defined, concurrent joins must never exceed the limit. Enforce this with PostgreSQL transactions/locking rather than an unprotected read-check-insert sequence.

Case-insensitive Game Nick uniqueness must also hold under concurrent onboarding changes and be enforced at the persistence boundary.

Provider-result materialization is concurrency-sensitive as well. Concurrent follow/request actions for the same provider/external ID must converge on one authoritative local Game through a unique provider identity constraint plus an atomic application transaction.

Creating a Game Request is deliberately **not** idempotent server-side. Join, leave, close, and cancel each serialize on the request's row lock and resolve a duplicate to one winner and one deterministic domain error; creation has no comparable identity to key on. Every candidate rule — uniqueness over creator, Game, and start time being the obvious one — forbids the legitimate case of one player organizing two separate groups for the same Game at the same time, and picking one anyway would make an arbitrary product rule permanent in a migration. Duplicate submission is prevented by the submit guard in `web/src/app.ts` and, below it, by the browser's refusal to submit a form while one submission is in flight; both are covered by `web/tests/rapid-interaction.spec.ts`. This is sufficient because the web UI is the only client: LANsurfer exposes no JSON API, so there is no unguarded caller to defend against. Revisit only if a non-browser client is introduced, which is itself a scope change.

1.0 does not add Event-seat, Open Play, Clan ownership, Clan membership, or other post-1.0 invariants.

---

## 12. Migration Architecture

Use goose with embedded migration files.

Startup sequence:

```text
load configuration
       |
       v
connect/retry PostgreSQL
       |
       v
acquire PostgreSQL advisory migration lock
       |
       v
run embedded goose UP migrations
       |
       v
initialize application
       |
       v
start HTTP server
```

Migration failure is fatal. DOWN migrations are never run automatically.

Tracks may add independent forward migrations in parallel. Keep migrations narrowly scoped to the owning module/track and avoid modifying another track's tables except through an explicitly approved migration contract.

Track F's final prototype-to-1.0 upgrade validation runs only after the integrated migration sequence is stable. The selected pilot additionally requires at least one exercised upgrade path with existing data before the Pilot Gate is green.

---

## 13. Authentication Architecture

LANsurfer separates local users from external identities.

```text
LANsurfer User
      |
      +-- Identity: OIDC provider / subject
      |
      +-- Identity: Discord provider / subject
```

Stable 1.0 supports provider-neutral generic OIDC and Discord OAuth2 authorization-code login as independent providers. The selected pilot requires Discord login, but that pilot choice does not remove OIDC from stable 1.0.

The initial deployment may use Keycloak, but application/domain code must not assume Keycloak.

Do not use external provider IDs as LANsurfer user IDs and never merge identities based solely on email addresses.

Linking and unlinking are explicit authenticated account operations, and an account must retain at least one identity. Persist short-lived, one-use authentication/link flow records bound to the initiating browser, provider, operation, and current user where applicable.

Both providers resolve to the same local User/session model. Required Game Nick selection is provider-independent. A Discord display name may seed the Game Nick form but does not become LANsurfer public identity automatically.

Post-1.0 Twitch authentication is a third provider adapter in the same registry. It reuses OIDC discovery, PKCE, nonce, authorization-code exchange, ID-token validation, and the existing OAuth flow service. Its fixed `provider_kind` and `provider_key` are both `twitch`; its verified Twitch `sub` remains the external identity subject. It requests `openid` and the `preferred_username` ID-token claim, never email. The same local identity, linking, onboarding, and session paths apply unchanged.

Provider names and email addresses are private account data. Normal player and global administrator views must not expose them to anyone except the account owner.

---

## 14. Sessions

Use server-side sessions stored in PostgreSQL. The browser receives only an opaque, high-entropy random session ID in a cookie with bounded lifetime, `HttpOnly`, `SameSite`, and `Secure` in production.

No JWT browser sessions. Sessions must be revocable server-side.

---

## 15. Configuration Architecture

Infrastructure configuration is environment-based. Examples include:

```text
LANSURFER_DATABASE_URL
LANSURFER_BASE_URL
LANSURFER_LISTEN_ADDR
LANSURFER_OIDC_ISSUER
LANSURFER_OIDC_CLIENT_ID
LANSURFER_OIDC_CLIENT_SECRET
LANSURFER_DISCORD_CLIENT_ID
LANSURFER_DISCORD_CLIENT_SECRET
LANSURFER_DISCORD_WEBHOOK_CLIENT_ID
LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET
LANSURFER_DATA_ENCRYPTION_KEY
LANSURFER_WEBPUSH_PUBLIC_KEY
LANSURFER_WEBPUSH_PRIVATE_KEY
LANSURFER_WEBPUSH_SUBJECT
LANSURFER_TWITCH_CLIENT_ID
LANSURFER_TWITCH_CLIENT_SECRET
LANSURFER_TWITCH_AUTH_ENABLED
LANSURFER_IGDB_ENABLED
LANSURFER_COVER_CACHE_DIR
LANSURFER_LEGAL_ENTITY_NAME
LANSURFER_LEGAL_ADDRESS
LANSURFER_LEGAL_CONTACT_EMAIL
LANSURFER_IMPRINT_URL
```

`LANSURFER_OIDC_CLIENT_SECRET` is optional for public clients using PKCE and required when the provider configures LANsurfer as a confidential client.

Twitch application credentials are shared only by Twitch authentication and IGDB discovery. Each capability is disabled by default and enabled with its own strict boolean flag; enabling either requires both shared credentials. The two capabilities do not share access tokens, clients, retries, or runtime state. Legacy `LANSURFER_IGDB_CLIENT_ID` and `LANSURFER_IGDB_CLIENT_SECRET` are not supported.

`LANSURFER_COVER_CACHE_DIR` selects the IGDB cover cache store. When set, LANsurfer uses that writable directory as the persistent disk cache; when unset, it uses a fixed 64 MiB in-process LRU. The cache uses one store or the other, not a two-tier cache.

Discord authentication and Discord delivery use separate configuration pairs; an operator may point both at the same Discord application, but configuring one does not enable the other. Web Push is disabled when both VAPID keys are absent; partial/invalid VAPID configuration is an error, and configured Web Push also requires `LANSURFER_WEBPUSH_SUBJECT` plus a valid data-encryption key. Configured Discord delivery likewise requires a valid data-encryption key.

Provider credentials, the 32-byte data-encryption key, VAPID private key, and Discord-delivery OAuth secret remain operator configuration and must not be stored as ordinary product settings.

Minimum instance identity, if configurable, is deployment/product state with a deliberately small surface. Do not reintroduce the removed 1.0 welcome-text/contact/logo/icon branding editor through configuration indirection.

Operator legal identity (`LANSURFER_LEGAL_ENTITY_NAME`, `LANSURFER_LEGAL_ADDRESS`, `LANSURFER_LEGAL_CONTACT_EMAIL`, `LANSURFER_IMPRINT_URL`) is the one contact-bearing exception: it fills the fixed Terms/Privacy text and footer, travels to templates through request-context middleware like the instance name, and is required (entity name and contact email) whenever Discord login is configured.

Do not introduce a config file format unless a concrete requirement appears.

---

## 16. Branding and Installable Web Shell Architecture

LANsurfer keeps a fixed, accessible visual system and bundled application branding. Templates consume centralized branding values instead of repeating constants. Bundle approved logos/mascot/font assets with the application; do not add a runtime font or branding CDN.

Stable 1.0 does not require PostgreSQL-backed administrator-managed welcome text, contact link, uploaded logo, uploaded application icon, white-labeling, or theme controls. A deployment may expose only the minimum instance identity required to distinguish the installation, plus the operator legal identity described in §15.

Serve a manifest with a stable application identity/start URL and bundled/default presentation. A minimal service worker supports installation and Web Push; it does not promise offline application behavior.

Track D owns service-worker registration, lifecycle, and update behavior. Track E attaches Web Push behavior to that contract rather than creating a competing service worker or registration path.

Pilot validation names platform categories (Desktop, Android, iPhone), while concrete browser/version evidence belongs in the validation artifact rather than being permanently pinned in the architecture.

---

## 17. Game Catalog and IGDB Architecture

PostgreSQL owns the authoritative local Game catalog. IGDB is an optional discovery/enrichment integration for general deployments and is mandatory only for the selected pilot readiness criteria.

The architecture separates two concepts:

```text
IGDB
  |
  | background synchronization
  v
local provider index (discovery infrastructure)
  |
  | follow / create Game Request
  v
atomic materialization
  |
  v
authoritative LANsurfer Game
```

### Provider synchronization

When IGDB is disabled or unconfigured:

- no provider synchronization runs
- application health is not failed merely because IGDB is absent
- player search uses authoritative local Games only
- global administrators may create/manage local Games manually

When enabled:

- start the initial synchronization asynchronously after HTTP/application availability
- synchronize lightweight search/classification metadata for all IGDB game records; do not filter game types at ingestion
- keep enough provider data to search/disambiguate by name/alias, provider ID, type/category, useful relationships, platform/release context, and cover reference metadata
- do not mirror the full provider graph or bulk-download cover images; cover bytes are cached on demand only for cover IDs LANsurfer already knows
- do not expose partially synchronized IGDB results during the first baseline sync
- after the first successful baseline, run automatic refresh once per day and offer global administrators **Sync now**
- retry failures independently of core request traffic
- keep serving the last successful index while a later refresh is running or has failed
- expose at least `disabled`, `not synced`, `syncing`, `current`, `stale`, and `failed` operator states

The administrator status page reads both counts in one query: `RecordCount` is the active generation's `record_count`, and `StagedRecords` is a `count(*)` over `igdb_game_index` joined to `igdb_sync_generations` through `igdb_provider_state.latest_attempt_generation_id` while that generation is still `syncing`. The two are deliberately separate, so an in-progress baseline never looks like a shrunken active catalog. While the state is `syncing`, that card polls its own fragment route (`/admin/igdb/status`) with HTMX; the polled response replaces the element that carries the polling attributes, so the settled state stops polling without a separate stop condition or client script. The fragment sits behind the same administrator authorization as the page.

Default player-visible provider categories are semantic product policy rather than hardcoded numeric enum assumptions. Ordinary search includes main games, expansions, standalone expansions, remakes, remasters, ports, and expanded games. DLC/add-ons, bundles, mods, episodes/seasons, packs, and updates remain synchronized but hidden by default. An unmaterialized provider record is player-visible only when its Game Type is supported and IGDB `version_parent` is null; `parent_game` is never a visibility filter. Search, direct provider selection, and materialization enforce the same rule (`game.VisibleProviderRecord`), and already materialized local Games stay governed by local Game state. Editions are identified by these structured relationships, never by name heuristics.

### Materialization and authority

Searching/viewing a provider result is read-only. Following it or creating a Game Request for it invokes the `game` module to atomically materialize one authoritative local Game keyed by provider/external ID. Any signed-in player may trigger this when IGDB is enabled; there is no administrator approval step.

Never merge by name. Once materialized, local Games remain authoritative and usable if the provider is disabled, unavailable, changed, or later loses the source record.

Administrator reconciliation stays inside the `game` module. Linking reloads the selected record from the active provider generation inside the write transaction; browser-submitted provider metadata is never persisted. Unlinking removes the IGDB mapping and enrichment snapshot together while leaving the local Game and its relationships untouched.

Merging takes explicit source and target IDs, locks both Game rows in stable order, and completes in one transaction. The target keeps its ID, name, slug, and active state. Game Requests are reassigned, follows are copied with `(user_id, game_id)` conflict deduplication, aliases are copied with the existing case-insensitive per-Game uniqueness rule, and a single source IGDB mapping/enrichment snapshot moves to the target. If both Games have IGDB mappings, the transaction stops before moving anything; an administrator must explicitly unlink the incorrect identity first. The source Game is deleted only after every transfer succeeds.

Permanent deletion locks and rechecks the Game in the transaction. It is rejected while Game Requests or follows exist. The `game_requests.game_id` foreign key remains `ON DELETE RESTRICT`; aliases, external identities, and enrichment may cascade only after those business-reference guards pass.

Unmaterialized discovery reads presentation metadata from the active provider generation. Once materialized, player-facing reads use the authoritative local Game plus a local last-known-good IGDB enrichment snapshot (`game_igdb_enrichment`). Successful provider syncs refresh only non-empty enrichment fields; provider disappearance or disablement does not remove the snapshot.

If a synchronized provider record disappears:

- delete it when no authoritative local Game references it
- when referenced, retain an archived/tombstoned provider identity for stable identity/history/reconciliation and exclude it from ordinary provider discovery

Disabling IGDB after prior use may leave the synchronized provider index stored, but that index is not exposed as active provider search while disabled. Re-enabling resumes synchronization without recreating authoritative local Games.

### Provider boundary

Provider API and synchronization mechanics live behind narrow interfaces. The exact interface may include paginated/batched synchronization operations rather than only interactive `Search`/`Get`, because the approved architecture is no longer operator-driven one-record-at-a-time import.

Do not couple the primary `games` table schema to IGDB-specific fields. Use generic provider/external identity and focused provider-index storage.

IGDB cover bytes are served same-origin through a bounded LANsurfer proxy/cache; player browsers never contact IGDB directly. The browser supplies only `size` (`small` or `big`) and an image ID matching `^[a-z0-9]{1,32}$`; on a cache miss the ID must also exist in `igdb_game_index.cover_image_id` or `game_igdb_enrichment.cover_image_id`. LANsurfer constructs the upstream URL server-side from a fixed `images.igdb.com` origin and fixed size mapping, with a combined 5-second catalog-and-fetch deadline, a 5-second upstream client timeout, redirects disabled, a 1 MiB response cap, and body-sniffed JPEG/PNG/WebP acceptance. Unknown IDs and failed fetches use a fixed 4,096-entry in-memory negative cache for up to 10 minutes. At most eight shared cold-miss operations run per process; a new cold key receives `503` without queuing when saturated, while cache hits and callers joining an existing singleflight do not consume another slot. The cache is either the configured disk store or the fixed 64 MiB in-process LRU, and missing/failed covers retain the local placeholder. Never accept arbitrary upstream URLs or generalize this into a remote-asset proxy.

---

## 18. Frontend Architecture

`templ` renders full pages and HTMX fragments. DaisyUI provides the stable base component vocabulary; Tailwind handles layout/local variations. Avoid another UI framework.
Icons are the Phosphor webfonts (Regular default, Fill for emphasized state), consumed as plain CSS classes in templ markup with `currentColor`; TypeScript does not manage icons.

TypeScript handles only browser-specific progressive enhancement such as SSE connection handling, toast/dialog helpers, installability/service-worker registration, and Web Push permission/subscription behavior. Do not implement domain decisions in TypeScript.

When Tracks D and E both need browser changes, Track D owns the service-worker shell/lifecycle and Track E owns push-specific behavior.

---

## 19. Static Assets

Build frontend assets during development/CI. Production artifacts are embedded using `go:embed`.

The deployed application should not require Node.js, package managers, Tailwind CLI, templ CLI, or esbuild at runtime.
Icon webfonts and their stylesheet are bundled and embedded like the fonts through `scripts/prepare-assets.mjs`; there is no runtime icon CDN and no remote-content exception in Content Security Policy.

---

## 20. Operations

Normal self-hosting is:

```text
Docker Compose
├── lansurfer
└── postgres
```

The standalone binary must also support an externally managed PostgreSQL instance.

Provide structured logging, `/healthz`, `/readyz`, `lansurfer version`, healthcheck, data-encryption-key generation, and VAPID key-generation commands.

Request logging records an `outcome` of `completed`, `client-aborted`, or `timed-out`, derived from the request context's cancellation cause rather than from the status code, which stays `200` for a response the client never received. The cause is attached by the request-timeout middleware, so that middleware must wrap request logging. Realtime stream open and close are logged with the number of streams the instance currently holds and a close reason, because long-lived `EventSource` connections consuming the browser's per-origin connection pool were the root cause in issue #15 and were not visible in the logs at all. This is operator diagnosability through `log/slog`; LANsurfer does not add a metrics or observability stack.

Track F may build release automation and operator tooling in parallel. Stable 1.0 publishes Linux amd64/arm64 binaries and a matching multi-architecture GHCR image using native Go tooling and pinned official Docker/GitHub Actions. Release outputs include checksums, SBOMs, and provenance attestations. Stable releases receive exact, major.minor, major, and `latest` tags; prereleases receive exact tags only.

The production Compose example pins the LANsurfer image, does not publish PostgreSQL to the host, and retains only application + database as required services. TLS and reverse proxy remain operator responsibilities.

Migrations remain forward-only. Final prototype-to-1.0 upgrade, backup/restore, and key-backup validation belong to release integration after persistent migrations settle.

The selected pilot additionally requires before admission:

- production deployment path exercised
- clean-host installation exercised
- documented backup with an actually exercised restore
- at least one exercised upgrade path using existing data
- representative load/performance rehearsal covering browsing/search, create/join/leave, SSE updates, and notification behavior

Concrete tested hardware, network, browser versions, load envelope, and observed response/recovery results belong in the validation artifact rather than permanent architecture constants.

Supported production deployment must preserve required HTTP behavior through the chosen proxy/deployment path, including normal regression coverage for core interactions. Issue #15's reported hang is not an open architecture investigation requirement.

---

## 21. Security Boundaries

Important baseline controls:

- OIDC state validation
- Discord OAuth2 state validation
- PKCE when applicable
- secure session cookies
- CSRF protection for browser writes
- input validation
- templ escaping
- security headers
- careful trusted-proxy configuration
- one-use, browser-bound authentication/link flow state
- provider identity privacy
- server-constructed IGDB cover fetches with no third-party requests from player browsers
- encrypted Discord webhook credentials with key rotation/backup guidance
- Web Push payload data minimization and explicit permission/consent

Do not invent cryptographic constructions. Use established protocol behavior, Go standard-library AES-GCM for approved credential encryption, and the approved Web Push library behind narrow adapters. Secrets and raw session/authentication-flow tokens must never be logged or stored in recoverable plaintext.

---

## 22. Architectural Enforcement

Where practical, CI/lint rules may enforce forbidden dependency directions. Business packages should not import concrete persistence adapters.

A dependency rule tool such as `depguard` may be considered if it remains simple and provides real enforcement value.

Parallel-track development makes boundaries especially important: contracts may be shared, implementations and persistence ownership may not.

---

## 23. Architectural Decision Policy

Follow the [approval rules](../AGENTS.md#changes-requiring-approval). Document approved architectural changes here or in a short ADR, including the reason for the decision. Small internal implementation choices do not need an ADR.

The roadmap's parallel tracks are planning boundaries, not new architectural layers. Do not create track packages, track-specific frameworks, or abstractions whose only purpose is coordinating project management.
