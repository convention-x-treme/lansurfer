# LANsurfer Roadmap

This roadmap carries LANsurfer from its completed web prototype to a focused stable 1.0 self-hosting release. The 1.0 work is organized as **parallel development tracks**, not a linear sequence of feature milestones. Track dependencies describe only the points where work must converge; otherwise tracks may advance independently.

Product behavior and scope are governed by [PRODUCT.md](PRODUCT.md). Technical decisions and module boundaries are governed by [ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](../AGENTS.md).

Do not implement post-1.0 candidates for completeness. Within 1.0, work may proceed in any track whose stated integration dependencies are satisfied.

## 1.0 direction

LANsurfer 1.0 is optimized for a simple deployment model: **one LANsurfer installation serves one LAN party or gaming event context**.

For stable 1.0:

- Game Requests are instance-wide product objects. Creating, discovering, and joining a request must not require selecting or joining an Event, Clan, or other parent context.
- Do not introduce a generic `context`, polymorphic parent, or similar abstraction merely to prepare for future Event/Clan scoping.
- Multi-Event participation, Open Play, Event membership/access boundaries, Clans, and other persistent shared-service/community features are outside 1.0.
- Generic OIDC and Discord OAuth2 are independent supported login paths. Accounts may explicitly link both providers; identities are never merged by email.
- Public player identity in 1.0 is the unique, case-insensitive **Game Nick**. Rich biographies, per-Game player handles, and richer public profile pages are deferred beyond 1.0.
- English-only is acceptable for stable 1.0. Internationalization/localization is deferred beyond 1.0.
- Curated Game Request quick-info tags and tag-based filtering are deferred beyond 1.0.
- LANsurfer keeps fixed/default branding. A full administrator-managed branding editor, uploaded instance logos/icons, white-labeling, and custom theming are outside 1.0. Only minimum instance identity needed for a recognizable deployment may remain configurable.
- LANsurfer owns an authoritative local Game catalog. IGDB is an optional general deployment integration that provides a locally synchronized discovery index; it is mandatory only for the selected LAN pilot gate.
- Durable notifications use Web Push and one instance-level Discord destination. There is no persistent in-app notification inbox.

Post-1.0 items are intentionally **not scheduled to a version** unless the roadmap is explicitly changed.

## Completed prototype baseline

The completed prototype established:

- a Go modular monolith with PostgreSQL, migrations, sqlc, templ, HTMX, Tailwind, and DaisyUI
- generic OIDC login, local users, revocable PostgreSQL sessions, opaque cookies, and global administrator promotion
- Event and local Game catalog administration, aliases, external-ID storage, and Game subscriptions
- Game Request creation, joining, leaving, cancellation, expiration, and transaction-safe capacity enforcement
- SSE-driven live updates with polling fallback
- embedded production assets, responsive desktop/mobile UX, security controls, integration coverage, and self-hosting foundations

The prototype schema and behavior are an input to the 1.0 migration path, not a promise that every prototype interface or relationship remains part of the 1.0 product model.

## Development model

Tracks A–F may run concurrently. A dependency blocks only the dependent integration work, not unrelated work inside the track.

| Track | Scope | Planning status | Integration dependencies |
| --- | --- | --- | --- |
| **A** | Single-Event Core & Request Discovery | Prioritized | Track B only for final public Game Nick rendering; Track E consumes A's outbox contract |
| **B** | Game Nick & Authentication | Prioritized | None |
| **C** | Game Catalog & IGDB Discovery | Prioritized | None for provider/index work; A for final request-card/read-model presentation where needed |
| **D** | Minimum Instance Identity & PWA | Prioritized | None; service-worker contract is consumed by E |
| **E** | Durable Notifications | Prioritized with integration dependencies | A for Game Request outbox hookup; D for browser Web Push integration; B only where Game Nick is rendered |
| **F** | Distribution & Operations | Prioritized | Final upgrade/restore validation depends on persistent migrations from A–E |

### Planning status and delivery evidence

**Prioritized** describes planning, not verified implementation progress. Mark work **In progress** only with a linked delivery issue/PR and a responsible implementer. **Integration ready** requires demonstrated track-local outcomes, stable contracts, and a linked list of remaining cross-track checks. **Done** additionally requires merged work and applicable integration checks.

