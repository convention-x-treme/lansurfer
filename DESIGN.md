---
name: LANsurfer
description: "Find your crew. Catch the next game."
colors:
  deep-ocean: "#0D1B2A"
  ocean-depth: "#1B263B"
  harbour: "#163F57"
  deep-blue: "#0077A8"
  surf-blue: "#00B4D8"
  aqua-wave: "#48CAE4"
  sea-foam: "#90E0EF"
  sea-mist: "#E1EDF4"
  tide-line: "#BED3DF"
  white-sand: "#F8F9FA"
  golden-hour: "#F4D35E"
  sunset: "#FF6B35"
  coral: "#FF3F73"
typography:
  page-title:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "36px"
    fontWeight: 900
    lineHeight: 1.15
    letterSpacing: "-0.03em"
  screen-title:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "30px"
    fontWeight: 900
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  section-title:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 900
    lineHeight: 1.15
    letterSpacing: "-0.02em"
  card-title:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "17px"
    fontWeight: 900  row-title:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 600  body:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400  body-small:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400  button:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 600  navigation-item:
    fontFamily: "Schibsted Grotesk, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400  counter:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "16px"
    fontWeight: 700  meta:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "13px"
    fontWeight: 500
    letterSpacing: "0.12em"
rounded:
  selector: "0.25rem"
  field: "0.375rem"
  box: "0.5rem"
components:
  button-primary:
    backgroundColor: "{colors.surf-blue}"
    textColor: "{colors.deep-ocean}"
    typography: "{typography.button}"
  button-primary-light:
    backgroundColor: "{colors.deep-blue}"
    textColor: "{colors.white-sand}"
    typography: "{typography.button}"
  button-urgent:
    backgroundColor: "{colors.sunset}"
    textColor: "{colors.deep-ocean}"
    typography: "{typography.button}"
  button-quiet:
    backgroundColor: "{colors.harbour}"
    textColor: "{colors.sea-foam}"
    typography: "{typography.button}"
---

# LANsurfer Design System

## Overview

This file is the authoritative reference for LANsurfer's design system: brand foundation, semantic colors, typography, iconography, themes, accessibility rules, component semantics, logo, application icons, and mascot. The token values in the frontmatter are normative; the prose explains how to apply them.

[`docs/ui-kit`](docs/ui-kit/README.md) is the reference for specific screen compositions and interactions: screen hierarchy, layout, component dimensions, states, and interaction decisions for the surfaces it represents. New UI work must follow this file and adopt the UI kit rather than treating an earlier prototype as the visual reference.

### Authority

