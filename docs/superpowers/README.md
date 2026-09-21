# Feature planning artifacts

`docs/superpowers/` contains bounded feature-planning artifacts. These are supporting working documents, not repository-wide sources of current truth.

Canonical ownership is:

- `docs/DOMAIN_LANGUAGE.md` — canonical domain terms and distinctions.
- `docs/PRODUCT.md` — durable product behavior and release scope.
- `docs/ARCHITECTURE.md` — durable architecture and cross-module contracts.
- `AGENTS.md` — repository rules and approval boundaries.
- `docs/ROADMAP.md` — tracks, dependencies, and release gates.
- GitHub issues/PRs — current progress and execution history.
- `docs/DEVELOPMENT_WORKFLOW.md` — planning-to-implementation process and artifact lifecycle.

A feature design or plan never overrides those sources.

## Working layout

For non-trivial work that benefits from an explicit handoff:

```text
docs/superpowers/<feature>/
├─ YYYY-MM-DD-<topic>-design.md
└─ YYYY-MM-DD-<topic>-plan.md
```

Optional validation/review evidence may use a descriptive `*-review.md`, `*-report.md`, or a dedicated `docs/validation/` artifact when it is genuinely useful after implementation.

Do not add global `specs/`, `plans/`, tracked `.superpowers/sdd/`, or tool-specific prompt archives.

## Lifecycle

### During planning

- Use a design/spec only when a bounded change needs detail beyond PRODUCT/ARCHITECTURE.
- Use an implementation plan only when the coding handoff benefits from explicit executable tasks.
- Keep implementation plans concrete: affected files, exact changes, tests, generated output, and verification.
- Planning artifacts may live on a planning branch/draft PR before implementation.

### During implementation

- The coding agent executes the agreed plan rather than treating it as a new source of product scope.
- If repository evidence contradicts the plan, follow the authority order in `DEVELOPMENT_WORKFLOW.md`.

### Before merge

- Promote durable product decisions into PRODUCT.
- Promote durable architecture/contracts into ARCHITECTURE.
- Keep current progress in the GitHub tracker/PR.
- Delete implementation plans that have served their purpose unless merged PRs/issues intentionally reference them as part of the historical record.
- Delete tool-specific implementation prompts.
- Retain a design only when its rationale remains useful beyond the canonical docs.
- Retain review/validation evidence only when it remains meaningful evidence for release, quality, or a future decision.

Git history and the merged PR are the normal record of completed execution. A plan should not be retained merely because it once contained checked boxes.

The repository-root `.superpowers/` directory is local temporary execution state. It is ignored by Git and is not durable documentation.

## Retained historical rationale

Some older designs predate this stricter lifecycle and remain because they contain useful rationale. Treat them as historical supporting material, not current implementation status.

### Authentication

- `auth/2026-09-19-game-nick-auth-design.md` — design rationale for Game Nick and provider-neutral OIDC/Discord authentication.
- `auth/2026-09-19-game-nick-auth-plan.md` — retained historical exception referenced by the completed Track B tracker. New completed plans should normally be deleted.

### IGDB

- `igdb/2026-09-17-discovery-design.md` — original IGDB discovery rationale.
- `igdb/2026-09-18-discovery-ux-refinement-design.md` — player-facing discovery/Game Request UX rationale.
- `igdb/2026-09-19-ux-final-review-fix-report.md` — retained review evidence.

### PWA

- `pwa/2026-09-20-track-d-instance-pwa-design.md` — retained rationale for minimum instance identity and the PWA/service-worker boundary.
- `pwa/2026-09-20-track-d-instance-pwa-plan.md` — retained historical exception because merged PR #42 explicitly references this plan.

When a retained design no longer adds information beyond PRODUCT/ARCHITECTURE, prefer deleting it rather than maintaining a parallel source of truth.
