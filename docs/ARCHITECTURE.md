# LANsurfer Architecture

## 1. Overview

LANsurfer is a self-hosted web application for finding people to play games with at LAN parties, gaming events, and gaming communities.

The application is designed as a **modular monolith** with server-side rendering and minimal infrastructure.

Primary goals:

- easy self-hosting
- low operational overhead
- long-term maintainability
- clear business-module boundaries
- strong PostgreSQL consistency
- progressive enhancement instead of SPA complexity
- a small number of runtime components

The intended runtime architecture for v0.1 is:

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

For v0.1, the event broker may run in memory.

The event publishing abstraction should remain replaceable so PostgreSQL `LISTEN/NOTIFY` can be used later if multiple LANsurfer application instances are deployed.

WebSockets are not part of the initial architecture.
Introducing them requires approval and a meaningful bidirectional use case.

---

## 5. Modular Monolith

The application is separated by business capability.

Initial module candidates:

```text
auth
user
game
event
gamerequest
```

Supporting technical areas:

```text
web
query
platform
app
```

A likely initial structure is:

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
├── event/
├── gamerequest/
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

### `auth`

Owns:

- external identities
- authentication flow
- browser sessions

### `game`

Owns:

- games
- aliases
- external IDs
- game subscriptions
- admin game catalog behavior

### `event`

Owns:

- LAN/gaming events

### `gamerequest`

Owns:

- game requests
- request membership
- request lifecycle
- capacity rules

A module must not directly update another module's tables merely because they are in the same database.

---

## 8. Cross-Module Communication

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

## 11. GameRequest Concurrency

Joining a GameRequest is a concurrency-sensitive operation.

If `MaxPlayers` is defined, concurrent joins must never allow the member count to exceed the limit.

The implementation must use a PostgreSQL transaction and appropriate locking rather than:

1. read count
2. check in application memory
3. insert

without protection.

The exact SQL/locking strategy is intentionally left to implementation, but must be covered by PostgreSQL integration tests.

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

---

## 13. Authentication Architecture

LANsurfer separates local users from external identities.

```text
LANsurfer User
      |
      +-- Identity: OIDC provider / subject
      |
      +-- future additional identities
```

The first authentication mechanism is generic OIDC.

The initial deployment uses Keycloak, but application/domain code must not assume Keycloak.

Do not use external provider IDs as LANsurfer user IDs.

Do not automatically merge identities based solely on email addresses.

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
```

`LANSURFER_OIDC_CLIENT_SECRET` is optional for public clients using PKCE and is
required when the provider configures LANsurfer as a confidential client.
Opaque browser session tokens are generated randomly and stored only as hashes;
they do not require a separate application session secret.

Product/runtime customization may later be stored in PostgreSQL.

Do not introduce a config file format unless a concrete requirement appears.

---

## 16. Branding Architecture

Keep the product name, tagline, semantic colors, and asset paths in one small centralized value/object near the web composition layer. Templates must consume it rather than repeat constants. Tailwind/DaisyUI theme definitions and CSS custom properties must map to the same semantic color roles in both dark and light modes.

Bundle approved logos, mascot artwork, and Montserrat/JetBrains Mono webfonts with the production assets. They must be served from the embedded application; do not add a runtime image or font CDN. The wordmark remains text until an approved production signet is available.

Branding is fixed application data in v0.1, not environment configuration or PostgreSQL state. This boundary should still allow future database-backed instance branding without a frontend rewrite.

See [Product branding](PRODUCT.md#20-branding) for the fixed v0.1 branding and customization scope, and the [visual branding guide](BRANDING.md) for the canonical design tokens and usage rules.

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

Future provider boundary:

```go
type CatalogProvider interface {
    Search(ctx context.Context, query string) ([]CatalogGame, error)
    Get(ctx context.Context, externalID string) (CatalogGame, error)
}
```

Do not implement scraping.

Do not couple the primary `games` table to specific providers.

Use a generic `game_external_ids` relation.

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

Do not implement domain/business decisions in TypeScript.

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

Additional observability is deferred.

---

## 21. Security Boundaries

Important baseline controls:

- OIDC state validation
- PKCE when applicable
- secure session cookies
- CSRF protection for browser writes
- input validation
- templ escaping
- security headers
- careful trusted-proxy configuration

Do not implement custom cryptography.

---

## 22. Architectural Enforcement

Where practical, CI/lint rules may enforce forbidden dependency directions.

For example, business packages should not import concrete persistence adapters.

A dependency rule tool such as `depguard` may be considered if it remains simple and provides real enforcement value.

Architecture documentation alone should not be the only defense against accidental boundary erosion.

---

## 23. Architectural Decision Policy

Follow the [approval rules](../AGENTS.md#constraints-requiring-approval). Document approved architectural changes here or in a short ADR, including the reason for the decision. Small internal implementation choices do not need an ADR.
