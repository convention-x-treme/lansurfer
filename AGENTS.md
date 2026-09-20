# LANsurfer repository guidelines

LANsurfer is a self-hosted Looking-for-Group web application and a modular Go monolith. Keep operation simple and dependencies few.

## Commands

Use Taskfile workflows, not Makefiles; run `task --list` for the authoritative task list.

- `task dev`: build and run the Compose stack.
- `task run`: run the Go application with environment configuration.
- `task build`: build `bin/lansurfer`.
- `task test`: run race-enabled unit tests.
- `task test:integration`: run PostgreSQL integration tests in an isolated container.
- `task test:browser`: run Chromium interaction-hardening regression tests against the `internal/web/browsertest` fixture.
- `task test:pwa`: run the focused PWA browser suite (service-worker registration/lifecycle) against the same fixture; keep this separate from `task test:browser` rather than growing it into general E2E coverage.
- `task generate`: regenerate sqlc code.
- `task lint`: check formatting, modules, and Go source.

## Architecture and coding rules

- Keep a modular monolith organized by business capability (`auth`, `user`, `game`, `gamerequest`, and the 1.0 `notification` module). The prototype `event` module has been removed by Track A's single-event migration; do not reintroduce it, and do not add `clan` or other post-1.0 modules until their roadmap work is explicitly scheduled. Keep infrastructure, queries, and web delivery separate from domain modules.
- Business modules own their behavior and writes. Never access another module's persistence implementation or write its tables directly. Call the owning service through a small explicit interface or publish an application/domain event.
- Define narrow interfaces in the consuming package. Keep concrete infrastructure behind them. Wire dependencies explicitly in the composition root; do not use a dependency-injection framework or service locator.
- Use dedicated query/read models for cross-module UI reads and joins. Query packages must not perform domain writes. The 1.0 Trending view is a focused read model over Game Request activity, not authorization for a general analytics/event store.
- `game_request_activity` is both the durable request-local activity record and Track A's transactional notification-intent handoff to Track E. Write its entry in the same transaction as the change it describes. Treat the single-table design as provisional until Track E consumes it: prefer it over a parallel outbox table, and split only if delivery needs state that does not belong to `gamerequest`.
- `sequence` identifies an activity row and orders one Game Request's history. It is not a global replay cursor, because sequence values are assigned at insert time rather than commit time. Never build a single high-watermark cursor on it. `origin` marks migration-reconstructed rows as `backfill`; those are history only and must never be delivered.
- Keep generated sqlc types inside persistence/read infrastructure. Map them to domain or explicit read models at boundaries.
- Avoid generic `shared`, `common`, `utils`, repository, DTO/mapping, CQRS, or other framework-style abstractions. Prefer a little duplication to a premature abstraction.
- Keep HTTP handlers limited to transport concerns, authentication context, service/query calls, domain-error translation, and templ rendering. Keep business rules out of handlers. Do not create an internal JSON API solely for the web UI.
- Use explicit domain errors and translate them to HTTP behavior only in the web layer. Domain packages must not encode HTTP semantics.
- Protect important invariants in PostgreSQL with constraints, indexes, transactions, locks, and foreign keys where appropriate. Treat joining a `GameRequest`, enforcing capacity, claiming a case-insensitive Game Nick, and materializing the same provider Game concurrently as concurrency-sensitive behavior.
- Test meaningful domain behavior, PostgreSQL behavior, concurrent joins/capacity, provider materialization races, and important HTTP behavior. Avoid excessive mocking.
- Browser tests cover only interaction hardening that no Go test can reach: rapid-click guards, realtime stream lifecycle, abort/failure recovery, and SSE-versus-swap behavior. Keep product behavior in Go tests; do not grow `web/tests` into a general end-to-end suite.
- Use templ/HTMX SSR and SSE; keep TypeScript limited to progressive enhancement. Prefer native browser primitives. Embed production assets. The only approved remote-content exception is the IGDB cover host and safeguards documented in `docs/PRODUCT.md`; it does not permit other runtime CDNs or a general external-asset system.
- Run migrations before HTTP startup; migration failure must prevent startup. Never run DOWN migrations automatically.
- Keep generic OIDC provider-neutral; Discord OAuth2 login is an independent provider. Use local user IDs, explicit identity linking, revocable PostgreSQL sessions, and opaque cookies. No local passwords, JWT browser sessions, or identity merging based only on email.
- Stable 1.0 public player identity is the unique case-insensitive **Game Nick**. Do not reintroduce biographies, per-Game player handles, richer public profiles, or provider identity as 1.0 public identity unless scope is explicitly changed.
- Keep the local Game catalog authoritative; no scraping. IGDB is an optional general deployment integration and a required readiness dependency only for the selected pilot. When enabled, synchronize a lightweight local provider index in the background; do not make startup or existing local workflows depend on provider availability.
- Provider search/view alone must not create a LANsurfer `Game`. Following a provider result or creating a Game Request for it atomically materializes one authoritative local Game through the `game` module, keyed by provider/external ID. Any signed-in player may trigger that activation when IGDB is enabled. Never auto-merge by name.
- Synchronize lightweight metadata for all IGDB game records and apply player-visible type policy at search time. Do not mirror the full provider graph or cover bytes. The first provider baseline is not player-visible until complete; later refreshes keep serving the last successful index.
- When a provider record disappears, delete it only if no authoritative local Game references it. Otherwise archive/tombstone the provider identity and keep the local Game, follows, requests, and activity intact.
- In IGDB-disabled local-only mode, ordinary players do not create free-form Games; global administrators manage the authoritative local catalog manually.
- Keep LANsurfer colors, typography, accessibility rules, component semantics, and bundled branding fixed and centralized. Stable 1.0 does **not** include the previous full administrator-managed branding surface for welcome text, contact link, uploaded logo/icon, custom theming, or white-labeling. Only minimum instance identity needed to distinguish a deployment may be configurable.
- Track D still owns the PWA manifest/service-worker shell needed for installation and Web Push. Do not promise offline application behavior.
- Web Push supports per-player audience selection between followed/My Games (default) and all new Game Requests (explicit opt-in), while preserving explicit permission/enablement. Never push the creator about their own newly created request.
- Discord delivery keeps one message per Game Request and edits it as state changes. Public message identity uses Game Nick, not provider identity. Full participant roster and durable request activity stay in LANsurfer; sanitize descriptions and disable mentions.
- For 1.0, Game Requests are instance-wide. Do not introduce a generic `context`, polymorphic parent, or other abstraction solely to prepare for future Event, Clan, or shared-service scoping.
- English-only is acceptable for stable 1.0. Do not add localization architecture or curated quick-info tag taxonomy merely because issues exist for them; both are deferred beyond 1.0 unless scope changes again.

