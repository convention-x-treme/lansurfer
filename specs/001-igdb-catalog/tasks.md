# Implementation tasks: IGDB catalog import and rich game metadata

## Status

Draft.

These tasks are part of the SDD pilot and are not authorization to start the later IGDB roadmap milestone before repository policy permits it.

## References

- Specification: [`spec.md`](spec.md)
- Implementation plan: [`plan.md`](plan.md)
- Existing roadmap/product/architecture on `main` remain authoritative.

## Task rules

- Resolve the blocking open technical questions in `plan.md` before implementation tasks that depend on them.
- Keep provider HTTP details out of the Game domain/application types.
- Keep all normal player workflows independent from IGDB API availability.
- Each implementation task must include the relevant generated-code/test updates in the same PR unless splitting them is explicitly justified.
- Mark a task complete only after recording what was verified.

## Tasks

### TASK-001 — Resolve persisted rich-metadata model

**Depends on:** none

**Outcome**

The plan has an approved decision for platform persistence, release-date precision, and controlled cover-reference format.

**Implementation notes**

- Inspect current Game catalog/read-model queries and intended UI usage.
- Prefer the smallest local model that satisfies the specification.
- Do not create a generalized platform/catalog taxonomy without a concrete requirement.
- Update `plan.md` with the selected schema and rationale.

**Verification**

- [ ] Decision covers summary, first release date, platform names, and one cover reference.
- [ ] Existing Games can represent "no rich metadata" without backfill guesses.
- [ ] Cover representation cannot become an arbitrary remote URL channel.

**Done when**

`plan.md` contains concrete persisted types/schema for every supported rich field and no blocking persistence question remains.

---

### TASK-002 — Add local rich Game persistence

**Depends on:** TASK-001

**Outcome**

PostgreSQL and the Game persistence layer can store/read the approved rich metadata while preserving all existing catalog behavior.

**Implementation notes**

- Add a new forward-only goose migration; do not rewrite `00003_events_and_game_catalog.sql`.
- Reuse existing `game_external_ids` for IGDB identity.
- Extend `internal/game` local models only with fields that are genuinely part of the local Game representation.
- Add/update Game sqlc queries and run `task generate`.
- Ensure old rows remain valid with absent metadata.

**Verification**

- [ ] Unit tests for validation/mapping as applicable.
- [ ] PostgreSQL integration test upgrades existing Game rows without data loss.
- [ ] Existing catalog create/update/list/subscription tests remain green.
- [ ] `task generate`
- [ ] `task test:integration`
- [ ] `task lint`

**Done when**

Rich metadata is durable local catalog state and existing installations upgrade safely without needing IGDB.

---

### TASK-003 — Define provider-neutral catalog interface and application operations

**Depends on:** TASK-001; may proceed in parallel with TASK-002 once field types are decided

**Outcome**

The Game capability exposes a narrow provider-neutral contract and explicit application operations for search/preview, create-from-provider, attach, refresh preview, and selected refresh application.

**Implementation notes**

- Define provider-neutral result types; do not expose IGDB transport structs.
- Keep `CatalogProvider` consumer-owned in the Game capability.
- Use fake providers in tests.
- Model attach so it cannot silently overwrite local fields.
- Model refresh application using an explicit selected-field set/mask rather than generic map-based persistence.

**Verification**

- [ ] Search/preview causes no persistence.
- [ ] Attach alone causes no implicit metadata overwrite.
- [ ] Selected refresh changes only selected fields.
- [ ] Provider errors do not mutate local state.
- [ ] Unit/domain tests cover the application decisions.
- [ ] `task test`
- [ ] `task lint`

**Done when**

The application behavior can be tested entirely with a fake provider and without IGDB-specific types outside the adapter boundary.

---

### TASK-004 — Implement optional IGDB adapter and configuration

**Depends on:** TASK-003

**Outcome**

Configured installations can search/fetch IGDB through the provider-neutral interface; unconfigured installations continue normally.

**Implementation notes**

- Add IGDB/Twitch credentials to the existing environment configuration model.
- Resolve and document the partial-configuration behavior from `plan.md`.
- Implement Twitch client-credentials token handling with in-memory expiry-aware caching.
- Implement bounded HTTP timeouts and IGDB search/get requests.
- Translate provider responses into provider-neutral result types.
- Use standard-library HTTP unless a dependency is separately approved.
- Never log secrets or access tokens.

