# LANsurfer Roadmap

This document is a compact delivery map for stable 1.0. It defines the active tracks, their integration dependencies, and the release gates.

It is **not** the live project-status database. Current checklists, implementation progress, assignees, and PR evidence belong in the linked GitHub tracking issues and pull requests.

Product behavior and release scope are governed by [PRODUCT.md](PRODUCT.md). Durable technical decisions and cross-module contracts are governed by [ARCHITECTURE.md](ARCHITECTURE.md) and [AGENTS.md](../AGENTS.md). The planning/implementation process is defined in [DEVELOPMENT_WORKFLOW.md](DEVELOPMENT_WORKFLOW.md).

Do not infer scope from an old issue, design, or implementation plan. Do not implement post-1.0 candidates unless PRODUCT and this roadmap are explicitly changed.

## Development model

Tracks may advance in parallel when their stated dependencies are satisfied. A dependency blocks only the integration work that consumes it.

For current status, use the linked tracker. Closed trackers are completed delivery records; open trackers carry the remaining checklist.

| Track | Scope | Tracker | Integration dependencies |
| --- | --- | --- | --- |
| **A** | Single-Event Core & Request Discovery | [#24](https://github.com/convention-x-treme/lansurfer/issues/24) | Track E consumes the durable Game Request activity handoff |
| **B** | Game Nick & Authentication | [#25](https://github.com/convention-x-treme/lansurfer/issues/25) | None |
| **C** | Game Catalog & IGDB Discovery | [#26](https://github.com/convention-x-treme/lansurfer/issues/26) | None for provider/index work |
| **D** | Minimum Instance Identity & PWA | [#27](https://github.com/convention-x-treme/lansurfer/issues/27) | Track E consumes the service-worker contract; Track G owns final visual presentation |
| **E** | Durable Notifications | [#28](https://github.com/convention-x-treme/lansurfer/issues/28) | A for notification intent, B for public Game Nick, D for Web Push/service-worker integration, G for presentation |
| **F** | Distribution & Operations | [#29](https://github.com/convention-x-treme/lansurfer/issues/29) | Final upgrade/restore validation consumes persistent changes from A-E and production assets from G |
| **G** | Default Brand & UI System Adoption | [#30](https://github.com/convention-x-treme/lansurfer/issues/30) | Feature-surface integration follows the affected A-F contracts |

## Track A — Single-Event Core & Request Discovery

**Tracker:** [#24](https://github.com/convention-x-treme/lansurfer/issues/24)

Owns instance-wide Game Request behavior, discovery scopes, Trending, request-local activity, and the transactional handoff consumed by Track E.

Durable Game Request behavior belongs in PRODUCT; the `game_request_activity` contract and ordering/replay invariants belong in ARCHITECTURE/AGENTS.

## Track B — Game Nick & Authentication

**Tracker:** [#25](https://github.com/convention-x-treme/lansurfer/issues/25)

Owns public Game Nick identity, generic OIDC, Discord authentication, explicit identity linking/unlinking, sessions, and provider-private/public identity boundaries.

Additional authentication providers are not implied by this track.

## Track C — Game Catalog & IGDB Discovery

**Tracker:** [#26](https://github.com/convention-x-treme/lansurfer/issues/26)

Owns the authoritative local Game catalog, optional IGDB discovery/indexing, materialization, provider lifecycle behavior, and enrichment.

Provider UX follow-ups are tracked separately and do not redefine the catalog authority model.

## Track D — Minimum Instance Identity & PWA

**Tracker:** [#27](https://github.com/convention-x-treme/lansurfer/issues/27)

Owns the minimum instance identity boundary, manifest, service-worker lifecycle/installability contract, secure-context diagnostics, and required PWA validation evidence.

Track D does not create administrator-managed branding/customization. Track E extends the same service-worker path for Web Push; Track G owns final visual treatment.

## Track E — Durable Notifications

**Tracker:** [#28](https://github.com/convention-x-treme/lansurfer/issues/28)

Owns durable notification work/delivery state, retries, personal Web Push, and one instance-level Discord destination.

External transport failure must not roll back Game Request writes. Public notification identity uses Game Nick and must not expose provider-private identity.

## Track F — Distribution & Operations

**Tracker:** [#29](https://github.com/convention-x-treme/lansurfer/issues/29)

Owns reproducible release artifacts, supported production deployment, operator documentation, backup/restore, upgrades, and release verification.

Final upgrade/restore evidence must exercise the persistent schema and keys used by the integrated A-E application.

## Track G — Default Brand & UI System Adoption

**Tracker:** [#30](https://github.com/convention-x-treme/lansurfer/issues/30)

Owns the fixed/default LANsurfer presentation system defined by [BRANDING.md](BRANDING.md) and [ui-kit](ui-kit/README.md), and its adoption across supported product surfaces.

Track G changes presentation, not product/domain ownership. It does not create an administrator branding editor, white-label system, or custom-theme product.

# Pilot Gate

The Pilot Gate is a deliberately bounded real-LAN validation milestone, not the stable 1.0 release gate.

The gate requires evidence for the pilot subset described in PRODUCT and the active trackers, including:

- Discord authentication plus Game Nick onboarding for pilot participants;
- a complete successful IGDB baseline before provider discovery is relied on;
- core Game Request discovery/create/join/leave/close/cancel flows;
- durable notification behavior required by the pilot;
- real PWA/Web Push validation on the required platform categories;
- production deployment, clean-host installation, actual backup/restore, and a populated upgrade rehearsal;
- a representative load/recovery rehearsal with recorded environment and observed behavior.

Validation artifacts must name the tested revision, concrete environment/browser versions where relevant, decision date, responsible GitHub user, unresolved blockers, and go/no-go decision.

Implementation status alone does not make the Pilot Gate green.

# 1.0 Integration Gate

The formal Integration Gate passes only when the required stable-1.0 track outcomes are integrated and verified together.

Required integration checks include:

- OIDC-only, Discord-only, and linked accounts converge on the same local User/Game Nick model;
- public surfaces use Game Nick without provider-private identity leakage;
- Game Requests remain instance-wide and preserve capacity/concurrency invariants;
- discovery, Trending, request activity, and notification intent behave as approved;
- notification transport failure remains isolated from Game Request writes;
- Web Push extends Track D's service worker and respects consent/eligibility/privacy;
- Discord delivery remains separate from Discord authentication;
- local-only and IGDB-enabled catalog modes both work as approved;
- minimum instance identity, manifest/service worker, CSP, and the fixed/default UI system coexist correctly;
- production-generated assets/fonts/licenses are embedded correctly;
- a populated pre-1.0 installation upgrades without data loss;
- production deployment plus backup/restore and operator flows are exercised.

Treat data loss, authentication/authorization bypass, provider-private-data exposure, capacity violations, broken required migration/restore paths, and reproducible persistent hangs in supported core workflows as release blockers.

# Stable 1.0 Release Gate

Depends on the 1.0 Integration Gate being green.

Before `v1.0.0`:

- publish and verify the release candidate and supported artifacts;
- reconcile accepted/deferred scope in PRODUCT and the trackers;
- accept only release blockers, security/migration fixes, and documentation corrections after the candidate;
- re-run the integrated authentication, Game Request, discovery, provider-failure, notification, backup/restore, and supported-upgrade paths;
- verify no stable-1.0 workflow accidentally depends on unscheduled Event/Clan/context, localization, rich-profile, curated-tag, or administrator-branding features;
- verify the canonical BRANDING/UI-kit presentation and production asset embedding;
- update `SECURITY.md` to the supported stable policy;
- record the release decision against the exact candidate revision.

Release when the required track outcomes and gate evidence remain green and no release blockers remain.

# Post-1.0 work

Post-1.0 proposals are deliberately unscheduled until PRODUCT and this roadmap explicitly accept them. Their existence as issues does not authorize implementation.

Examples include multi-Event/shared-service behavior, Clans, richer profiles, localization, curated Game Request tags, additional authentication/catalog providers, richer administrator-managed branding, and other future product expansions.

Do not prebuild generic abstractions for unscheduled work.
