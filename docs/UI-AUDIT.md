# Dark-mode presentation audit

Audit date: 2026-09-16. Branch: `feat/adopt-design-kit`, at commit `5f18dfe`.

This document records the open presentation defects found in LANsurfer's dark theme and the remediation backlog derived from them. It is a Track G working document, not a design authority: [`BRANDING.md`](BRANDING.md) and [`docs/ui-kit`](ui-kit/README.md) remain the joint design authority, and this audit only measures the implementation against them. Findings are removed from the backlog as they are fixed; the document is retired when the backlog is empty.

## Method

- Game Request list, detail, composer, and empty state rendered from their `templ` components with fixture data into static HTML, served against the real generated `internal/web/assets/dist/app.css`.
- Rendered in Chromium at 1440×900 and 390×844 with `data-theme="lansurfer-dark"`.
- Contrast computed from the resolved token values, including `color-mix` blends and alpha compositing, against WCAG 2.2 SC 1.4.3 (4.5:1 text) and SC 1.4.11 (3:1 non-text).
- Theme-cascade behavior verified by inspecting the emitted DaisyUI selectors in the built stylesheet.

### Verification limits

- Pages were rendered from components with fixture data, not from a live PostgreSQL-backed instance. Hover, focus, and HTMX swap states were read from CSS rather than exercised in the browser.
- Light mode was not re-audited; finding 1 is the only light-mode entry and was found through the shared cascade.
- The administration, catalog, and login surfaces were reviewed in code only.

## Score

| # | Dimension | Score | Key finding |
| --- | --- | ---: | --- |
| 1 | Accessibility | 3/4 | Form-field boundaries at 1.87:1 are not identifiable |
| 2 | Performance | 4/4 | SSR, embedded assets, paint-only transitions, global reduced-motion |
| 3 | Responsive | 2/4 | Mobile drops every status chip, leaving state carried by colour alone |
| 4 | Theming | 2/4 | Dark tokens leak into the automatic light rendering |
| 5 | Anti-patterns | 3/4 | No generic-template tells; monospace is used well beyond its documented job |
| | **Total** | **14/20** | Theming and responsive behavior need work |

The colour system itself holds: body text runs 14–16:1, filled buttons 6–7:1, and the focus ring 10–12:1 on every dark surface. The defects are at the edges — theme switching, control boundaries, and state signalling.

## Findings

### P1 — fix before release

1. **Dark tokens leak into the automatic light rendering.** `web/src/app.css` sets `--focus-ring`, `--surface-quiet`, `--ink-quiet`, `--surface-danger*`, `--ink-danger`, and `--shadow-raised` on `:root` with dark values, and provides light values only under `[data-theme="lansurfer-light"]`. DaisyUI emits `lansurfer-light` on plain `:root` and `lansurfer-dark` under `@media (prefers-color-scheme: dark) :root:not([data-theme])`; the built stylesheet contains no `prefers-color-scheme: light` rule. A visitor with no theme cookie on a light operating system therefore gets light surfaces with the dark tokens: a Golden Hour focus ring on Sea Mist at roughly 1.3:1, Harbour quiet buttons, no hairline shadow, and none of the light contrast corrections for chips, game abbreviations, and alerts. Move the light override *values* into the theme blocks as tokens so the switch is a single selector list covering both `[data-theme="lansurfer-light"]` and `:root:not([data-theme])` under `prefers-color-scheme: light`.
2. **Form fields are not identifiable in dark mode.** The DaisyUI input border resolves to Ink 20%, giving 1.87:1 against the panel, and the field fill (Deep Ocean) sits at 1.15:1 against the card (Ocean Depth). Nothing marks the control. Fails SC 1.4.11. Raising the dark `--input-color` to roughly 38% reaches 3:1.
3. **One action, two colours.** `detailJoinButtonClass` in `internal/web/pages/game_requests.templ` returns `btn btn-success`, a Sea Foam fill, while the list row uses `btn-primary`. Sea Foam's documented job is labels and quiet-button ink, not an action fill, and the commit level in `BRANDING.md` is Surf Blue.
4. **The live-notice stack covers the right rail.** `#live-notices` is fixed at `right-4 top-20`, which lands on the 18.5 rem rail from 1280 px upward and hides its heading on both the list and the detail view. The connection alert is not dismissible, so it remains for the duration of an outage.
5. **Mobile carries state by colour alone.** Below 48 rem the row hides every status chip, leaving Last place, Joined, Host, and Full readable only through a border tint and a coloured counter. Breaks the `BRANDING.md` rule against colour-only state and SC 1.4.1.
6. **Heading structure.** The Game Request list, the primary signed-in surface, has no `h1`; the first heading on the page is the rail's "Most requested". The administration pages set `<h1 class="data-text count-open text-xs">`, a 13 px monospace heading used as a breadcrumb.

