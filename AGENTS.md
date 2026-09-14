# LANsurfer repository guidelines

LANsurfer is a self-hosted Looking-for-Group web application and a modular Go monolith. Keep operation simple and dependencies few.

## Commands

Use Taskfile workflows, not Makefiles; run `task --list` for the authoritative task list.

- `task dev`: build and run the Compose stack.
- `task run`: run the Go application with environment configuration.
- `task build`: build `bin/lansurfer`.
- `task test`: run race-enabled unit tests.
- `task test:integration`: run PostgreSQL integration tests in an isolated container.
- `task generate`: regenerate sqlc code.
- `task lint`: check formatting, modules, and Go source.

## Architecture and coding rules

- Keep a modular monolith organized by business capability (`auth`, `user`, `game`, `gamerequest`, and the 1.0 `notification` module). The prototype `event` module may remain only where needed for migration compatibility while the single-event 1.0 model is implemented. Do not add `clan` or other post-1.0 modules until their roadmap work is explicitly scheduled. Keep infrastructure, queries, and web delivery separate from domain modules.
- Business modules own their behavior and writes. Never access another module's persistence implementation or write its tables directly. Call the owning service through a small explicit interface or publish an application/domain event.
- Define narrow interfaces in the consuming package. Keep concrete infrastructure behind them. Wire dependencies explicitly in the composition root; do not use a dependency-injection framework or service locator.
- Use dedicated query/read models for cross-module UI reads and joins. Query packages must not perform domain writes.
- Keep generated sqlc types inside persistence/read infrastructure. Map them to domain or explicit read models at boundaries.
- Avoid generic `shared`, `common`, `utils`, repository, DTO/mapping, CQRS, or other framework-style abstractions. Prefer a little duplication to a premature abstraction.
- Keep HTTP handlers limited to transport concerns, authentication context, service/query calls, domain-error translation, and templ rendering. Keep business rules out of handlers. Do not create an internal JSON API solely for the web UI.
- Use explicit domain errors and translate them to HTTP behavior only in the web layer. Domain packages must not encode HTTP semantics.
- Protect important invariants in PostgreSQL with constraints, indexes, transactions, locks, and foreign keys where appropriate. Treat joining a `GameRequest` and enforcing capacity as concurrency-sensitive behavior.
- Test meaningful domain behavior, PostgreSQL behavior, concurrent joins/capacity, and important HTTP behavior. Avoid excessive mocking.
- Use templ/HTMX SSR and SSE; keep TypeScript limited to progressive enhancement. Prefer native browser primitives. Embed production assets. The only approved remote-content exception is the planned IGDB cover host and safeguards documented in `docs/PRODUCT.md`; it does not permit other runtime CDNs.
- Run migrations before HTTP startup; migration failure must prevent startup. Never run DOWN migrations automatically.
- Keep generic OIDC provider-neutral; Discord OAuth2 login is an independent provider. Use local user IDs, explicit identity linking, revocable PostgreSQL sessions, and opaque cookies. No local passwords, JWT browser sessions, or identity merging based only on email.
- Keep the local game catalog authoritative; no scraping. The optional IGDB adapter imports/enriches local records, and core operation must not depend on provider availability.
- Keep LANsurfer colors, typography, accessibility rules, and component semantics fixed and centralized. The approved 1.0 instance identity is limited to the fields and local uploads in `docs/PRODUCT.md`; it is not a theme system. Use the domain terminology there, not surf metaphors in code or database names.
- For 1.0, Game Requests are instance-wide. Do not introduce a generic `context`, polymorphic parent, or other abstraction solely to prepare for future Event, Clan, or shared-service scoping.

## Parallel development rules

The 1.0 roadmap is organized into parallel development tracks. Multiple tracks may be implemented concurrently when their stated dependencies are satisfied.

- Treat [ROADMAP.md](docs/ROADMAP.md#development-model) as the authority for track scope, readiness, and cross-track dependencies.
- Do not block work merely because another 1.0 track is unfinished. Block only the integration work that the roadmap explicitly marks as dependent.
- Keep cross-track contracts small and explicit. The main planned contracts are Game Request writes/outbox (Track A → E), provider-independent authentication/onboarding inside Track B, and service worker/Web Push integration (Track D → E).
- Prefer separate branches/PRs per coherent track or cross-track contract. Avoid combining unrelated track implementation into one change merely because the work happens concurrently.
- A track may define an interface or integration point needed by another track, but must not reach into the other track's persistence or implementation details.
- If parallel changes touch the same schema, route, template, or composition-root wiring, coordinate the contract first and keep each change narrowly scoped to reduce merge conflicts.
- The 1.0 Integration Gate is where independently completed tracks are proven together. Do not weaken a track's local acceptance criteria on the assumption that integration testing will catch it later.
- Post-1.0 candidates remain out of bounds unless the roadmap is explicitly changed. Parallel development applies to approved 1.0 tracks, not to speculative future scope.

## Constraints requiring approval

If a task conflicts with project rules, stop and request approval. Changes to the technology stack, module boundaries, database, authentication/session or security model, deployment model, public domain model/terminology, approved 1.0 scope, or license require explicit approval. Small internal implementation choices do not.

In particular:

- Do not introduce an SPA, React/Vue/Svelte/Angular or meta-frameworks, frontend state frameworks, GraphQL, ORMs, UI component libraries beyond DaisyUI, or Sass/Less.
- Do not introduce Redis, message brokers, microservices, Kubernetes-specific application architecture, event sourcing, full CQRS, or additional required runtime services.
- Add dependencies or abstractions only for a current concrete need that existing tools cannot meet. Propose significant new direct dependencies before adding them.

## Workflow and documentation

- Inspect existing code before designing structures. Work within an approved 1.0 track whose dependencies in [ROADMAP.md](docs/ROADMAP.md#development-model) are satisfied. Do not implement post-1.0 candidates for completeness. Planned 1.0 descriptions are not evidence that a feature already exists. Update the relevant track status or acceptance notes as work is completed.
- Read the sections relevant to the task, not all documents on every change: [ARCHITECTURE.md](docs/ARCHITECTURE.md) owns technical decisions; [PRODUCT.md](docs/PRODUCT.md) owns behavior, terminology, and scope. For web work read architecture sections 2–4 and 18–19; for persistence/authentication read sections 10–15 and 21; for catalog/branding read sections 16–17 and the corresponding product sections. Consult sections 5–9 for module boundaries and cross-track integration. Use [README.md](README.md) for setup.
- Run relevant generators, tests, and the production build when practical. Summarize implementation decisions, verification limits, deliberate omissions, and any cross-track contract affected by the change.
