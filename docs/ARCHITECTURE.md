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

LANsurfer uses the following primary runtime architecture:

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

This document records the current technical architecture and durable invariants. Product behavior and approved scope belong in [PRODUCT.md](PRODUCT.md); current work, dependencies, validation status, and release gates belong in GitHub Issues and milestones.

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
- start/close/cancel a request
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

For the current single-instance deployment model, the SSE broker may run in memory. Durable external notifications use PostgreSQL persistence and do not make SSE durable.

```text
Game Request transaction
  ├── domain write
  └── game_request_activity row when applicable
          |
          v
notification capability delivery state / notification worker
  ├── Web Push adapter
  └── Discord webhook adapter
```

Game Request writes and the corresponding live `game_request_activity` row must be transactionally consistent. That row remains the single Game Request capability → E notification-intent handoff as well as request-local history. A failed or rolled-back mutation leaves neither the domain change nor a deliverable activity row.

notification capability consumes that handoff asynchronously and owns consumer-side durable state:

- one `notification_work` record per consumed live activity;
- zero or more `notification_delivery` records for concrete external targets;
- durable uniqueness so repeated discovery cannot duplicate work;
- a first-consumer baseline that marks already-visible live history as non-deliverable instead of using a timestamp or global sequence high-watermark;
- no delivery for `origin=backfill`.

`game_request_activity.sequence` identifies an activity row and orders activity within one Game Request; it is not a global replay cursor. PostgreSQL durable leases/row locking coordinate due delivery work. The notification worker polls PostgreSQL rather than adding Redis, a broker, or LISTEN/NOTIFY.

Delivery state distinguishes retryable failure, permanent failure, exhaustion, ambiguous/uncertain provider acceptance, and intentional suppression when present-day eligibility no longer permits an already-materialized delivery. Uncertain delivery is never blindly retried because the external effect may already have occurred. Authorized manual retry opens a fresh retry round and re-checks current eligibility.

LANsurfer's own retry schedule uses capped exponential backoff with jitter and a six-attempt automatic round. Its computed backoff never exceeds five minutes. An explicit provider `Retry-After` is authoritative and is honored in full even when it exceeds that cap. External transport failure never fails or reverses the originating Game Request action.

The event publishing abstraction should remain replaceable so PostgreSQL `LISTEN/NOTIFY` can be considered later if multiple application instances are ever approved. Multi-instance/high-availability operation is not part of the current supported deployment model.

WebSockets are not part of the initial architecture. Introducing them requires approval and a meaningful bidirectional use case.

### Web Push

Web Push uses VAPID behind a narrow adapter around the approved `github.com/marknefedov/go-webpush/v2` dependency and extends PWA/instance identity capability's single existing service worker with push/click behavior rather than registering another worker. E2 adds no offline shell or fetch caching. Manual validation evidence belongs on the relevant GitHub issues and pull requests rather than in this durable architecture document.

Store multiple labeled subscriptions per user. Browser permission, active subscription/device state, LANsurfer enablement, and new-request audience preference remain separate. New Game Requests use followed/My Games by default or all new requests through explicit opt-in. Join/leave/start/close/cancel notify current request participants independently of that creation-audience setting. The activity actor is excluded from their own personal push. A `request_updated` activity creates Web Push targets only when planned start changed; delivery rechecks that the request remains open at the new time. Capacity/description-only updates create no personal push.

Activity recipient/device sets are materialized once when work is first processed. Delayed/retried sends re-check eligibility and may suppress an existing delivery, but old activity work never gains new users or devices. Reminder deliveries also freeze their initial target set and recheck current participant, preference, schedule, and subscription eligibility before each send.

Activity pushes use wire version 4 and remain structured and privacy-minimized: activity kind, Game, lifecycle status, planned time, optional actual start time, same-origin deep link, minimum instance context, current member count and optional maximum players, and optional creator or actor public Game Nick. Playing payloads require an actual start; open and cancelled payloads reject one. The service worker labels planned and actual times explicitly, omits unavailable times, and uses a Game-only title for non-open snapshots. `started` remains actor-neutral; the Host is excluded as a recipient rather than named in the notification. Planned-time edits and reminders use a separate six-field time-only payload. Do not include request description, roster, cover, provider identity data, or activity history. The worker suppresses OS notifications while a same-origin LANsurfer window is visible so SSE remains the in-browser path.

