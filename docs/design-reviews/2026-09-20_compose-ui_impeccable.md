Audit — "Create Game Request" composer (pick list + details)

Measured live against the browser fixture at 320 / 390 / 1280px in both themes. Contrast computed from resolved `oklab`/`color(srgb)` values, not eyeballed.

## Audit Health Score

| # | Dimension | Score | Key Finding |
|---|-----------|-------|-------------|
| 1 | Accessibility | 3 | Server error message isn't associated with the field it names; unselected Start chip border is 2.18:1 (dark) / 1.47:1 (light) |
| 2 | Performance | 4 | 7.5 KB HTML, embedded assets (23 KB CSS + 20 KB JS gzipped), no SSE opened on this page, DCL 22 ms |
| 3 | Responsive Design | 3 | No overflow at 320px; touch targets 40px where the coarse-pointer rule only lifts `.btn-sm` |
| 4 | Theming | 4 | Every value is a token or `color-mix` on one; both themes verified |
| 5 | Anti-Patterns | 4 | No tells — no gradient text, no glass, no eyebrows, no side stripes, no nested cards |
| **Total** | | **18/20** | **Excellent (minor polish)** |

## Anti-Patterns Verdict — **PASS**

Nothing here reads as AI-generated. The pick row is a cover + title link, not an icon-heading-text card grid; the details step is one column of labelled native controls; color is Restrained (Surf Blue accent only on the primary action, the selected chip, and the chosen-game edge). One honest gap versus the design kit: the kit draws the Create button in Sunset, and `docs/BRANDING.md` reserves Sunset for `btn-urgent`, so it stays Surf Blue — deliberate, documented.

## Executive Summary

- **18/20**, issues: 0 P0 · 3 P1 · 3 P2 · 3 P3
- Top issues: pick cover renders 96px instead of the specified 75px; the validation error is 288px away from its field with no programmatic link; Start chips fail non-text contrast.

## Findings

### [P1] Pick list cover is 96px, spec says max 75px
**Location**: `web/src/app.css:619` vs `web/src/app.css:885-889` · **Category**: Responsive / correctness
`.pick-cover { width: 4.6875rem }` (line 619) is overridden by the shared portrait-tile rule `.request-cover, .trending-cover, .detail-cover, .pick-cover { width: 6rem }` at line 885 — same specificity, later wins. Measured: `96px`. The 3:4 ratio is correct.
**Fix**: move `.pick-cover`'s width after the shared rule (or set it there).
**Command**: `/impeccable polish`

### [P1] Validation error is not tied to the field it describes
**Location**: `internal/web/pages/game_requests.templ:291-293`, `:373` · **Category**: Accessibility (WCAG 1.3.1, 3.3.1)
Submitting `1000` re-renders with "Maximum players must be a whole number from 0 to 999." in a page-level alert **288px above** the field. The input carries no `aria-invalid`, no `aria-describedby`, and focus stays on `<body>`. A screen-reader user lands at the top with no route to the offending control; a sighted user on a scrolled page may not see it. The design kit itself draws this error inside the field group.
**Fix**: render the message in the Max players group, give it an id, point `aria-describedby` at it, set `aria-invalid="true"`, and focus the field on the error re-render.
**Command**: `/impeccable harden`

### [P1] Start chips fail non-text contrast
**Location**: `web/src/app.css:1107-1116` (`.start-choice span`) · **Category**: Accessibility (WCAG 1.4.11, 3:1)
The unselected chip's only visible boundary is its border: **2.18:1** dark, **1.47:1** light against the panel. The chip label itself is fine (8.11:1).
**Fix**: raise the chip border to ≥3:1 (e.g. `color-mix(primary 40%, border-raised)`), or give unselected chips a filled surface.
**Command**: `/impeccable polish`

### [P2] Touch targets are 40px where 44px is the house rule
**Location**: `.start-choice span`, `.input`, `.btn` at `@media (pointer: coarse)` (`web/src/app.css:1479`) · **Category**: Responsive
The coarse-pointer block lifts only `.btn-sm` and `.action-static` to 44px. At coarse pointer the Start chips, Max players, Date, Time, Create and Cancel all stay 40px — fine for WCAG 2.5.8 (24px), short of the 44px the project already decided on for a standing player.
**Fix**: extend the coarse block to `.btn`, `.input`, and `.start-choice span`.
**Command**: `/impeccable adapt`

