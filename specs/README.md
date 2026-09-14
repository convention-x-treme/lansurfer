# Specification-Driven Development pilot

This directory is an experiment for evaluating a lightweight Specification-Driven Development (SDD) workflow in LANsurfer.

It does **not** change the repository-wide development process, `AGENTS.md`, roadmap sequencing, or the approved product scope. Existing repository guidance remains authoritative. In particular, a specification in this pilot does not by itself authorize implementation work that the current roadmap does not yet permit.

## Pilot workflow

Each feature is described by three documents:

1. `spec.md` — product behavior, scenarios, acceptance criteria, constraints, and explicit non-goals. It answers **what and why** without prescribing implementation details unnecessarily.
2. `plan.md` — the proposed technical implementation derived from the specification and existing architecture. It records affected modules, persistence, interfaces, integration points, risks, and verification strategy.
3. `tasks.md` — small, ordered, verifiable implementation tasks derived from the approved spec and plan.

The intended flow is:

```text
existing PRODUCT / ROADMAP
          |
          v
        spec.md
          |
          v
        plan.md
          |
          v
        tasks.md
          |
          v
      code + tests + PR
```

`ARCHITECTURE.md` and `AGENTS.md` remain constraints on every phase.

## Rules for the pilot

- Do not silently change product scope while writing a plan or implementing tasks. Change the spec first.
- Keep requirements observable and testable where practical.
- Record unknowns as open questions instead of inventing behavior.
- Keep implementation details out of `spec.md` unless they are product/security constraints.
- Keep `plan.md` concrete enough that another developer or coding agent can implement it without redesigning the feature.
- Keep tasks small enough to review independently and give each task an explicit verification result.
- If implementation reveals that the spec or plan is wrong, update the relevant document before continuing.

## Pilot feature

`001-igdb-catalog/` applies the workflow to the already planned IGDB Catalog Import and Rich Game Metadata feature. The specification is derived from the existing `main` branch roadmap/product/architecture rather than introducing new scope.

## Templates

`_templates/` contains intentionally small starting points. They are part of the experiment and may be revised before SDD is adopted repository-wide.
