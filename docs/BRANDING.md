# LANsurfer Branding

This document and the [`docs/ui-kit`](ui-kit/README.md) references together define LANsurfer's design system through 1.0. This document owns the fixed brand foundation, semantic colors, typography, theme behavior, accessibility rules, asset policy, voice, and instance-identity boundaries. The UI kit owns the target screen hierarchy, layout, component dimensions, states, and interactions for the surfaces it represents.

Both sources are required design guidance. New UI work must follow this document and adopt the UI kit rather than treating the completed prototype as the visual reference. If the two design sources appear to conflict, preserve this document's semantic tokens, accessibility rules, and equal dark/light support while following the UI kit's represented composition and interaction model; resolve a genuine conflict by updating both sources together.

Neither design source may redefine product behavior, domain terminology, approved scope, technology, or module ownership. [PRODUCT.md](PRODUCT.md), [ROADMAP.md](ROADMAP.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](../AGENTS.md) remain authoritative for those concerns.

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

The palette is fixed through 1.0. Templates and styles must consume centralized semantic tokens instead of repeating hexadecimal values.

| Token | Value | CMYK (uncoated) | Intended role |
| --- | --- | --- | --- |
| Deep Ocean | `#0D1B2A` | 69 · 36 · 0 · 84 | Dark-mode page background and dark surfaces |
| Ocean Depth | `#1B263B` | 54 · 36 · 0 · 77 | Cards, rows, and navigation |
| Harbour | `#163F57` | 75 · 28 · 0 · 66 | Raised state, borders, skeletons, and avatars |
| Deep Blue | `#0077A8` | 100 · 29 · 0 · 34 | Light-mode primary action and gradients |
| Surf Blue | `#00B4D8` | 100 · 17 · 0 · 15 | Dark-mode primary action and selection |
| Aqua Wave | `#48CAE4` | 68 · 11 · 0 · 11 | Counters, links, and secondary emphasis |
| Sea Foam | `#90E0EF` | 40 · 6 · 0 · 6 | Labels on dark surfaces and quiet accents |
| White Sand | `#F8F9FA` | 1 · 0 · 0 · 2 | Body text on dark surfaces and light-mode background |
| Golden Hour | `#F4D35E` | 0 · 14 · 61 · 4 | Scheduled times, trending, and organizer marks |
| Sunset | `#FF6B35` | 0 · 58 · 79 · 0 | Urgency, happening now, and the last available place |
| Coral | `#FF3F73` | 0 · 75 · 55 · 0 | Cancellation, deletion, and errors |

CMYK values are uncoated-stock conversions for merchandise, print, and signage; re-profile them against the printer's ICC profile before production. Screen work always uses the HEX values.

Surf Blue is the primary action and selection colour in the dark theme, Deep Blue in the light theme, where Surf Blue does not reach sufficient contrast on White Sand. Use one primary action per screen.

### Derived inks

Muted inks are blends of White Sand and Deep Ocean for screen use, not palette colours of their own. In print, tint White Sand or Deep Ocean instead.

| Ink | Value | CMYK (uncoated) | Intended role |
| --- | --- | --- | --- |
| Ink 62% | `#8DA9BF` | 26 · 12 · 0 · 25 | Meta text, labels, inactive navigation |
| Ink 84% | `#C7DCEA` | 15 · 6 · 0 · 8 | Body copy inside cards |

Implementations produce them through the theme's own text colour (for example `color-mix` against `--color-base-content`) so both themes stay correct, rather than hardcoding the hexadecimal values.

### Wave gradient

`linear-gradient(135deg, #0D1B2A 0%, #0077A8 40%, #48CAE4 70%, #FF6B35 100%)` is reserved for key visuals such as the login stage. It is not a surface treatment for ordinary cards, rows, or buttons.

Use the ocean blues for the majority of the interface. Sunset, Coral, and Golden Hour are semantic accents, not decorative alternatives to the primary color.

### Accessible color use

- Use Deep Ocean text on Surf Blue, Aqua Wave, Sea Foam, Golden Hour, Sunset, and Coral fills. White Sand text does not have sufficient contrast on those colors for normal-sized text.
- Use White Sand text on Deep Ocean, Ocean Depth, Harbour, and Deep Blue.
- Do not set Surf Blue text or small Surf Blue elements on White Sand. Use Deep Blue there.
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

## LANsurfer logo and lockup

Set the wordmark in Schibsted Grotesk italic: 900 for `LAN`, 600 for `surfer`, tracking −0.03em. This is the only italic in the system. Use it with the canonical tagline for the LANsurfer lockup. No production signet has been approved. Until one exists:

- use the `LANsurfer` wordmark as text;
- do not promote a prototype or UI-kit placeholder to the production logo or favicon;
- do not use the mascot as a substitute favicon or signet;
- keep at least the height of the wordmark's capital `L` as clear space around a future lockup.