- The checked-in `DESIGN.md` wins over any external design project, export, or snapshot, including the Claude Design project [LANsurfer UI Kit](https://claude.ai/design/p/c259a8f4-c239-427e-8e18-32443502b873) from which the palette and typography were derived. An external change becomes part of the design system only when a pull request updates this file.
- If this file and the UI kit appear to conflict, preserve this file's semantic tokens, accessibility rules, and equal dark/light support while following the UI kit's represented composition and interaction model. Resolve a genuine conflict by updating both together.
- A design source never redefines product behavior, domain terminology, technology, or module ownership. [PRODUCT.md](PRODUCT.md), [TERMINOLOGY.md](docs/TERMINOLOGY.md), [ARCHITECTURE.md](docs/ARCHITECTURE.md), and [AGENTS.md](AGENTS.md) own those concerns. A frame, token, or component in a design source is not evidence that a behavior belongs to the product.

### Brand foundation

- Product name: **LANsurfer**
- Tagline: **Find your crew. Catch the next game.**
- Character: social, energetic, welcoming, and immediately understandable at a busy LAN event
- Visual direction: an ocean and surf identity applied to color, imagery, and marketing personality
- Product language: familiar gaming terms; the surf identity never renames product concepts (see [TERMINOLOGY.md](docs/TERMINOLOGY.md#brand-voice-boundary))

Always write the product name as `LANsurfer`, including the lowercase `surfer`. The tagline may be set in uppercase in a visual lockup, but its canonical wording and punctuation do not change.

### Themes

Dark mode is the primary event-floor presentation, and light mode is an equal supported presentation for daylight, office, projector, and accessibility contexts. Neither mode may lose information or actions.

- The primary interactive accent is Surf Blue in the dark theme and Deep Blue in the light theme (see [Colors](#colors)).
- Semantic meanings for Golden Hour, Sunset, and Coral remain identical across themes.
- Theme choice is a presentation preference, not configurable instance branding ([PRODUCT.md](PRODUCT.md#13-instance-identity-branding-and-pwa)).

## Colors

The palette is part of the fixed product brand. Templates and styles consume centralized semantic tokens instead of repeating hexadecimal values.

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

Use the ocean blues for the majority of the interface. Sunset, Coral, and Golden Hour are semantic accents, not decorative alternatives to the primary color.

### Derived inks

Muted inks are blends of White Sand and Deep Ocean for screen use, not palette colours of their own. In print, tint White Sand or Deep Ocean instead.

| Ink | Value | CMYK (uncoated) | Intended role |
| --- | --- | --- | --- |
| Ink 62% | `#8DA9BF` | 26 · 12 · 0 · 25 | Meta text, labels, inactive navigation |
| Ink 84% | `#C7DCEA` | 15 · 6 · 0 · 8 | Body copy inside cards |

The hexadecimal values are the dark-theme reference, not tokens. Implementations produce muted inks through the theme's own text colour (for example `color-mix` against `--color-base-content`) so both themes stay correct, rather than hardcoding the hexadecimal values.

Deep Ocean is a blue dark, so it loses contrast faster than a neutral grey as it is diluted. On light surfaces, muted ink stops at 65% of the text colour, and ordinary meta text uses 70%; below that it fails 4.5:1 against Sea Mist. Use 80% for quiet headings.

### Wave gradient

`linear-gradient(135deg, #0D1B2A 0%, #0077A8 40%, #48CAE4 70%, #FF6B35 100%)` is reserved for key visuals such as the login stage. It is not a surface treatment for ordinary cards, rows, or buttons.

### One job per colour

The palette is large because print, merchandise, and key visuals need it. On screen every colour carries exactly one job, so a colour means the same thing wherever it appears.

| Colour | Screen job |
| --- | --- |
| Deep Ocean | Page background (dark), text on bright fills |
| Ocean Depth | Cards, rows, navigation |
| Harbour | Raised quiet surfaces: supporting buttons, borders, member avatars, skeletons |
| Deep Blue | Primary action, selection, and active navigation in the light theme; key-visual gradients |
| Surf Blue | Primary action, selection, and active navigation in the dark theme |
| Aqua Wave | Counters, links, joined state |
| Sea Foam | Labels on dark surfaces, ink on quiet buttons, body of the light quiet button |
| Sea Mist | Page background and recessed fields (light) |
| Tide Line | Borders, rules, and member avatars (light) |
| White Sand | Body text on dark, raised surfaces (light) |
| Golden Hour | Time and stewardship: start times, trending, ORGA, host actions |
| Sunset | Scarcity and active-now state: the last free place, "now", playing |
| Coral | Leaving, cancelling, deleting, errors |

A colour that would appear outside its job is the wrong colour. Decoration is not a job.

Sunset is not a `--color-*` entry in the DaisyUI theme; consume it through the central brand definition and always pair it with a readable state label.

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
- 700 Bold: active navigation and emphasized labels
- 600 Semibold: buttons, row titles, and strong emphasis
- 500 Medium: light emphasis
- 400 Regular: every piece of running text and inactive navigation

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
| Row title | Schibsted Grotesk | 16 px | 600 | normal |
| Body | Schibsted Grotesk | 15 px | 400 | normal |
| Body small | Schibsted Grotesk | 14 px | 400 | normal |
| Button | Schibsted Grotesk | 15 px | 600 | normal |
| Navigation item | Schibsted Grotesk | 15 px | 400, active 700 | normal |
| Counter | JetBrains Mono | 16 px | 700 | normal |
| Meta and badge | JetBrains Mono | 13 px | 500–700 | 0.12em caps |

Further rules:

- 13 px mono is the smallest type in the product. Below that, drop the element instead of shrinking it.
- Line height 1.15 for headlines, 1.45–1.55 for copy.
- Letter-spacing appears only on headlines and caps labels.
- Caps labels are always mono, 13 px, 0.12em tracking, in Sea Foam or Golden Hour.

### Font delivery

The variable webfonts are bundled with the application: Schibsted Grotesk upright and italic, plus JetBrains Mono. Provide system sans-serif and monospace fallbacks so content remains usable while fonts load or if they fail. Ship each font's license beside the asset. The no-CDN delivery rule is a technical boundary owned by [ARCHITECTURE.md](docs/ARCHITECTURE.md#16-branding-and-installable-web-shell-architecture).

## Layout

Screen composition, dimensions, and interaction states come from the [UI kit](docs/ui-kit/README.md). Its standalone `index.html` is an earlier export: it still shows the previous Montserrat typography and lacks Harbour and Deep Blue, so read colour and typography from this file and use the snapshot only for composition, states, and interaction.

- Follow the UI kit's choice of dense rows, detail surfaces, forms, and overlays for each task. Use restrained borders, clear grouping, and high information density that remains readable at a glance.
- The kit is dark-first and shows a 1440 px desktop composition. Implementations also provide the mobile behavior indicated by the kit and the equally supported light presentation.
- A missing frame is not permission to omit an interaction state, responsive behavior, or accessibility requirement.
- Extend the same tokens and component vocabulary to supported screens not yet represented by a frame instead of inventing a parallel visual system.
- Ocean styling must not obscure the primary action or instance identity.

## Elevation & Depth

The two themes build depth in opposite directions.

- Dark mode lifts a surface by lightening it: Deep Ocean page, Ocean Depth cards and navigation, Harbour raised state. Dark surfaces separate by value alone and carry no shadow.
- Light mode lowers the canvas instead: Sea Mist is the page, White Sand is the raised surface for cards, rows, and navigation, and a Tide Line border sits on every raised surface. Light surfaces also carry a single hairline shadow. A light screen that is White Sand throughout has no layers left to read.
- Inputs and other recessed elements invert that relationship: Sea Mist inside a White Sand panel in light mode, Deep Ocean inside an Ocean Depth card in dark mode.

## Shapes

Corner radii are deliberately tighter than the UI-kit snapshot: selectors `0.25rem`, fields `0.375rem`, boxes `0.5rem`. Borders are 1 px.

## Components

### Action hierarchy

Buttons carry the same six levels everywhere. The level, not the page, decides the colour; one filled primary action per view keeps "this moves you forward" readable at a glance.

| Level | Class | Treatment | Examples |
| --- | --- | --- | --- |
| Commit | `btn-primary` | Surf Blue fill, Deep Ocean text (light theme: Deep Blue fill, White Sand text) | Join, Create Game Request, Follow Game, Find Players (game card) |
| Urgent commit | `btn-urgent` | Sunset fill, Deep Ocean text | Join on the last free place |
| Supporting | `btn-quiet` | Harbour surface, Sea Foam text (light theme: Sea Foam and Tide Line surface, dark Deep Blue text) | View, Search, Save, Copy link, Follow (icon-only, game card) |
| Step back | `btn-step-back` | Supporting at rest, Coral surface and text on hover and focus | Leave, Unfollow, Close for new players, Deactivate game, Disable, Remove device |
| Destructive | `btn-danger` | Coral-tinted surface and Coral text at rest | Cancel Game Request, Delete permanently, Disconnect |
| Tertiary | `btn-ghost` | Text only | Clear, Log out, Dismiss |

Rules that keep the hierarchy honest:

- The icon always sits left of the label, including forward and external-link actions ("View", "Open on IGDB"). An icon-only button is square (`btn-square`) with the icon centred.
- Every button carries a Phosphor icon, with or without a visible label. A text-only button is an exception that a human reviewer approves explicitly in the pull request; the icon-only case still needs an accessible name (see [Icons](#icons)).
- Every action with a negative effect — delete, remove, disable, deactivate, disconnect, leave, unfollow, cancel — shows Coral on hover and focus. Reversible ones use `btn-step-back`, irreversible ones `btn-danger`; never `btn-ghost` or `btn-quiet`, whose hover stays neutral.
- Reversible actions never wear Coral at rest. Leaving a Game Request or unfollowing a game is repeatable, so the colour only appears once the pointer or keyboard is on the control. Permanent alarm colour on a repeatable action trains players to ignore it.
- Irreversible destructive actions wear Coral before the click and ask for confirmation. A non-destructive irreversible lifecycle commit may use its normal commit/host styling but still requires explicit confirmation when the product flow calls for it (for example **Start game**).
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
- State chips (`OPEN`, `PLAYING`, `JOINED`, `HOST`, `LAST PLACE`, `FULL`) are mono, uppercase, and tinted with the colour of the state they name. `PLAYING` uses Sunset and always remains readable as text rather than relying on colour alone.
- Errors use Coral with a clear message. Informational feedback uses Aqua Wave or Surf Blue. Feedback classes follow [Action feedback](#action-feedback).
- Badges and metadata stay visually quieter than titles and primary actions.

The same component hierarchy is used for in-browser live updates and for the Web Push and Discord settings surfaces.

## Do's and Don'ts

- **Do** consume semantic tokens from the central brand definition. **Don't** repeat hexadecimal values in templates or styles.
- **Do** keep one filled primary action per view. **Don't** use Sunset, Coral, or Golden Hour as decoration.
- **Do** pair every state colour with a label, icon, or other visible treatment. **Don't** communicate state through colour or an icon alone.
- **Do** keep both themes complete. **Don't** ship a state, action, or piece of information in only one theme.
- **Do** reuse the established components and tokens for new screens. **Don't** build a parallel component or visual system.
- **Do** keep surf personality in imagery and marketing copy. **Don't** rename product concepts with surf vocabulary; [TERMINOLOGY.md](docs/TERMINOLOGY.md#brand-voice-boundary) owns that rule.
- **Don't** let a design frame, token, or component introduce or imply product behavior that [PRODUCT.md](PRODUCT.md) does not define.

## Icons

Phosphor Icons is the approved icon library for LANsurfer. It is an icon font, not a UI component framework; the technology boundaries in [ARCHITECTURE.md](docs/ARCHITECTURE.md#18-frontend-architecture) are unchanged by it.

- **Weights.** Regular is the default weight for every icon. Fill is reserved for emphasized state, such as followed or selected. Do not mix weights decoratively within one component, and do not use other weights (Bold, Thin, Light, Duotone).
- **Delivery.** Icons ship as a bundled webfont plus its stylesheet, exactly like the Schibsted Grotesk and JetBrains Mono webfonts, with no per-icon JavaScript. Ship Phosphor's MIT license beside the asset, as with the fonts.
- **Color.** Icons inherit `currentColor` and never introduce a colour of their own. The [one-job-per-colour](#one-job-per-colour) rules govern them like text: a Coral icon means destruction, Golden Hour means time and stewardship.
- **Size and alignment.** Size icons in `em` relative to the adjacent text so they scale with the UI scale table, and keep the optical box aligned with the line height.
- **Accessibility.** Decorative icons are `aria-hidden="true"` and never carry meaning alone. An icon-only control always provides an accessible name. Icons follow the same rule as colour: never the sole carrier of state.

## Logo and lockup

Set the wordmark in Schibsted Grotesk italic: 900 for `LAN`, 600 for `surfer`, tracking −0.03em. This is the only italic in the system. Use it with the canonical tagline for the LANsurfer lockup. Current artwork: [logo-with-slogan.png](docs/logo/logo-with-slogan.png). The standalone [icon.png](docs/logo/icon.png) is the brand icon. These are product identity assets, not configurable instance branding.

- Use the `LANsurfer` wordmark as text.
- Do not promote a prototype or UI-kit placeholder to the production logo or favicon.
- Do not use the mascot as a substitute favicon or signet.
- Keep at least the height of the wordmark's capital `L` as clear space around a lockup.
- Do not redraw, crop, recolor, or substitute the supplied logo and icon without updating this file and the bundled product assets together.
- Keep the fixed **Powered by Convention-X-Treme** operator attribution visible. It is not an instance-identity field ([PRODUCT.md](PRODUCT.md#13-instance-identity-branding-and-pwa)).

### Application icons

The favicons and install icons in [`docs/pwa-icons`](docs/pwa-icons/) are renditions of [icon.png](docs/logo/icon.png), cropped to the artwork's bounds (its transparent margin removed) before scaling; `icon.png` itself stays unmodified:

- `icon-master-1024.png`: 1024 px native-resolution square crop centred on the artwork, transparent; the source for the `any` icons.
- `icon-192.png`, `icon-512.png` (manifest `any`) and `favicon-16/32/48.png`: transparent. Favicons use the tight artwork square so the wave fills the tab; `favicon.ico` holds the 16 and 32 px frames.
- `apple-touch-icon-180.png` and `icon-512-maskable.png`: opaque on White Sand, the artwork's authored ground. The maskable icon scales the artwork's enclosing circle, splash included, into the central 80% safe zone.

Renditions are checked in and copied into the embedded assets by `npm run prepare:assets`; the build needs no image tooling.

## Mascot

The LANsurfer otter is a guide, not a decorative pattern. The [initial-mascot.png](docs/mascot/initial-mascot.png) is the original source artwork. Additional poses include [mascot-welcome-pose.png](docs/mascot/mascot-welcome-pose.png) and [mascot-with-surfbord.png](docs/mascot/mascot-with-surfbord.png); [mascot-sprite-sheet.png](docs/mascot/mascot-sprite-sheet.png) contains the mascot sprite sheet.

The app serves pre-sized WebP renditions from [docs/mascot/web/](docs/mascot/web/), not the masters. After replacing a master, regenerate its rendition as described in `scripts/prepare-assets.mjs`.

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
