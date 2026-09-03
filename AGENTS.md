# AGENTS.md

# LANsurfer — Codex Development Rules

This file defines the rules Codex and other coding agents must follow when working on LANsurfer.

LANsurfer is intentionally designed as a small, maintainable, self-hosted modular monolith. Prefer boring, explicit, idiomatic Go over clever abstractions.

If a task conflicts with this file, stop and explain the conflict before changing the architecture.

---

## 1. Product Context

LANsurfer is an open-source Looking-for-Group application for LAN parties, gaming events, and gaming communities.

The initial real-world target is the CXT LAN party with roughly 400 participants.

The first prototype is **web-only**.

The core flow is:

1. A user logs in.
2. The user follows games they are interested in.
3. A user creates a `GameRequest`.
4. Other interested users immediately see the request.
5. Users join the request.
6. The group starts playing.

The first version should optimize for:

- simple self-hosting
- low maintenance
- clear code
- predictable upgrades
- few infrastructure dependencies
- server-side rendering
- progressive enhancement
- strong module boundaries
- easy onboarding for contributors

---

## 2. Required Technology Stack

Use the following stack unless an explicit architectural decision changes it.

### Backend

- Go
- `chi` for HTTP routing

### Frontend

- Server-side rendering
- `templ`
- HTMX
- Tailwind CSS
- DaisyUI
- TypeScript only where client-side behavior is actually needed
- `esbuild` for TypeScript/JavaScript bundling
- native browser functionality where practical

### Realtime

- Server-Sent Events (SSE)

Use ordinary HTTP/HTMX requests from browser to server and SSE from server to browser.

### Database

- PostgreSQL
- `pgx`
- `sqlc`

### Database migrations

- `goose`
- migrations embedded into the Go binary
- automatic forward migrations on application startup
- never automatically perform down migrations

### Build and developer tooling

- Taskfile
- `go:embed` for production frontend assets and migrations
- Docker
- Docker Compose

### Authentication

- generic OpenID Connect
- initial deployment uses Keycloak
- LANsurfer owns local users
- no local password authentication
- server-side PostgreSQL sessions
- opaque secure session cookies

---

## 3. Explicitly Forbidden by Default

Do **not** introduce any of the following unless the current task has a concrete requirement and the architectural change has been explicitly approved:

- React
- Vue
- Svelte
- Angular
- Next.js
- Nuxt
- SPA architecture
- frontend state-management frameworks
- GraphQL
- GORM
- another ORM
- Redis
- RabbitMQ
- Kafka
- NATS
- microservices
- Kubernetes-specific application architecture
- JWT browser sessions
- local password authentication
- Makefiles
- dependency injection frameworks
- event sourcing
- full CQRS frameworks
- generic repository frameworks
- generic DTO/mapping frameworks
- large JavaScript component frameworks
- additional UI component libraries beyond DaisyUI
- Sass
- Less
- business logic implemented in TypeScript
- external CDN dependencies for production assets
- provider-specific assumptions for OIDC authentication
- scraping third-party gaming websites

Adding dependencies "for future flexibility" is not sufficient justification.

---

## 4. Architecture Style

LANsurfer is a **modular monolith**.

Use pragmatic ideas from:

- Hexagonal Architecture
- Vertical Slice Architecture
- Domain-driven modular boundaries
- CQRS-light for read models

Do not implement these patterns dogmatically.

### Core rule

> Business modules own their behavior and writes. Infrastructure is behind explicit boundaries. Cross-module reads may use dedicated query/read models.

Organize code primarily by business capability rather than by technical layer.

Likely modules include:

- `auth`
- `user`
- `game`
- `event`
- `gamerequest`

Infrastructure and web delivery remain separate.

---

## 5. Module Boundary Rules

### 5.1 Modules own their persistence needs

A business module defines the small interfaces it needs.

Example:

```go
package gamerequest

type Repository interface {
    Create(ctx context.Context, request GameRequest) (GameRequest, error)
    ByID(ctx context.Context, id ID) (GameRequest, error)
}
```

Interfaces should normally be defined by the **consumer**, not the implementation package.

### 5.2 Do not access another module's persistence implementation

Forbidden example:

```go
gamerequest.Service -> postgres.GameQueries
```

Also forbidden:

```go
gamerequest.Service -> game.Repository
```

If `gamerequest` needs game information, define a small consumer-owned capability:

```go
type GameLookup interface {
    Exists(ctx context.Context, id GameID) (bool, error)
}
```

### 5.3 Writes go through the owning module

A module must not directly update another module's tables as an incidental side effect.

Use:

- the owning service
- a small explicit interface
- or an application/domain event for side effects

### 5.4 Cross-module reads are allowed through a query layer

Complex UI screens may need data from multiple modules.

Use dedicated read models under a query/read package.

Example:

```text
HTTP
  -> Dashboard Query
  -> PostgreSQL JOIN
  -> Dashboard ViewModel
  -> templ
```

The query layer may read across module-owned tables.

It must not implement domain writes.

### 5.5 sqlc types stay in infrastructure