An open request creates at most one starting-soon reminder opportunity per exact `(game_request_id, starts_at)` pair, when the planned time is within 15 minutes. The parent row commits even with zero eligible devices, so later subscriptions do not expand old work. Workers reserve a reminder delivery immediately before provider send. A reserved claim that expires becomes `uncertain`, never automatically reclaimed; an expired unreserved claim remains retryable. Unknown provider acceptance is `uncertain` because duplicate OS notifications cannot be excluded. Known retryable outcomes clear the reservation marker.

Treat PushSubscription endpoint/key material as secret-adjacent application data: encrypt endpoint, `p256dh`, and `auth` with AES-GCM using the notification capability data-encryption key, never log or display decrypted material, and use a keyed endpoint fingerprint for equality/uniqueness lookup without storing the raw endpoint alongside ciphertext. A separate long-lived opaque browser-device cookie is stored only as a server-side hash and is independent of the authentication-session lifetime.

### Discord delivery

Discord authentication/login and Discord notification delivery are separate integrations. Delivery uses separate operator OAuth configuration and the `webhook.incoming` scope. Setup uses OAuth; it does not use a bot, Gateway, pasted webhook URL, or test-message workflow. OAuth access/refresh tokens are discarded after webhook setup. Webhook ID/token are encrypted together at rest; secret-bearing diagnostic output is prohibited.

At most one Discord destination is current per instance. Every successful connect, reconnect, or replacement creates a new non-secret destination-generation UUID. Targets include that generation and never resolve to a later one. Disconnect suppresses only work for the disconnected generation. A `10015 Unknown Webhook` response breaks only the exact current generation, clears its encrypted credential, retains safe diagnostic metadata, terminates the current delivery, and suppresses its remaining pending work. Reconnect never replays historical work.

Each destination generation and Game Request has one synchronized Discord message. Initial delivery uses webhook execute with `wait=true` and stores the returned message ID; later activity edits that message. Rendering reads current Game Request state, not historical activity payloads. Rapid intermediate activity may be superseded/coalesced. PostgreSQL serialization protects external ordering. Message sync retains the highest applied activity version.

Automatic message CREATE is non-idempotent. Before a CREATE, LANsurfer durably records a reservation. At most one automatic CREATE attempt may proceed while its outcome remains unresolved. An ambiguous outcome becomes `uncertain` and is never automatically replayed. Automatic lease expiry or restart never clears the reservation. An explicit administrator `retry-uncertain` may override the guard only after atomically revalidating the exact current destination generation and reservation owner; the administrator accepts the documented possibility of an external duplicate. This is not distributed exactly-once delivery.

The compact approved embed uses the Game Requests list title (creator Game Nick and Game), a lifecycle-selected Discord-local `Planned start` or `Started at` field, and an optional Game cover thumbnail from the fixed `images.igdb.com` origin. Discord can fetch the cover even when the instance's base URL is local; the deep link still uses that configured base URL. This external cover reference applies only to Discord embeds: the LANsurfer web UI continues to serve covers same-origin. The status remains readable text and colors the embed accent: surf blue for open, Sunset for playing, muted for closed, coral for cancelled. Other content is:


- current capacity/free-place summary;
- short sanitized/plain-content description;
- a final embed field with a clickable `Open in browser ↗` link to the configured LANsurfer deep link;
- minimum instance context.

It never includes the full participant roster or activity history and never uses Discord login identity as LANsurfer public player identity. Every create/edit sends empty `allowed_mentions`.

---

## 5. Modular Monolith

Business modules are:

```text
auth
user
game
gamerequest
notification
```

The prototype `event` module has been removed: the Game Request forward migration dropped the Game Request/Event association, so current behavior does not depend on Event selection, membership, lifecycle, or authorization. Do not introduce an `event`, `clan`, or generic context module unless approved product scope and current GitHub planning require it.

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
├── notification/  # notification capability
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

Account deletion is `user.Service.DeleteAccount`, which verifies the retyped Game Nick and deletes the `users` row in one statement guarded against removing the last administrator (administrator rows are locked in id order so concurrent deletions serialize). Every foreign key into `users` is `ON DELETE CASCADE`, so the database removes sessions, identities, follows, memberships, created Game Requests (and through them activity, notification work, and deliveries) atomically; no module writes another module's tables. A new table that references `users` must cascade as well; an integration test fails on any other `ON DELETE` action.

Rich biographies, per-Game player handles, and richer public profiles are outside the current product model.

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

