# Implementation plan: <name>

## Status

Draft | Approved | Implemented

## Specification

Link to the corresponding `spec.md`.

## Existing system

Summarize the relevant current modules, persistence, routes/UI, and constraints that the implementation must preserve.

## Proposed design

Describe the smallest design that satisfies the specification and existing architecture.

### Module ownership

- Module: responsibilities and writes.

### Interfaces and boundaries

- Consumer-owned interfaces or external-provider adapters.

### Persistence and migrations

- Schema changes, constraints, migration behavior, and data migration if any.

### Web/UI changes

- Handlers, read models, templ components, progressive enhancement.

### Configuration and external services

- Environment/configuration changes and failure behavior.

## Integration points

- Other modules/tracks and the exact contract between them.

## Risks and edge cases

- Risk and mitigation/test strategy.

## Verification strategy

- Unit/domain tests.
- PostgreSQL integration tests.
- HTTP/UI tests.
- Manual/provider verification where automation cannot cover the external system.

## Deliberate omissions

- Behavior intentionally not implemented by this plan.

## Open technical questions

- Questions that block finalizing the plan.
