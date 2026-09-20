# LANsurfer Roadmap

This roadmap carries LANsurfer from its completed web prototype to a focused stable 1.0 self-hosting release. The 1.0 work is organized as **parallel development tracks**, not a linear sequence of feature milestones. Track dependencies describe only the points where work must converge; otherwise tracks may advance independently. Track G carries the fixed LANsurfer brand and UI system across those product capabilities; it changes presentation, not approved product scope.

Product behavior and scope are governed by [PRODUCT.md](PRODUCT.md). Technical decisions and module boundaries are governed by [ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](../AGENTS.md). The canonical default visual system is governed jointly by [BRANDING.md](BRANDING.md) and [`docs/ui-kit`](ui-kit/README.md); those design sources may not redefine product behavior, domain terminology, approved scope, technology, or module ownership.

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
- LANsurfer's fixed/default branding is the complete default brand and UI system defined by [BRANDING.md](BRANDING.md) and [`docs/ui-kit`](ui-kit/README.md), including its palette, typography, dark/light presentation, accessibility rules, component semantics, and bundled brand assets. A full administrator-managed branding editor, uploaded instance logos/icons, white-labeling, custom colors/fonts/CSS, and custom theming are outside 1.0. Only minimum instance identity needed for a recognizable deployment may remain configurable.
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

The prototype schema and behavior are an input to the 1.0 migration path, not a promise that every prototype interface, relationship, or visual treatment remains part of the 1.0 product model.

## Development model

Tracks A–G may run concurrently. A dependency blocks only the dependent integration work, not unrelated work inside the track.

Each track has a GitHub tracking issue carrying its current checklist. The issue records progress; this roadmap remains the authority for scope.