Migration `00006_single_event_core` drops `game_requests.event_id` and its index; every prototype Game Request keeps its id, creator, members, status, description, and timestamps. The legacy `events` table is deliberately left in place with its rows: no application code reads it, no row references it, and the supported forward migration intentionally preserves operator data. A later release may remove the table once operators have had a chance to export it. Do not reintroduce an `event` module, Event selection, or Event membership.

### `gamerequest`

Owns:

- Game Requests
- request membership
- request lifecycle
- capacity rules
- durable request-local activity for successful create/join/leave/start/close/cancel/request_updated changes
- the transactional notification-intent record notification capability consumes
- per-player reading history of Game Requests

The Game Request lifecycle is `open -> playing -> closed` or `open -> cancelled`. The Host-only `open -> playing` transition records a durable actual `started_at` value and a `started` activity row in the same transaction. `playing` remains an active/discoverable state. New `closed` transitions must come from `playing`; historical `closed` rows that predate this lifecycle may retain `started_at IS NULL` because the real start time must not be fabricated.

`request_updated` is a durable activity kind and therefore also notification-handoff work. One successful Host Save creates at most one such row even when several editable details change. Typed metadata records only changed dimensions: nullable `previous_max_players`/`new_max_players` (where `NULL` means unlimited), nullable `previous_starts_at`/`new_starts_at`, and `description_changed boolean`. A check constraint requires at least one real changed dimension and requires every other activity kind to leave these metadata values empty/false. A previous finite maximum may be the legacy value 1; a new finite maximum must be at least 2. Description text itself is not duplicated into activity. No generic JSON metadata or patch payload exists. The notification path treats `request_updated` as ordinary work that resolves Discord targets, so the synchronized message edits to the current request snapshot, while Web Push resolves zero targets. Trending's activity-kind filter is unchanged and excludes it.

One table, `game_request_activity`, currently serves both of those responsibilities. Every successful change writes its entry inside the same transaction as the change itself, so a rolled-back mutation leaves neither the change nor a notification intent, and a committed one survives a restart before delivery. A second table would duplicate the same write for the same guarantee. notification capability1's implementation confirmed this design: the `notification` module consumes this table asynchronously and keeps its own `notification_work`/`notification_delivery` state, with no second write into `gamerequest`'s tables or transactions. Splitting the notification record out remains the expected change only if a future track demonstrates delivery-specific state that does not belong here.

Two rules govern how a consumer reads it:

- **`sequence` identifies a row and orders one Game Request's history. It is not a global replay cursor.** The value is assigned when the row is inserted, not when its transaction commits, and creating a Game Request takes no shared lock, so two concurrent changes can commit in the opposite order to their sequence values. A consumer holding a single high-watermark would permanently skip the row that committed late. Ordering within one request is safe because join, leave, start, close, cancel, and request-detail updates serialize on that request's row lock and `created` is always first — which is also the only ordering delivery needs, since stale-update protection is per request. A consumer therefore keeps durable per-row delivery state keyed by `sequence` as an opaque identifier, rather than advancing one cursor.
- **`origin` separates recorded activity from reconstructed activity.** Rows written while the application is running are `live`. Rows the `00006` migration reconstructed from prototype membership and status are `backfill`: they describe activity that predates the record and must never be delivered. A consumer starting for the first time on an installation that already has `live` history is responsible for its own baseline, so an upgrade does not deliver the archive.

Reading history lives in its own table, `game_request_views`, and is deliberately not `game_request_activity`. Activity is domain history and notification capability's notification-intent handoff; opening a detail page is neither, so a view must never be delivered, must never reach the realtime stream, and must never enter the activity table. It is also not transactional with any Game Request change: the write happens while serving a successful read, and losing it costs a navigation entry rather than corrupting a Game Request. One row per (player, request) with the timestamp updated on re-reading keeps the table a set of distinct requests, so it needs no pruning to stay short, and the foreign key to `game_requests` removes the history with the request.

Request-local activity is not event sourcing, generic audit logging, chat, or notification delivery history.

