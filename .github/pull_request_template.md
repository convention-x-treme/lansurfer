<!-- Title: type(scope): short imperative summary, or type(scope)!: summary for breaking changes. See CONTRIBUTING.md. -->

## Summary

Describe the change and why it is needed.

## Verification

- [ ] `task lint`
- [ ] `task test`
- [ ] `task test:integration` when database behavior changes
- [ ] Canonical docs are updated when their subject changes: behavior (`PRODUCT.md`), architecture (`docs/ARCHITECTURE.md`), vocabulary (`docs/TERMINOLOGY.md`), design system (`DESIGN.md`)
- [ ] Commits are signed off (`git commit -s`) under the [DCO](https://developercertificate.org/)

## Architecture check

- [ ] The change respects module ownership and dependency direction
- [ ] New dependencies or significant architectural decisions are explained
