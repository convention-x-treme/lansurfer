# LANsurfer repository guidelines

LANsurfer is a self-hosted Looking-for-Group web application and a modular Go monolith. Keep operation simple, dependencies few, and changes inside the approved product/architecture boundaries.

## Start here

Use the smallest authoritative source that answers the task:

- `README.md`: what LANsurfer is and how to run it.
- `docs/PRODUCT.md`: durable product contract — user-visible behavior, approved scope, and product boundaries. It carries no schedule or implementation status.
- `docs/ARCHITECTURE.md`: technical architecture, module ownership, and durable invariants.
- `docs/TERMINOLOGY.md`: canonical project vocabulary and distinctions that agents/contributors must preserve.
- `DESIGN.md`: authoritative design system — brand, colors, typography, icons, themes, accessibility, components, logo, and mascot. It wins over external design projects.
- `docs/ui-kit/`: reference for specific screen compositions and interactions.
- GitHub Issues + milestones: software-development work only — proposals, priorities, dependencies, bugs, tests, tooling, and technical documentation. There is no repository roadmap file; do not add one. Live deployment, event preparation, provider administration, and go/no-go approvals are not tracked there.
- `Taskfile.yml`: authoritative development commands.
- `docs/superpowers/`: feature-branch working specs/designs/plans when useful. They never override canonical docs and normally do not survive the final merge.

Read only the sections relevant to the task. An open issue or retained implementation plan is not by itself evidence that work belongs in the current release or is still unimplemented.

## Commands

Use Taskfile workflows, not Makefiles; run `task --list` for the authoritative task list.

- `task dev`: build and run the Compose stack.
- `task run`: run the Go application with environment configuration.
- `task build`: build `bin/lansurfer`.
- `task test`: run race-enabled unit tests.
- `task test:integration`: run PostgreSQL integration tests in an isolated container.
- `task test:browser`: run Chromium interaction-hardening regression tests.
- `task test:pwa`: run the focused PWA browser suite (service-worker registration/lifecycle).
- `task generate`: regenerate templates, frontend assets, and sqlc code.
- `task lint`: check formatting, modules, Go source, and TypeScript.

Run the checks relevant to the files and behavior changed. Do not substitute broad test runs for targeted reasoning about the affected invariant.

## Architecture and coding guardrails

- Keep the modular monolith organized by business capability. Current business modules are `auth`, `user`, `game`, `gamerequest`, and `notification`. The prototype `event` module is removed. Do not introduce `event`, `clan`, a generic parent/context abstraction, or other modules outside the current product model unless PRODUCT approves the scope and current GitHub planning schedules the work.
- A business module owns its behavior and writes. Never reach into another module's persistence implementation or write its tables directly. Call a small explicit interface or publish an application/domain event.
- Define narrow interfaces in the consuming package and wire concrete dependencies explicitly in the composition root. Do not use a dependency-injection framework or service locator.
- Cross-module joins belong in dedicated read/query models. Query packages do not perform domain writes.
- Keep generated sqlc types inside persistence/read infrastructure and map them to domain or explicit read models at boundaries. Generated files are not hand-edited.
- Keep HTTP handlers limited to transport, authentication context, service/query calls, domain-error translation, and templ rendering. Business rules stay out of handlers and domain packages do not encode HTTP semantics.
- Prefer a little duplication to generic `shared`, `common`, `utils`, repository/DTO/mapping frameworks, full CQRS, or other abstractions without a concrete current need.
- Use templ/HTMX server rendering and SSE with TypeScript limited to progressive enhancement. Do not create an internal JSON API solely for the web UI. Production assets are embedded. IGDB covers are served same-origin by LANsurfer; on a cache miss, the server may fetch only from the fixed IGDB image origin under the bounded cover-proxy contract. Selected provider avatars use the same bounded proxy, fetching only from the stored picture of a currently selected identity. Do not introduce a generic remote-asset proxy.
- Run migrations before HTTP startup; migration failure is fatal. Never run DOWN migrations automatically.
- Use local user IDs, explicit identity linking, revocable PostgreSQL sessions, and opaque cookies. Do not add local passwords, browser JWT sessions, or identity merging based only on email.
- Protect important invariants in PostgreSQL with constraints, indexes, transactions, locks, and foreign keys. Joining/capacity, case-insensitive Game Nick claims, identity linking/unlinking, and provider-Game materialization are concurrency-sensitive.