Request-local chat lives in `game_request_messages`, owned by `gamerequest` and separate from activity: messages are not lifecycle history and never create notification work. The detail page's **Lobby** combines chat messages and durable Game Request activity. The dedicated query reads both sources from one membership-checked snapshot, selects the latest 200 combined entries, and returns them oldest-first with deterministic timestamp/source/ID ordering. For presentation only, the same read also carries the Host's user ID and each chat author's selected avatar key, so the Lobby can mark Host messages and show avatars for authors who have since left; neither is stored with the message. Current members (including the Host) may read the Lobby in every lifecycle state; a player who leaves loses access immediately. Nonmembers cannot read it. Chat posting remains limited to current members while the request is `open` or `playing`, authorized under the request row lock, so a message cannot land after the poster left or the request closed. The realtime stream carries a content-free `GameRequestChatMessage` signal as SSE event `game-request-chat`; the stream handler rechecks current Lobby access per signal. Message content is read only through `GET /game-requests/{id}/lobby`. Posting is rate limited per poster and request under the same lock (default 5 messages per 20 seconds, configurable with `LANSURFER_CHAT_RATE_LIMIT` and `LANSURFER_CHAT_RATE_WINDOW`). `POST /game-requests/{id}/chat` returns the authorized Lobby fragment for HTMX and redirects ordinary form submissions.

Structured session information is owned by `gamerequest` in `game_request_session_entries`, with UUID child rows, immutable creation-order `sequence`, and case-insensitive per-request keys. Reads return entries only from a single snapshot that confirms current membership and an `open` or `playing` request. The Host alone mutates entries under the same Game Request row lock used by membership and lifecycle writes; the 12-entry limit is checked under that lock. Session-information writes create no activity or notification handoff.

Real mutations publish content-free `GameRequestSessionInformationChanged` signals. SSE event `game-request-session-information` uses the neutral `detailRequestId` scope shared with chat and is delivered only after current participant access is confirmed for each signal. Session entry values are fetched only from the separately authorized fragment endpoint. Secret entries add `is_secret boolean NOT NULL DEFAULT false`; values remain plaintext in PostgreSQL and use the same read authorization. Realtime signals carry no entry content, logs, metrics, notifications, public/list views, and Discord synchronization must not expose secret values. In the UI, secret values are masked by default, copied without reveal, and automatically hidden within 20 seconds. Reveal state stays client-only and resets on fragment refresh.

### `notification` (notification capability)

Owns:

- `notification_work`: one durable row per consumed live `game_request_activity` row, snapshotting the fields (`game_request_id`, `actor_id`, `kind`, `occurred_at`) delivery processing and diagnostics need, so neither has to join back into `gamerequest`'s tables afterward.
- `notification_start_reminder`: one durable opportunity per exact request/planned-time pair; reminder deliveries use the same target/status rows but belong to this parent instead of `notification_work`.
- Web Push subscriptions and audience preferences (`notification_push_subscription`, `notification_push_preference`), and the single instance-level Discord destination. Both configured transports are registered at startup; an activity becomes work only after every configured transport resolves its targets successfully.

The singleton Discord destination row has a separate UUID identity for each successful connection generation. Replacement rotates that identity atomically; broken state retains it, while disconnect removes the current row. `notification_discord_message_sync` retains one message reference and highest applied request activity sequence per destination identity and Game Request, even after replacement or disconnect. PostgreSQL conditional updates reject stale versions, and compare-and-clear recovery keeps the highest applied version. A transaction-scoped advisory lock on the destination UUID and Game Request UUID serializes the Discord sync read, current snapshot read, external create/edit, and sync update. A shared lock on the destination row prevents replacement during the external mutation. Only newer pending/claimed deliveries of the same generation and request supersede older work; the marker advances to the activity sequence of the delivery that actually mutates Discord. Webhook create is not idempotent, so LANsurfer makes at most one automatic create attempt per destination generation and Game Request: before the provider call, a `notification_discord_create_reservation` row commits on a separate pooled connection while the sync lock is held. Recording the message ID deletes it in the sync transaction; only a provider-proven rejection (never transmitted, or an explicit 4xx or 5xx answer) deletes it otherwise. A retained reservation without a message reference means Discord may hold an unrecorded message, so every later automatic attempt for that generation and request ends `uncertain` without contacting Discord; reconnecting starts a new generation. Deleted messages are recovered from the latest request state when the webhook remains valid; deleted webhooks mark the destination broken. Connect and Reconnect use a same-origin OAuth bridge so the CSRF POST complies with `form-action 'self'`.

