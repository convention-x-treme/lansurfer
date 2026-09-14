# Implementation plan: IGDB catalog import and rich game metadata

## Status

Draft pilot plan.

Implementation must not start merely because this plan exists. The repository's current roadmap sequencing and `AGENTS.md` remain authoritative until the IGDB work is actually approved to begin.

## Specification

See [`spec.md`](spec.md).

## Existing system

The current `main` branch already provides the foundations this feature should extend rather than replace:

- `internal/game` owns the authoritative local Game catalog and subscription behavior.
- `game.Game` currently contains local core fields (`ID`, `Name`, `Slug`, `Active`, aliases, timestamps) and the catalog service delegates persistence through a narrow repository interface.
- PostgreSQL already has a generic `game_external_ids` table with uniqueness per provider/external ID and per Game/provider, so IGDB identity does not require a provider-specific relation.
- Game persistence lives under `internal/game/postgres` using explicit SQL/sqlc.
- global-administration HTTP behavior is already concentrated in `internal/web/handler/administration.go` and related administration pages/components.
- normal player Game browsing and Game Request rendering use separate web handlers/read models and should continue to work without any external provider.
- migrations are embedded goose migrations under `internal/platform/postgres/migrations`.
- operator configuration is environment-based under `internal/platform/config`.

The architecture already calls for an optional catalog provider boundary and explicitly keeps the local catalog authoritative.

## Proposed design

Implement IGDB as an optional adapter around a provider-neutral catalog boundary owned by the Game capability. The external adapter can fetch candidate data, but only explicit administrator commands persist selected values into the local catalog.

The design should keep three concerns distinct:

1. **Provider read model** — temporary IGDB search/preview data that is never treated as a local Game.
2. **Local catalog state** — authoritative Game fields stored in PostgreSQL.
3. **Admin commands** — explicit create/attach/refresh actions that validate an intended change and persist it transactionally.

### Module ownership

#### `game`

Owns:

- supported rich local Game metadata
- external Game identities through the existing generic external-ID relation
- provider-neutral search/lookup result types needed by administration
- application operations for create-from-provider, attach-provider-identity, refresh-preview, and apply-selected-refresh
- validation of which fields are accepted for persistence

The `game` module must not know Twitch/IGDB HTTP details.

#### IGDB adapter

Add a concrete adapter behind a narrow interface, preferably under a Game-owned infrastructure package such as `internal/game/igdb` unless inspection during implementation shows a better existing convention.

It owns:

- Twitch client-credentials token acquisition/caching
- IGDB HTTP requests and response decoding
- translation from provider responses into provider-neutral Game catalog result types
- provider-specific error classification needed by the application layer

It must not write PostgreSQL or local Game state.

#### `web`

Owns transport/UI only:

- admin search form/results
- provider result preview
- explicit create/attach actions
- refresh preview with field-level selection
- rendering stored rich metadata/covers in normal Game and Game Request views
- translating application errors into appropriate UI behavior

### Interfaces and boundaries

Introduce a consumer-owned interface in or near the Game application service, conceptually:

```go
type CatalogProvider interface {
    Search(ctx context.Context, query string) ([]CatalogGame, error)
    Get(ctx context.Context, externalID string) (CatalogGame, error)
}
```

`CatalogGame` should contain only the fields required for search/preview and supported import. It must not expose IGDB transport structs outside the adapter.

Provider configuration is optional. The application composition root should supply either an IGDB-backed provider when credentials are configured or no provider/disabled capability when they are not. Core Game services must not depend on provider availability.

The local persistence API should expose explicit operations rather than a generic "save arbitrary provider object" call. Exact method names can be chosen during implementation, but the boundary should support:

- finding an existing provider identity for a Game
- attaching a provider identity while respecting uniqueness
- persisting selected rich fields
- creating a normal local Game plus provider identity in one transaction when using create-from-provider

### Persistence and migrations

Reuse the existing `game_external_ids` relation for `provider = 'igdb'`.

Add only the local rich fields required by the approved scope. A likely minimal shape is:

