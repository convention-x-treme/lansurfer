# Superpowers project artifacts

Durable Superpowers design documents and implementation plans live under this directory and are grouped by feature:

```text
docs/superpowers/<feature>/
├─ YYYY-MM-DD-<topic>-design.md
└─ YYYY-MM-DD-<topic>-plan.md
```

Keep a feature's design/specification and its implementation plan together in the same feature directory. Do not split durable artifacts into global `specs/` and `plans/` directories.

The repository-root `.superpowers/` directory has a different purpose: it is local, temporary execution state for Superpowers/SDD workflows. It is ignored by Git and is not a source of durable project documentation.

## IGDB

The IGDB feature family currently contains these documents, in chronological order:

1. `igdb/2026-09-17-discovery-design.md` — original IGDB discovery architecture.
2. `igdb/2026-09-17-discovery-plan.md` — implementation plan for the original discovery architecture.
3. `igdb/2026-09-18-discovery-ux-refinement-design.md` — approved player-facing discovery and Game Request UX refinement.
4. `igdb/2026-09-18-discovery-ux-refinement-plan.md` — implementation plan for that refinement.

When adding another IGDB design or plan, place it in `docs/superpowers/igdb/` and link related documents using their repository-relative paths.
