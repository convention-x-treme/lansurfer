Audit — administration screens (`/admin/games`, `/admin/games/{id}/igdb`, `/admin/games/{id}/merge`, `/admin/igdb`, `/admin/pwa`)

Measured live against a throwaway fixture that renders the real templates and the embedded production assets, at 390 / 1440px in both themes. Contrast composited from resolved `oklab` / `color(srgb)` values in the browser, not eyeballed.

## Audit Health Score (before)

| # | Dimension | Score | Key Finding |
|---|-----------|-------|-------------|
| 1 | Accessibility | 3 | No contrast failures; `aria-live` missing on the result list, and the catalog was one unbroken wall of always-open forms with no scanning structure |
| 2 | Performance | 3 | Six Games rendered 2 939px of page and 4 forms each; a real catalog ships every form for every Game on every load |
| 3 | Responsive Design | 3 | No overflow, but the catalog card's own class collided with the player-facing `.game-card` (cover-first, `padding: 0`), so admin content sat flush against the card border |
| 4 | Theming | 4 | Every value a token or a `color-mix` on one; both themes correct |
| 5 | Anti-Patterns | 3 | No AI tells; product-register failures instead — Search/Clear buttons against the product's own live-filtering rule, and a bespoke `grid-cols-[auto_minmax(0,1fr)_auto]` row instead of the shared cover/row vocabulary |
| **Total** | | **16/20** | **Good (address weak dimensions)** |

## Anti-Patterns Verdict — **PASS** (cross-register), **FAIL** (product register)

Nothing reads as AI-generated: no gradient text, no glass, no eyebrows, no side stripes, no hero metrics. The failures were the product-register kind — inconsistent vocabulary between administration and the rest of the application:

- `/admin/games` used a **Search** submit button and a **Clear** link. PRODUCT ("Live filtering") says every filter applies as its value changes and that a separate Filter/Apply control is not provided. Both player-facing lists already do this.
- `AdminGameCard` reused the class name `.game-card`, which the Games rework redefined as the cover-first player card with no padding. The admin card inherited `padding: 0` and `overflow: hidden`.
- The IGDB link result rebuilt a row layout in utility classes instead of using `CoverArt` and the shared cover tile, so the same Game looked different in administration than anywhere else.
- Sibling administration screens navigated through a pair of buttons in each top bar, while every other section navigates from the sidebar.
- The manual "Add game" composer held a permanent left column ahead of the catalog, although IGDB discovery now materializes Games on follow/request.

## Findings and what changed

| Sev | Finding | Change |
|-----|---------|--------|
| P1 | `.game-card` collision left admin card content flush against its border | Catalog entries are their own `.catalog-entry` component; nothing shares a class with the player card |
| P1 | Search/Clear buttons contradict PRODUCT's live-filtering rule | `/admin/games` and the IGDB link search debounce-apply themselves over htmx, replace the URL, and drop stale in-flight requests; a `<noscript>` submit stays for no-JavaScript use |
| P1 | Every Game rendered four always-open forms | One Game is a `<details>` row: collapsed it states cover, name, slug, usage, aliases and identity; opened it holds identity, aliases and deletion. Native disclosure, so it still works without scripting |
| P2 | No cover art in administration although every other surface has it | `ListGames` now selects `cover_image_id` from the enrichment snapshot; rows render the shared `CoverArt` tile at a new 2.75rem `.catalog-cover` width |
| P2 | Nothing stated the shape of the catalog | Count line: `N games · N active · N linked to IGDB`, plus a "Not linked to IGDB" filter — the state that still needs administrator work |
| P2 | Cross-screen navigation lived in per-page button pairs | Sidebar carries `Administration` with nested `IGDB sync` and `PWA`, the same pattern as `Games` / `Followed`; per-page pairs removed |
| P2 | Manual creation led the screen | Collapsed `<details>` under the catalog, naming when it is the right tool |
| P3 | Link/merge screens carried a bare back link and no title in the top bar | Both now use the standard top bar: title, one-line context, `Back to catalog` |
| P3 | `/admin/pwa` stacked two short definition lists down a wide empty page | Two columns from `xl` upward |

## Second pass — panel readability and edit grouping

The redesigned entry panel exposed its own layout problems once it held real enrichment data.

| Finding | Change |
|---------|--------|
| Aliases sat in a section below the provider snapshot, so one Game's editable fields were split across a read-only block | Name, slug, aliases and availability are one **Game details** section; the provider snapshot is a separate **IGDB record** section |
| Genres and game modes shared one undifferentiated strip of badges (ghost vs outline, indistinguishable at a glance) | One labelled row per kind of fact: Type, Genres, Modes, Platforms, Released, Multiplayer. A fixed-basis flex label aligns every row without a breakpoint and lets values wrap under it on a narrow panel |
| The summary was 13px across the full panel width | 14px / 1.6 line-height, capped at 68ch, `text-wrap: pretty` |
| Each platform's multiplayer data was one sentence (`PC (Microsoft Windows): LAN co-op · online max 12`) | One row per platform: platform name, then capability chips; rows separated by a hairline |
| Player caps were listed apart from the capability they belonged to (`online co-op max 8 online max 12`) | Each capability carries its own cap (`Online co-op (max 8)`); the platform-wide caps read `Up to 12 online` in the data face |
| Spacing alone no longer grouped the three jobs in the panel | A hairline separates edit / inspect / delete |

`multiplayerModeText` is replaced by `multiplayerPlatformLabel` + `multiplayerCapabilities` + `multiplayerLimits`, so the template composes the parts instead of receiving one pre-joined sentence. `AdminIGDBMetadata` now takes an `AdminGameFacts` value, which is what lets the provider-candidate screen state Type / Platforms / Released as rows rather than as a third small grey line.

