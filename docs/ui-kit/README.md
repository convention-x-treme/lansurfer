# LANsurfer UI Kit

Target design for login and Game Requests. Dark first, desktop 1440, English as the UI language.

The leading source for colour and typography is the Claude Design project [LANsurfer UI Kit](https://claude.ai/design/p/c259a8f4-c239-427e-8e18-32443502b873); `index.html` is an older snapshot and still shows Montserrat type and the pre-Harbour palette. Follow [BRANDING.md](../BRANDING.md) for tokens.

`index.html` is a standalone file (fonts, images, and scripts embedded) and runs without a build step. Open it in the browser, then pan and zoom with the trackpad or scroll.

## Frames

| ID | Screen | Route |
| --- | --- | --- |
| 1a | Login | `GET /auth/login` |
| 1b | Game Requests (home page) | `GET /game-requests` |
| 1c | List states: Arrival, Skeleton, Empty, Alerts | `GET /game-requests/refresh`, `GET /events/game-requests` |
| 1d | Composer and mobile drawer | `GET /game-requests/new`, `POST /game-requests` |
| 1f | Game Request Detail | `GET /game-requests/{requestID}` |
| 1e | Tokens, Buttons, Badges, Dimensions | — |

## Decisions Defined by the Kit

- "Dashboard" has been removed as a term: the home page is called Game Requests.
- Dense rows: abbreviation, place counter, relative start time, Join. Details live on their own detail page.
- Join changes the row to "Leave"; no toast. Toasts are only for errors and things outside the current viewport.
- Vocabulary: **places** for available spots, **max players** for the field. "Seat" is not used.
- Status: `open` / `closed` / `cancelled`; no waitlist, no mode, no voice field. This matches `internal/gamerequest` and `internal/query/gamerequest`.
- Seat and voice-channel concepts are excluded because they do not exist in the approved 1.0 model; visual references must not add them implicitly.
- Sunset (`#FF6B35`) is the semantic color for "last place" and "now", but is not a `--color-*` in the DaisyUI theme: consume it through the central brand definition.
- Muted inks (`#8DA9BF`, `#C7DCEA`) are blends of White Sand and Deep Ocean, not standalone palette colors.

## Open

- According to BRANDING.md, light mode is equally supported, but it is not yet represented in the kit.
