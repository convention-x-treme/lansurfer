# LANsurfer Branding

This document defines the fixed visual identity for LANsurfer v0.1. It extracts the compatible visual direction from the [LANsurfer Branding & UI/UX concept](<LANsurfer Branding & UI_UX v2 by Claude Design.pdf>) while preserving the product terminology and scope in [PRODUCT.md](PRODUCT.md).

The concept PDF is design input, not a source of product requirements. When the two disagree, `PRODUCT.md`, [ROADMAP.md](ROADMAP.md), and [AGENTS.md](../AGENTS.md) remain authoritative.

## Brand foundation

- Product name: **LANsurfer**
- Tagline: **Find your crew. Catch the next game.**
- Character: social, energetic, welcoming, and immediately understandable at a busy LAN event
- Visual direction: an ocean and surf identity applied to color, imagery, and marketing personality
- Product language: familiar gaming terms such as Game, Game Request, Players, Join, Leave, Follow Game, and My Games

Always write the product name as `LANsurfer`, including the lowercase `surfer`. The tagline may be set in uppercase in a visual lockup, but its canonical wording and punctuation do not change.

## Color system

The palette is fixed for v0.1. Templates and styles must consume centralized semantic tokens instead of repeating hexadecimal values.

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

The concept includes a blue-to-Sunset wave gradient. Reserve it for occasional key visuals or marketing artwork. Do not use it for ordinary buttons, text, cards, or page backgrounds, and do not invent fixed stops until an approved production asset defines them.

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
- 700 Bold: screen and card titles
- 600 SemiBold: buttons and active navigation
- 500 Medium: body copy and labels

### Data typeface

Use **JetBrains Mono** for compact data where aligned character shapes improve scanning, such as player counts, times, seat references, tags, and configuration snippets. Do not use it for paragraphs or ordinary navigation.

### UI scale

| Use | Size | Weight |
| --- | ---: | ---: |
| Screen title | 32 px | 700 |
| Game name | 24 px | 700 |
| Body and navigation | 16 px | 500 |
| Button | 15 px | 600 |
| Badge and metadata | 13 px | 500 |

Bundle production webfonts with the application and serve them from embedded assets. Runtime font CDNs are not allowed. Provide system sans-serif and monospace fallbacks so content remains usable while fonts load or if they fail.

## Logo and lockup

The concept establishes a Montserrat ExtraBold wordmark and tagline lockup, but explicitly identifies its signet as unfinished. Until an approved signet asset exists:

- use the `LANsurfer` wordmark as text;
- do not treat a concept-page placeholder as the production logo or favicon;
- do not use the mascot as a substitute favicon or signet;
- keep at least the height of the wordmark's capital `L` as clear space around a future lockup.

The eventual signet must remain recognizable at 32 px, work with no more than two blues plus optional Golden Hour, and have a one-color version suitable for print, stamps, merchandise, and sponsor walls. Logo assets should be stored once and referenced through the centralized brand definition.

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

Prefer clear cards, restrained borders, generous grouping, and high information density that remains readable at a glance. Ocean styling must not obscure the primary action or current event context.

## Component semantics

Apply the visual language to the product's established terms:

- Primary buttons such as **Create Game Request** and **Join** use Surf Blue with Deep Ocean text.
- Destructive actions such as **Cancel Game Request** use Coral and require an explicit text label.
- A scheduled start time may use Golden Hour.
- A player count uses JetBrains Mono; the last available place may use Sunset, and a full request uses a muted treatment.
- Errors use Coral with a clear message. Informational feedback uses Aqua Wave or Surf Blue.
- Badges and metadata stay visually quieter than titles and primary actions.

The same component hierarchy may later be reused for in-app live-update notices. This does not add Web Push, Discord, or a persistent notification feature to v0.1.

## Voice and terminology boundary

Brand personality may appear in marketing headlines and occasional supporting copy, but users must not learn a surf vocabulary to operate LANsurfer. Do not rename Game Requests to Waves, Players to Crew, available places to Spots, or Join to Catch in navigation, controls, code, or persistence.

The following ideas shown in the source concept are not adopted by this visual guide and remain deferred unless the product documents are explicitly changed:

- Web Push and Discord bot delivery
- a separate Ready-to-Play state
- configurable instance branding or a YAML theme file
- additional social login providers
- ticket and seat claims
- a waitlist
- internationalization as a v0.1 deliverable

## Implementation boundary

Keep the fixed brand definition near the web composition layer as one small value/object containing the name, tagline, semantic colors, and asset paths. Tailwind/DaisyUI theme values, CSS custom properties, and templ components should derive from that definition or its generated/static stylesheet rather than introduce parallel constants.

Brand values are not environment configuration and must not be stored in PostgreSQL for v0.1. Future instance branding remains a separate post-v0.1 concern.
