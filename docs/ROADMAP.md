# LANsurfer Roadmap

This roadmap defines implementation milestones for the first LANsurfer prototype.

The milestones describe outcomes, not exact code structure.

Follow the [development workflow](../AGENTS.md#workflow-and-documentation) and consult the architecture/product sections relevant to the current milestone. Each milestone must leave the repository buildable and testable.

## Progress

- [x] Milestone 0 — Repository Foundation
- [x] Milestone 1 — Runnable Backend and PostgreSQL
- [ ] **Current:** Milestone 2 — Frontend Foundation
- [ ] Milestone 3 — Authentication and Users
- [ ] Milestone 4 — Events and Local Game Catalog
- [ ] Milestone 5 — Game Subscriptions
- [ ] Milestone 6 — Core GameRequest Workflow
- [ ] Milestone 7 — SSE Realtime Experience
- [ ] Milestone 8 — Prototype UX and Hardening
- [ ] Milestone 9 — Open Source Self-Hosting Release

Future milestones remain deferred until the v0.1 roadmap is complete.

---

# Milestone 0 — Repository Foundation

## Goal

Create a clean open-source repository foundation before application features.

## Expected outcomes

- Go module initialized
- standard repository layout
- `AGENTS.md`
- `docs/ARCHITECTURE.md`
- `docs/PRODUCT.md`
- `docs/ROADMAP.md`
- AGPL-3.0 license present
- basic README references correct project goals
- `.gitignore`
- editor/tooling basics where justified

## Implementation details

- exact Go module path once repository URL/module name is known
- minor documentation organization
- basic formatting conventions

---

# Milestone 1 — Runnable Backend and PostgreSQL

## Goal

A minimal LANsurfer server boots reliably and connects to PostgreSQL.

## Expected outcomes

### Application

- Go application entry point
- chi router
- configuration from environment variables
- structured logging
- graceful shutdown

### PostgreSQL

- pgx connection/pool
- connection retry during startup
- sqlc configuration
- initial migration structure
- goose integrated as an embedded migration runner
- automatic UP migrations before HTTP server startup
- PostgreSQL advisory migration lock

### Operations

- `/healthz`
- `/readyz`
- Dockerfile
- Docker Compose with PostgreSQL
- PostgreSQL healthcheck
- LANsurfer depends on database readiness but also performs its own retries
- Taskfile tasks for common development/build/test/generate operations

## Acceptance

A new developer/operator can start the stack with a documented Compose command and receive successful health/readiness responses.

No application domain features are required yet.

---

# Milestone 2 — Frontend Foundation

## Goal

Establish the server-rendered frontend toolchain without implementing product complexity.

## Expected outcomes

- templ integrated
- HTMX bundled locally
- Tailwind CSS
- DaisyUI
- TypeScript
- esbuild
- production frontend assets embedded via `go:embed`
- base page layout
- responsive navigation
- fixed centralized LANsurfer branding following the [visual branding guide](BRANDING.md)
- basic error/empty-state patterns

## Constraints

- no configurable branding
- no SPA
- no additional JS component library
- no CDN dependency required at runtime

## Acceptance

The application renders a responsive LANsurfer page from the Go binary and supports a simple HTMX interaction.

---

# Milestone 3 — Authentication and Users

## Goal

Users can authenticate through generic OIDC and receive a local LANsurfer account/session.

## Expected outcomes

### OIDC

- generic OIDC provider configuration
- issuer discovery
- secure authorization callback flow
- state validation
- PKCE where appropriate

### Local user model

Initial data model:

```text
users
user_identities
auth_sessions
```

### User

Suggested fields:

- ID
- DisplayName
- Role
- CreatedAt
- UpdatedAt

Initial roles:

- user
- admin

### Identity

Suggested fields:

- ID
- UserID
- Provider
- Subject
- DisplayName
- Email
- CreatedAt

Constraint:

```text
UNIQUE(provider, subject)
```

### Sessions

- server-side session records in PostgreSQL
- opaque random cookie
- HttpOnly
- Secure in production
- SameSite
- expiry
- logout/revocation

## Not included

- local passwords
- Discord
- passkeys
- guest accounts
- multi-provider account-linking UI unless naturally required

## Acceptance

A Keycloak-backed deployment can log a user into LANsurfer and persist/reuse a server-side session.

---

# Milestone 4 — Events and Local Game Catalog

## Goal

Admins can prepare LANsurfer for an event and maintain the games available for LFG usage.

## Expected outcomes

### Event

Basic Event model and admin management sufficient for one current Event.

Suggested fields:

- ID
- Name
- StartsAt
- EndsAt
- current/active state
- CreatedAt
- UpdatedAt

Support historical/multiple Events in the schema where reasonable, while keeping the UI simple.

### Game

Suggested tables:

```text
games
game_aliases
game_external_ids
```

Suggested Game fields:

- ID
- Name
- Slug
- Active
- CreatedAt
- UpdatedAt

### Admin game management

Admin can:

- list games
- create game
- edit game
- activate/deactivate game
- add/remove aliases

### Search

Game aliases should participate in practical game search.

### External IDs

Create the generic data model if useful.

Do not implement an external provider yet.

## Acceptance

An admin can prepare the game catalog for a real LAN event without editing SQL directly.

---

# Milestone 5 — Game Subscriptions

## Goal

Users can define which games they are interested in.

## Expected outcomes

Suggested relation:

```text
game_subscriptions
- user_id
- game_id
- created_at
```

Users can:

- browse/search games
- follow a game
- unfollow a game
- view "My Games" or equivalent

HTMX should provide smooth follow/unfollow interaction.

## Acceptance

Subscription state persists and can be used by later GameRequest/live-update functionality.

---

# Milestone 6 — Core GameRequest Workflow

## Goal

Implement the core LANsurfer LFG feature.

## Expected data model

```text
game_requests
game_request_members
```

### GameRequest

Suggested properties:

- ID
- EventID
- GameID
- CreatorID
- MaxPlayers nullable
- StartsAt
- Description
- Status
- CreatedAt
- UpdatedAt

Initial statuses:

- open
- closed
- cancelled

### GameRequestMember

Suggested properties:

- GameRequestID
- UserID
- JoinedAt

Constraint:

```text
UNIQUE(game_request_id, user_id)
```

## Expected behavior

Users can:

- list active GameRequests
- create a GameRequest
- view a GameRequest
- join
- leave
- close their own request
- cancel their own request

Recommended defaults:

- creator automatically joins
- MaxPlayers includes creator
- MaxPlayers may be absent
- StartsAt can represent now or scheduled start
- creator does not transfer ownership in v0.1

### Concurrency

Joining a limited request must be protected by PostgreSQL transaction/locking behavior.

Two simultaneous joins must never exceed MaxPlayers.

Add integration tests for this.

## Acceptance

The complete manual browser workflow "create -> discover -> join -> leave/close" works reliably.

---

# Milestone 7 — SSE Realtime Experience

## Goal

Users see relevant GameRequest changes without manually refreshing.

## Expected outcomes

- in-memory application event publisher/broker
- SSE endpoint for authenticated users
- lifecycle handling for disconnected clients
- GameRequest events such as:
  - GameRequestCreated
  - GameRequestJoined
  - GameRequestLeft
  - GameRequestClosed
- frontend SSE handling
- HTMX/DOM refresh strategy
- subscribed-game request highlighting or toast feedback

## Constraints

- no WebSockets
- no Redis/NATS
- no persistent notification system
- no Discord/Web Push

## Acceptance

With two browsers open, relevant GameRequest changes become visible in the second browser without a manual full-page refresh.

---

# Milestone 8 — Prototype UX and Hardening

## Goal

Make the prototype usable at a real LAN event.

## Expected outcomes

### UX

- mobile-friendly primary workflows
- clear current Event context
- clear game search
- fast GameRequest creation
- simple join/leave behavior
- useful empty states
- sensible validation feedback
- accessible forms/buttons/dialogs

### Security

- CSRF protection
- basic security headers
- careful session handling
- input limits/validation
- trusted proxy behavior documented/configured

### Reliability

- important domain error mapping
- improved startup errors
- database readiness behavior
- graceful SSE shutdown
- useful structured logs

### Tests

- important domain/service tests
- PostgreSQL integration tests
- GameRequest concurrency tests
- important handler tests

## Acceptance

The product is suitable for a controlled real-world CXT pilot.

---

# Milestone 9 — Open Source Self-Hosting Release

## Goal

Prepare the first publicly usable version for other LAN parties and clubs.

## Expected outcomes

- production Docker image
- example `compose.yaml`
- documented environment variables
- upgrade process documented
- automatic migration behavior documented
- initial admin bootstrap documented
- backup recommendations for PostgreSQL
- contributor documentation
- architecture documentation updated to match reality
- release binaries where useful
- CI build/test/lint pipeline

## Acceptance

A technically competent third party can deploy LANsurfer without needing project-specific knowledge.

---

# Future Milestones — Not Part of v0.1

The following are future possibilities and should not influence v0.1 implementation unless a current design would otherwise create a clear dead end.

## External Game Catalog Providers

Potential admin flow:

```text
Admin -> Games -> Import
```

Possible providers:

- IGDB
- Steam
- others

Providers import/enrich local games.

They do not become the local source of truth.

---

## Discord

Possible future capabilities:

- Discord OAuth2 login
- account linking
- Discord bot
- GameRequest notifications
- Discord server/channel integration

Discord authentication and Discord bot/community integration should remain separate concepts.

---

## Web Push

Browser push notifications for followed games.

Not required for initial realtime operation.

---

## Persistent Notifications

A notification inbox/history for events that occurred while the browser was closed.

Deferred until the need is validated.

---

## Ready-to-Play Availability

Users may signal that they are currently available for selected games.

Potential model:

```text
User
Game
AvailableUntil
```

Do not implement until the GameRequest workflow is proven.

---

## Instance Branding

Future admin customization may include:

- instance name
- logo
- favicon
- colors
- welcome text

Branding is per LANsurfer instance.

Keep this separate from Event data.

---

## Multiple Application Instances

If hosted LANsurfer later needs horizontal scaling:

- PostgreSQL remains shared storage
- replace/extend the in-memory realtime broker
- PostgreSQL LISTEN/NOTIFY is the preferred first option to investigate

Do not add distributed infrastructure before this requirement exists.