The notification module consumes durable Game Request outbox work asynchronously; it does not own or write Game Requests, and Game Request capability never writes into `notification`'s tables. Discovery uses a `NOT EXISTS` anti-join against `game_request_activity.sequence` plus a unique constraint on `notification_work.activity_sequence`, not a high-watermark cursor, so a row committed late is never silently skipped. A one-time, idempotent baseline pass materializes all pre-existing `origin=live` activity as never-deliverable "baseline" work before the worker starts; `origin=backfill` rows are excluded from discovery entirely. Delivery claiming uses `FOR UPDATE SKIP LOCKED` with a 60-second lease so a crashed worker's claim becomes eligible again automatically, with no in-process mutex and no additional required runtime service. The worker re-checks target eligibility against the owning transport before every send, including retries. A proven loss of eligibility marks the delivery `suppressed`; a failed eligibility read instead records the ordinary retry/backoff without calling the provider.

E1 exposes authorized diagnostics and manual retry as `lansurfer admin notifications problems|retry|retry-uncertain` — a CLI surface, not an HTTP page. `lansurfer admin notifications generate-secrets` is an operator helper that prints fresh VAPID keys and a data-encryption key to stdout without loading configuration or persisting anything. brand/UI capability owns any future in-app presentation.

E2 stores each PushSubscription endpoint, `p256dh`, and `auth` encrypted at rest with AES-256-GCM and uses a keyed endpoint fingerprint for uniqueness/ownership lookup. `/account/notifications` manages permission/subscription, audience preference, and devices; logout removes the current browser's subscription. The Web Push transport materializes eligible recipients, rechecks eligibility before sends, and uses a dedicated HTTPS-only outbound client that validates every DNS answer at dial time, dials only public IP addresses, disables proxies, and never follows redirects. E3 registers its separate Discord transport and supports connected, broken, replaced, and disconnected destinations; retries do not repeat ambiguous CREATEs. Manual validation status and device-specific evidence live on the relevant GitHub issues and pull requests rather than in this architecture document.

A module must not directly update another module's tables merely because they are in the same database.

---

## 8. Cross-Module and Cross-Capability Communication

When a module needs another module's behavior, prefer a small explicit interface. Cross-module joins are allowed for read-only query models, not business writes.

The key cross-capability contracts are:

- **Game Request capability → notification capability:** Game Request capability owns Game Request write transactions and outbox creation; notification capability consumes durable outbox records. Final persistence belongs inside Game Request capability's transaction boundary.
- **authentication/Game Nick capability internal:** OIDC and Discord identities resolve to the same local User. Game Nick onboarding is provider-independent and happens after authentication when required.
- **authentication/Game Nick capability → public presentation:** public identity consumers use Game Nick rather than provider names/emails. This dependency is for rendering only, not for core request writes or notification worker construction.
- **PWA/instance identity capability → notification capability:** PWA/instance identity capability owns service-worker registration/lifecycle. notification capability owns push permission, subscriptions, push handling, and payload behavior. Backend Web Push work does not need to wait for PWA/instance identity capability.

Do not solve these contracts by sharing persistence implementations or creating generic cross-capability frameworks.

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

Joining a Game Request is concurrency-sensitive. If `MaxPlayers` is defined, concurrent joins must never exceed the limit. Enforce this with PostgreSQL transactions/locking rather than an unprotected read-check-insert sequence. Join, Leave, lifecycle transitions, and Host request-detail edits share that one locked `game_requests` row as their consistency boundary, with no trigger, advisory lock, retry layer, or coordination table: a join racing a capacity reduction (or a leave racing one) resolves by lock order and can never commit a member count above a finite maximum. Join and Leave are accepted while the request is `open` or `playing`. Host detail editing is accepted in those same active states: `open` permits capacity, planned start, and description changes; `playing` permits capacity and description only, with any attempted planned-start change rejected under the lock. The update contract does not contain Game, creator, lifecycle status, or `started_at`, so those values cannot be changed through this path. Capacity is never reduced below current membership, and a semantically identical submitted state is a no-op that writes nothing. `max_players` carries no database `>= 2` constraint, because that would invalidate older rows holding `1`; the finite minimum of 2 is enforced on new and edited input.

Case-insensitive Game Nick uniqueness must also hold under concurrent onboarding changes and be enforced at the persistence boundary.

Provider-result materialization is concurrency-sensitive as well. Concurrent follow/request actions for the same provider/external ID must converge on one authoritative local Game through a unique provider identity constraint plus an atomic application transaction.