**Verification**

- [ ] Adapter tests use `httptest` or equivalent fake HTTP endpoints, not live IGDB.
- [ ] Missing configuration leaves normal application startup/workflows usable according to the approved config behavior.
- [ ] Provider 4xx/429/5xx/network failures map to recoverable errors.
- [ ] Tokens/credentials do not appear in logs/test snapshots.
- [ ] `task test`
- [ ] `task lint`

**Done when**

A fake/controlled HTTP test proves the adapter can authenticate, search, fetch, and fail safely behind the provider-neutral interface.

---

### TASK-005 — Implement transactional create/attach/refresh persistence

**Depends on:** TASK-002, TASK-003

**Outcome**

Explicit administrator-approved provider actions persist local Games, IGDB identity, and selected metadata atomically and safely.

**Implementation notes**

- Add repository methods/transactions tailored to the application operations.
- Create-from-provider must atomically persist local Game + selected metadata + `igdb` external identity.
- Translate existing provider-ID uniqueness violations into explicit application/domain errors.
- Attach an IGDB identity without overwriting local metadata unless a separate selected import is confirmed.
- Refresh persistence updates only the approved selected fields/platform values.

**Verification**

- [ ] PostgreSQL test proves create-from-provider is atomic on failure.
- [ ] Duplicate IGDB external ID cannot attach to two Games.
- [ ] One Game cannot receive two IGDB identities.
- [ ] Attach preserves existing local metadata.
- [ ] Refresh persists exactly selected fields.
- [ ] `task test:integration`
- [ ] `task lint`

**Done when**

All local mutations required by the spec are transaction-safe and enforced independently of the web UI.

---

### TASK-006 — Add administrator IGDB search and preview UI

**Depends on:** TASK-003, TASK-004

**Outcome**

Global administrators can search IGDB and inspect a candidate without mutating local catalog state.

**Implementation notes**

- Extend existing administration routing/handlers/pages rather than adding an internal JSON API.
- Enforce existing global-admin middleware/authorization.
- Keep search GET/read behavior separate from create/attach POST writes.
- Render provider errors as recoverable admin feedback.
- Show only fields needed for disambiguation and supported import.

**Verification**

- [ ] Non-admin access is denied.
- [ ] Search/preview never creates/updates a Game or external ID.
- [ ] Unconfigured/provider-error states are understandable and do not break the admin catalog.
- [ ] HTTP/handler tests cover authorization and no-mutation behavior.
- [ ] `task test`
- [ ] `task lint`

**Done when**

An administrator can safely reach a read-only preview from a provider search result.

---

### TASK-007 — Add explicit create-from-IGDB and attach flows

**Depends on:** TASK-005, TASK-006

**Outcome**

An administrator can create a local Game from a previewed IGDB candidate or explicitly attach that candidate to a chosen existing local Game.

**Implementation notes**

- Keep local Game name/slug confirmation explicit.
- Re-fetch provider data on write where appropriate instead of trusting client-submitted provider metadata.
- Use CSRF-protected POST actions.
- Make attachment target selection explicit; no name-based auto-match.
- On uniqueness conflict, preserve existing data and show a useful error.

**Verification**

- [ ] Create flow produces expected local Game + IGDB identity + explicitly selected metadata.
- [ ] Attach flow records identity without hidden overwrite.
- [ ] Similar/equal names do not trigger automatic attachment/merge.
- [ ] Non-admin and CSRF failure paths are covered.
- [ ] HTTP + PostgreSQL integration coverage exists for successful/failed writes.
- [ ] `task test`
- [ ] `task test:integration`
- [ ] `task lint`

**Done when**

Both explicit import paths satisfy the specification without provider-driven implicit merging.

---

### TASK-008 — Add refresh preview and field-level apply

**Depends on:** TASK-004, TASK-005, TASK-007

**Outcome**

An administrator can compare current IGDB data with local metadata and selectively apply individual supported fields.

**Implementation notes**

- Render local vs provider values for supported fields only.
- Use explicit field selections.
- Resolve the refresh-finalization strategy from `plan.md`; default preference is re-fetching provider data on final apply.
- Keep unselected fields/platform state unchanged.

**Verification**