## Parallel development rules

The 1.0 roadmap is organized into parallel development tracks. Multiple tracks may be implemented concurrently when their stated dependencies are satisfied.

- Treat [ROADMAP.md](docs/ROADMAP.md#development-model) as the authority for track scope, Pilot Gate, readiness, and cross-track dependencies.
- Do not block work merely because another 1.0 track is unfinished. Block only the integration work that the roadmap explicitly marks as dependent.
- Keep cross-track contracts small and explicit. The main planned contracts are Game Request writes/outbox (Track A → E), provider-independent authentication/onboarding inside Track B, public Game Nick rendering from B into A/E, and service worker/Web Push integration (Track D → E).
- Prefer separate branches/PRs per coherent track or cross-track contract. Avoid combining unrelated track implementation into one change merely because the work happens concurrently.
- A track may define an interface or integration point needed by another track, but must not reach into the other track's persistence or implementation details.
- If parallel changes touch the same schema, route, template, or composition-root wiring, coordinate the contract first and keep each change narrowly scoped to reduce merge conflicts.
- The dedicated Pilot Gate is a validation milestone, not a new architecture layer and not the stable 1.0 release gate.
- The 1.0 Integration Gate is where independently completed tracks are proven together. Do not weaken a track's local acceptance criteria on the assumption that integration testing will catch it later.
- Post-1.0 candidates remain out of bounds unless the roadmap is explicitly changed. Parallel development applies to approved 1.0 tracks, not speculative future scope.

## Pilot validation guidance

The selected LAN pilot is deliberately bounded and must not be encoded with a private event date or attendance estimate.

Before the Pilot Gate is green, repository evidence must cover the capabilities listed in `docs/ROADMAP.md`, including:

- Discord authentication and required Game Nick onboarding
- complete initial IGDB baseline before provider search is enabled for pilot participants
- Game Request discovery, 6-hour request-based Trending, and durable request-local activity
- one-message-per-request Discord delivery and personal Web Push
- real Web Push/PWA verification in the Desktop, Android, and iPhone platform categories; record concrete browser/version evidence in the validation artifact rather than pinning versions here
- production deployment and clean-host install
- backup plus an actually exercised restore
- at least one exercised upgrade path with existing data
- representative load/recovery rehearsal; record hardware, network, tested load envelope, response/update observations, and failures in the validation artifact rather than inventing permanent SLOs

Issue #15 is closed. PR #17 documented the confirmed SSE/HTTP/1.1 connection-exhaustion root cause and production HTTP/2 mitigation; PR #18 covered additional rapid-interaction hardening. Its remainder was split into #31 (committed browser regression tests), #32 (request-outcome and stream-lifecycle diagnostics), and #33 (creation idempotency). Keep regression coverage for the original hang and supported production HTTP behavior — the stream-lifecycle fix lives in `web/src/app.ts` and no Go test reaches it — and do not treat HTTP/2 alone as the root cause.

## Constraints requiring approval

If a task conflicts with project rules, stop and request approval. Changes to the technology stack, module boundaries, database, authentication/session or security model, deployment model, public domain model/terminology, approved 1.0 scope, or license require explicit approval. Small internal implementation choices do not.

In particular:

- Do not introduce an SPA, React/Vue/Svelte/Angular or meta-frameworks, frontend state frameworks, GraphQL, ORMs, UI component libraries beyond DaisyUI, or Sass/Less.
- Do not introduce Redis, message brokers, microservices, Kubernetes-specific application architecture, event sourcing, full CQRS, or additional required runtime services.
- Add dependencies or abstractions only for a current concrete need that existing tools cannot meet. Propose significant new direct dependencies before adding them.

## Workflow and documentation

- Inspect existing code before designing structures. Work within an approved 1.0 track whose dependencies in [ROADMAP.md](docs/ROADMAP.md#development-model) are satisfied. Do not implement post-1.0 candidates for completeness. Planned descriptions are not evidence that a feature already exists.
- Read the sections relevant to the task, not all documents on every change: [ARCHITECTURE.md](docs/ARCHITECTURE.md) owns technical decisions; [PRODUCT.md](docs/PRODUCT.md) owns behavior, terminology, and scope. Use [README.md](README.md) for setup.
- If a product or architecture decision changes, update ROADMAP, PRODUCT, ARCHITECTURE, and this guidance together where they make the same promise.
- Run relevant generators, tests, and the production build when practical. Summarize implementation decisions, verification limits, deliberate omissions, and any cross-track contract affected by the change.