Contrast, composited, dark / light: summary 9.2 / 9.0, fact label 5.9 / 4.7, value 9.2 / 9.0, chip 9.2 / 9.0, cap chip 7.5 / 8.1, platform name 12.9 / 15.3, field label 14.4 / 16.5, field hint 8.1 / 6.8. Verified at 390 and 1440px in both themes.

## Third pass — typography, tags, and one place per decision

| Request | Change |
|---------|--------|
| Summary should use the full width | `.igdb-summary` drops the 68ch measure; it keeps 14px / 1.6 |
| Open the record on igdb.com | `ListGames` resolves IGDB's own slug from the active provider index (cast off `game_igdb_enrichment.external_id`, whose CHECK is numeric, so no unsafe cast and no seq scan), and the admin provider search returns it too. `Open on IGDB ↗` opens `https://www.igdb.com/games/<slug>` in a new tab with `rel="noopener noreferrer"`, path-escaped. No slug (provider disabled, never synced, record tombstoned) means no button rather than a guessed URL |
| Sub-headings should look alike, in mono | `Game details`, `Search aliases`, `IGDB record`, `Availability and deletion` all render as `.catalog-section-heading`: mono, 13px, 700, uppercase, `0.02em`. The section/field split that made two of them 14px semibold is gone |
| Name and slug labels in mono | `.catalog-input-label`, same face and size at weight 600 without the uppercase, since it names one input rather than a region |
| Nicer tag badges | `.igdb-chip` is a filled pill on a 14% primary tint at weight 600, instead of a 1px outlined box; cap chips keep the mono face on a neutral tint |
| The add-alias field takes too much room | Aliases and the add field share one `.alias-row`: a 9rem input and an `Add` button sit inline after the last alias chip, instead of a full-width input plus button on their own row. Alias chips are filled pills that turn Coral on hover/focus, since pressing one removes it; the 2.75rem touch target is kept for coarse pointers only |
| Deactivation and deletion belong together | One `Availability and deletion` section with two `.catalog-decision` rows — sentence left, action right — because choosing between hiding a Game and deleting it is a single decision |

Contrast, composited, dark / light: section heading 8.1 / 6.8, input label 8.1 / 6.8, tag chip 7.3 / 4.9, cap chip 7.1 / 7.8, alias chip 10.7 / 13.4, decision text 9.2 / 8.3. Every value clears 4.5:1.

## Fourth pass — the summary is not cut

Administration states the stored summary in full. The 400-rune cap `AdminIGDBMetadata` applied is a player-card preview rule, and this surface exists to inspect the snapshot, so a cut sentence was the one thing an operator could not check. `.igdb-summary` also sets `white-space: pre-line`, so a provider summary with paragraph breaks reads as paragraphs rather than as one block. `truncateSummary` moved to `games.templ`, beside its only remaining caller.

### Availability and deletion column split

`.catalog-decision` is a `2fr 1fr` grid from 40rem up: the sentence takes two thirds, the control one third and right-aligned, so both rows' buttons share a column edge. Below 40rem the row stacks and the control keeps its natural width (`justify-self: start`) rather than stretching into a full-width bar, which is what a grid item does by default. The 62ch measure on the text is gone — the column governs the line length now.

### Panel changes no longer reload the screen

Adding an alias posted the form and redirected, which reloaded `/admin/games`, collapsed the `<details>` the administrator was working in, and threw away the scroll position. Every mutation inside a panel now targets its own entry instead:

- the name/slug form, the alias add and remove forms, and the activation form carry `hx-post` with `hx-target="closest .catalog-entry"` and `hx-swap="outerHTML"`, plus the product's usual `hx-sync="this:drop"` / `hx-disabled-elt` rapid-click guards
- `AdminGameEntry(item, expanded, message)` renders the re-read Game, still open, so the swap puts back what was replaced; a rejected change comes back the same way with the complaint inside its own panel, answered 200 because htmx leaves a non-2xx response unswapped
- without scripting the same routes still post and redirect, now to `#game-<id>`, so a reload at least lands on the Game being edited
- `app.ts` returns the caret to the alias field after the swap, so several aliases can be added in a row
- the two confirmed actions use `hx-confirm` alone. Carrying `data-confirm` as well prompted twice, since htmx ships in the same bundle as that handler and there is no scripting-off path where only one of them runs

Driven in Chromium against the real router: scroll unchanged (520 → 520), entry still open, only that entry swapped, focus back in the alias field, duplicate alias answered inline, deactivation flipping the chip and the button in place, one confirmation dialog.

## Verification

- `go test -race ./...`, `task test:integration` (the modified `ListGames` runs against real PostgreSQL), `task lint`: all pass.
- Contrast, composited, dark / light: entry title 14.4 / 16.5, row meta 7.8 / 6.4, section heading 6.4 / 4.8, `Edit` disclosure 10.2 / 6.4, state chips 10.2 / 5.1–6.4. Every value clears 4.5:1.
- 390px: the row wraps chips and the disclosure onto their own right-aligned line rather than squeezing the name into a one-character column.
- Six Games render in one 1 440×1 100 viewport, against 2 939px of page before.

## Not changed

- `Sync now` stays inside the status card rather than moving to the top bar: the card is the polled htmx fragment, and the action is part of the state it reports.
- Provider record counts stay unseparated digits (`284123`) in the mono/tabular face, matching the assertions and the rest of the data vocabulary.