Review checkpoint: **2026-09-15**, against `main` at [`e356791`](https://github.com/convention-x-treme/lansurfer/commit/e356791affdef6a2eb4968c1256c17be870e8407). At that revision the current Game Request service still requires a current Event during creation, so the single-event target is not evidence that Track A is delivered.

Do not infer delivery dates, staffing, owners, or completion percentages from this roadmap.

### Cross-track contracts

1. **Track A → Track E: Game Request notification contract**
   - Track A owns Game Request writes and the transactional outbox integration point.
   - Track E owns durable workers, retry behavior, delivery persistence, and transport adapters.
   - Domain write + notification intent commit in the same PostgreSQL transaction where notification work is required.
   - Agree stable request/work identifiers, change kinds, ordering, replay, and stale-update handling. A failed/rolled-back mutation leaves no notification intent.

2. **Track B internal: authentication → Game Nick onboarding**
   - OIDC and Discord resolve to the same local LANsurfer User model.
   - Game Nick onboarding is provider-independent and occurs after successful authentication when required.
   - A Discord display name may be a suggested Game Nick value only; LANsurfer owns uniqueness/public identity.

3. **Track B → Tracks A/E: public player identity**
   - Public request/activity/Discord identity uses Game Nick, never provider usernames/emails.
   - Only final presentation depends on this contract; core request writes and notification transport work do not wait for it.

4. **Track D → Track E: service-worker contract**
   - Track D owns the installable web shell, manifest, service-worker registration/lifecycle/update behavior.
   - Track E owns push permission, subscriptions, push event handling, eligibility, and payloads.

5. **Track F consumes all tracks at validation**
   - Release automation/tooling may proceed early.
   - Final migration, backup/restore, and upgrade rehearsals wait until the relevant persistent schema is stable enough to validate.

# Track A — Single-Event Core & Request Discovery

## Goal

Align the prototype with the focused 1.0 deployment model so the installation itself is the event context, while making current Game Requests easy to discover and understand.

## Expected outcomes

### Single-event core

- Treat the LANsurfer installation itself as the 1.0 event context.
- Let signed-in players discover, create, join, leave, close, and cancel Game Requests without selecting or joining an Event, Open Play, Clan, or other parent context.
- Preserve existing prototype Game Requests during forward migration while removing Event membership/lifecycle requirements from normal request behavior.
- Keep capacity enforcement concurrency-safe.
- Stabilize Game Request write/transaction boundaries used by Track E's durable outbox.

### Request discovery

Stable 1.0 includes:

- search by Game name and aliases
- followed/My Games discovery
- open-capacity/free-places filtering

Additional filters proposed in issue #12 are not automatically 1.0 requirements unless explicitly approved later.

### Trending

- Provide a compact request-based Trending view over a rolling **6-hour** window.
- State the window visibly, for example `Trending · last 6 hours`.
- Derive ranking from local Game Request activity, not persisted search queries or page views.
- Allow a sparse/empty view during quiet periods rather than silently widening the window.

### Durable request-local activity

Persist chronological successful:

- create
- join
- leave
- close
- cancel

Activity is separate from current membership and separate from notification delivery history. Failed/rejected/duplicate mutations do not create activity entries. Leaving removes current membership but not historical activity. Public actor identity uses Game Nick where relevant.

Do not generalize this into event sourcing, generic audit logging, chat/comments, or a global activity feed.

## Acceptance

The complete Game Request flow works without Event/Clan/context selection or membership. Existing requests survive migration, capacity invariants remain concurrency-safe, request discovery/Trending/activity behave as specified, and Track E has a stable transactional integration point.

### Verification evidence

- Exercise a fresh installation and populated prototype fixtures with absent/current/historical Event states. Preserve request IDs, creators, memberships, status, and timestamps; document legacy Event-data treatment.
- Cover duplicate/overlapping joins/leaves, competing joins for the last place, and close/cancel races.
- Prove failed/rejected/duplicate mutations do not produce durable activity.
- Prove the 6-hour Trending window and filters use the intended read model without raw-query tracking.
- At the A/E boundary, prove a rollback leaves neither the domain change nor its outbox work, while a committed change retains notification intent across restart-before-delivery.
- Keep regression coverage for rapid HTMX/SSE interaction and supported production HTTP behavior. Issue #15 remains open until its accepted hardening scope is complete: PR #17 documents the confirmed SSE/HTTP/1.1 connection-exhaustion root cause and production HTTP/2 mitigation, while PR #18 covers additional rapid-interaction hardening. HTTP/2 is a mitigation for the original browser connection-pool symptom, not the root cause itself.

# Track B — Game Nick & Authentication

## Goal

Give every player one stable public LANsurfer identity and support practical authentication without coupling LANsurfer permanently to Discord.

## Expected outcomes

### Public identity

- Require a unique, case-insensitive public **Game Nick** after first successful login; upgraded prototype users complete this once.
- Use Game Nick in request participant lists, durable request activity, Discord request messages, and other public player references.
- Keep authentication-provider name/email private to the account owner in normal product UI.
- Do **not** require biography, per-Game handles, richer public profiles, or avatar uploads for stable 1.0.

### Authentication providers

- Keep generic OIDC provider-neutral.
- Add Discord OAuth2 authorization-code login as a separate provider/callback.
- Support OIDC-only, Discord-only, and explicitly linked accounts.
- Link/unlink only through explicit signed-in account flows; at least one identity remains linked.
- Never merge identities/accounts by email.
- Persist short-lived one-use flow state bound to browser/provider/operation/user where applicable.
- Keep local user IDs, revocable PostgreSQL sessions, opaque cookies, and the no-local-password model.

## Acceptance

A user can authenticate through either stable-1.0 provider, explicitly link both when desired, is never implicitly merged by email, completes provider-independent Game Nick onboarding, and exposes no provider-private identity in public surfaces.

### Verification evidence

- Exercise OIDC-only, Discord-only, and both-provider configurations plus upgraded users reaching onboarding through existing sessions.
- Reject expired/replayed/wrong-browser/wrong-provider/wrong-operation flows and identity-link conflicts.
- Enforce case-insensitive Game Nick uniqueness under concurrent claims.
- Check pages, fragments, read models, activity, and Discord rendering for provider-name/email leakage.

# Track C — Game Catalog & IGDB Discovery

## Goal

Provide fast player-facing Game discovery without making an external provider authoritative or making core LANsurfer operation depend on provider availability.

## Expected outcomes

### General deployment behavior

- LANsurfer's local Game catalog remains authoritative.
- IGDB is optional for general/self-hosted deployments.
- With IGDB disabled/unconfigured, no provider sync runs; player search uses local Games only; global administrators can create/manage local Games manually.
- Ordinary signed-in players do not get free-form local Game creation in local-only mode.
- Disabled/unconfigured IGDB is a normal `disabled` provider state, not an application health failure.

### Synchronized provider index

When IGDB is enabled:

- Start the initial provider sync asynchronously after LANsurfer becomes available; do not block application startup.
- Synchronize lightweight search/classification metadata for **all IGDB game records**; do not filter provider record types at ingestion.
- Store only fields useful for search/classification/disambiguation, such as provider ID, names/aliases, type/category, useful relationship signals, platform/release context, and cover-reference metadata.
- Do not mirror the full provider graph or cover image bytes.
- Do not expose partially synchronized IGDB player results during the first baseline sync.
- After a successful baseline, refresh automatically once per day and provide global administrators **Sync now**.
- Retry refresh failures independently of core application traffic.
- Continue serving the last successful provider index while a later refresh is running/failed.
- Expose at least `disabled`, `not synced`, `syncing`, `current`, `stale`, and `failed` operator states.

### Player-visible type policy

Ordinary player search shows by default:

- main games
- standalone expansions
- remakes
- remasters
- ports
- independently playable expanded/edition variants

The following remain synchronized but hidden from ordinary player search by default:

- DLC/add-ons
- bundles
- mods
- episodes/seasons
- packs
- updates

Use semantic categories in product documentation rather than depending on fragile hardcoded provider numeric enum values.

### Materialization into authoritative Games

- Searching/viewing a provider result does not create a local Game.
- Following a provider result or creating a Game Request for it automatically and atomically materializes an authoritative local Game.
- Any signed-in user may trigger this activation when IGDB is enabled; no administrator approval is required.
- Concurrent activation of the same provider record converges on one local Game through provider/external-ID identity.
- Never merge Games by name.
- Materialized local Games remain authoritative and usable if IGDB is unavailable, disabled, or changes later.

### Removal/archival policy

When a provider record disappears:

- remove the provider-index record if no authoritative local Game references it
- if an authoritative local Game references it, keep the provider identity archived/tombstoned and exclude it from ordinary new-player provider discovery
- never remove/invalidate the local Game, follows, Game Requests, or request activity merely because the provider source disappeared

If IGDB is disabled after prior use, promoted local Games remain intact. A previously synchronized provider index may remain stored but is not exposed as active provider search while disabled. Re-enabling resumes synchronization without recreating authoritative Games.

### Covers

IGDB covers may load directly from `images.igdb.com` only. Keep the fixed CSP allowlist, `Referrer-Policy: no-referrer`, local placeholder behavior, required attribution/IP-disclosure documentation, and no local cover proxy/cache.

## Acceptance

Local-only operation remains fully usable; enabled-provider discovery uses a complete baseline rather than partial first-sync results; materialization is atomic/idempotent; daily/manual refresh is resilient; and provider removal cannot silently destroy authoritative LANsurfer data.

### Verification evidence

- Exercise disabled/unconfigured state and administrator-only manual local Game management.
- Interrupt/restart the initial sync and verify provider results remain hidden until a complete baseline succeeds.
- Exercise daily/manual refresh, provider timeouts/rate limits/errors, stale/failed state, and continued use of the last successful index.
- Test default visible/hidden type policy and search disambiguation.
- Race multiple follow/request activations for the same provider ID and prove one authoritative local Game results.
- Exercise disable/re-enable and provider-record deletion with both unreferenced deletion and referenced archival/tombstone paths.
- Verify cover CSP/no-referrer/placeholder/attribution behavior.

# Track D — Minimum Instance Identity & PWA

## Goal

Keep LANsurfer recognizable as an installation while providing the PWA/service-worker behavior required for installability and Web Push, without building a branding-management product.

## Expected outcomes

- Keep fixed/default LANsurfer colors, typography, accessibility behavior, component semantics, and bundled branding.
- Permit only the minimum configurable instance identity needed to distinguish a deployment where necessary.
- Do not add a stable-1.0 administrator surface for configurable welcome text, contact link, uploaded logo, uploaded application icon, custom theme, or white-labeling.
- Serve a manifest with stable application identity/start URL and appropriate bundled/default presentation.
- Add the minimal service-worker shell needed for installation and Web Push; do not promise offline application behavior.
- Define service-worker registration, lifecycle, update, and logout behavior so Track E can attach push handling safely.

## Acceptance

The application is recognizable/installable, the manifest/service worker are stable enough for Track E, and no removed full-branding surface reappears indirectly.

### Verification evidence

- Record install/update behavior in the required pilot platform categories and later release validation matrix.
- Verify service-worker update/logout behavior does not expose private stale content.
- Verify unsupported installation/push capability leaves ordinary browser workflows usable.

# Track E — Durable Notifications

## Goal

Deliver relevant Game Request changes beyond the open browser without coupling external transport failure to core user actions.

## Expected outcomes

### Notification architecture

- Add focused durable notification delivery through PostgreSQL outbox/work persistence and asynchronous workers.
- Persist delivery status, retry schedule, external message references, and enough ordering/replay information to avoid stale Discord edits overwriting newer state.
- Never fail/roll back a Game Request action because an external delivery fails.
- Keep SSE as the ephemeral in-browser update path.
- Retry 429 via `Retry-After`; retry network/5xx failures with capped exponential backoff+jitter; expose exhausted/permanent/uncertain delivery for authorized diagnosis/manual retry.
- Do not add a persistent in-app notification inbox.

### Web Push

- Use VAPID and the approved narrow Web Push adapter.
- Support multiple labeled subscriptions per user.
- Require feature detection and explicit user gesture/permission.
- Preserve explicit enablement/consent.
- Support two player audience modes:
  - **followed/My Games only** — default
  - **all new Game Requests** — explicit opt-in
- Do not push the creator for their own newly created request.
- Re-check eligibility before delayed/retried sends.
- Keep payload limited to Game, time, deep link, and minimum instance context; omit creator identity, description, roster, and activity.

### Discord delivery

- Keep Discord delivery independent from Discord authentication.
- Configure at most one instance-level Discord destination through Discord `webhook.incoming` OAuth; do not accept pasted webhooks or add a bot/gateway.
- Encrypt stored webhook identifiers/tokens at rest and never reveal stored secrets.
- Create one message per Game Request with `wait=true`, store its message ID, and edit that same message as the request changes.
- Include Game, planned start time, status, capacity/free-place summary, creator Game Nick, short sanitized description, and LANsurfer deep link.
- Do not mirror the full participant roster or durable activity history.
- Provider identity data is not public identity.
- Disable Discord mentions.

## Acceptance

Outbox persistence survives restarts, retries are bounded/observable, Web Push targeting/privacy are correct, Discord keeps one current message per request, and provider outages never prevent Game Request actions.

### Verification evidence

- Exercise worker interruption, abandoned work recovery, duplicate work, out-of-order changes, and provider-accepted-but-response-lost uncertainty.
- Test 429 handling, capped retries, exhausted/permanent failures, and manual retry authorization.
- Recheck push eligibility after unfollow/toggle-off/audience changes and creator exclusion.
- Demonstrate disabled/failing transports do not block request writes.

# Track F — Distribution & Operations

## Goal

Make LANsurfer reproducibly installable, upgradable, backed up, restored, and diagnosable by self-hosting operators.

## Expected outcomes for stable 1.0

- Publish official Linux amd64/arm64 binaries and matching multi-architecture GHCR image.
- Add `lansurfer version`, healthcheck, encryption-key generation, and Web Push key-generation commands.
- Tag stable artifacts by exact version, major.minor, major, and `latest`; prereleases by exact version only.
- Publish checksums, SBOMs, and provenance attestations.
- Use native Go tooling plus pinned official Docker/GitHub Actions; do not add GoReleaser.
- Provide production Compose with pinned application image, PostgreSQL not exposed on the host, and only application/database as required services.
- Keep TLS/reverse proxy as operator responsibility and document supported operation.
- Document all configuration, administrator bootstrap, backup/restore, encryption/VAPID key backup, upgrades, and rollback limits.
- Keep migrations forward-only and test supported populated upgrade paths.

## Acceptance

Release artifacts are reproducible/verifiable, clean-host deployment works, backup/restore is exercised, and supported populated upgrades preserve data.

### Verification evidence

- Record source revision, pinned build inputs, architectures, checksums, SBOMs, attestations, and artifact-verification steps.
- Run documented clean-host installation without developer checkout/unlisted tools/exposed PostgreSQL.
- Restore populated DB + required keys into a clean deployment and verify Games/subscriptions, users/Game Nicks, requests/memberships/activity, provider identities/index state as applicable, and notification references.
- Exercise a documented populated upgrade plus migration-failure/restart recovery; failed migration prevents HTTP startup.

# Pilot Gate

The next validation milestone is a deliberately bounded real LAN pilot. It is **not** the stable 1.0 release gate and does not require every stable-1.0 capability. Event-specific dates and attendance numbers are intentionally not recorded in the repository.

The Pilot Gate is green only when the following approved pilot subset is implemented and evidenced.

## Player access and identity

- Discord login is the required pilot authentication path. Requiring a Discord account is acceptable for this pilot.
- After first successful Discord login, every participant chooses/confirms a unique case-insensitive Game Nick before using core Game Request flows.
- OIDC is not a pilot prerequisite, but remains stable-1.0 scope.
- English UI is sufficient for the pilot.
- Rich profiles, localization, curated request tags, and full branding are not pilot prerequisites.

## Game discovery and requests

- IGDB is configured for the pilot.
- At least one **complete successful initial IGDB baseline sync** must finish before pilot participants are admitted to provider discovery.
- The app itself may start before the sync; local authoritative Games/core flows remain independent of provider startup.
- Partial first-sync provider results are not exposed to players.
- Search supports Game name/aliases plus followed/My Games and open-capacity/free-places discovery.
- Trending uses the approved visible rolling 6-hour request-activity window.
- Durable request-local activity exists for create/join/leave/close/cancel.

## Notifications and PWA

- One instance-level Discord destination is configured.
- New Game Requests publish one Discord message and later request changes edit that same message.
- Discord content follows Track E's approved public fields/privacy rules.
- Personal Web Push is working.
- Player Web Push audience is configurable between followed/My Games (default) and all new Game Requests (explicit opt-in); the creator receives no creation push for their own request.
- Real PWA/Web Push verification exists for **Desktop, Android, and iPhone** platform categories.
- The validation artifact records the concrete browser/version used per platform category and demonstrates the applicable install, permission/consent, subscription, and real push-receipt flow. Browser versions are not permanently pinned in this roadmap.

## Instance/PWA presentation

- Minimum recognizable instance identity is sufficient.
- Manifest/service-worker behavior required for installability and Web Push is working.
- No full administrator branding editor is required.

## Operations and reliability

- Production deployment path is exercised.
- Installation on a clean host is exercised.
- Backup is documented **and an actual restore is exercised**.
- At least one upgrade path using existing data is exercised.
- A representative documented load/recovery rehearsal covers realistic parallel browsing/search, request create/join/leave flows, SSE updates, and notification behavior.
- The validation artifact records tested hardware, network conditions, client/browser mix where relevant, tested load envelope, observed response/update behavior, failures, and recovery characteristics. Do not invent permanent concurrency/latency numbers in the roadmap.
- Supported production deployment preserves required HTTP behavior and core interaction reliability under normal regression testing. Issue #15 is not waived by this documentation: a reproducible persistent hang in a supported core workflow remains a pilot blocker until the relevant fix and regression evidence are merged.

## Pilot evidence

Link the tested revision/artifact and the collected validation evidence. Do not infer success from implementation status alone.

**Decision ownership convention:** every pilot go/no-go validation artifact must name one responsible GitHub user and record the decision, date, tested revision, and unresolved blockers. Stable-release validation uses the same convention: the release artifact/PR names one responsible GitHub user and records the release decision against the exact candidate revision. This is a per-decision responsibility record, not a permanent staffing assignment.

# 1.0 Integration Gate

Run cross-track integration checks as contracts land. The formal stable-1.0 Integration Gate passes only when the required track outcomes and evidence are complete.

## Required integration checks

- OIDC-only, Discord-only, and explicitly linked accounts reach the same local User/Game Nick model.
- Public identity surfaces use Game Nick without provider-private data leakage.
- Game Requests are fully instance-wide and preserve concurrency/capacity behavior after prototype migration.
- Discovery, 6-hour Trending, and durable request activity behave as approved.
- Game Request writes produce durable notification intent atomically where required, without transport failure affecting the originating action.
- Web Push works through Track D's service worker and respects enablement/audience/privacy rules.
- Discord delivery remains separate from Discord authentication and keeps one synchronized message per Game Request.
- IGDB-disabled local-only operation works with administrator-managed local Games.
- IGDB-enabled operation handles complete baseline, daily/manual refresh, provider failure, disable/re-enable, type visibility, materialization races, and removal/archive policy.
- Minimum instance identity, manifest, service worker, CSP, and remote-cover exception coexist correctly.
- A populated prototype installation upgrades through 1.0 migrations without data loss.
- Production build, Compose deployment, backup/restore, and documented operator flows are exercised against the integrated application.

## Blocker policy

Treat data loss, authentication/authorization bypass, provider-private-data exposure, capacity violations, broken required migration/restore paths, and reproducible persistent hangs in supported core workflows as release blockers. Record unresolved failures explicitly; a feature checklist or documentation PR cannot waive them.

Concrete final performance/load targets should be evidence-based. The pilot rehearsal provides measurements; stable-release thresholds may be refined from that evidence rather than guessed in advance.

# Stable 1.0 Release Candidate and Release Gate

## Depends on

The stable 1.0 Integration Gate being green.

## Expected outcomes

- Publish a 1.0 release candidate with complete migration, operations, security, privacy, authentication-provider, catalog-provider, and notification-provider documentation.
- Reconcile accepted/deferred scope decisions before the candidate. Unapproved proposals remain unscheduled.
- After the candidate, accept only release blockers, security fixes, migration fixes, and documentation corrections.
- Re-run integrated OIDC/Discord onboarding, Game Nick identity, instance-wide Game Request, discovery/Trending/activity, provider-failure, notification-retry, backup/restore, and supported upgrade paths.
- Verify no 1.0 workflow requires Event selection/membership, Open Play, Clans, rich profiles, localization, curated tags, or a full branding editor.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of release preparation.
- Publish `v1.0.0` only when release-candidate blockers are resolved.

## Acceptance

All track and Integration Gate acceptance criteria remain green in a release build, documentation matches observable behavior, supported release artifacts are available, and no release blockers remain.

# Review decisions and backlog alignment

Review snapshot: **2026-09-15**. Issue existence is not evidence of implementation. The following dispositions are approved for the reviewed roadmap.

| Issue / topic | Stable 1.0 disposition |
| --- | --- |
| [#15 — interaction hangs](https://github.com/convention-x-treme/lansurfer/issues/15) | Open until its accepted hardening scope is complete. The confirmed original hang was caused by long-lived SSE `EventSource` connections exhausting the browser's HTTP/1.1 per-origin connection pool; HTTP/2 multiplexing prevents that production-path pool exhaustion but is not the root cause. PR #17 addresses the stream-lifecycle problem and PR #18 adds rapid-interaction hardening; closure still depends on the issue's remaining acceptance evidence. |
| [#12 — request search and filters](https://github.com/convention-x-treme/lansurfer/issues/12) | Include Game name/alias search, followed/My Games, and open capacity/free places. Other proposed filters are not automatically approved by this review. |
| [#11 — English/German i18n](https://github.com/convention-x-treme/lansurfer/issues/11) | Deferred beyond stable 1.0. Stable 1.0 is English-only. |
| [#10 — curated request tags](https://github.com/convention-x-treme/lansurfer/issues/10) | Deferred beyond stable 1.0. Free-text request descriptions remain sufficient. |
| [#13 — trending/current interest](https://github.com/convention-x-treme/lansurfer/issues/13) | Include request-based Trending with a visible rolling 6-hour window. Do not persist raw search queries or introduce search-interest tracking. |
| [#14 — request activity history](https://github.com/convention-x-treme/lansurfer/issues/14) | Include durable request-local create/join/leave/close/cancel activity, separate from membership and notification delivery. |
| Rich player profiles | Deferred beyond stable 1.0. Game Nick is the required public identity. |
| Full instance branding | Removed from stable 1.0. Keep fixed/default LANsurfer branding plus only minimum instance identity; no branding editor/white-label/theme surface. |
| Generic OIDC | Remains stable-1.0 scope even though Discord is sufficient/required for the selected pilot. |
| [#7 — optional dotenv](https://github.com/convention-x-treme/lansurfer/issues/7), [#8 — development database modes](https://github.com/convention-x-treme/lansurfer/issues/8) | Contributor-workflow items; track separately from production release acceptance. |

## Decision ownership convention

- Each pilot validation artifact names one responsible GitHub user and records go/no-go, decision date, exact tested revision, and unresolved blockers.
- Each stable release artifact/PR names one responsible GitHub user and records the release decision against the exact candidate revision.
- Responsibility is assigned per decision; this roadmap does not infer permanent staffing or ownership.

If later decisions change product behavior or architecture, update ROADMAP, PRODUCT, ARCHITECTURE, and repository guidance together.

# Post-1.0 candidates — not scheduled

Possible future work includes:

- multiple scheduled Events in one LANsurfer installation
- persistent Open Play
- Event membership/managers/invitations/rosters/seats/lifecycle/history
- Event-scoped Game Requests
- per-context Discord destinations or notification preferences
- Clans and persistent social discovery
- player biographies, per-Game handles, richer public profiles
- localization/internationalization
- curated request tags/tag filters
- richer administrator-managed instance branding if a concrete need is approved
- additional authentication/catalog providers

If scheduled later, design the concrete model for the actual need. Do not prebuild generic abstractions in 1.0.

# Explicitly not planned

The following have no planned release and should not influence current implementation unless the roadmap is explicitly changed:

- a generic polymorphic `context`/parent abstraction for Game Requests
- a persistent in-app notification inbox
- generic reports, suspensions, or a complex moderation system
- Clan posts, chat, private Clan LFG, or custom roles
- Game Request waitlists, ready checks, or comments
- guest accounts, passkeys, or account self-deletion
- full theme customization/white-label system
- catalog providers other than IGDB
- local caching/proxying of remote covers
- multi-instance/high-availability application architecture
- WebSockets, native applications, tournaments, ratings, teams, or federation