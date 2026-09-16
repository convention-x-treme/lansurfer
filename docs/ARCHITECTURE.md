# LANsurfer Architecture

## 1. Overview

LANsurfer is a self-hosted web application for finding people to play games with at LAN parties and gaming events.

The application is designed as a **modular monolith** with server-side rendering and minimal infrastructure.

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

This document records both the implemented prototype baseline and the approved 1.0 target. The [roadmap development model](ROADMAP.md#development-model) defines which 1.0 tracks may proceed in parallel and where integration dependencies apply. Target descriptions do not authorize post-1.0 work.

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

- list games
- follow/unfollow a game
- create a game request
- join/leave a request
- close/cancel a request
- edit basic profile/admin data

The preferred flow is:

```text
HTMX request
   -> Go handler
   -> service/query
   -> PostgreSQL
   -> templ component
   -> HTML fragment
```

This avoids maintaining:

- duplicate frontend/backend models
- a frontend router
- a client-side state architecture
- an internal JSON API only used by the same application
- duplicated validation/business rules

TypeScript is intentionally limited to progressive enhancement.

---

## 4. Realtime Model

SSE is used for realtime updates.

Browser-to-server communication remains ordinary HTTP.

Server-to-browser communication uses one or more SSE streams.

Example:

```text
User A
  |
  | POST /game-requests
  v
GameRequest Service
  |
  | transaction
  v
PostgreSQL
  |
  | GameRequestCreated
  v
Event Publisher
  |
  +--> SSE broker
        |
        +--> Browser B
        +--> Browser C
```

For the prototype and single-instance 1.0 release, the SSE event broker may run in memory. Durable external notifications use a PostgreSQL outbox and do not make SSE durable.

The event publishing abstraction should remain replaceable so PostgreSQL `LISTEN/NOTIFY` can be used later if multiple LANsurfer application instances are ever approved. Multi-instance/high-availability operation is not part of 1.0.

WebSockets are not part of the initial architecture. Introducing them requires approval and a meaningful bidirectional use case.

### Durable external notifications (Track E)

Game Request writes and durable notification intent commit together in the Game Request module's PostgreSQL transaction. A background worker claims outbox work and calls narrow transport adapters owned by the notification module.

```text
Game Request transaction
  ├── domain write
  └── outbox record
          |
          v
notification worker
  ├── Web Push adapter
  └── Discord webhook adapter
```

Track E may implement the worker model, retries, delivery persistence, and transport adapters in parallel with Track A. The final outbox hookup waits for Track A to stabilize Game Request write paths and transaction boundaries.

Persist delivery status, retry schedule, and external message references. External failure never fails or reverses the originating Game Request action. Honor `Retry-After` for HTTP 429 responses; use capped exponential backoff with jitter for network and 5xx failures; mark permanent or exhausted failures for authorized manual retry.

Web Push uses VAPID behind a narrow adapter around the approved `github.com/marknefedov/go-webpush/v2` dependency. Store multiple labeled subscriptions and remove invalid endpoints when the push service reports permanent expiration. Push payload construction enforces the privacy and targeting rules in [PRODUCT.md](PRODUCT.md#12-live-and-durable-notifications).

Discord delivery uses the `webhook.incoming` OAuth flow rather than pasted URLs or a bot/gateway. Encrypt webhook IDs and tokens at rest with AES-GCM and an operator-provided 32-byte key. Execute new messages with `wait=true`, retain the returned message ID, edit the same message for later changes, sanitize content, and send an empty `allowed_mentions` policy.

---

## 5. Modular Monolith

The application is separated by business capability.

Business modules through the prototype are:

```text
auth
user
game
event
gamerequest
```

The approved 1.0 target adds:

```text
notification
```

The prototype `event` module may remain while Track A preserves/migrates legacy Event-linked data, but normal 1.0 Game Request behavior must not depend on Event selection, membership, lifecycle, or authorization. `clan` is post-1.0 and must not be introduced until that work is explicitly scheduled.

Supporting technical areas:

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
│   ├── app.go
│   └── routes.go
│
├── auth/
├── user/
├── game/
├── event/         # prototype/migration compatibility only for 1.0
├── gamerequest/
├── notification/  # Track E
│
├── query/
│
├── web/
│   ├── handler/
│   ├── middleware/
│   ├── pages/
│   └── components/
│
└── platform/
    ├── postgres/
    ├── sse/
    ├── config/
    └── logging/
```

This is guidance rather than a mandatory exact tree.

Do not create empty architecture packages solely to match this diagram.

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

### Consumer-owned interfaces

Interfaces should generally live where they are consumed.

Example:

```go
package gamerequest

type Repository interface {
    Create(ctx context.Context, request GameRequest) (GameRequest, error)
    ByID(ctx context.Context, id ID) (GameRequest, error)
}
```

The PostgreSQL adapter implements this interface without the domain depending on PostgreSQL.

---

## 7. Module Ownership

Each module owns its writes.

Examples:

### `user`

Owns:

- users
- user profile fields
- internal LANsurfer roles
- Game Nicks, biographies, and per-Game handles

### `auth`

Owns:

- external identities
- authentication flow
- browser sessions
- one-use provider authentication/link flows

### `game`

Owns:

- games
- aliases
- external IDs
- game subscriptions
- admin game catalog behavior

### `event` (prototype compatibility)

Owns the existing prototype Event data while it remains in the schema. Track A decides the concrete forward migration needed to preserve prototype Game Requests while removing Event requirements from normal 1.0 behavior. Do not expand Event into new 1.0 membership, invitation, seat, lifecycle, or access-control features.

### `gamerequest`

Owns:

- game requests
- request membership
- request lifecycle
- capacity rules
- transactional notification outbox records describing Game Request changes

### `notification` (Track E)

Owns:

- Web Push subscriptions and global preferences
- the single instance-level Discord destination
- delivery attempts, retries, and external message references

The notification module consumes durable Game Request outbox work. It does not own or write Game Requests.

A module must not directly update another module's tables merely because they are in the same database.

---

## 8. Cross-Module and Cross-Track Communication

When a module needs another module's behavior, prefer a small explicit interface.

Example:

`gamerequest` needs to ensure a referenced game exists.

It may define:

```go
type GameLookup interface {
    Exists(ctx context.Context, id GameID) (bool, error)
}
```

The `game` service can satisfy this interface.

This is preferred over:

```text
gamerequest -> game repository implementation
```

or:

```text
gamerequest -> postgres generated queries
```

Parallel roadmap tracks follow the same principle. Agree on the smallest stable contract and let each track own its implementation.

The main 1.0 cross-track contracts are:

- **Track A → Track E:** Track A owns Game Request write transactions; Track E consumes durable outbox records. Track E may develop workers/transports first, but final outbox persistence belongs inside Track A's transaction boundary.
- **Track B internal:** OIDC and Discord identities both resolve to the same local User. Game Nick onboarding is provider-independent and happens after authentication when required.
- **Track D → Track E:** Track D owns service-worker registration/lifecycle. Track E owns push permission, subscriptions, push handling, and payload behavior. Backend Web Push work does not need to wait for Track D.
- **Track F:** release automation can proceed independently; only final migration/backup/restore validation waits for the persistent schema from the other tracks to settle.

Do not solve these contracts by sharing persistence implementations or creating generic cross-track frameworks.

---

## 9. Read Models / CQRS-light

Cross-module joins are allowed for read-only UI use cases.

Example dashboard:

```text
GameRequest
Game
Creator
Member count
Current user joined state
```

Rather than chaining several domain services only to build a page, use a dedicated query/read model.

Conceptually:

```text
web handler
   |
   v
dashboard query
   |
   v
PostgreSQL JOIN
   |
   v
DashboardViewModel
   |
   v
templ
```

Rules:

- query/read models may read multiple module tables
- they do not perform business writes
- they return UI/read-specific models
- they are not domain entities

This is intentionally lightweight and does not imply event sourcing or a full CQRS architecture.

---

## 10. PostgreSQL and sqlc

PostgreSQL is the only supported database for the initial architecture.

Use:

- explicit SQL
- sqlc code generation
- pgx

Generated sqlc types stay inside the persistence/read layer where practical.

Business modules should not depend on generated database row types.

Important invariants should be enforced with PostgreSQL where appropriate.

Examples:

- foreign keys
- unique constraints
- transaction isolation
- row locks
- check constraints

---

## 11. GameRequest and Identity Concurrency

Joining a GameRequest is a concurrency-sensitive operation.

If `MaxPlayers` is defined, concurrent joins must never allow the member count to exceed the limit.

The implementation must use a PostgreSQL transaction and appropriate locking rather than:

1. read count
2. check in application memory
3. insert

without protection.

The exact SQL/locking strategy is intentionally left to implementation, but must be covered by PostgreSQL integration tests.

The 1.0 identity work also requires case-insensitive Game Nick uniqueness to hold under concurrent onboarding/profile changes. Enforce this with an appropriate PostgreSQL constraint/index and test it at the persistence boundary.

1.0 does not add Event-seat, Open Play, Clan ownership, Clan membership, or other post-1.0 invariants. Do not preserve those concepts through speculative constraints or abstractions.

---

## 12. Migration Architecture

Use goose.

Migration files are embedded into the application binary.

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

Migration failure is fatal.

Down migrations are never run automatically.

This is optimized for the self-hosting workflow:

```bash
docker compose pull
docker compose up -d
```

An updated binary should bring the schema forward automatically.

Tracks may add independent forward migrations in parallel. Keep migrations narrowly scoped to the owning module/track and avoid modifying another track's tables except through an explicitly approved migration contract. Track F's final prototype-to-1.0 upgrade validation runs only after the integrated migration sequence is stable.

---

## 13. Authentication Architecture

LANsurfer separates local users from external identities.

```text
LANsurfer User
      |
      +-- Identity: OIDC provider / subject
      |
      +-- Identity: Discord provider / subject (Track B)
```

The implemented prototype mechanism is generic OIDC. The 1.0 target also supports Discord OAuth2 authorization-code login as an independent provider while retaining OIDC.

The initial deployment uses Keycloak, but application/domain code must not assume Keycloak.

Do not use external provider IDs as LANsurfer user IDs.

Do not automatically merge identities based solely on email addresses.

Linking and unlinking are explicit authenticated account operations, and an account must retain at least one identity. Persist short-lived, one-use authentication/link flow records bound to the initiating browser, provider, operation, and current user where applicable. Keep the existing OIDC callback and use a provider-specific Discord callback.

Both authentication providers resolve to the same local User/session model. After authentication, onboarding checks such as required Game Nick selection are provider-independent; provider-specific handlers must not duplicate user-profile business rules.

Provider names and email addresses are private account data. Normal player and global administrator views must not expose them to anyone except the account owner.

---

## 14. Sessions

Use server-side sessions stored in PostgreSQL.

Conceptual structure:

```text
auth_sessions
- id
- user_id
- expires_at
- created_at
- last_seen_at
```

The browser receives only an opaque, high-entropy random session ID in a cookie with bounded lifetime, `HttpOnly`, `SameSite`, and `Secure` in production.

No JWT browser sessions.

Sessions must be revocable server-side.

---

## 15. Configuration Architecture

Infrastructure configuration is environment-based.

Examples:

```text
LANSURFER_DATABASE_URL
LANSURFER_BASE_URL
LANSURFER_LISTEN_ADDR
LANSURFER_OIDC_ISSUER
LANSURFER_OIDC_CLIENT_ID
LANSURFER_OIDC_CLIENT_SECRET
LANSURFER_DISCORD_CLIENT_ID
LANSURFER_DISCORD_CLIENT_SECRET
LANSURFER_DATA_ENCRYPTION_KEY
LANSURFER_WEBPUSH_PUBLIC_KEY
LANSURFER_WEBPUSH_PRIVATE_KEY
LANSURFER_IGDB_CLIENT_ID
LANSURFER_IGDB_CLIENT_SECRET
```

`LANSURFER_OIDC_CLIENT_SECRET` is optional for public clients using PKCE and is required when the provider configures LANsurfer as a confidential client. Opaque browser session tokens are generated randomly and stored only as hashes; they do not require a separate application session secret.

Instance display data and uploaded identity assets are product state stored in PostgreSQL. Provider credentials, the 32-byte data-encryption key, and VAPID private key remain operator configuration and must not be stored as ordinary product settings.

Do not introduce a config file format unless a concrete requirement appears.

---

## 16. Branding and Installable Web Shell Architecture

Keep the product name, tagline, semantic colors, the operating community's name and public URL for the **Powered by …** attribution, and asset paths in one small centralized value/object near the web composition layer. Templates must consume it rather than repeat constants. Tailwind/DaisyUI theme definitions and CSS custom properties must map to the same semantic color roles in both dark and light modes.

The rendered document carries the visitor's theme. An explicit light/dark choice is a non-essential preference cookie that the web layer reads into the request context, and the layout writes it onto `<html>`; without a choice the element carries no theme attribute and the stylesheet's `prefers-color-scheme` default applies. The Content-Security-Policy forbids inline scripts, so a script that applied the theme after load would repaint every navigation; keep theme selection out of the startup path and let the toggle only update the live document and the cookie.

Bundle approved LANsurfer logos, mascot artwork, and the Schibsted Grotesk (upright and italic) and JetBrains Mono webfonts with the production assets. They must be served from the embedded application; do not add a runtime font or branding CDN. The wordmark remains text until an approved production signet is available.

The prototype branding is fixed application data. Track D adds PostgreSQL-backed instance display name, welcome text, contact link, logo, and icon, controlled by global administrators. Uploaded PNG assets are validated at the documented byte and pixel limits and stored in PostgreSQL. Colors, typography, accessibility behavior, component semantics, and the fixed “Powered by Convention-X-Treme” operator attribution remain fixed.

Serve a dynamic manifest with a stable application ID and start URL. Omit icon entries when no valid icon is configured and use a generated monogram in the regular UI. A minimal service worker supports installation and later Web Push, not offline application guarantees.

Track D owns service-worker registration, lifecycle, and update behavior. Track E attaches Web Push behavior to that contract rather than creating a competing service worker or independent registration path.

See [Product branding](PRODUCT.md#14-instance-identity-and-branding) for the 1.0 customization boundary and the [visual branding guide](BRANDING.md) for canonical design tokens and usage rules.

---

## 17. Game Catalog Architecture

PostgreSQL owns the local game catalog.

External services are optional import/enrichment providers.

```text
External Catalog
      |
      | explicit admin import
      v
LANsurfer Game
      |
      v
normal local operation
```

LANsurfer must continue to function without external catalog access.

Provider boundary:

```go
type CatalogProvider interface {
    Search(ctx context.Context, query string) ([]CatalogGame, error)
    Get(ctx context.Context, externalID string) (CatalogGame, error)
}
```

Do not implement scraping.

Do not couple the primary `games` table to specific providers.

Use a generic `game_external_ids` relation.

Track C supplies an optional IGDB adapter authenticated with operator-provided Twitch credentials. Administrator actions search and preview provider results, then either create a local Game or explicitly attach an external ID. Refresh is also previewed and applies only administrator-selected fields. Never merge by name or silently overwrite local edits.

The local model may store a summary, first release date, platform names, and one cover reference. Catalog and Game Request workflows must continue when IGDB is unconfigured or unavailable.

IGDB covers are the only approved remote content exception. Allow images only from `images.igdb.com` in the Content Security Policy, set `Referrer-Policy: no-referrer`, and render a local placeholder on failure. Loading the cover still discloses the viewer's IP address to IGDB and must be documented. Do not generalize this into a remote-asset system or cover proxy/cache.

Track C may implement the catalog/provider work independently. Rendering new metadata inside Game Request read models/cards may be integrated after Track A stabilizes the affected read-model/template surface.

---

## 18. Frontend Architecture

### templ

`templ` renders full pages and HTMX fragments.

Example conceptual component hierarchy:

```text
pages/
  dashboard.templ
  games.templ
  game_request.templ
  admin_games.templ

components/
  navigation.templ
  game_card.templ
  game_request_card.templ
  avatar.templ
  alert.templ
```

### DaisyUI and Tailwind

DaisyUI provides the stable base component vocabulary.

Tailwind may handle layout and local variations.

Avoid introducing another UI framework.

### TypeScript

TypeScript handles only browser-specific behavior.

Likely examples:

- SSE connection handling
- toast behavior
- dialog helpers
- installability/service-worker registration
- Web Push permission/subscription progressive enhancement

Do not implement domain/business decisions in TypeScript.

When Tracks D and E both need browser changes, Track D owns the service-worker shell/lifecycle and Track E owns push-specific behavior. Keep the integration contract narrow to avoid parallel branches replacing the same browser infrastructure.

---

## 19. Static Assets

Build frontend assets during development/CI.

Production artifacts are embedded using `go:embed`.

The deployed application should not require:

- Node.js
- npm/pnpm
- Tailwind CLI
- templ CLI
- esbuild

These are build-time concerns only.

---

## 20. Operations

Normal self-hosting:

```text
Docker Compose
├── lansurfer
└── postgres
```

Goal:

```bash
docker compose up -d
```

and the service becomes usable after startup/migrations.

The standalone binary must also support an externally managed PostgreSQL instance.

Provide:

- structured logging via `log/slog`
- `/healthz`
- `/readyz`
- `lansurfer version`
- healthcheck, data-encryption-key generation, and VAPID key-generation commands

Additional observability is deferred.

Track F may build release automation and operator tooling in parallel with feature tracks. For 1.0, publish Linux amd64/arm64 binaries and a matching multi-architecture GHCR image using native Go tooling and pinned official Docker/GitHub Actions. Release outputs include checksums, SBOMs, and provenance attestations. Stable releases receive exact, major.minor, major, and `latest` tags; prereleases receive exact tags only.

The production Compose example pins the LANsurfer image, does not publish PostgreSQL to the host, and retains only the application and database as required services. TLS and reverse-proxy operation are documented operator responsibilities.

Migrations remain forward-only. Final prototype-to-1.0 upgrade, backup/restore, and key-backup validation belong to the Integration Gate after persistent migrations from the feature tracks have settled. Document the supported path for each release rather than promising arbitrary direct upgrades across all 1.x versions.

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
- fixed CSP allowlisting for the IGDB cover exception
- encrypted Discord webhook credentials with key rotation/backup guidance
- push payload data minimization

Do not invent cryptographic constructions. Use established protocol behavior, Go standard-library AES-GCM for approved credential encryption, and the approved Web Push library behind narrow adapters. Secrets and raw session/authentication-flow tokens must never be logged or stored in recoverable plaintext.

---

## 22. Architectural Enforcement

Where practical, CI/lint rules may enforce forbidden dependency directions.

For example, business packages should not import concrete persistence adapters.

A dependency rule tool such as `depguard` may be considered if it remains simple and provides real enforcement value.

Architecture documentation alone should not be the only defense against accidental boundary erosion.

Parallel-track development makes these boundaries more important: contracts may be shared, implementations and persistence ownership may not. CI should continue to catch boundary violations regardless of which track introduced them.

---

## 23. Architectural Decision Policy

Follow the [approval rules](../AGENTS.md#constraints-requiring-approval). Document approved architectural changes here or in a short ADR, including the reason for the decision. Small internal implementation choices do not need an ADR.

The roadmap's parallel tracks are planning boundaries, not new architectural layers. Do not create track packages, track-specific frameworks, or abstractions whose only purpose is coordinating project management.