Creating a Game Request is deliberately **not** idempotent server-side. Join, leave, close, cancel, and request-detail edits each serialize on the request's row lock and resolve duplicate/racing mutations deterministically; creation has no comparable identity to key on. Every candidate rule — uniqueness over creator, Game, and start time being the obvious one — forbids the legitimate case of one player organizing two separate groups for the same Game at the same time, and picking one anyway would make an arbitrary product rule permanent in a migration. Duplicate submission is prevented by the submit guard in `web/src/app.ts` and, below it, by the browser's refusal to submit a form while one submission is in flight; both are covered by `web/tests/rapid-interaction.spec.ts`. This is sufficient because the web UI is the only client: LANsurfer exposes no JSON API, so there is no unguarded caller to defend against. Revisit only if a non-browser client is introduced, which is itself a scope change.

The current product model does not include Event-seat, Open Play, Clan ownership, Clan membership, or similar future-domain invariants.

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

Independent capabilities may add forward migrations in parallel. Keep migrations narrowly scoped to the owning module and avoid modifying another module's tables except through an explicitly approved migration contract.

`main` is the only deployable database history. Feature-branch migration numbers and prototype database data have no compatibility guarantee; reconcile parallel migrations after the existing `main` sequence without changing migrations already merged into `main`. Prototype databases created from non-`main` revisions may be recreated from the final migration set. Verify that sequence against a fresh empty PostgreSQL database; supported upgrades are only between `main` revisions.

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

LANsurfer supports provider-neutral generic OIDC and Discord OAuth2 authorization-code login as independent providers.

The initial deployment may use Keycloak, but application/domain code must not assume Keycloak.

Do not use external provider IDs as LANsurfer user IDs and never merge identities based solely on email addresses.

Linking and unlinking are explicit authenticated account operations, and an account must retain at least one identity. Persist short-lived, one-use authentication/link flow records bound to the initiating browser, provider, operation, and current user where applicable.

Both providers resolve to the same local User/session model. Required Game Nick selection is provider-independent. A Discord display name may seed the Game Nick form but does not become LANsurfer public identity automatically.

Twitch authentication is an optional third provider adapter in the same registry. It reuses OIDC discovery, PKCE, nonce, authorization-code exchange, ID-token validation, and the existing OAuth flow service. Its fixed `provider_kind` and `provider_key` are both `twitch`; its verified Twitch `sub` remains the external identity subject. It requests `openid` and the `preferred_username` ID-token claim, never email. The same local identity, linking, onboarding, and session paths apply unchanged.

`auth` owns provider avatars. Each identity stores an optional `avatar_ref` (Discord avatar hash; Twitch or OIDC `https` `picture` URL, Twitch limited to `static-cdn.jtvnw.net`), a derived content key `avatar_key` (`hex(sha256(kind|subject|avatar_ref))[:32]`), and `avatar_selected`. A partial unique index allows at most one selected identity per user; no selected row means Default. Every login/link refreshes `avatar_ref`/`avatar_key` but never `avatar_selected`; unlinking deletes the row and therefore the selection. An invalid or missing avatar never fails authentication. Read models join the selected `avatar_key` read-only.

Provider names and email addresses are private account data. Normal player and global administrator views must not expose them to anyone except the account owner.

### Return path after login

The post-login destination is carried in a short-lived `lansurfer_return` cookie (HttpOnly, SameSite=Lax, Secure when configured, `Path=/`, 15 minutes), not in the OAuth `state`, the session, or the database. `GET /auth/login?return_to=<path>` and `RequireAuthentication` (full-page GET requests only, never HTMX fragments or the event stream) capture it; the callback's normal-login branch and onboarding completion consume and clear it. Link and `/account` flows ignore it. The path is validated on capture and again on consumption: a local path only, not `//` or `/\`, no scheme or host, and not under `/auth` or `/onboarding`. A missing or invalid value falls back to `/game-requests`, except that completing a brand-new account's Game Nick onboarding falls back to the stateless `/welcome` step instead.

The public landing page reads the existing Trending ranking plus one aggregate read-model query in `internal/query/gamerequest`; it performs no writes and exposes no player identity.

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

`LANSURFER_COVER_CACHE_DIR` selects the media cache store shared by IGDB covers and provider avatars. When set, LANsurfer uses that writable directory as the persistent disk cache; when unset, it uses a fixed 64 MiB in-process LRU. The cache uses one store or the other, not a two-tier cache.

