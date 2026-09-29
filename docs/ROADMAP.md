# LANsurfer 1.0 planning

Current planning, priorities, status, validation checklists, and release readiness are maintained in GitHub Issues and the **1.0 milestone** rather than in this document.

This file remains as a stable landing page because historical issues, pull requests, and documentation link to `docs/ROADMAP.md`.

## Sources of truth

- Product behavior and approved release scope: [PRODUCT.md](PRODUCT.md)
- Technical architecture and durable invariants: [ARCHITECTURE.md](ARCHITECTURE.md)
- Project terminology: [TERMINOLOGY.md](TERMINOLOGY.md)
- Brand and UI rules: [BRANDING.md](BRANDING.md) and [ui-kit](ui-kit/README.md)
- Repository workflow and approval boundaries: [AGENTS.md](../AGENTS.md)
- Current work and status: GitHub Issues in the 1.0 milestone

## Current 1.0 capability work

The historical Track A–G planning vocabulary is deprecated for active work. Use capability names instead.

- Game Request coordination and playing flow — issue #82
- Distribution & operations — issue #29
- Brand & UI system adoption — issue #30
- Discord compliance & App Verification readiness — issue #39
- Self-hosted legal documents & operator compliance — issue #91
- Open-source publication & governance readiness — issue #92

Completed implementation remains discoverable through merged pull requests and closed issues; it is not duplicated here as a status history.

## Release gates

- **Pilot validation** — issue #93
- **1.0 integration validation** — issue #94
- **1.0 release readiness** — issue #95

Each gate records evidence against a concrete revision/artifact. A feature being implemented or merged is not, by itself, evidence that a validation gate has passed.

## Planning rules

Do not add mutable checklists or implementation status back to this file. Put that information in GitHub Issues.

Do not implement post-1.0 candidates merely because an old design, issue, or historical planning document exists. `PRODUCT.md` defines approved scope; GitHub Issues define current scheduled work.

Historical references to anchors from the former long-form roadmap may no longer resolve. The corresponding implementation and planning history remains available in Git history, pull requests, and issues.
