# LANsurfer repository guidelines

LANsurfer is a self-hosted Looking-for-Group web application and a modular Go monolith. Keep operation simple, dependencies few, and changes inside the approved product/architecture boundaries.

## Start here

Use the smallest authoritative source that answers the task:

- `README.md`: what LANsurfer is and how to run it.
- `docs/PRODUCT.md`: intended user-visible behavior, terminology, and release scope.
- `docs/ARCHITECTURE.md`: technical architecture, module ownership, and durable invariants.
- `docs/ROADMAP.md`: delivery tracks, dependencies, and Pilot/Integration/Release gates; GitHub tracking issues carry live progress.
- `docs/DEVELOPMENT_WORKFLOW.md`: planning-to-implementation handoff, implementation-ready definition, and artifact lifecycle.
- `docs/BRANDING.md` and `docs/ui-kit/`: approved presentation system.
- `Taskfile.yml`: authoritative development commands.
- `docs/superpowers/`: bounded feature designs and temporary implementation plans. These never override PRODUCT, ARCHITECTURE, ROADMAP, or this file.

Read only the sections relevant to the task. An open issue, implementation plan, or historical design is not by itself evidence that work belongs in the current release or is still unimplemented.

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

- Keep the modular monolith organized by business capability. Current business modules are `auth`, `user`, `game`, and `gamerequest`; `notification` is the approved Track E addition. The prototype `event` module is removed. Do not introduce `event`, `clan`, a generic parent/context abstraction, or other post-1.0 modules unless current PRODUCT/ROADMAP scope explicitly schedules them.
- A business module owns its behavior and writes. Never reach into another module's persistence implementation or write its tables directly. Call a small explicit interface or publish an application/domain event.
- Define narrow interfaces in the consuming package and wire concrete dependencies explicitly in the composition root. Do not use a dependency-injection framework or service locator.
- Cross-module joins belong in dedicated read/query models. Query packages do not perform domain writes.
- Keep generated sqlc types inside persistence/read infrastructure and map them to domain or explicit read models at boundaries. Generated files are not hand-edited.
- Keep HTTP handlers limited to transport, authentication context, service/query calls, domain-error translation, and templ rendering. Business rules stay out of handlers and domain packages do not encode HTTP semantics.
- Prefer a little duplication to generic `shared`, `common`, `utils`, repository/DTO/mapping frameworks, full CQRS, or other abstractions without a concrete current need.
- Use templ/HTMX server rendering and SSE with TypeScript limited to progressive enhancement. Do not create an internal JSON API solely for the web UI. Production assets are embedded; the IGDB cover host is the only approved remote-content exception.
- Run migrations before HTTP startup; migration failure is fatal. Never run DOWN migrations automatically.
- Use local user IDs, explicit identity linking, revocable PostgreSQL sessions, and opaque cookies. Do not add local passwords, browser JWT sessions, or identity merging based only on email.
- Protect important invariants in PostgreSQL with constraints, indexes, transactions, locks, and foreign keys. Joining/capacity, case-insensitive Game Nick claims, identity linking/unlinking, and provider-Game materialization are concurrency-sensitive.

### Game Request activity and Track E handoff

`game_request_activity` currently serves both as durable request-local history and as Track A's transactional notification-intent handoff to Track E. Write the row in the same transaction as the successful Game Request change. Keep this single-table boundary unless Track E demonstrates delivery state that does not belong to `gamerequest`; a split then is an explicit architecture change, not a second parallel source of truth.

`sequence` identifies an activity row and orders one Game Request's history. It is not a global replay cursor because values are assigned at insert time rather than commit time. Never advance one global high-watermark from it. `origin=backfill` rows are reconstructed history and must never be delivered.

## Testing

- Test meaningful domain behavior, PostgreSQL behavior, concurrency-sensitive invariants, and important HTTP behavior close to the owning code. Avoid excessive mocking.
- PostgreSQL-specific behavior belongs in the integration suite.
- Browser tests exist only for interaction behavior Go tests cannot exercise: rapid-click guards, realtime stream lifecycle, abort/failure recovery, and SSE-versus-HTMX-swap behavior. Do not grow `web/tests` into a general end-to-end suite.
- Regenerate committed generated output when its sources change and verify the production build when the change affects generated/runtime assets.

## Scope and workflow

Follow `docs/DEVELOPMENT_WORKFLOW.md` for the planning-to-implementation handoff.

- Inspect existing implementation and tests before designing a change.
- Use PRODUCT for release scope, ROADMAP for delivery dependencies/gates, and GitHub tracking issues for current progress. Do not implement post-1.0 candidates merely because an issue or historical design exists.
- For non-trivial work, separate planning from implementation: planning resolves product/architecture questions and prepares an implementation-ready design/plan; the coding agent executes that handoff rather than rediscovering or silently redesigning the feature.
- The planning phase does not implement application code. Small local fixes with already-unambiguous behavior may skip a formal design/plan.
- Prefer one coherent branch/PR per feature, fix, or cross-track contract. A planning draft PR should normally continue into implementation rather than spawning a second implementation PR.
- PRODUCT owns durable behavior/scope; ARCHITECTURE owns durable technical decisions. If either changes, update the canonical documents that make the same promise.
- ROADMAP is a compact delivery map, not a historical status database. Track current checklists and progress in GitHub issues/PRs.
- Specifications/designs are bounded decision aids. Implementation plans are temporary execution aids. Before merge, promote durable decisions into PRODUCT/ARCHITECTURE and remove temporary plans unless they still provide concrete long-term value.
- Summarize implementation decisions, verification performed/omitted, and any affected cross-track contract in the PR.

### Implementation-ready handoff

Do not label a non-trivial handoff implementation-ready until:

- scope is confirmed;
- goals and non-goals are explicit;
- product/architecture questions are resolved or explicitly approved;
- acceptance criteria are testable;
- affected areas and generated-output requirements are identified;
- tests and verification commands are specified;
- the coding agent is not being asked to make new product or architecture decisions.

If repository evidence discovered during implementation contradicts a plan, do not force the plan. Preserve the approved intent where unambiguous; return to planning/approval when a genuinely new product or architecture decision is required.

## Changes requiring approval

Stop and request explicit approval before changing the technology stack, module boundaries, database model, authentication/session or security model, deployment model, public domain terminology, approved 1.0 scope, or license.

In particular, do not introduce an SPA/framework (React/Vue/Svelte/Angular), frontend state framework, GraphQL, an ORM, UI component libraries beyond DaisyUI, Sass/Less, Redis, a message broker, microservices, Kubernetes-specific application architecture, event sourcing, full CQRS, or additional required runtime services without an approved concrete need. Propose significant new direct dependencies before adding them.
