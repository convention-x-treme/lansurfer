# Feature-branch working artifacts

This directory may contain temporary specs and designs that help humans and coding agents collaborate on a feature branch. These files are **working material, not repository-wide sources of truth**.

Canonical sources are:

- product behavior and approved scope: `PRODUCT.md`
- technical architecture and durable invariants: `docs/ARCHITECTURE.md`
- terminology: `docs/TERMINOLOGY.md`
- design system: `DESIGN.md`
- current development work/status/validation: GitHub Issues and milestones
- repository workflow: `AGENTS.md`

## Recommended lifecycle

1. Start from a GitHub Issue with the problem, desired outcome, scope, and acceptance criteria.
2. Add a design/spec on the feature branch only when a bounded decision needs more detail than the canonical docs.
3. Add implementation plans/tasks/prompts on the feature branch when they materially help execution or handoff between chats/agents.
4. Keep those working files current while the branch is active.
5. Before merge, harvest durable information:
   - product behavior/scope → `PRODUCT.md`
   - technical decisions/invariants → `ARCHITECTURE.md`
   - vocabulary → `TERMINOLOGY.md`
   - design-system rules → `DESIGN.md`
   - remaining work → GitHub Issues
   - useful non-automated evidence → the relevant GitHub Issue or PR
6. Remove temporary plans/tasks/prompts from the final tree. Remove the design/spec too once its durable decisions are represented canonically, unless it contains genuinely useful rationale that cannot be expressed there.
7. Prefer squash merging branches that contained temporary planning artifacts.

A feature branch is therefore allowed to carry extensive specs/tasks for ChatGPT or coding-agent collaboration. Their presence on a branch does not imply they should become permanent documentation on `main`.

## What may remain

Retain a design record only when it contains durable rationale that is still useful and is not duplicated by the canonical docs. Mark retained records clearly as historical/supporting material and remove execution instructions, stale status, and unchecked task lists.

The repository-root `.superpowers/` directory is ignored by Git and can also be used for purely local execution state that never needs branch collaboration.
