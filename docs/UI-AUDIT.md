# Dark-mode presentation audit

Audit date: 2026-09-16. Branch: `feat/adopt-design-kit`, at commit `5f18dfe`.
Remediation: 2026-09-17, branch `feat/impeccable-recommendations`.

This document records the presentation defects found in LANsurfer's dark theme and the remediation derived from them. It is a historical presentation-audit working document, not a design authority: [`DESIGN.md`](../DESIGN.md) (formerly `docs/BRANDING.md`) and [`docs/ui-kit`](ui-kit/README.md) remain the design authority, and this audit only measures the implementation against them. **Every finding below was fixed and the remediation backlog is empty; finding 9 has since been partly reopened by the kit, as recorded under it.** The document is kept until a fresh audit replaces its score; it is retired once that audit is recorded.

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

The 2026-09-16 score below is the audited state. It is deliberately **not** revised here: the remediation was verified per finding, not by re-running the full audit, and a score is only meaningful when the whole method is re-run.

| # | Dimension | Score | Key finding |
| --- | --- | ---: | --- |
| 1 | Accessibility | 3/4 | Form-field boundaries at 1.87:1 are not identifiable |
| 2 | Performance | 4/4 | SSR, embedded assets, paint-only transitions, global reduced-motion |
| 3 | Responsive | 2/4 | Mobile drops every status chip, leaving state carried by colour alone |
| 4 | Theming | 2/4 | Dark tokens leak into the automatic light rendering |
| 5 | Anti-patterns | 3/4 | No generic-template tells; monospace is used well beyond its documented job |
| | **Total** | **14/20** | Theming and responsive behavior need work |

The colour system itself held: body text ran 14–16:1, filled buttons 6–7:1, and the focus ring 10–12:1 on every dark surface. The defects were at the edges — theme switching, control boundaries, and state signalling.

## Findings and resolutions

Each entry keeps its original measurement and records what replaced it. Ratios in the resolution column were computed from the shipped token values.

### P1

1. **Dark tokens leaked into the automatic light rendering.** `:root` carried dark values for `--focus-ring`, `--surface-quiet`, `--ink-quiet`, `--surface-danger*`, `--ink-danger`, and `--shadow-raised`, with light values only under `[data-theme="lansurfer-light"]`. A visitor with no theme cookie on a light operating system got light surfaces with dark tokens.
   **Resolved.** One token set on `:root`, each declaration switched with `light-dark()`. DaisyUI sets `color-scheme` on every theme selector it emits, including `:root:not([data-theme])` under `prefers-color-scheme`, so the no-cookie case now resolves with the rest. Every per-component `[data-theme="lansurfer-light"]` override is gone, replaced by `--ink-open`, `--ink-hot`, `--ink-last`, `--ink-chip-last`, `--ink-selected`, `--ink-nav-active`, `--ink-abbreviation`, `--surface-avatar-member`, `--border-raised`, `--input-border`, and the alert tint tokens.
2. **Form fields were not identifiable in dark mode.** DaisyUI's border resolved to Ink 20% — 1.87:1 against the panel. Failed SC 1.4.11.
   **Resolved.** `--input-border` overrides `--input-color` on `.input`, `.select`, `.textarea`, and `.file-input`: 4.41:1 against the field fill and 3.84:1 against the card in dark mode, 3.70:1 and 4.19:1 in light. The field's focus ring is now the system's 3 px `--focus-ring` rather than the 2 px outline DaisyUI derives from the same border colour.
3. **One action, two colours.** `detailJoinButtonClass` returned `btn btn-success`.
   **Resolved.** Returns `btn btn-primary`, matching the list row and the commit level in `DESIGN.md`.
4. **The live-notice stack covered the right rail.** Fixed at `right-4 top-20`, landing on the 18.5 rem rail from 1280 px up; the connection alert was not dismissible.
   **Resolved.** The stack anchors bottom-right in every viewport, so the mobile-only override is gone, and every live notice — including the connection alert — is built by one `liveNotice()` helper that always adds a Dismiss control. The oob sweep no longer expires the connection alert; reopening the stream clears it.
5. **Mobile carried state by colour alone.** Below 48 rem the row hid every status chip.
   **Resolved.** The chip wraps onto a second line inside the row instead of being hidden, so Host, Joined, Last place, and Full stay readable as text.
6. **Heading structure.** The Game Request list had no `h1`; administration used `<h1 class="data-text count-open text-xs">` as a breadcrumb.
   **Resolved.** An `.app-title` screen title per the `DESIGN.md` type scale on the Game Request list, Games, My Games, the composer, both administration pages, and the error surfaces. Alert headings inside those pages are no longer `h1`.

### P2

7. **State text below 4.5:1.** `status-chip-last` 4.18, `btn-step-back:hover` 4.19, `app-navigation-link-active` 4.49.
   **Resolved.** `--ink-chip-last` reaches 4.65:1 on its own tint; the step-back hover surface gets `--ink-danger-strong` at 5.16:1; the active navigation tint drops to 12% for 5.03:1 in dark mode and 5.19:1 in light.