- `games.summary` — nullable text
- `games.first_release_date` — nullable date (or timestamp/date representation selected after confirming how IGDB precision maps to product display)
- `games.cover_ref` — nullable controlled IGDB cover reference, not an arbitrary remote URL

For platform names, prefer a representation that preserves simple local authority and avoids premature catalog normalization. Before implementation, resolve the open question from the spec: either a small child relation such as `game_platforms(game_id, name)` or another explicit local representation. Do not create a generalized platform domain unless a concrete need requires it.

Add a new forward-only goose migration rather than modifying migration `00003`.

Persistence requirements:

- existing Games migrate with rich fields absent/null and continue working unchanged
- attaching `igdb` uses existing uniqueness constraints to prevent the same IGDB ID from being attached to multiple Games
- create-from-provider persists the Game, selected metadata, and external identity atomically
- refresh applies only the administrator-selected field set in one local transaction

Update sqlc queries/generated code through the existing `task generate` workflow.

### Configuration and external services

Add/activate environment configuration for:

- `LANSURFER_IGDB_CLIENT_ID`
- `LANSURFER_IGDB_CLIENT_SECRET`

Treat the pair as optional integration configuration. Decide during implementation whether a partially configured pair is a startup configuration error or disables the integration with a clear operator error; document the chosen behavior consistently.

The adapter should use Twitch's supported OAuth client-credentials flow and IGDB's supported API. Token lifetime should be respected and tokens cached in memory; do not persist access tokens in product tables.

Use the Go standard library HTTP client unless a concrete deficiency is identified. Do not introduce a provider SDK without approval.

### Search and preview flow

Suggested admin flow:

```text
Admin Games
   |
   +-- Search IGDB
           |
           v
      provider candidates
           |
           +-- Preview candidate
                   |
                   +-- Create new local Game
                   |
                   +-- Attach to selected existing Game
```

Search and preview are read-only with respect to PostgreSQL except ordinary request/session effects. No Game or external-ID row is created until an explicit create/attach action is submitted and authorized.

Search result fields should be intentionally small: provider ID, display name, release information sufficient for disambiguation, platforms, summary excerpt if useful, and cover preview reference if allowed by the same remote-cover policy.

### Create-from-provider

The create form should still let LANsurfer control required local fields such as name and slug. Provider suggestions may prefill them, but the administrator confirms the final local values.

On confirmation:

1. revalidate the submitted local Game fields
2. validate the provider identity/result needed for the operation; re-fetch by external ID where appropriate instead of trusting hidden form metadata
3. persist the local Game, `igdb` external identity, and selected supported metadata atomically
4. return the ordinary local Game/admin view

### Attach-to-existing

Attaching means associating the selected IGDB identity with an existing local Game. Do not use name matching to select the target automatically.

Initial attachment should not silently change local fields. If the product wants metadata at the same time, use an explicit preview/field-selection step equivalent to refresh so the administrator chooses what to import.

### Refresh preview and apply

Refresh should be a two-step operation:

1. fetch current provider data and compare it with the persisted local fields
2. render a field-level diff with explicit selections
3. on submit, apply only selected supported fields

Do not trust the browser to submit replacement field values as authoritative provider data. Prefer posting selected field identifiers plus the provider external ID/version context, then re-fetching current provider data before application, or otherwise use a short-lived server-controlled preview representation if re-fetch cost becomes problematic. The simpler safe approach should win.

### Cover reference and rendering

Persist a controlled IGDB cover reference rather than arbitrary image URLs. The renderer/helper owns construction of the final `https://images.igdb.com/...` URL and allowed size variant.

Required presentation behavior:

- CSP `img-src` permits only the approved IGDB image host in addition to existing local sources
- application responses use `Referrer-Policy: no-referrer` as documented
- templates render a bundled/local placeholder for no cover or image-load failure where practical
- no server-side cover proxy/cache is introduced

Audit the existing security-header middleware before changing headers so the exception remains narrow.

### Player-facing rich metadata

Extend read models, not domain-module coupling, for cross-module UI such as Game Request cards/details.

The ordinary Game catalog can expose stored summary/release/platform/cover data directly from local persistence/read models. Game Request pages/cards that combine Game + Request data should add these local Game fields through the existing read-query approach rather than calling IGDB or introducing provider dependencies into `gamerequest`.

