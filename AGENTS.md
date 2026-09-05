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

- Keep a modular monolith organized by business capability (`auth`, `user`, `game`, `event`, `gamerequest`). Keep infrastructure, queries, and web delivery separate from domain modules.
- Business modules own their behavior and writes. Never access another module's persistence implementation or write its tables directly. Call the owning service through a small explicit interface or publish an application/domain event.
- Define narrow interfaces in the consuming package. Keep concrete infrastructure behind them. Wire dependencies explicitly in the composition root; do not use a dependency-injection framework or service locator.
- Use dedicated query/read models for cross-module UI reads and joins. Query packages must not perform domain writes.
- Keep generated sqlc types inside persistence/read infrastructure. Map them to domain or explicit read models at boundaries.
- Avoid generic `shared`, `common`, `utils`, repository, DTO/mapping, CQRS, or other framework-style abstractions. Prefer a little duplication to a premature abstraction.
- Keep HTTP handlers limited to transport concerns, authentication context, service/query calls, domain-error translation, and templ rendering. Keep business rules out of handlers. Do not create an internal JSON API solely for the web UI.
- Use explicit domain errors and translate them to HTTP behavior only in the web layer. Domain packages must not encode HTTP semantics.
- Protect important invariants in PostgreSQL with constraints, indexes, transactions, locks, and foreign keys where appropriate. Treat joining a `GameRequest` and enforcing capacity as concurrency-sensitive behavior.
- Test meaningful domain behavior, PostgreSQL behavior, concurrent joins/capacity, and important HTTP behavior. Avoid excessive mocking.
- Use templ/HTMX SSR and SSE; keep TypeScript limited to progressive enhancement. Prefer native browser primitives. Embed production assets; no runtime CDNs.
- Run migrations before HTTP startup; migration failure must prevent startup. Never run DOWN migrations automatically.
- Use provider-neutral OIDC and local user IDs, with revocable PostgreSQL sessions and opaque cookies. No local passwords, JWT browser sessions, or identity merging based only on email.
- Keep the local game catalog authoritative; no scraping or external catalog runtime dependency.
- Keep branding fixed and centralized for v0.1. Use the domain terminology in `docs/PRODUCT.md`, not surf metaphors in code or database names.

## Constraints requiring approval

If a task conflicts with project rules, stop and request approval. Changes to the technology stack, module boundaries, database, authentication/session or security model, deployment model, public domain model/terminology, v0.1 scope, or license require explicit approval. Small internal implementation choices do not.

In particular:

- Do not introduce an SPA, React/Vue/Svelte/Angular or meta-frameworks, frontend state frameworks, GraphQL, ORMs, UI component libraries beyond DaisyUI, or Sass/Less.
- Do not introduce Redis, message brokers, microservices, Kubernetes-specific application architecture, event sourcing, full CQRS, or additional required runtime services.
- Add dependencies or abstractions only for a current concrete need that existing tools cannot meet. Propose significant new direct dependencies before adding them.

## Workflow and documentation

- Inspect existing code before designing structures. Follow the current milestone in [ROADMAP.md](docs/ROADMAP.md#progress); do not implement future features for completeness. Update progress as work is completed.
- Read the sections relevant to the task, not all documents on every change: [ARCHITECTURE.md](docs/ARCHITECTURE.md) owns technical decisions; [PRODUCT.md](docs/PRODUCT.md) owns behavior, terminology, and scope. For web work read architecture sections 2–4 and 18–19; for persistence/authentication read sections 10–15 and 21; for catalog/branding read sections 16–17 and the corresponding product sections. Consult sections 5–9 for module boundaries. Use [README.md](README.md) for setup.
- Run relevant generators, tests, and the production build when practical. Summarize implementation decisions, verification limits, and deliberate omissions.
