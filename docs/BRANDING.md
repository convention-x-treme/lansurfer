# LANsurfer Branding

This document and the [`docs/ui-kit`](ui-kit/README.md) references together define LANsurfer's current approved design system. This document owns the fixed brand foundation, semantic colors, typography, theme behavior, accessibility rules, asset policy, voice, and instance-identity boundaries. The UI kit owns the target screen hierarchy, layout, component dimensions, states, and interactions for the surfaces it represents.

Both sources are required design guidance. New UI work must follow this document and adopt the UI kit rather than treating the completed prototype as the visual reference. If the two design sources appear to conflict, preserve this document's semantic tokens, accessibility rules, and equal dark/light support while following the UI kit's represented composition and interaction model; resolve a genuine conflict by updating both sources together.

Neither design source may redefine product behavior, domain terminology, approved scope, technology, or module ownership. [PRODUCT.md](PRODUCT.md), [TERMINOLOGY.md](TERMINOLOGY.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](../AGENTS.md) remain authoritative for those concerns; current planning/status lives in GitHub Issues and milestones.

## UI kit relationship

The leading design source is the Claude Design project [LANsurfer UI Kit](https://claude.ai/design/p/c259a8f4-c239-427e-8e18-32443502b873), whose `UI-Kit-Colors` and `UI-Kit-Typography` files define the palette and typography recorded below. Where the local snapshot below disagrees with that project, the project wins and this document is updated.

The local UI kit consists of:

- [`ui-kit/README.md`](ui-kit/README.md), which records the represented routes and explicit interaction and terminology decisions;
- [`ui-kit/index.html`](ui-kit/index.html), which provides the visual frames for login, Game Request list and states, creation, mobile behavior, details, and component tokens.

`ui-kit/index.html` is a snapshot of an earlier export. It still shows the previous Montserrat typography and lacks Harbour and Deep Blue, so read colour and typography from this document and the Claude Design project instead, and use the snapshot only for composition, states, and interaction. The standalone HTML is a design reference, not production application code or a runtime dependency. Translate it into the approved templ/HTMX, Tailwind, and DaisyUI frontend architecture, with TypeScript limited to progressive enhancement. Extend the same tokens and component vocabulary to supported screens not yet represented by a frame instead of inventing a parallel visual system.

The current kit is dark-first and shows a 1440 px desktop composition, but implementations must also provide the mobile behavior indicated by the kit and the equally supported light presentation required below. A missing frame is not permission to omit an interaction state, responsive behavior, or accessibility requirement.

## Brand foundation

- Product name: **LANsurfer**
- Tagline: **Find your crew. Catch the next game.**
- Character: social, energetic, welcoming, and immediately understandable at a busy LAN event
- Visual direction: an ocean and surf identity applied to color, imagery, and marketing personality
- Product language: familiar gaming terms such as Game, Game Request, Players, Join, Leave, Follow Game, and My Games

Always write the product name as `LANsurfer`, including the lowercase `surfer`. The tagline may be set in uppercase in a visual lockup, but its canonical wording and punctuation do not change.

## Color system

The palette is part of the current approved product brand. Templates and styles must consume centralized semantic tokens instead of repeating hexadecimal values.

| Token | Value | CMYK (uncoated) | Intended role |
| --- | --- | --- | --- |
| Deep Ocean | `#0D1B2A` | 69 · 36 · 0 · 84 | Dark-mode page background and dark surfaces |
| Ocean Depth | `#1B263B` | 54 · 36 · 0 · 77 | Cards, rows, and navigation |
| Harbour | `#163F57` | 75 · 28 · 0 · 66 | Raised state, borders, skeletons, and avatars |
| Deep Blue | `#0077A8` | 100 · 29 · 0 · 34 | Light-mode primary action and gradients |
| Surf Blue | `#00B4D8` | 100 · 17 · 0 · 15 | Dark-mode primary action and selection |
| Aqua Wave | `#48CAE4` | 68 · 11 · 0 · 11 | Counters, links, and secondary emphasis |
| Sea Foam | `#90E0EF` | 40 · 6 · 0 · 6 | Labels on dark surfaces and quiet accents |
| Sea Mist | `#E1EDF4` | 8 · 3 · 0 · 4 | Light-mode page background and recessed light-mode fields |
| Tide Line | `#BED3DF` | 15 · 5 · 0 · 13 | Light-mode borders, rules, and member avatars |
| White Sand | `#F8F9FA` | 1 · 0 · 0 · 2 | Body text on dark surfaces and raised light-mode surfaces |
| Golden Hour | `#F4D35E` | 0 · 14 · 61 · 4 | Scheduled times, trending, and organizer marks |
| Sunset | `#FF6B35` | 0 · 58 · 79 · 0 | Urgency, happening now, and the last available place |
| Coral | `#FF3F73` | 0 · 75 · 55 · 0 | Cancellation, deletion, and errors |

CMYK values are uncoated-stock conversions for merchandise, print, and signage; re-profile them against the printer's ICC profile before production. Screen work always uses the HEX values.

Surf Blue is the primary action and selection colour in the dark theme, Deep Blue in the light theme, where Surf Blue does not reach sufficient contrast on White Sand. Use one primary action per screen.

The two themes build depth in opposite directions. Dark mode lifts a surface by lightening it (Deep Ocean page, Ocean Depth cards, Harbour raised state). Light mode lowers the canvas instead: Sea Mist is the page and the recessed field, White Sand is the raised surface, and Tide Line draws the borders between them. A light screen that is White Sand throughout has no layers left to read.

### Derived inks

Muted inks are blends of White Sand and Deep Ocean for screen use, not palette colours of their own. In print, tint White Sand or Deep Ocean instead.

| Ink | Value | CMYK (uncoated) | Intended role |
| --- | --- | --- | --- |
| Ink 62% | `#8DA9BF` | 26 · 12 · 0 · 25 | Meta text, labels, inactive navigation |
| Ink 84% | `#C7DCEA` | 15 · 6 · 0 · 8 | Body copy inside cards |

Implementations produce them through the theme's own text colour (for example `color-mix` against `--color-base-content`) so both themes stay correct, rather than hardcoding the hexadecimal values.

Deep Ocean is a blue dark, so it loses contrast faster than a neutral grey as it is diluted. On light surfaces, muted ink stops at 65% of the text colour, and ordinary meta text uses 70%; below that it fails 4.5:1 against Sea Mist. Use 80% for quiet headings.

### Wave gradient

`linear-gradient(135deg, #0D1B2A 0%, #0077A8 40%, #48CAE4 70%, #FF6B35 100%)` is reserved for key visuals such as the login stage. It is not a surface treatment for ordinary cards, rows, or buttons.

Use the ocean blues for the majority of the interface. Sunset, Coral, and Golden Hour are semantic accents, not decorative alternatives to the primary color.

### Accessible color use

- Use Deep Ocean text on Surf Blue, Aqua Wave, Sea Foam, Golden Hour, Sunset, and Coral fills. White Sand text does not have sufficient contrast on those colors for normal-sized text.
- Use White Sand text on Deep Ocean, Ocean Depth, Harbour, and Deep Blue.
- Do not set Surf Blue text or small Surf Blue elements on White Sand. Use Deep Blue there.
- Deep Blue text clears 4.5:1 on White Sand but not on Sea Mist. On Sea Mist surfaces — recessed fields, game abbreviations, member avatars — darken it toward Deep Ocean first.
- The keyboard focus ring is Golden Hour on dark surfaces and Deep Blue on light ones. Golden Hour on Sea Mist is roughly 1.3:1 and is not a visible ring.
- In light mode, alerts are a tinted surface with a matching border and dark ink rather than a saturated fill. The meaning of the colour does not change with the treatment.
- Never communicate state through color alone. Pair it with a label, icon, or other visible treatment.
- Preserve visible keyboard focus, hover, active, disabled, error, and success states in both themes.
- Validate final combinations and interactive states against WCAG contrast requirements during implementation; opacity and blended backgrounds can change the effective contrast.

## Typography

### Primary typeface

Use **Schibsted Grotesk** (SIL Open Font License 1.1) throughout the interface:

- 900 Black: all titles and the wordmark
- 600 Bold: strong emphasis and buttons
- 500 Semibold: light emphasis and active navigation
- 400 Regular: every piece of running text

No other weights are used. Italic is reserved for the LANsurfer wordmark and never appears in UI copy, titles, or taglines.

### Data typeface

Use **JetBrains Mono** (SIL Open Font License 1.1) for compact data where aligned character shapes improve scanning: places and player counts, times, nicknames, routes, tags, and configuration snippets. Never use it for running text or ordinary navigation.

### UI scale

| Use | Typeface | Size | Weight | Tracking |
| --- | --- | ---: | ---: | ---: |
| Page title | Schibsted Grotesk | 36 px | 900 | −0.03em |
| Screen title | Schibsted Grotesk | 30 px | 900 | −0.02em |
| Section title | Schibsted Grotesk | 22 px | 900 | −0.02em |
| Card title | Schibsted Grotesk | 17 px | 900 | normal |
| Row title | Schibsted Grotesk | 16 px | 700 | normal |
| Body | Schibsted Grotesk | 15 px | 400 | normal |
| Body small | Schibsted Grotesk | 14 px | 400 | normal |
| Button | Schibsted Grotesk | 15 px | 500 | normal |
| Navigation item | Schibsted Grotesk | 15 px | 500 | normal |
| Counter | JetBrains Mono | 16 px | 700 | normal |
| Meta and badge | JetBrains Mono | 13 px | 500–700 | 0.12em caps |

Further rules:

- 13 px mono is the smallest type in the product. Below that, drop the element instead of shrinking it.
- Line height 1.15 for headlines, 1.45–1.55 for copy.
- Letter-spacing appears only on headlines and caps labels.
- Caps labels are always mono, 13 px, 0.12em tracking, in Sea Foam or Golden Hour.

### Font delivery

Bundle the variable webfonts with the application and serve them from embedded assets: Schibsted Grotesk upright and italic, plus JetBrains Mono. Runtime font CDNs are not allowed, and neither are Google Fonts stylesheet links. Provide system sans-serif and monospace fallbacks so content remains usable while fonts load or if they fail. Ship each font's license beside the asset.

## Icons

Phosphor Icons is the current approved icon library for LANsurfer. It is an icon font, not a UI component framework; the technology boundaries in [ARCHITECTURE.md](ARCHITECTURE.md) are unchanged by it.

- **Weights.** Regular is the default weight for every icon. Fill is reserved for emphasized state, such as followed or selected. Do not mix weights decoratively within one component, and do not use other weights (Bold, Thin, Light, Duotone).
- **Delivery.** Icons ship as a bundled webfont plus its stylesheet, copied into the embedded assets exactly like the Schibsted Grotesk and JetBrains Mono webfonts. There is no runtime icon CDN, no remote icon URL, and no per-icon JavaScript. Ship Phosphor's MIT license beside the asset, as with the fonts.
- **Color.** Icons inherit `currentColor` and never introduce a colour of their own. The one-job-per-colour rules above govern them like text: a Coral icon means destruction, Golden Hour means time and stewardship.
- **Size and alignment.** Size icons in `em` relative to the adjacent text so they scale with the UI scale table, and keep the optical box aligned with the line height.
- **Accessibility.** Decorative icons are `aria-hidden="true"` and never carry meaning alone. An icon-only control always provides an accessible name. Icons follow the same rule as colour: never the sole carrier of state.

## LANsurfer logo and lockup

Set the wordmark in Schibsted Grotesk italic: 900 for `LAN`, 600 for `surfer`, tracking −0.03em. This is the only italic in the system. Use it with the canonical tagline for the LANsurfer lockup. Current artwork: [logo-with-slogan.png](logo/logo-with-slogan.png). The standalone [icon.png](logo/icon.png) is the brand icon. These are product identity assets, not configurable instance branding.

- use the `LANsurfer` wordmark as text;
- do not promote a prototype or UI-kit placeholder to the production logo or favicon;
- do not use the mascot as a substitute favicon or signet;
- keep at least the height of the wordmark's capital `L` as clear space around a future lockup.

Keep at least the height of the wordmark's capital `L` as clear space around a lockup. The favicons and install icons in [`pwa-icons`](pwa-icons/) are renditions of [icon.png](logo/icon.png), cropped to the artwork's bounds (its transparent margin removed) before scaling; `icon.png` itself stays unmodified:

- `icon-master-1024.png`: 1024 px native-resolution square crop centred on the artwork, transparent; the source for the `any` icons.
- `icon-192.png`, `icon-512.png` (manifest `any`) and `favicon-16/32/48.png`: transparent. Favicons use the tight artwork square so the wave fills the tab; `favicon.ico` holds the 16 and 32 px frames.
- `apple-touch-icon-180.png` and `icon-512-maskable.png`: opaque on White Sand, the artwork's authored ground. The maskable icon scales the artwork's enclosing circle, splash included, into the central 80% safe zone.

Renditions are checked in and copied into the embedded assets by `npm run prepare:assets`; the build needs no image tooling.

Do not redraw, crop, recolor, or substitute the supplied logo and icon without updating this guide and the bundled product assets together.

### Instance identity

LANsurfer keeps fixed/default product branding. A deployment may expose only the minimum configurable instance identity needed to distinguish the installation, as defined by [PRODUCT.md](PRODUCT.md); LANsurfer does not provide a branding-management surface.

- Keep the fixed **Powered by Convention-X-Treme** operator attribution visible. It is not an instance-identity field and is not admin-editable.
- Continue to use this document's palette, typography, component semantics, accessibility rules, and light/dark presentations.
- Use bundled/default product assets for logo and application-icon presentation unless a later approved scope explicitly adds configurable assets.
- Do not add controls for welcome text, contact links, uploaded logos/icons, custom colors, fonts, CSS, white-labeling, or theme editing without an approved product-scope change.

Minimum instance identity is not Event-specific and must not grow into a second branding system.

## Mascot

The LANsurfer otter is a guide, not a decorative pattern. The [initial-mascot.png](mascot/initial-mascot.png) is the original source artwork. Additional poses include [mascot-welcome-pose.png](mascot/mascot-welcome-pose.png) and [mascot-with-surfbord.png](mascot/mascot-with-surfbord.png); [mascot-sprite-sheet.png](mascot/mascot-sprite-sheet.png) contains the mascot sprite sheet.

The app serves pre-sized WebP renditions from [mascot/web/](mascot/web/), not the masters. After replacing a master, regenerate its rendition as described in `scripts/prepare-assets.mjs`.

Use the mascot:

- for a welcome or marketing hero;
- in a helpful empty state;
- when a short explanation benefits from warmth or personality.

Usage rules:

- Show no more than one otter illustration per screen.
- Do not place the otter above or inside a busy list of Game Requests.
- Do not use the otter merely to fill space.
- Provide useful alternative text when the pose communicates information; use an empty `alt` value when it is purely decorative.
- Keep important text and controls outside the bitmap so they remain selectable, translatable, and accessible.

Additional poses should share the same recognizable character, headset, ocean palette, rendering style, and friendly tone before being added to the asset set.

## Themes and surfaces

Dark mode is the primary event-floor presentation, and light mode is an equal supported presentation for daylight, office, projector, and accessibility contexts. Neither mode may lose information or actions.

- Dark pages use Deep Ocean with Ocean Depth cards and navigation surfaces.
- Light pages use a Sea Mist canvas with White Sand cards, rows, navigation, and a Tide Line border on every raised surface. Light surfaces also carry a single hairline shadow; dark surfaces separate by value alone and carry none.
- Inputs and other recessed elements invert that relationship: Sea Mist inside a White Sand panel in light mode, Deep Ocean inside an Ocean Depth card in dark mode.
- Surf Blue remains the primary interactive accent in both themes.
- Semantic meanings for Golden Hour, Sunset, and Coral remain identical across themes.
- Theme choice is a presentation preference, not configurable instance branding. It is remembered in a preference cookie and rendered into the document, so a navigation never paints the other theme first.

Follow the UI kit's choice of dense rows, detail surfaces, forms, and overlays for each task. Use restrained borders, clear grouping, and high information density that remains readable at a glance. Ocean styling must not obscure the primary action or instance identity.

## Component semantics

### One job per colour

The palette is large because print, merchandise, and key visuals need it. On screen every colour carries exactly one job, so a player can read a screen by colour alone.

| Colour | Screen job |
| --- | --- |
| Deep Ocean | Page background (dark), text on bright fills |
| Ocean Depth | Cards, rows, navigation |
| Harbour | Raised quiet surfaces: supporting buttons, borders, member avatars, skeletons |
| Deep Blue | Primary action in the light theme, key-visual gradients |
| Surf Blue | Primary action in the dark theme, selection, active navigation |
| Aqua Wave | Counters, links, joined state |
| Sea Foam | Labels on dark surfaces, ink on quiet buttons, body of the light quiet button |
| Sea Mist | Page background and recessed fields (light) |
| Tide Line | Borders, rules, and member avatars (light) |
| White Sand | Body text on dark, raised surfaces (light) |
| Golden Hour | Time and stewardship: start times, trending, ORGA, host actions |
| Sunset | Scarcity and urgency: the last free place, "now" |
| Coral | Leaving, cancelling, deleting, errors |

A colour that would appear outside its job is the wrong colour. Decoration is not a job.

### Action hierarchy

Buttons carry the same six levels everywhere. The level, not the page, decides the colour; one filled Surf Blue action per view keeps "this moves you forward" readable at a glance.

| Level | Class | Treatment | Examples |
| --- | --- | --- | --- |
| Commit | `btn-primary` | Surf Blue fill, Deep Ocean text (light theme: Deep Blue fill, White Sand text) | Join, Create Game Request, Follow Game |
| Urgent commit | `btn-urgent` | Sunset fill, Deep Ocean text | Join on the last free place |
| Supporting | `btn-quiet` | Harbour surface, Sea Foam text (light theme: Sea Foam and Tide Line surface, dark Deep Blue text) | Find Players, View, Search, Save, Copy link |
| Step back | `btn-step-back` | Supporting at rest, Coral surface and text on hover and focus | Leave, Unfollow, Close for new players, Deactivate game, Disable, Remove device |
| Destructive | `btn-danger` | Coral-tinted surface and Coral text at rest | Cancel Game Request, Delete permanently, Disconnect |
| Tertiary | `btn-ghost` | Text only | Clear, Log out, Dismiss |

Rules that keep the hierarchy honest:

- Every button carries a Phosphor icon, with or without a visible label. A text-only button is an exception that a human reviewer approves explicitly in the pull request; the icon-only case still needs an accessible name (see [Icons](#icons)).
- Every action with a negative effect — delete, remove, disable, deactivate, disconnect, leave, unfollow, cancel — shows Coral on hover and focus. Reversible ones use `btn-step-back`, irreversible ones `btn-danger`; never `btn-ghost` or `btn-quiet`, whose hover stays neutral.
- Reversible actions never wear Coral at rest. Leaving a Game Request or unfollowing a game is repeatable, so the colour only appears once the pointer or keyboard is on the control. Permanent alarm colour on a repeatable action trains players to ignore it.
- Irreversible actions wear Coral before the click and ask for confirmation.
- Outline buttons are not a level. A supporting action gets the Harbour surface instead, so it reads as a real choice rather than a disabled control.
- Disabled controls keep a legible label: they are dimmed, not erased.
- Every level keeps the palette's contrast rules: button text meets 4.5:1 against its own fill in both themes.

### Action feedback

Every button action ends in visible feedback on the page the player lands on: an inline message, a notice after the redirect, or a live toast. An action never fails silently, and a success the page does not already show by itself gets a message too.

| Outcome | Class | Role | Example |
| --- | --- | --- | --- |
| Reached a goal: something new now exists or now works for the player | `alert-success` | `status` | "Push notifications enabled on this device.", "Game Request created.", "Discord destination connected." |
| Routine change saved, or neutral information | `alert-info` | `status` | "Notification audience updated.", "Device removed.", "Push notifications paused." |
| Partly succeeded or needs attention | `alert-warning` | `status` | "Web Push is not configured on this instance." |
| Failed | `alert-error` | `alert` | "That sign-in method could not be linked." |

`alert-success` (Sea Foam) is the rarer of the two positive tones: keep it for outcomes a player would tell someone about, so it keeps its weight. Editing, pausing, or removing something is `alert-info` even though it succeeded. Failures name the problem and the recovery, and appear next to the control or at the top of the page, never only in the browser console.

### Other components

- A scheduled start time may use Golden Hour.
- Player and available-place counts use JetBrains Mono; the last available place uses Sunset, and a full request uses a muted treatment.
- State chips (`OPEN`, `JOINED`, `HOST`, `LAST PLACE`, `FULL`) are mono, uppercase, and tinted with the colour of the state they name.
- Errors use Coral with a clear message. Informational feedback uses Aqua Wave or Surf Blue. Feedback classes follow [Action feedback](#action-feedback).
- Badges and metadata stay visually quieter than titles and primary actions.

The same component hierarchy is used for in-browser live updates and supports Web Push and Discord settings. Durable delivery does not add a persistent in-app notification inbox.

## Voice and terminology boundary

Brand personality may appear in marketing headlines and occasional supporting copy, but users must not learn a surf vocabulary to operate LANsurfer. Do not rename Game Requests to Waves, Players to Crew, available places to Spots, or Join to Catch in navigation, controls, code, or persistence.

The design system does not introduce or imply:

- a separate Ready-to-Play state
- a YAML theme file or full theme customization
- Discord bot/gateway interactions
- ticket claims or venue maps
- a waitlist
- internationalization/localization unless approved product scope changes

## Implementation boundary

Keep the fixed LANsurfer brand definition near the web composition layer as one small value/object containing the product name, tagline, semantic colors, the operating community's name and public URL used by the **Powered by …** attribution, and built-in asset paths. Tailwind/DaisyUI theme values, CSS custom properties, and templ components should derive from that definition or its generated/static stylesheet rather than introduce parallel constants.

Every **Powered by …** credit in the interface renders from the operator name and URL in that definition. Changing the credited community is a single-value change there, not a template edit or administrator setting.

Fixed brand values and the operator attribution are not environment configuration or mutable PostgreSQL state. Any minimum instance identity permitted by PRODUCT/ROADMAP is separate product state and must remain deliberately small rather than becoming a branding editor.

The approved remote IGDB cover host is a catalog-content exception, not a branding exception. It does not permit remote logos, icons, fonts, theme assets, or arbitrary images.