### [P2] Live search gives no pending feedback
**Location**: `GameRequestDiscoverySearchForm` (`game_requests.templ:395`) · **Category**: Accessibility / UX
The 300ms-debounced swap has no indicator and no `aria-busy`. On a congested event network the list just sits there. htmx already puts `.htmx-request` on the form — nothing styles it.
**Fix**: style `.htmx-request` on the search form (a quiet spinner or dimmed list) and set `aria-busy` on `#game-request-discovery` during the request.
**Command**: `/impeccable polish`

### [P2] "Change game" is a 32px link that carries a critical reset
**Location**: `game_requests.templ:343` · **Category**: Responsive / UX
`btn-sm` → 32px (44px at coarse pointer via the existing rule), 140px wide, sitting under a 100px cover. It's the only way back to the pick list; fine on desktop, tight in the field.
**Fix**: promote to default `btn` size on the details step.
**Command**: `/impeccable adapt`

### [P3] Empty-state heading nests at the wrong level
**Location**: `GameRequestPickEmpty` (`game_requests.templ:487`) · **Category**: Accessibility
"You follow no games yet." is an `<h2>`, sibling to the `<h2>` "Find your game" it sits under. Heading outline reads as two peers rather than section + state.
**Fix**: `<h3>`, or drop the heading semantics.

### [P3] Cover images are `loading="lazy"` above the fold
**Location**: `CoverArt` (`game_requests.templ:126-133`) · **Category**: Performance
Correct for the list; on the details step the chosen game's cover is the one image in the viewport and lazy delays it slightly.
**Fix**: pass eager/`fetchpriority="high"` for the selected-game cover only.
**Command**: `/impeccable optimize`

### [P3] Max players hint duplicates the placeholder
**Location**: `game_requests.templ:373-374` · **Category**: Copy
Placeholder "No limit" + hint "0 or empty is an open round with no player limit." say the same thing twice, 13px apart.
**Command**: `/impeccable clarify`

## Patterns & Systemic Issues

- **Cascade-order collisions in a single-layer CSS file.** Both cover-width bugs come from the same cause: component rules at ~line 600 competing with the shared tile rule at ~line 885 at equal specificity. A shared `--cover-width` custom property per component would remove the ordering dependency. (Related: daisyUI's `.input` sits in the `utilities` layer, so `.request-search`'s `font-size` at line 748 is also dead — same class of problem, outside this surface.)
- **Non-text contrast is the consistent weak spot** while text contrast is uniformly strong (5.2:1–16.5:1 across both themes). Borders were chosen for subtlety without the 3:1 check.
- **Error handling stops at "render the message."** No field association, no focus move, no inline placement — one pattern, used on every composer error.

## Positive Findings

- **Text contrast is comfortably AA everywhere**, both themes: subtitle 7.75 / 6.30, hints 6.89 / 5.32, buttons 7.06+, placeholder 8.55 / 5.94.
- **Every field is properly labelled** (`for`/`id` throughout, `sr-only` label on search, `fieldset` + `legend` for Start), radio-based chips give real keyboard semantics, `#game-request-discovery` is `aria-live="polite"`.
- **No horizontal overflow at 320px**, nothing escapes the panel, and the layout degrades by stacking rather than shrinking.
- **Progressive enhancement holds**: search falls back to a `noscript` submit onto a real page, the clamp is enhancement-only, and the server validates the same range independently.
- **No SSE stream on the composer** — the realtime connection is only opened where it's used.
- **The theme system is doing real work**: both themes derive from the same tokens; no hard-coded hex in any composer rule.

## Recommended Actions

1. **[P1] `/impeccable polish`** — fix the 96px cover override and raise the Start chip border to 3:1.
2. **[P1] `/impeccable harden`** — inline, associated, focused field errors on the composer.
3. **[P2] `/impeccable adapt`** — extend the 44px coarse-pointer rule past `.btn-sm`; promote "Change game".
4. **[P3] `/impeccable clarify`** — de-duplicate the Max players placeholder and hint.
5. **[P3] `/impeccable optimize`** — eager-load the selected game's cover.

You can ask me to run these one at a time, all at once, or in any order you prefer.

Re-run `/impeccable audit` after fixes to see your score improve.