The eventual LANsurfer signet must remain recognizable at 32 px, work with no more than two blues plus optional Golden Hour, and have a one-color version suitable for print, stamps, merchandise, and sponsor walls. Product logo assets should be stored once and referenced through the centralized brand definition.

### Instance identity in 1.0

An administrator may add an instance display name, welcome text, contact link, logo, and application icon. These values identify the hosting community; they do not replace the LANsurfer product identity.

- Keep the fixed **Powered by Convention-X-Treme** operator attribution visible. It is not an instance-identity field and is not admin-editable.
- Continue to use this document's palette, typography, component semantics, accessibility rules, and light/dark presentations.
- Accept only a validated PNG logo up to 2 MiB and 2048×1024 pixels.
- Accept only a validated square PNG icon from 192 to 512 pixels and up to 512 KiB.
- Store uploaded identity assets in PostgreSQL and serve them from LANsurfer.
- When an icon or logo is absent, omit missing manifest icons and use the generated instance monogram where the UI needs an identity mark.

Instance identity is not Event-specific. It does not permit custom colors, fonts, CSS, remote branding assets, removal of the operator attribution, or a theme editor.

## Mascot

The LANsurfer otter is a guide, not a decorative pattern. The current source artwork is [Otter02.png](Otter02.png).

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
- Light pages use White Sand with white or lightly ocean-tinted raised surfaces and restrained blue borders.
- Surf Blue remains the primary interactive accent in both themes.
- Semantic meanings for Golden Hour, Sunset, and Coral remain identical across themes.
- Theme choice is a presentation preference, not configurable instance branding.

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
| Sea Foam | Labels on dark surfaces, ink on quiet buttons |
| White Sand | Body text on dark, page background (light) |
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
| Supporting | `btn-quiet` | Harbour surface, Sea Foam text | Find Players, View, Search, Save, Copy link |
| Step back | `btn-step-back` | Supporting at rest, Coral surface and text on hover and focus | Leave, Unfollow, Close for new players, Deactivate game |
| Destructive | `btn-danger` | Coral-tinted surface and Coral text at rest | Cancel Game Request |
| Tertiary | `btn-ghost` | Text only | Clear, Log out, Dismiss |

Rules that keep the hierarchy honest:

- Reversible actions never wear Coral at rest. Leaving a Game Request or unfollowing a game is repeatable, so the colour only appears once the pointer or keyboard is on the control. Permanent alarm colour on a repeatable action trains players to ignore it.
- Irreversible actions wear Coral before the click and ask for confirmation.
- Outline buttons are not a level. A supporting action gets the Harbour surface instead, so it reads as a real choice rather than a disabled control.
- Disabled controls keep a legible label: they are dimmed, not erased.
- Every level keeps the palette's contrast rules: button text meets 4.5:1 against its own fill in both themes.

### Other components

- A scheduled start time may use Golden Hour.
- Player and available-place counts use JetBrains Mono; the last available place uses Sunset, and a full request uses a muted treatment.
- State chips (`OPEN`, `JOINED`, `HOST`, `LAST PLACE`, `FULL`) are mono, uppercase, and tinted with the colour of the state they name.
- Errors use Coral with a clear message. Informational feedback uses Aqua Wave or Surf Blue.
- Badges and metadata stay visually quieter than titles and primary actions.

The same component hierarchy is used for in-browser live updates and may support the planned 1.0 Web Push and Discord settings. Durable delivery does not add a persistent in-app notification inbox.

## Voice and terminology boundary

Brand personality may appear in marketing headlines and occasional supporting copy, but users must not learn a surf vocabulary to operate LANsurfer. Do not rename Game Requests to Waves, Players to Crew, available places to Spots, or Join to Catch in navigation, controls, code, or persistence.

The design system does not introduce or imply:

- a separate Ready-to-Play state
- a YAML theme file or full theme customization
- Discord bot/gateway interactions
- ticket claims or venue maps
- a waitlist
- internationalization as a 1.0 deliverable

## Implementation boundary

Keep the fixed LANsurfer brand definition near the web composition layer as one small value/object containing the product name, tagline, semantic colors, the operating community's name and public URL used by the **Powered by …** attribution, and built-in asset paths. Tailwind/DaisyUI theme values, CSS custom properties, and templ components should derive from that definition or its generated/static stylesheet rather than introduce parallel constants.

Every **Powered by …** credit in the interface renders from the operator name and URL in that definition. Changing the credited community is a single-value change there, not a template edit.

Fixed brand values are not environment configuration or mutable PostgreSQL state. The limited instance identity fields are separate PostgreSQL product data with the validation rules above.

The approved remote IGDB cover host is a catalog-content exception, not a branding exception. It does not permit remote logos, icons, fonts, theme assets, or arbitrary images.