- [ ] Preview itself does not mutate local state.
- [ ] Selecting one field updates only that field.
- [ ] Local edits survive when their fields are not selected.
- [ ] Provider failure between preview/apply leaves local state unchanged and yields a recoverable error.
- [ ] Tests cover no-change, partial-change, and provider-failure cases.
- [ ] `task test`
- [ ] `task test:integration`
- [ ] `task lint`

**Done when**

Refresh is visibly administrator-controlled at field level and cannot silently replace local edits.

---

### TASK-009 — Add controlled cover rendering and security policy

**Depends on:** TASK-001, TASK-002

**Outcome**

Stored IGDB cover references render through a fixed approved host policy with a local fallback and documented privacy behavior.

**Implementation notes**

- Centralize cover URL construction from the constrained stored reference.
- Allow only `images.igdb.com` as the new remote image host.
- Update existing CSP/security-header configuration narrowly.
- Ensure `Referrer-Policy: no-referrer` is applied consistently.
- Use a bundled/local placeholder when cover data is absent or image loading fails where browser/template behavior permits.
- Do not add a proxy/cache.

**Verification**

- [ ] Arbitrary remote URLs cannot be produced from stored catalog data.
- [ ] Security-header tests assert the exact approved IGDB host and no broader remote-content wildcard.
- [ ] Game without a cover renders normally.
- [ ] Invalid/missing cover uses local fallback behavior.
- [ ] `task test`
- [ ] `task lint`

**Done when**

The cover exception is narrow, testable, and cannot become a general remote asset facility.

---

### TASK-010 — Surface stored rich metadata in player-facing views

**Depends on:** TASK-002, TASK-009

**Outcome**

The local Game catalog and relevant Game Request views present stored rich metadata without any IGDB API dependency in player requests.

**Implementation notes**

- Extend local read models/query SQL for catalog and Game Request presentation.
- Preserve module ownership: Game Request domain code must not call the provider.
- Keep cards concise; detailed metadata can live on appropriate detail views according to existing UX conventions.
- Render gracefully for legacy/local Games with no rich metadata.

**Verification**

- [ ] Catalog renders Games with and without rich metadata.
- [ ] Game Request cards/details use only stored local metadata.
- [ ] Player pages remain functional with IGDB disabled or simulated provider outage.
- [ ] No player-facing request path invokes the IGDB API in tests/fakes.
- [ ] Relevant HTTP/read-model tests pass.
- [ ] `task test`
- [ ] `task lint`

**Done when**

Players receive the approved richer presentation while provider availability remains irrelevant to normal browsing/Game Request use.

---

### TASK-011 — Add provider attribution, setup, and privacy documentation

**Depends on:** TASK-004, TASK-009

**Outcome**

Operators/users can understand how to configure IGDB, the provider dependency boundaries, attribution requirements, and direct-cover privacy implications.

**Implementation notes**

- Document Twitch/IGDB credential configuration without embedding secrets.
- Document that IGDB is optional and which workflows degrade when absent.
- Include required attribution according to the provider requirements in force at implementation time.
- State that direct cover loading discloses viewer IP addresses to IGDB.
- Document the fixed cover-host/no-proxy design.

**Verification**

- [ ] Documentation matches actual config names/behavior.
- [ ] Attribution/provider terms have been checked against current IGDB requirements at implementation time.
- [ ] Privacy disclosure is visible in the appropriate documentation/UI surface.

**Done when**

A new operator can configure or deliberately leave IGDB unconfigured and understands the privacy/availability trade-offs.

---

### TASK-012 — Complete IGDB feature verification

**Depends on:** TASK-007, TASK-008, TASK-009, TASK-010, TASK-011

**Outcome**

The implementation is demonstrably complete against `spec.md` rather than merely having all code paths present.

**Implementation notes**

- Trace every `REQ-xxx` and acceptance criterion in `spec.md` to an automated test, documented manual verification, or explicit justification.
- Run the repository verification workflows.
- Perform one manual smoke test with operator-owned IGDB/Twitch credentials; never commit them.
- Record any deliberate omissions in the implementation PR.

**Verification**

- [ ] Every specification requirement is accounted for.
- [ ] `task generate`
- [ ] `task lint`
- [ ] `task test`
- [ ] `task test:integration`
- [ ] `task build`
- [ ] Manual configured-provider smoke test recorded.
- [ ] Unconfigured/provider-outage core-workflow smoke test recorded.

**Done when**

The feature can be reviewed from specification → plan → tasks → tests/observable behavior with no unexplained scope gaps.