### P2 — fix in the next pass

7. **State text below 4.5:1.** `status-chip-last` at 4.18, `btn-step-back:hover` at 4.19, and `app-navigation-link-active` at 4.49, all at 13–15 px.
8. **Cancelled requests still read as open.** `playerCountClass` branches on full and last-place only, so a cancelled request renders its count in Aqua Wave, the open-places colour.
9. **Sunset inflation on the detail view.** A last-place request shows the card border, the counter, the chip, the Join button, and the "Last place open" row all in Sunset. Scarcity stops reading as scarcity.
10. **Monospace beyond its job.** "Log out", "All Game Requests →", "not followed", "New request", "joins update live", "players", and the composer's help sentence are set in JetBrains Mono. `BRANDING.md` restricts it to counts, times, nicknames, routes, tags, and configuration snippets.
11. **The sticky mobile header ghosts.** `.mobile-header` is 94% Deep Ocean with no `backdrop-filter`, so rows scroll visibly through the wordmark.
12. **Placeholder em dashes ship as UI.** "created —" appears in the detail topbar and host row, and again in the activity list.
13. **The arrival highlight is nearly invisible.** `@keyframes request-arrival` peaks at Surf Blue 24% over Ocean Depth, 1.54:1 against the resting row — the only signal that a new request arrived live.
14. **Surfaces have no contrast headroom.** Ocean Depth on Deep Ocean is 1.15:1 and the Harbour border on Ocean Depth is 1.36:1. Both are legible on a good panel (OKLCH ΔL of 0.051 and 0.082) and leave nothing for a projector or a washed-out event-floor monitor.
15. **Quiet and danger buttons have almost no body.** The `btn-quiet` fill is 1.36:1 and the `btn-danger` fill 1.21:1 against the card, so the decision to give supporting actions a surface instead of an outline barely lands, and the destructive action reads as text alone.
16. **Unused vertical space.** At 1440×900 the list ends near 490 px and the detail view near 650 px, while the topbar carries two lines of 13 px monospace and no title.
17. **`100vh` throughout.** `.app-shell`, `.app-sidebar`, `.request-shell`, and `.auth-stage` are clipped under mobile browser chrome; `100dvh` is the fix.
18. **32 px touch targets.** `btn-sm` on the row Join, Leave, and View actions, the theme toggle, and Log out. This passes SC 2.5.8 at 24 px but is below the 44 px ergonomic target for a standing, hurried player.

### P3 — polish

19. `status-chip-full` is applied in markup but has no rule; the neutral fallback is correct, so either define it or drop the class.
20. `.request-rail` is declared twice in `app.css` with conflicting `display` and `gap`.
21. No `prefers-contrast: more` variant exists in either theme.

## Systemic issues

- **The theme switch is spread across component selectors instead of a token set.** Every light correction is another `[data-theme="lansurfer-light"]` rule, and each one misses the no-cookie case. This is the root cause of finding 1 and will keep reproducing as surfaces are added.
- **Dark mode has one elevation, not a scale.** Page, card, drawer, toast, and rail are all Ocean Depth with no shadow, so overlays do not read as being above the page.
- **Non-text contrast was never in the loop.** Text contrast is handled rigorously, with exact ratios cited in code comments. Every failure above is a boundary, a fill, or a state flash.

## What holds

Token discipline is real: no stray hexadecimal values in components, and `color-mix` derived from theme tokens so both presentations track together. Skeletons are used instead of spinners. The focus ring is 3 px with an offset. Placeholder contrast and disabled-control legibility both have explicit rules. Reduced motion is honoured globally. The theme attribute is server-rendered, so nothing repaints on load. One job per colour is documented and, Sunset aside, followed.

## Remediation backlog

Grouped for implementation. Each group is a coherent change and can be a separate branch.

- [ ] **Theme cascade and control boundaries** — findings 1, 2, 17.
- [ ] **Responsive state and notice placement** — findings 4, 5, 18.
- [ ] **Action and copy consistency** — findings 3, 6, 12.
- [ ] **Typography roles** — findings 6 (administration headings), 10.
- [ ] **Colour semantics and depth** — findings 7, 8, 9, 14, 15.
- [ ] **Live-update feedback** — findings 11, 13.
- [ ] **Cleanup** — findings 16, 19, 20, 21.

Re-run the audit after the P1 groups land and record the new score above.