Generated sqlc database types must not become application-wide domain types.

Map them to:

- domain models
- read models
- explicit values

when crossing the infrastructure boundary.

### 5.6 Avoid "shared", "common", and "utils" dumping grounds

Do not create generic catch-all packages.

Create narrowly named packages only when there is a real reusable concept.

---

## 6. Dependency Injection

Use explicit Go construction.

Do not introduce a DI framework.

The application composition root may know all modules and implementations.

Conceptual example:

```go
db := postgres.New(...)
events := eventbus.New(...)

gameRepo := postgres.NewGameRepository(db)
gameService := game.NewService(gameRepo)

requestRepo := postgres.NewGameRequestRepository(db)
requestService := gamerequest.NewService(
    requestRepo,
    gameService,
    events,
)

app := web.New(...)
```

Explicit wiring is preferred over reflection, code-generation-based DI, service locators, or magic.

---

## 7. HTTP and SSR Rules

HTTP handlers should stay thin.

Handlers may:

- parse route/form input
- perform syntactic validation
- obtain the authenticated user
- call a service/query
- translate known domain errors to HTTP responses
- render `templ` pages/components

Handlers must not contain business rules.

The default UI interaction model is:

```text
Browser
  -> HTMX request
  -> Go handler
  -> service/query
  -> PostgreSQL
  -> templ
  -> HTML response
```

Do not create an internal JSON API solely for the LANsurfer web frontend.

External APIs can be added later when there is an actual external consumer.

---

## 8. Realtime Rules

Use SSE for server-to-browser realtime updates.

Browser-to-server actions remain normal HTTP requests.

Conceptual flow:

```text
POST /game-requests
  -> persist
  -> publish GameRequestCreated
  -> SSE
  -> connected browsers refresh/update
```

For v0.1, an in-memory event broker is acceptable.

Keep the event publishing boundary small enough that a later PostgreSQL `LISTEN/NOTIFY` implementation can be introduced if multiple LANsurfer application instances are needed.

Do not use WebSockets unless a future feature requires meaningful bidirectional realtime communication.

---

## 9. Frontend Rules

### 9.1 templ owns server-rendered components

Business/domain-specific UI components belong in `templ`.

Examples:

- Game card
- Game request card
- navigation
- user avatar
- subscription control
- admin game editor

### 9.2 DaisyUI is the base UI vocabulary

Use DaisyUI and Tailwind for styling.

Do not add another component system unless explicitly approved.

### 9.3 Prefer native browser primitives

Prefer native:

- `<dialog>`
- `<details>`
- `<form>`
- `<select>`
- `<input>`
- Popover API where appropriate

before introducing JavaScript abstractions.

### 9.4 TypeScript is progressive enhancement

TypeScript may handle browser-specific behavior such as:

- SSE client handling
- dialogs
- toasts
- browser notifications later
- small UI enhancements

Do not duplicate domain/business rules in TypeScript.

The server is authoritative.

### 9.5 No external runtime CDNs

Production CSS, JavaScript, icons, and other required frontend assets should be bundled and embedded in the Go binary.

---

## 10. Database Rules

Use PostgreSQL intentionally.

Use PostgreSQL features when they solve a concrete problem, but avoid unnecessary database-specific cleverness.

Use `pgx` for runtime database access and `sqlc` for typed SQL.

Prefer explicit SQL over ORM abstractions.

Important invariants must be protected at the database level where practical using:

- constraints
- unique indexes
- transactions
- row locking
- foreign keys

Do not rely exclusively on browser validation or in-memory checks.

---

## 11. Migration Rules

Use goose.

Migration files are part of the source repository and are embedded into the binary.

Normal startup should:

1. load configuration
2. connect/retry PostgreSQL
3. acquire a migration lock
4. apply pending UP migrations
5. initialize the application
6. start serving HTTP

If a migration fails, the HTTP server must not start.

Use a PostgreSQL advisory lock so multiple application processes cannot run migrations concurrently.

Never automatically perform DOWN migrations.

Manual migration/status CLI commands may be added when useful.

---

## 12. Authentication Rules

LANsurfer owns a local `User`.

External providers authenticate identities.

The initial provider is generic OIDC; the first real deployment uses Keycloak.

Do not hard-code Keycloak concepts into the domain.

A user may eventually have multiple external identities.

Do not use provider IDs as LANsurfer user IDs.

Do not automatically merge identities solely because email addresses match.

Do not implement local passwords.

Do not implement password reset, password complexity, or password MFA.

Possible future identity mechanisms include:

- Discord OAuth2
- passkeys
- temporary guest access

These are not part of the first prototype unless explicitly requested.

---

## 13. Session Rules

Browser sessions are server-side.

Store session state in PostgreSQL.

Use an opaque high-entropy session cookie.

Use appropriate settings:

- HttpOnly
- Secure in production
- SameSite
- bounded lifetime

Sessions must be revocable server-side.

Do not use JWTs for browser sessions.

---

## 14. Domain Terminology

Use explicit, boring terminology in code and the database.

