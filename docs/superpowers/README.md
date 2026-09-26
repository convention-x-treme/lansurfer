# Superpowers project artifacts

Superpowers artifacts are **supporting records**, not repository-wide sources of current truth. Current product behavior lives in `docs/PRODUCT.md`, architecture in `docs/ARCHITECTURE.md`, delivery status in `docs/ROADMAP.md`, and repository working rules in `AGENTS.md`.

Durable feature artifacts live together under:

```text
docs/superpowers/<feature>/
├─ YYYY-MM-DD-<topic>-design.md
├─ YYYY-MM-DD-<topic>-plan.md
└─ YYYY-MM-DD-<topic>-review-or-report.md
```

Do not add new durable artifacts to global `specs/`, `plans/`, or tracked `.superpowers/sdd/` directories.

## Lifecycle

- Use a design/spec only when a bounded feature needs detail that PRODUCT/ARCHITECTURE do not.
- Use an implementation plan only while execution benefits from one; small fixes and normal features do not require a plan by default.
- After implementation, move durable behavior/architecture decisions into PRODUCT/ARCHITECTURE and progress into ROADMAP/issues.
- Completed designs/plans may be retained only when their rationale or execution record remains useful. Mark them **historical** so unchecked boxes or old branch instructions cannot be mistaken for current status.
- Prefer deleting completed execution plans when they add no durable value. Git history and merged PRs remain the implementation record.

The repository-root `.superpowers/` directory is local temporary execution state. It is ignored by Git and is not durable documentation.

## Authentication

Track B is implemented. Its retained historical records are:

1. `auth/2026-09-19-game-nick-auth-design.md` — design rationale for Game Nick and provider-neutral OIDC/Discord authentication.
2. `auth/2026-09-19-game-nick-auth-plan.md` — historical execution checklist for PR #41.

## IGDB

Track C is implemented. These retained files are historical supporting records:

1. `igdb/2026-09-17-discovery-design.md` — original IGDB discovery architecture.
2. `igdb/2026-09-17-discovery-plan.md` — historical implementation plan for the original discovery architecture.
3. `igdb/2026-09-18-discovery-ux-refinement-design.md` — player-facing discovery and Game Request UX refinement.
4. `igdb/2026-09-18-discovery-ux-refinement-plan.md` — historical implementation plan for that refinement.
5. `igdb/2026-09-19-ux-final-review-fix-report.md` — final UX review/fix report and verification evidence.

## Notifications

Track E1 (the durable notification foundation) is implemented, including the `suppressed` terminal delivery state added after the initial implementation to catch up with the settled design. Its retained historical record is:

1. `notifications/2026-09-20-track-e-notification-foundation-design.md` — design rationale for the two-level work/delivery persistence model, baseline initialization, claiming/lease recovery, retry/backoff policy, and the `suppressed` state.
2. `notifications/2026-09-20-track-e1-notification-foundation-plan.md` — historical execution plan for E1.

Track E2 (Web Push) server delivery, encrypted subscriptions/preferences, account settings, browser enable/revocation flows, and push/click handling in Track D's existing service worker are implemented. Manual observations confirm real-browser permission/subscription registration, real Push receipt, and visible-window suppression; click/deep-link, logout/device removal, session expiry, and exact browser/device versions remain open. See [`docs/validation/track-e-web-push.md`](../validation/track-e-web-push.md). No offline/fetch caching or second service worker was added. E3 Discord delivery is implemented on the current branch; E3-24 final verification is pending, and its retained plan is historical.

The approved E3 design and historical implementation plan remain supporting history:

1. [`notifications/2026-09-20-track-e-discord-delivery-design.md`](notifications/2026-09-20-track-e-discord-delivery-design.md) — approved design rationale.
2. [`notifications/2026-09-24-track-e3-discord-delivery-plan.md`](notifications/2026-09-24-track-e3-discord-delivery-plan.md) — historical implementation record; E3-24 verification is pending.

## Legal pages

1. `legal/2026-09-22-legal-pages-design.md` — public Terms, Privacy, and contact/reporting surface for issue #39 section 1.
2. `legal/2026-09-22-legal-pages-plan.md` — execution checklist for that design.

When adding another durable artifact, place it in the relevant feature directory and link related documents with repository-relative paths.