8. **Cancelled requests still read as open.** `playerCountClass` branched on full and last-place only.
   **Resolved.** Both counter helpers treat any non-open status as muted.
9. **Sunset inflation on the detail view.** Card border, counter, chip, Join button, and the "Last place open" row were all Sunset.
   **Resolved.** The hero's Sunset border, the duplicate "Last place" chip, the row's Sunset rule, and the row label's Sunset ink are gone. Scarcity now reads on the counter, the urgent Join action, and the empty-place marker.
   **Reopened by the kit, 2026-09-20.** `UI-Kit-Request-Detail` and `UI-Kit-Game-Requests-v2` both put the Sunset border and the "Last place" chip back, so the list row and the detail hero now carry them again and state scarcity the same way. The next audit has to measure whether that is one signal read twice or the inflation this finding named.
10. **Monospace beyond its job.** Running text, navigation, and controls were set in JetBrains Mono.
    **Resolved.** "Log out", the viewer role, "All Game Requests →", "followed"/"not followed", "New request", "joins update live", "players", the composer's help sentence, the login divider, "Coming soon", and the operator attribution are Schibsted Grotesk. Mono is kept for counts, times, nicknames, slugs, and tags.
11. **The sticky mobile header ghosted.** 94% Deep Ocean with no `backdrop-filter`.
    **Resolved.** The header is opaque.
12. **Placeholder em dashes shipped as UI.** `clockLabel` returned "—" for a zero time.
    **Resolved.** `clockLabel` returns an empty string and the detail top bar, the member row, and the activity list omit the element entirely. The member and activity `<time>` elements now carry the matching `datetime`.
13. **The arrival highlight was nearly invisible.** 1.54:1 against the resting row.
    **Resolved.** The keyframe peaks at 2.34:1 and flashes the row border to Surf Blue on the same beat.
14. **Surfaces had no contrast headroom.** Harbour border on Ocean Depth at 1.36:1.
    **Resolved.** `--border-raised` lifts the same hue with Sea Foam to 2.17:1 against the card and 2.49:1 against the page, and is used by every raised-surface border. Overlays additionally carry `--shadow-overlay`, giving dark mode a second elevation for the drawer and the toast stack.
15. **Quiet and danger buttons had almost no body.** 1.36:1 and 1.21:1 against the card.
    **Resolved.** The quiet surface reaches 1.93:1 with Sea Foam ink at 5.26:1; the danger surface reaches 1.47:1 with ink at 5.04:1 and 1.85:1 on hover with ink at 5.16:1. Light mode's quiet surface moved from a Sea Foam wash to a Tide Line body at 1.71:1.
16. **Unused vertical space; no title in the top bar.**
    **Resolved by finding 6.** The top bar carries the screen title with the counters beneath it, and wraps the page action under the title below 48 rem.
17. **`100vh` throughout.**
    **Resolved.** `body`, `.app-shell`, `.app-sidebar`, `.request-shell`, `.auth-stage`, and the mascot's cap use `100dvh`.
18. **32 px touch targets.**
    **Resolved.** `@media (pointer: coarse)` raises `.btn-sm` and `.action-static` to 44 px without changing desktop density.

### P3

19. **`status-chip-full` had no rule.** **Resolved.** Dropped from the markup; the neutral chip is the documented muted treatment for a full request.
20. **`.request-rail` was declared twice** with conflicting `display` and `gap`. **Resolved.** One declaration.
21. **No `prefers-contrast: more` variant.** **Resolved.** The variant raises `--border-raised` and `--input-border`, lifts muted inks to full base content, and outlines status chips instead of tinting them.

## Systemic issues

- **The theme switch was spread across component selectors instead of a token set.** Closed by finding 1: theme differences now live in one `light-dark()` token block and no component carries a `[data-theme]` override.
- **Dark mode had one elevation, not a scale.** Closed by finding 14: `--shadow-raised` for surfaces, `--shadow-overlay` for the drawer and the toast stack.
- **Non-text contrast was never in the loop.** Every boundary, fill, and state flash above now has a measured ratio recorded beside it.

## What holds

Token discipline is real: no stray hexadecimal values in components, and `color-mix` derived from theme tokens so both presentations track together. Skeletons are used instead of spinners. The focus ring is 3 px with an offset. Placeholder contrast and disabled-control legibility both have explicit rules. Reduced motion is honoured globally. The theme attribute is server-rendered, so nothing repaints on load. One job per colour is documented and followed.

## Remediation backlog

- [x] **Theme cascade and control boundaries** — findings 1, 2, 17.
- [x] **Responsive state and notice placement** — findings 4, 5, 18.
- [x] **Action and copy consistency** — findings 3, 6, 12.
- [x] **Typography roles** — findings 6 (administration headings), 10.
- [x] **Colour semantics and depth** — findings 7, 8, 9, 14, 15.
- [x] **Live-update feedback** — findings 11, 13.
- [x] **Cleanup** — findings 16, 19, 20, 21.

Re-run the full audit against a live instance and record the new score above; hover, focus, and HTMX/SSE swap states in particular were verified from CSS and fixture renders rather than exercised end to end.
