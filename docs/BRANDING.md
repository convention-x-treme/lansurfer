# LANsurfer Branding

This document and the [`docs/ui-kit`](ui-kit/README.md) references together define LANsurfer's design system through 1.0. This document owns the fixed brand foundation, semantic colors, typography, theme behavior, accessibility rules, asset policy, voice, and instance-identity boundaries. The UI kit owns the target screen hierarchy, layout, component dimensions, states, and interactions for the surfaces it represents.

Both sources are required design guidance. New UI work must follow this document and adopt the UI kit rather than treating the completed prototype as the visual reference. If the two design sources appear to conflict, preserve this document's semantic tokens, accessibility rules, and equal dark/light support while following the UI kit's represented composition and interaction model; resolve a genuine conflict by updating both sources together.

Neither design source may redefine product behavior, domain terminology, approved scope, technology, or module ownership. [PRODUCT.md](PRODUCT.md), [ROADMAP.md](ROADMAP.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [AGENTS.md](../AGENTS.md) remain authoritative for those concerns.

## UI kit relationship

The UI kit consists of:

- [`ui-kit/README.md`](ui-kit/README.md), which records the represented routes and explicit interaction and terminology decisions;
- [`ui-kit/index.html`](ui-kit/index.html), which provides the visual frames for login, Game Request list and states, creation, mobile behavior, details, and component tokens.

The standalone HTML is a design reference, not production application code or a runtime dependency. Translate it into the approved templ/HTMX, Tailwind, and DaisyUI frontend architecture, with TypeScript limited to progressive enhancement. Extend the same tokens and component vocabulary to supported screens not yet represented by a frame instead of inventing a parallel visual system.

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

| Token | Value | Intended role |
| --- | --- | --- |
| Deep Ocean | `#0D1B2A` | Dark-mode page background and dark foreground text |
| Ocean Depth | `#1B263B` | Dark-mode raised surfaces, cards, and navigation |
| Surf Blue | `#00B4D8` | Primary actions, active navigation, and selection |
| Aqua Wave | `#48CAE4` | Secondary emphasis and informational accents |
| Sea Foam | `#90E0EF` | Subtle highlights and light accent surfaces |
| White Sand | `#F8F9FA` | Light-mode page background and text on dark surfaces |
| Golden Hour | `#F4D35E` | Scheduled times and time-related emphasis |
| Sunset | `#FF6B35` | Urgency, happening now, and the last available player place |
| Coral | `#FF3F73` | Destructive actions, cancellation, and errors |

Use the ocean blues for the majority of the interface. Sunset, Coral, and Golden Hour are semantic accents, not decorative alternatives to the primary color.

### Accessible color use

- Use Deep Ocean text on Surf Blue, Aqua Wave, Sea Foam, Golden Hour, Sunset, and Coral fills. White Sand text does not have sufficient contrast on those colors for normal-sized text.
- Use White Sand text on Deep Ocean and Ocean Depth.
- Never communicate state through color alone. Pair it with a label, icon, or other visible treatment.
- Preserve visible keyboard focus, hover, active, disabled, error, and success states in both themes.
- Validate final combinations and interactive states against WCAG contrast requirements during implementation; opacity and blended backgrounds can change the effective contrast.

## Typography

### Primary typeface

Use **Montserrat** throughout the interface:

- 800 ExtraBold: marketing headlines and the wordmark
- 700 Bold: screen, row, and detail titles
- 600 SemiBold: buttons and active navigation
- 500 Medium: body copy and labels

### Data typeface

Use **JetBrains Mono** for compact data where aligned character shapes improve scanning, such as player and place counts, times, tags, and configuration snippets. Do not use it for paragraphs or ordinary navigation.

### UI scale

| Use | Size | Weight |
| --- | ---: | ---: |
| Screen title | 32 px | 700 |
| Game name | 24 px | 700 |
| Body and navigation | 16 px | 500 |
| Button | 15 px | 600 |
| Badge and metadata | 13 px | 500 |

Bundle production webfonts with the application and serve them from embedded assets. Runtime font CDNs are not allowed. Provide system sans-serif and monospace fallbacks so content remains usable while fonts load or if they fail.

## LANsurfer logo and lockup

Use a Montserrat ExtraBold wordmark and the canonical tagline for the LANsurfer lockup. No production signet has been approved. Until one exists:

- use the `LANsurfer` wordmark as text;
- do not promote a prototype or UI-kit placeholder to the production logo or favicon;
- do not use the mascot as a substitute favicon or signet;
- keep at least the height of the wordmark's capital `L` as clear space around a future lockup.

The eventual LANsurfer signet must remain recognizable at 32 px, work with no more than two blues plus optional Golden Hour, and have a one-color version suitable for print, stamps, merchandise, and sponsor walls. Product logo assets should be stored once and referenced through the centralized brand definition.

### Instance identity in 1.0

An administrator may add an instance display name, welcome text, contact link, logo, and application icon. These values identify the hosting community; they do not replace the LANsurfer product identity.

- Keep visible **Powered by LANsurfer** attribution.
- Continue to use this document's palette, typography, component semantics, accessibility rules, and light/dark presentations.
- Accept only a validated PNG logo up to 2 MiB and 2048×1024 pixels.
- Accept only a validated square PNG icon from 192 to 512 pixels and up to 512 KiB.
- Store uploaded identity assets in PostgreSQL and serve them from LANsurfer.
- When an icon or logo is absent, omit missing manifest icons and use the generated instance monogram where the UI needs an identity mark.

Instance identity is not Event-specific. It does not permit custom colors, fonts, CSS, remote branding assets, removal of LANsurfer attribution, or a theme editor.

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

Apply the visual language to the product's established terms:

- Primary buttons such as **Create Game Request** and **Join** use Surf Blue with Deep Ocean text.
- Destructive actions such as **Cancel Game Request** use Coral and require an explicit text label.
- A scheduled start time may use Golden Hour.
- Player and available-place counts use JetBrains Mono; the last available place may use Sunset, and a full request uses a muted treatment.
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

Keep the fixed LANsurfer brand definition near the web composition layer as one small value/object containing the product name, tagline, semantic colors, and built-in asset paths. Tailwind/DaisyUI theme values, CSS custom properties, and templ components should derive from that definition or its generated/static stylesheet rather than introduce parallel constants.

Fixed brand values are not environment configuration or mutable PostgreSQL state. The limited instance identity fields are separate PostgreSQL product data with the validation rules above.

The approved remote IGDB cover host is a catalog-content exception, not a branding exception. It does not permit remote logos, icons, fonts, theme assets, or arbitrary images.