No player-facing request should contact the IGDB API. The only permitted runtime remote content for players is the approved cover image host.

## Integration points

### Game catalog ↔ administration UI

Administration handlers depend on Game application interfaces. They must not call the IGDB adapter directly when the application service should own authorization-independent business behavior such as attaching identities or applying selected fields.

### Game catalog ↔ Game Request read models

Only stored local metadata crosses into Game Request presentation. `gamerequest` domain writes remain unchanged.

### Platform config ↔ composition root

The application wiring constructs the optional IGDB provider from environment configuration and injects it into the relevant Game/admin application service.

### Security headers ↔ cover presentation

The cover-host CSP exception must be implemented once at the existing security-header boundary rather than ad hoc per template.

## Risks and edge cases

### Provider rate limits/outages

Risk: admin flows become slow or fail.

Mitigation: bounded request timeouts, clear recoverable errors, no dependency from normal local workflows, and tests with fake provider responses/errors.

### Accidental local overwrite

Risk: refresh destroys intentional local edits.

Mitigation: explicit diff/selection and repository methods that apply only a selected field mask.

### Duplicate provider identity

Risk: one IGDB Game becomes attached to multiple local Games.

Mitigation: existing PostgreSQL provider/external-ID uniqueness plus user-friendly conflict translation.

### Arbitrary remote content

Risk: provider data becomes a generic remote-image/XSS/privacy channel.

Mitigation: store a constrained cover reference, fixed IGDB host construction, templ escaping, CSP allowlist, and no arbitrary remote URL persistence.

### Stale preview

Risk: provider data changes between preview and apply.

Mitigation: re-fetch provider data on final apply or use an explicitly bounded server-controlled preview token. Prefer re-fetch initially.

### Provider schema leakage

Risk: IGDB-specific structs spread through the domain/UI and make local behavior provider-dependent.

Mitigation: translate at the adapter boundary into deliberately small provider-neutral result types.

## Verification strategy

### Unit/domain tests

- provider-neutral catalog service validates search/import/attach/refresh decisions
- field-selection refresh changes only selected fields
- no implicit overwrite during attach
- provider failures return appropriate application errors without local mutation
- controlled cover-reference validation/URL construction

Use fake `CatalogProvider` implementations; do not make real IGDB requests in normal tests.

### PostgreSQL integration tests

- migration upgrades existing catalog data without loss
- provider identity uniqueness constraints behave as expected
- create-from-provider is atomic
- selected-field refresh persists exactly the intended fields/platform set
- existing catalog/subscription behavior still works with null rich metadata

### HTTP/UI tests

- non-admin users cannot access IGDB administration actions
- search/preview do not mutate the local catalog
- create/attach/refresh flows require CSRF-protected explicit writes
- provider error states render recoverably
- local catalog and Game Request pages render with and without metadata
- CSP/security-header expectations include only the approved IGDB cover host

### Manual/provider verification

Against a real operator-owned Twitch/IGDB test configuration:

- search a known Game
- preview and create/attach it
- verify attribution
- verify cover loading and browser referrer behavior
- simulate missing/invalid credentials
- verify refresh after changing a local field

No real credentials belong in the repository or test fixtures.

## Deliberate omissions

- other catalog providers
- background synchronization
- automatic matching/merging
- cover download/cache/proxy
- generalized platform taxonomy
- player-triggered provider searches/imports
- provider-dependent Game Request domain behavior

## Open technical questions

The following should be resolved before changing this plan from Draft to Approved:

1. **Platform persistence:** child relation of local display names vs. another minimal representation.
2. **Release-date precision:** whether the local field should be a SQL `date`, timestamp, or representation that preserves provider uncertainty while meeting current UI needs.
3. **Cover reference format:** exact constrained value persisted from IGDB and one/few approved display size variants.
4. **Partial IGDB configuration:** fail startup vs. disable integration with operator-visible diagnostics when only one credential is supplied.
5. **Refresh finalization:** re-fetch on apply (preferred initial plan) vs. short-lived server-side preview state if provider/API constraints make re-fetch unsuitable.