Do not use surfing/branding words in the domain model.

Use:

- `User`
- `Identity`
- `AuthSession`
- `Game`
- `GameAlias`
- `GameExternalID`
- `GameSubscription`
- `Event`
- `GameRequest`
- `GameRequestMember`

Do not use domain terms such as:

- Wave
- Crew
- Spot

Branding may use surf metaphors in marketing/UI copy later, but domain terminology must remain understandable without knowing LANsurfer branding.

---

## 15. Game Catalog Rules

The local PostgreSQL game catalog is authoritative.

For v0.1, admins should be able to maintain games manually.

External sources are optional future import/enrichment providers.

A future provider may implement something conceptually similar to:

```go
type CatalogProvider interface {
    Search(ctx context.Context, query string) ([]CatalogGame, error)
    Get(ctx context.Context, externalID string) (CatalogGame, error)
}
```

External sources must not be required during normal LANsurfer operation.

Do not scrape external websites.

Do not add provider-specific fields such as `steam_id` or `igdb_id` to `games`.

Use a generic external ID relation.

---

## 16. v0.1 Scope Rules

The first prototype is web-only.

Include:

- OIDC authentication
- local users
- PostgreSQL sessions
- games
- game aliases
- game subscriptions
- events
- game requests
- game request membership
- admin game management
- SSR/HTMX UI
- SSE live updates
- basic admin/user roles
- Docker Compose self-hosting

Do not include unless separately approved:

- Discord bot
- Discord notifications
- Twitch integration
- Web Push
- mobile app
- persistent notification center
- teams
- chat
- ratings
- skill/rank matchmaking
- tournament management
- customizable instance branding
- external game provider integration

---

## 17. Branding Rule for v0.1

Custom branding is explicitly deferred.

For v0.1:

- use fixed LANsurfer branding
- keep branding values centralized
- do not scatter names/colors/asset paths through templates
- no branding tables
- no logo upload
- no theme editor

The design should allow database-backed per-instance branding later without a frontend rewrite.

Branding will be **per instance**, not per Event.

---

## 18. Error Handling

Prefer explicit domain errors.

Examples:

- `ErrNotFound`
- `ErrAlreadyJoined`
- `ErrGameRequestClosed`
- `ErrGameRequestFull`
- `ErrForbidden`

Translate domain errors to HTTP behavior in the web layer.

Do not encode HTTP semantics into domain packages.

---

## 19. Testing Rules

Testing should be pragmatic.

Prefer:

- unit tests for meaningful service/domain behavior
- PostgreSQL integration tests for database behavior
- concurrency tests for GameRequest joining/capacity
- `httptest` for important HTTP behavior

Avoid excessive mocking.

Do not optimize for an arbitrary coverage percentage.

Test behavior where regressions would matter.

---

## 20. Observability and Operations

Use `log/slog` unless there is a concrete reason not to.

Use structured logs.

Provide basic endpoints such as:

- `/healthz`
- `/readyz`

Do not add a full metrics/tracing platform in v0.1 unless required.

---

## 21. Configuration Rules

Prefer environment variables for technical/deployment configuration.

Examples:

- `LANSURFER_DATABASE_URL`
- `LANSURFER_BASE_URL`
- `LANSURFER_LISTEN_ADDR`
- `LANSURFER_SESSION_SECRET`
- OIDC issuer/client configuration

Do not add YAML/TOML configuration until there is a concrete need.

Runtime/product customization may later live in PostgreSQL, but that is out of scope for v0.1.

---

## 22. Build Rules

Do not create a Makefile.

Use Taskfile for developer workflows.

Expected tasks should be simple and discoverable, for example:

```text
task dev
task build
task test
task generate
task lint
```

Taskfile should orchestrate ordinary tools rather than hide complex custom behavior.

A production LANsurfer build should contain the generated frontend assets and migrations inside the Go binary.

---

## 23. Dependency Policy

Before adding a new direct dependency, ask:

1. Does the standard library already solve this well enough?
2. Does the dependency solve a current problem?
3. Is it actively maintained?
4. Is its API small and understandable?
5. Does it introduce another architectural paradigm?
6. Does it increase runtime/self-hosting complexity?

Prefer fewer dependencies.

Do not introduce dependencies only because they are popular.

---

## 24. Decision Policy for Codex

Codex may independently decide small implementation details such as:

- exact filenames
- private helper function names
- minor package internals
- query naming
- Taskfile task decomposition
- straightforward validation details
- internal test organization

Codex should **not** independently change:

- technology stack
- module boundaries
- database choice
- authentication model
- deployment model
- public domain terminology
- security model
- major v0.1 scope
- licensing model

If such a change appears necessary, explain the issue and request approval first.

---

## 25. Development Principle

The most important implementation rule is:

> Do not improve the architecture by adding abstractions or dependencies unless the current implementation has a concrete problem that they solve.

Prefer explicit code over hidden magic.

Prefer duplication over the wrong abstraction.

Prefer a small understandable module over a generic framework.

Keep LANsurfer boring enough that another maintainer can understand it years from now.