| Track | Scope | Tracking issue | Planning status | Integration dependencies |
| --- | --- | --- | --- | --- |
| **A** | Single-Event Core & Request Discovery | [#24](https://github.com/convention-x-treme/lansurfer/issues/24) | **Integration ready** — PRs #23/#35 merged; browser regression work #31 complete | Track E consumes A's current `game_request_activity` handoff |
| **B** | Game Nick & Authentication | [#25](https://github.com/convention-x-treme/lansurfer/issues/25) | **Integration ready** — PR #41 merged; tracker closed | None |
| **C** | Game Catalog & IGDB Discovery | [#26](https://github.com/convention-x-treme/lansurfer/issues/26) | **Integration ready** — PR #36 merged | None for provider/index work; issue #37 is an operator-UX follow-up |
| **D** | Minimum Instance Identity & PWA | [#27](https://github.com/convention-x-treme/lansurfer/issues/27) | **In progress** — draft PR #42; not yet on `main` | None; service-worker contract is consumed by E; G owns visual presentation |
| **E** | Durable Notifications | [#28](https://github.com/convention-x-treme/lansurfer/issues/28) | **In progress** — E1 (durable foundation) implemented; E2 (Web Push) and E3 (Discord) not yet implemented | A for Game Request handoff; D for browser Web Push integration; B where Game Nick is rendered; G presents settings/permission states |
| **F** | Distribution & Operations | [#29](https://github.com/convention-x-treme/lansurfer/issues/29) | **Prioritized** — not implemented on `main` | Final upgrade/restore validation depends on persistent migrations from A–E; release validation consumes G's production assets |
| **G** | Default Brand & UI System Adoption | [#30](https://github.com/convention-x-treme/lansurfer/issues/30) | **In progress** — foundation PRs #17/#19/#21 merged; cross-track surfaces/audit remain | None for shared tokens/shell/components; final feature-surface integration waits only for affected A–F contracts |

### Planning status and delivery evidence

Status describes repository evidence, not a percentage estimate. **Integration ready** means the track-local implementation and verification have landed and its cross-track contract is available; it does not mean the Pilot, 1.0 Integration, or Release gate has passed.

Current delivery snapshot: **2026-09-20**, against `main` after Tracks B and C merged.

- **Track A — Integration ready.** PR #23 removed Event-scoped request behavior, added durable request activity and the A→E transactional handoff, discovery/filtering, and 6-hour Trending. PR #35 added the approved Joined and Created-by-me scopes. Browser interaction/SSE regression coverage from #31 is committed and that issue is closed. Track B has since supplied Game Nick public rendering. The current handoff is the single `game_request_activity` record described in ARCHITECTURE; Track E may introduce delivery-specific persistence if its implementation demonstrates that need.
- **Track B — Integration ready.** PR #41 merged Game Nick, provider-neutral OIDC/Discord authentication, persistent browser-bound flows, explicit link/unlink, onboarding, and public-identity rendering. Issue #25 is closed. The retained feature design is supporting history; PRODUCT/ARCHITECTURE own the durable behavior and ROADMAP owns current status.
- **Track C — Integration ready.** PR #36 merged optional asynchronous IGDB synchronization, local provider search, materialization, provider lifecycle handling, and enrichment. Issue #37 remains a real operator-UX follow-up for in-progress sync visibility; it does not redefine Track C's architecture.
- **Track D — In progress, not on `main`.** Draft PR #42 implements the minimum instance-name/manifest/service-worker shell and PWA diagnostics, but explicitly leaves real-device validation and final icon approval outstanding. Do not describe Track D as implemented on `main` until that PR (or its successor) lands.
- **Track E — In progress.** The `notification` module's durable foundation (E1) is implemented: asynchronous consumption of Track A's `game_request_activity` handoff, two-level `notification_work`/`notification_delivery` persistence, PostgreSQL-backed claiming with crash-safe leases, capped exponential backoff with jitter, and authorized diagnostics/manual retry, proven with a fake transport. Personal Web Push (E2) and Discord delivery (E3) remain outstanding and unimplemented; no transport-specific behavior or UI exists yet.
- **Track F — Planned.** Stable distribution artifacts, clean-host/backup/restore/upgrade evidence, and production operations work remain outstanding.
- **Track G — In progress.** PRs #17, #19, and #21 established and remediated the shared design system, and implemented A/B/C surfaces consume it. Remaining work follows new D/E/F surfaces, the full re-audit when a new audit result is claimed, and release-time asset verification.

Tracking issues are progress views, not scope authorities. If a tracker body disagrees with merged code and these canonical documents, reconcile the tracker rather than treating its stale checklist as current truth.

### Cross-track contracts

1. **Track A → Track E: Game Request notification contract**
   - Track A owns Game Request writes and the transactional outbox integration point. That integration point is the `game_request_activity` table: one row per committed change, carrying `sequence`, `game_request_id`, `actor_id`, `kind`, `origin`, and `occurred_at`.
   - `sequence` is a stable row identifier and orders one Game Request's history. It is **not** a global replay cursor: sequence values are assigned at insert time, not commit time, so a consumer advancing a single high-watermark would skip a row whose transaction committed late. Track E keeps durable per-row delivery state instead, and relies only on ordering within a Game Request, which the request row lock guarantees.
   - `origin` distinguishes `live` rows from the `backfill` rows the single-event migration reconstructed. Backfilled rows are history only and are never deliverable. A consumer's first start on an installation that already has `live` history establishes its own baseline so an upgrade does not deliver the archive.
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
   - Track D owns the manifest, service-worker registration/lifecycle/update behavior, and installability contract. Track G owns the visual application shell and theme presentation.
   - Track E owns push permission, subscriptions, push event handling, eligibility, and payloads.

5. **Tracks A–F ↔ Track G: presentation contract**
   - [BRANDING.md](BRANDING.md) and [`docs/ui-kit`](ui-kit/README.md) jointly govern LANsurfer's fixed/default visual system. BRANDING owns brand semantics, palette/tokens, typography, themes, accessibility, asset policy, and component semantics; the UI kit owns represented screen composition, dimensions, states, and interactions. PRODUCT, ROADMAP, ARCHITECTURE, and AGENTS remain authoritative if a design source implies different product behavior, terminology, technology, module ownership, or scope.
   - Track G owns the shared visual shell, design-token implementation, reusable presentation components, and adoption of the default LANsurfer brand across supported screens. Tracks A–F continue to own domain behavior, writes, queries/read models, handler contracts, security rules, and domain-error translation.
   - Track G may build against existing contracts and representative states immediately. Final integration of a changed feature surface waits only for the affected track's user-visible behavior and read/command contract to stabilize.
   - Tracks A–F consume Track G's established components and interaction vocabulary for new screens rather than creating parallel component systems or copying visual constants.
   - Theme choice is a user presentation preference within the fixed LANsurfer design system. It is not instance-brand customization and must not grow into custom colors, fonts, CSS, uploaded brand assets, white-labeling, or a branding editor.
   - Production remains templ/HTMX server rendering with Tailwind, DaisyUI, limited progressive TypeScript, embedded approved assets, and no internal JSON API created solely for the UI.

6. **Track F consumes all tracks at validation**
   - Release automation/tooling may proceed early.
   - Final migration, backup/restore, and upgrade rehearsals wait until the relevant persistent schema is stable enough to validate.
   - Final release builds also consume Track G's production styles, scripts, fonts, and other approved default-brand assets and verify they are generated, embedded, licensed, and served without unapproved runtime build tooling or remote dependencies.

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
- an exclusive audience scope: All, My Games, Joined, Created by me
- open-capacity/free-places filtering, which combines with any scope

The Joined and Created by me scopes were approved from issue #12 after the initial Track A delivery. Further filters proposed there are not automatically 1.0 requirements unless explicitly approved later.

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
- Keep the committed rapid HTMX/SSE interaction and supported-production-HTTP regression coverage healthy. Issue #31 is closed with browser coverage in `web/tests`; #32 and #33 are also resolved. Issue #15's root cause remains the browser HTTP/1.1 connection-pool exhaustion caused by long-lived SSE streams; HTTP/2 is a production-path mitigation, not the root cause.

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

Keep LANsurfer recognizable as an installation while providing the PWA/service-worker behavior required for installability and Web Push, without building a branding-management product. Track D owns instance identity and installability contracts; the fixed/default visual brand itself is defined by BRANDING/UI kit and implemented through Track G.

## Expected outcomes

- Consume the fixed/default LANsurfer colors, typography, accessibility behavior, component semantics, and bundled branding defined by [BRANDING.md](BRANDING.md) and [`docs/ui-kit`](ui-kit/README.md); do not redefine them as instance configuration.
- Permit only the minimum configurable instance identity needed to distinguish a deployment where necessary.
- Do not add a stable-1.0 administrator surface for configurable welcome text, contact link, uploaded logo, uploaded application icon, custom theme, custom colors/fonts/CSS, or white-labeling.
- Serve a manifest with stable application identity/start URL and appropriate bundled/default presentation.
- Add the minimal service-worker shell needed for installation and Web Push; do not promise offline application behavior.
- Define service-worker registration, lifecycle, update, and logout behavior so Track E can attach push handling safely.

## Acceptance

The application is recognizable/installable, the manifest/service worker are stable enough for Track E, the presentation uses Track G's canonical default design system, and no removed administrator-managed branding surface reappears indirectly.

### Verification evidence

- Record install/update behavior in the required pilot platform categories and later release validation matrix.
- Verify service-worker update/logout behavior does not expose private stale content.
- Verify unsupported installation/push capability leaves ordinary browser workflows usable.
- Verify minimum instance identity remains distinct from the fixed product branding and cannot mutate the palette, typography, theme tokens, or bundled LANsurfer brand assets.

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
- Verify final release builds contain Track G's generated production CSS/JS/fonts/default-brand assets and their required licenses without depending on runtime font/CDN delivery.

# Track G — Default Brand & UI System Adoption

## Goal

Ship the canonical LANsurfer brand and design system as the fixed/default presentation for stable 1.0 across current and future product surfaces, replacing the prototype's presentation without changing product behavior or creating a branding-management product.

## Expected outcomes

### Design authority and default branding

- Implement [BRANDING.md](BRANDING.md) as the canonical fixed/default LANsurfer brand foundation through 1.0: palette, semantic tokens, Schibsted Grotesk/JetBrains Mono typography roles, supported dark/light presentation, accessibility rules, action hierarchy, asset policy, wordmark/mascot guidance, voice, and operator attribution.
- Use [`docs/ui-kit`](ui-kit/README.md) for represented composition, dimensions, states, and interactions, with BRANDING owning semantic tokens and accessibility behavior.
- Treat the new design system as the default product branding. “Fixed/default branding” means this canonical system may replace the prototype's old colors, typography, layout, and component treatments; it does **not** mean preserving the prototype visually.
- Do not add administrator-managed colors, fonts, CSS, uploaded logos/application icons, white-labeling, custom themes, or a general branding editor. Minimum instance identity remains Track D scope and must stay distinct from the product brand.

### Shared UI platform

- Provide one shared application shell, centralized design tokens, reusable components, consistent action/state semantics, responsive behavior, keyboard focus, reduced-motion behavior, and supported contrast behavior.
- Keep dark mode and light mode as equally supported presentations of the same default design system; theme choice is a user presentation preference, not instance branding.
- Keep production rendering in templ/HTMX with Tailwind and DaisyUI plus limited progressive TypeScript. Bundle approved fonts and product assets with the application; runtime font CDNs are not allowed.
- Keep generated frontend assets reproducible and embedded for production. The existing approved IGDB remote-cover exception remains a catalog concern and does not generalize into remote brand assets.

### Feature-surface adoption

- Adopt the design system across the shared shell and current authentication, Game Request, Games/My Games, administration, empty/error, and related supported surfaces represented by the existing product.
- As Tracks A–F add or change user-visible states, render them through the established shell, tokens, components, and interaction vocabulary rather than creating parallel visual systems.
- Track G does not own domain rules, persistence, read models, authorization, or product-scope decisions. It consumes the affected track's server-side contracts and translates them into consistent presentation.
- Preserve SSR/HTMX behavior and progressive enhancement; do not create an internal JSON API solely to serve the UI.

### Presentation audit and remediation

- Keep [`UI-AUDIT.md`](UI-AUDIT.md) as Track G implementation evidence rather than design authority.
- Verify both themes, desktop/mobile behavior, status text independent of color, keyboard focus, touch targets, reduced motion, increased-contrast behavior, form/control boundaries, live notices, and generated theme cascade behavior.
- Re-run the documented full audit when a new score is claimed; per-finding remediation is not itself a replacement full-audit score.

## Acceptance

Stable-1.0 web surfaces use one coherent default LANsurfer brand/UI system derived from BRANDING and the UI kit; dark/light and responsive/accessibility behavior are supported; approved production assets are generated and embedded; new A–F surfaces can consume the same components; and no administrator branding editor, white-label system, custom theme surface, or parallel prototype visual system is introduced.

### Verification evidence

- Current delivery work is tracked in PR #17 (initial UI-kit adoption), PR #19 (complete design-system adoption), and PR #21 (presentation audit remediation), with `deto1986` as the responsible implementer while the track is In progress.
- Run the repository's applicable formatting/lint, unit/integration, generated-asset, and build checks for the integrated revision.
- Exercise representative supported screens in dark/light presentation at desktop/mobile sizes, including keyboard and coarse-pointer interaction where applicable.
- Verify the production build serves the bundled fonts/assets and generated CSS/JS without unapproved remote design dependencies.
- Verify representative A–F states render through the shared components without moving domain behavior into templates or client-side code.

# Pilot Gate

The next validation milestone is a deliberately bounded real LAN pilot. It is **not** the stable 1.0 release gate and does not require every stable-1.0 capability. Event-specific dates and attendance numbers are intentionally not recorded in the repository.

The Pilot Gate is green only when the following approved pilot subset is implemented and evidenced.

## Player access and identity

- Discord login is the required pilot authentication path. Requiring a Discord account is acceptable for this pilot.
- After first successful Discord login, every participant chooses/confirms a unique case-insensitive Game Nick before using core Game Request flows.
- OIDC is not a pilot prerequisite, but remains stable-1.0 scope.
- English UI is sufficient for the pilot.
- Rich profiles, localization, curated request tags, and administrator-managed branding/customization are not pilot prerequisites.

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
- Pilot-facing supported surfaces use the canonical default LANsurfer branding/design system; no administrator customization surface is required.
- Manifest/service-worker behavior required for installability and Web Push is working.
- No full administrator branding editor is required.

## Operations and reliability

- Production deployment path is exercised.
- Installation on a clean host is exercised.
- Backup is documented **and an actual restore is exercised**.
- At least one upgrade path using existing data is exercised.
- A representative documented load/recovery rehearsal covers realistic parallel browsing/search, request create/join/leave flows, SSE updates, and notification behavior.
- The validation artifact records tested hardware, network conditions, client/browser mix where relevant, tested load envelope, observed response/update behavior, failures, and recovery characteristics. Do not invent permanent concurrency/latency numbers in the roadmap.
- Supported production deployment preserves required HTTP behavior and core interaction reliability under normal regression testing. This is not waived by documentation: a reproducible persistent hang in a supported core workflow remains a pilot blocker. The #15 fix and #31 browser regression coverage are merged; the Pilot Gate still requires the production/load/recovery evidence described here.

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
- Track G's canonical default branding/UI system is applied across stable-1.0 surfaces in both supported themes without introducing administrator branding customization or a parallel visual system.
- Production-generated UI assets, fonts, and licenses are embedded and compatible with the release/operations path.
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
- Verify no 1.0 workflow requires Event selection/membership, Open Play, Clans, rich profiles, localization, curated tags, or an administrator-managed branding editor.
- Verify the canonical default LANsurfer branding/design system from BRANDING/UI kit is present across supported release surfaces and that minimum instance identity does not mutate that system.
- Verify accessibility, responsive behavior, Content Security Policy, secret handling, rate limits, and production asset embedding.
- Update [SECURITY.md](../SECURITY.md) from its pre-stable policy to the supported 1.0 policy as part of release preparation.
- Publish `v1.0.0` only when release-candidate blockers are resolved.

## Acceptance

All track and Integration Gate acceptance criteria remain green in a release build, documentation matches observable behavior, supported release artifacts are available, and no release blockers remain.

# Review decisions and backlog alignment

Review snapshot: **2026-09-20**. Issue existence is not evidence of implementation. The following dispositions are approved for the reviewed roadmap.

| Issue / topic | Stable 1.0 disposition |
| --- | --- |
| [#15 — interaction hangs](https://github.com/convention-x-treme/lansurfer/issues/15) | Closed. The confirmed original hang was caused by long-lived SSE `EventSource` connections exhausting the browser's HTTP/1.1 per-origin connection pool; HTTP/2 multiplexing prevents that production-path pool exhaustion but is not the root cause. PR #17 addressed the stream-lifecycle problem and PR #18 added rapid-interaction hardening. The remainder was split into [#31](https://github.com/convention-x-treme/lansurfer/issues/31) (committed browser regression tests, the only Track A dependency), [#32](https://github.com/convention-x-treme/lansurfer/issues/32) (diagnostics), and [#33](https://github.com/convention-x-treme/lansurfer/issues/33) (creation idempotency). |
| [#12 — request search and filters](https://github.com/convention-x-treme/lansurfer/issues/12) | Closed. Stable 1.0 includes Game name/alias search, the exclusive All/My Games/Joined/Created-by-me scope, and open capacity/free places. Further filters are not automatically approved. |
| [#11 — English/German i18n](https://github.com/convention-x-treme/lansurfer/issues/11) | Deferred beyond stable 1.0. Stable 1.0 is English-only. |
| [#10 — curated request tags](https://github.com/convention-x-treme/lansurfer/issues/10) | Deferred beyond stable 1.0. Free-text request descriptions remain sufficient. |
| [#13 — trending/current interest](https://github.com/convention-x-treme/lansurfer/issues/13) | Include request-based Trending with a visible rolling 6-hour window. Do not persist raw search queries or introduce search-interest tracking. |
| [#14 — request activity history](https://github.com/convention-x-treme/lansurfer/issues/14) | Include durable request-local create/join/leave/close/cancel activity, separate from membership and notification delivery. |
| Rich player profiles | Deferred beyond stable 1.0. Game Nick is the required public identity. |
| Administrator-managed instance branding/customization | Removed from stable 1.0. The canonical fixed/default LANsurfer branding and design system in BRANDING/UI kit remains stable-1.0 scope through Track G; only operator controls for uploaded logos/icons, welcome/contact branding, custom colors/fonts/CSS/themes, white-labeling, and a general branding editor are excluded. Minimum instance identity remains Track D scope. |
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
