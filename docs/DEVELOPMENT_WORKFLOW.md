# Development workflow

LANsurfer uses a planning-to-implementation workflow inspired by BMad: non-trivial work is specified to an implementation-ready state before a coding agent starts changing application code.

The workflow is tool-neutral. A planning agent may be ChatGPT in a web chat, another assistant, or a human. A coding agent may be any implementation environment. The repository artifacts, not the tool that created them, define the handoff.

## Sources of truth

Use the smallest authoritative source that answers the question:

1. `docs/DOMAIN_LANGUAGE.md` — canonical domain terms and distinctions.
2. `docs/PRODUCT.md` — durable product behavior and release scope.
3. `docs/ARCHITECTURE.md` — durable technical decisions, module boundaries, and cross-module contracts.
4. `AGENTS.md` — repository rules, coding guardrails, and approval boundaries.
5. `docs/ROADMAP.md` — delivery tracks, dependencies, and release gates. It is not a live progress log.
6. GitHub tracking issues — current status, remaining checklists, and delivery progress.
7. Feature design/spec — bounded decisions for one change.
8. Feature implementation plan — temporary execution instructions.

An issue, old plan, or historical design never overrides PRODUCT, ARCHITECTURE, or AGENTS.

## Choose the workflow depth

Small, local, low-risk fixes may go directly to implementation when the expected behavior is already unambiguous and no product/architecture decision is required.

Use the full planning handoff for work that changes or coordinates behavior across meaningful boundaries, including new features, non-trivial UX, persistence, authentication/security, cross-module contracts, release behavior, or changes whose implementation would otherwise require the coding agent to make product decisions.

## Planning phase

The planning phase produces an implementation-ready handoff and does not implement application code.

### 1. Establish repository evidence

Before designing the change:

- read the relevant DOMAIN_LANGUAGE, PRODUCT, ARCHITECTURE, AGENTS, ROADMAP, and tracking-issue sections;
- inspect the existing implementation and tests;
- confirm that the requested work belongs in current scope;
- identify generated files, migrations, shared contracts, and verification requirements that the implementation may affect.

Do not design from an issue body alone.

### 2. Resolve the change

Write down:

- problem/context;
- goal;
- non-goals;
- relevant current behavior;
- design decisions and rejected alternatives when they matter;
- compatibility, security, privacy, persistence, and UX constraints as applicable;
- acceptance criteria.

The planning agent should resolve product and architecture questions before handoff. If an approval boundary in AGENTS is crossed, obtain that approval rather than leaving the choice to the coding agent.

### 3. Produce the implementation plan

For changes that benefit from a formal handoff, place the planning artifacts under:

```text
docs/superpowers/<feature>/
├─ YYYY-MM-DD-<topic>-design.md
└─ YYYY-MM-DD-<topic>-plan.md
```

The plan should contain executable tasks with:

- affected files/components;
- precise intended changes;
- tests for each meaningful behavior;
- generated-output requirements;
- verification commands;
- explicit scope boundaries.

Prefer tasks that can be completed and verified independently. Avoid vague tasks such as “finish backend” or “improve UI”.

### 4. Create the handoff branch / draft PR

A useful default branch name is:

```text
bmad/<issue>-<topic>-spec
```

The planning draft PR should state that it is planning-only and identify the design and implementation-plan files.

The same branch and draft PR should normally continue into implementation. This preserves one coherent feature history and gives the coding agent a single place to work.

### Implementation-ready definition

A handoff is implementation ready when all of the following are true:

- the work is confirmed to be in scope;
- goals and non-goals are explicit;
- relevant product/architecture decisions are resolved;
- acceptance criteria are testable;
- affected areas are identified closely enough to avoid rediscovery of the feature design;
- required tests/generation/verification are specified;
- there are no unresolved product or architecture questions delegated to the coding agent.

If these conditions are not met, continue planning rather than labeling the handoff ready.

## Implementation phase

The coding agent starts from the prepared branch and draft PR.

Its instruction should be intentionally small, for example:

> Read AGENTS.md, the feature design, and the implementation plan on this branch. Implement the plan end-to-end, run the specified verification, and update the draft PR with the results. Do not broaden scope.

The coding agent must:

1. re-read AGENTS and the referenced design/plan;
2. inspect the exact code before editing;
3. implement the agreed tasks without silently redesigning the feature;
4. add/update focused tests;
5. regenerate committed generated output when required;
6. run the planned verification;
7. update the draft PR with implementation decisions, verification performed/omitted, and any remaining blocker.

### When repository evidence contradicts the plan

The plan is an execution aid, not higher authority than the repository.

If implementation discovers that the plan conflicts with current code, PRODUCT, ARCHITECTURE, or a safety/security invariant:

- do not force the planned change;
- record the conflicting evidence;
- make the smallest correction that preserves the already-approved intent when it is unambiguous;
- return to planning/approval when the conflict requires a new product or architecture decision.

Do not use minor implementation detail differences as an excuse to reopen settled scope.

## PR and verification

One coherent feature/fix should normally use one branch and one PR.

Before declaring implementation complete:

- check every acceptance criterion;
- run the relevant Taskfile workflows;
- review generated diffs;
- record what was and was not verified;
- identify any affected domain/architecture contract;
- keep the PR description aligned with the implementation that actually exists.

A draft PR becomes ready for review only after the implementation and required verification are complete, unless the PR explicitly represents a planning/design review only.

## Artifact lifecycle after implementation

The repository should document how the system is intended to work, not preserve every execution checklist forever.

Before merge:

1. Promote durable product behavior into `PRODUCT.md` when needed.
2. Promote durable architecture decisions/contracts into `ARCHITECTURE.md` when needed.
3. Update the relevant GitHub tracker for current delivery status.
4. Delete temporary implementation plans unless they are intentionally retained as historical references from merged PRs/issues or still provide concrete long-term value.
5. Retain a feature design only when its rationale remains useful beyond PRODUCT/ARCHITECTURE; otherwise delete it as well.
6. Keep validation/review artifacts only when they are real release or quality evidence.

Git history and the merged PR are the normal execution record. Old plans should not be retained merely as proof that work happened; retain them only when existing merged PRs/issues intentionally reference them as part of the historical record. Tool-specific agent prompts should not be retained.

## Documentation ownership summary

| Information | Canonical home |
| --- | --- |
| Canonical domain terminology | `docs/DOMAIN_LANGUAGE.md` |
| What the product should do | `docs/PRODUCT.md` |
| How the system is structured / durable contracts | `docs/ARCHITECTURE.md` |
| Repository and agent rules | `AGENTS.md` |
| Planning/implementation process | `docs/DEVELOPMENT_WORKFLOW.md` |
| Tracks, dependencies, release gates | `docs/ROADMAP.md` |
| Current progress/checklists | GitHub tracking issues / PRs |
| Bounded feature rationale | temporary/retained feature design |
| Step-by-step execution | temporary implementation plan |
| Completed execution history | Git history / merged PR |