### Game Request activity and notification handoff

`game_request_activity` serves both as durable request-local history and as the transactional handoff from Game Request writes to notification processing. Write the row in the same transaction as the successful Game Request change. Notification-specific work/delivery state belongs to the notification module; do not create a second parallel source of request activity truth.

`sequence` identifies an activity row and orders one Game Request's history. It is not a global replay cursor because values are assigned at insert time rather than commit time. Never advance one global high-watermark from it. `origin=backfill` rows are reconstructed history and must never be delivered.

## Testing

- Test meaningful domain behavior, PostgreSQL behavior, concurrency-sensitive invariants, and important HTTP behavior close to the owning code. Avoid excessive mocking.
- PostgreSQL-specific behavior belongs in the integration suite.
- Never create, overwrite, or delete the developer's local `.env` (or other gitignored local files such as `.env.*`) while testing or smoke-running. It may hold irreplaceable secrets. Test dotenv or configuration behavior in a scratch directory (`mktemp -d`) or a temporary copy of the repo instead.
- Browser tests exist only for interaction or browser-platform behavior Go tests cannot exercise: rapid-click guards, realtime stream lifecycle, abort/failure recovery, SSE-versus-HTMX-swap behavior, service-worker lifecycle/install behavior, and browser-only Web Push permission/subscription/receipt behavior. Do not grow `web/tests` into a general end-to-end suite.
- Regenerate committed generated output when its sources change and verify the production build when the change affects generated/runtime assets.

## Scope and workflow

- Inspect existing implementation and tests before designing a change.
- Use GitHub Issues and milestones for current development status, dependencies, and validation. Do not infer scheduled work from an old spec or historical branch artifact, and do not treat an open proposal as approved product scope.
- Prefer one coherent branch/PR per feature, fix, or cross-capability contract. Coordinate a shared schema, route, template, or composition-root contract before parallel changes touch it.
- PRODUCT owns durable behavior/scope; ARCHITECTURE owns durable technical decisions; TERMINOLOGY owns shared vocabulary; DESIGN owns the design system. Keep those documents aligned when a change affects more than one authority, and state each rule once in its owning document.
- Specifications/designs are for bounded decisions that need more detail during implementation. Plans/tasks/prompts are execution aids, not permanent status documents.
- Specs, designs, plans, and task lists may be committed on a feature branch when that improves human/agent collaboration. Before merge, harvest durable behavior into PRODUCT, durable technical decisions into ARCHITECTURE, vocabulary into TERMINOLOGY, design-system rules into DESIGN, remaining work and manual validation evidence into the relevant GitHub Issue or PR; then remove temporary working artifacts from the final tree.
- Prefer squash merging branches that contained temporary planning artifacts so those intermediate files do not become part of the mainline commit history.
- Summarize implementation decisions, verification performed/omitted, and any affected cross-capability contract in the PR.

## Changes requiring approval

Stop and request explicit approval before changing the technology stack, module boundaries, domain relationships/data ownership/invariants in the database model, authentication/session or security model, deployment model, public domain terminology, approved product scope, or license.

Implementing schema, indexes, constraints, and migrations that realize an already-approved persistence design does not require renewed approval when the approved domain relationships, ownership, and invariants stay unchanged. Changing those semantics does require approval.

In particular, do not introduce an SPA/framework (React/Vue/Svelte/Angular), frontend state framework, GraphQL, an ORM, UI component libraries beyond DaisyUI, Sass/Less, Redis, a message broker, microservices, Kubernetes-specific application architecture, event sourcing, full CQRS, or additional required runtime services without an approved concrete need. Propose significant new direct dependencies before adding them.