Discord authentication and Discord delivery use separate configuration pairs; an operator may point both at the same Discord application, but configuring one does not enable the other. Web Push is disabled when both VAPID keys are absent; partial/invalid VAPID configuration is an error, and configured Web Push also requires `LANSURFER_WEBPUSH_SUBJECT` plus a valid data-encryption key. Configured Discord delivery likewise requires a valid data-encryption key.

Provider credentials, the 32-byte data-encryption key, VAPID private key, and Discord-delivery OAuth secret remain operator configuration and must not be stored as ordinary product settings.

Minimum instance identity, if configurable, is deployment/product state with a deliberately small surface. Do not introduce an administrator-managed welcome-text/contact/logo/icon branding surface through configuration indirection.

Operator legal identity (`LANSURFER_LEGAL_ENTITY_NAME`, `LANSURFER_LEGAL_ADDRESS`, `LANSURFER_LEGAL_CONTACT_EMAIL`, `LANSURFER_IMPRINT_URL`) is the one contact-bearing exception: it fills the fixed Terms/Privacy text and footer, travels to templates through request-context middleware like the instance name, and is required (entity name and contact email) whenever Discord login is configured.

Do not introduce a config file format unless a concrete requirement appears.

---

## 16. Branding and Installable Web Shell Architecture

LANsurfer keeps a fixed, accessible visual system and bundled application branding. Templates consume centralized branding values instead of repeating constants. Bundle approved logos/mascot/font assets with the application; do not add a runtime font or branding CDN.

LANsurfer does not provide PostgreSQL-backed administrator-managed welcome text, contact link, uploaded logo, uploaded application icon, white-labeling, or theme controls. A deployment may expose only the minimum instance identity required to distinguish the installation, plus the operator legal identity described in §15.

Serve a manifest with a stable application identity/start URL and bundled/default presentation. A minimal service worker supports installation and Web Push; it does not promise offline application behavior.

PWA/instance identity capability owns service-worker registration, lifecycle, and update behavior. notification capability attaches Web Push behavior to that contract rather than creating a competing service worker or registration path.

---

## 17. Game Catalog and IGDB Architecture

PostgreSQL owns the authoritative local Game catalog. IGDB is an optional discovery/enrichment integration.

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

The bounded proxy core (validation hook, store, negative cache, singleflight, admission, deadline) lives in `internal/platform/mediaproxy` and is instantiated twice: IGDB covers and provider avatars. Both instances share one store (bucket `small`/`big`/`avatar`) and have separate negative caches and slots. `GET /media/avatar/{avatarKey}` is public, accepts only `^[a-f0-9]{32}$`, and on a cache miss the key must belong to a currently selected identity. The upstream URL comes only from that stored row: `https://cdn.discordapp.com/avatars/{subject}/{hash}.png?size=128` for Discord, the stored `https` URL for Twitch/OIDC. Limits, headers, and failure behavior equal the cover proxy. Because OIDC hosts are operator-configured, the avatar fetcher additionally refuses non-default ports and dials to loopback, private, link-local, unspecified, and multicast addresses, and uses no environment proxy.

---

## 18. Frontend Architecture

`templ` renders full pages and HTMX fragments. DaisyUI provides the stable base component vocabulary; Tailwind handles layout/local variations. Avoid another UI framework.
Icons are the Phosphor webfonts (Regular default, Fill for emphasized state), consumed as plain CSS classes in templ markup with `currentColor`; TypeScript does not manage icons.

TypeScript handles only browser-specific progressive enhancement such as SSE connection handling, toast/dialog helpers, installability/service-worker registration, and Web Push permission/subscription behavior. Do not implement domain decisions in TypeScript.

Time handling: persist and exchange Game Request times as absolute instants (`timestamptz`, RFC3339). The server never formats viewer-facing clocks with `time.Local()`; it renders a UTC fallback plus the RFC3339 `datetime` on a `<time>` element marked `data-local-time`, and `app.ts` rewrites clocks and hover titles into the browser's zone, including after HTMX/SSE swaps. A custom start is entered as a local date/time plus the browser's IANA zone (`timezone`) and parsed server-side with `time.LoadLocation`, so DST applies at the selected date rather than at submission time. Relative labels ("in 30 min", "now") are server-rendered from the same instant. Web Push formats planned time in the client and Discord uses a Unix timestamp; both stay viewer-local.

When PWA/instance identity and notifications both need browser changes, the PWA/instance identity capability owns the service-worker shell/lifecycle and the notification capability owns push-specific behavior.

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

Request logging records an `outcome` of `completed`, `client-aborted`, or `timed-out`, derived from the request context's cancellation cause rather than from the status code, which stays `200` for a response the client never received. The cause is attached by the request-timeout middleware, so that middleware must wrap request logging. Realtime stream open and close are logged with the number of streams the instance currently holds and a close reason, because long-lived `EventSource` connections consuming the browser's per-origin connection pool were the root cause in issue #15 and were not visible in the logs at all. Prometheus scraping is optional; LANsurfer requires no external observability service.

The production Compose example pins the LANsurfer image, does not publish PostgreSQL to the host, and retains only application + database as required services. TLS and reverse proxy remain operator responsibilities.

Migrations remain forward-only. Final supported `main`-revision upgrade, backup/restore, and key-backup validation belong to release integration after persistent migrations settle.

Supported production deployment must preserve required HTTP behavior through the chosen proxy/deployment path, including normal regression coverage for core interactions. Issue #15's reported hang is not an open architecture investigation requirement.

### Prometheus metrics

`internal/platform/metrics` owns one instance-local Prometheus registry, standard
Go/process collectors, bounded HTTP instrumentation, pgx pool statistics, and
scrape-time aggregate SQL. HTTP middleware wraps recovery and preserves streaming
interfaces; labels use chi route patterns, never raw paths or request-derived IDs.
Collection is limited to one scrape with a shared three-second SQL deadline. Optional
source failures omit those samples and emit a fixed-source collection-success gauge.
Normal HTTP instrumentation performs no SQL or metrics-source calls.

The composition root wires a metrics-only key store into the transport handler.
`metrics_access_key` has a checked singleton primary key and one 32-byte SHA-256 hash
of a cryptographically random 32-byte key. Rotation is one UPSERT. Authentication
validates canonical base64url, reads the hash on every scrape, and compares in constant
time: no cache delays cross-process revocation. Admin generation/rotation reuses
session/role and CSRF middleware, returns plaintext only in the no-store POST response,
and never places it in a URL, cookie, persistent state or log.

Durable product counters sum retained `origin=live` request activity and
`metrics_product_totals` in one read statement. That fixed-kind table seeds retained
backfill totals once and preserves anonymous live totals through a statement-level
activity-deletion trigger, including account cascades. It stores no user/request IDs
and is not a second activity history or notification handoff. Insertions require no
global counter lock, preserving independent request commit ordering; rolled-back
changes and later backfills do not count. Deletion and aggregate preservation commit
together. PostgreSQL warnings isolate exceptional preservation failures from account
deletion, at the cost of incomplete totals if operators damage this optional state.
Already-deleted or never-recorded pre-upgrade history cannot be recovered. Normal
source failures never enter domain writes. The existing `started` activity from the
first `open -> playing` transition supplies `lansurfer_games_started_total`.

All metrics are aggregate-only: no identities, raw URLs/query parameters, arbitrary
content, session IDs, chat/session-information values or credentials. See the
[operator configuration](../README.md#prometheus-metrics) for key handling, durability,
failure behavior and the scrape example.

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
- server-constructed IGDB cover and provider-avatar fetches with no third-party requests from player browsers
- encrypted Discord webhook credentials with key rotation/backup guidance
- Web Push payload data minimization and explicit permission/consent

Do not invent cryptographic constructions. Use established protocol behavior, Go standard-library AES-GCM for approved credential encryption, and the approved Web Push library behind narrow adapters. Operator and integration credentials, OAuth/client secrets, authentication/session-flow tokens, VAPID private keys, Discord webhook credentials, and encryption keys must never be logged or stored in recoverable plaintext. Secret Session Information is ordinary authorized application data: values remain plaintext in PostgreSQL and in authorized HTML, with presentation privacy only; this data is not application-level encrypted and must never be logged.

---

## 22. Architectural Enforcement

Where practical, CI/lint rules may enforce forbidden dependency directions. Business packages should not import concrete persistence adapters.

A dependency rule tool such as `depguard` may be considered if it remains simple and provides real enforcement value.

Parallel development makes boundaries especially important: contracts may be shared, implementations and persistence ownership may not.

---

## 23. Architectural Decision Policy

Follow the [approval rules](../AGENTS.md#changes-requiring-approval). Document approved durable architectural changes here, including the reason where it materially helps future maintenance. Small internal implementation choices do not need a separate decision record.

Planning labels and workstreams are not architectural layers. Do not create packages, frameworks, or abstractions whose only purpose is coordinating project management.
