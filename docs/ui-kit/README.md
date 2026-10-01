# LANsurfer UI Kit

Target design for the landing page, login and Game Requests. Dark first, desktop 1440, English as the UI language.

The leading source for colour and typography is the Claude Design project [LANsurfer UI Kit](https://claude.ai/design/p/c259a8f4-c239-427e-8e18-32443502b873); `index.html` is an older snapshot and still shows Montserrat type and the pre-Harbour palette. Follow [BRANDING.md](../BRANDING.md) for tokens.

`index.html` is a standalone file (fonts, images, and scripts embedded) and runs without a build step. Open it in the browser, then pan and zoom with the trackpad or scroll.

## Frames

| ID | Screen | Route |
| --- | --- | --- |
| 0 | Landing page (logged out, proposal 1b) | `GET /` |
| 1a | Login | `GET /auth/login` |
| 1b | Game Requests (signed-in home) | `GET /game-requests` |
| 1c | List states: Arrival, Skeleton, Empty, Alerts | `GET /game-requests/refresh`, `GET /events/game-requests` |
| 1d | Composer and mobile drawer | `GET /game-requests/new`, `POST /game-requests` |
| 1f | Game Request Detail | `GET /game-requests/{requestID}` |
| 1e | Tokens, Buttons, Badges, Dimensions | — |
| 1g | Navigation (sidebar, all signed-in screens) | — |

## Decisions Defined by the Kit

- "Dashboard" has been removed as a term: the signed-in home is called Game Requests; the logged-out `GET /` is the landing page (`UI-Kit-Home` in the Claude Design project). Game tiles carry an "N open" badge top-right and a player-count badge top-left; the dashed last tile is "Any game".
- Dense rows: abbreviation, place counter, relative start time, Join. Details live on their own detail page.
- Join changes the row to "Leave"; no toast. Toasts are only for errors and things outside the current viewport.
- Vocabulary: **places** for available spots, **max players** for the field. "Seat" is not used.
- Status: `open` / `playing` / `closed` / `cancelled`; no waitlist, no mode, no voice field. `playing` is an active/current request state entered by the Host's explicit **Start game** action.
- Seat and voice-channel concepts are excluded because they do not exist in the approved 1.0 model; visual references must not add them implicitly.
- Sunset (`#FF6B35`) is the semantic color for "last place", "now", and `playing`, but is not a `--color-*` in the DaisyUI theme: consume it through the central brand definition and always pair it with a readable state label.
- Muted inks (`#8DA9BF`, `#C7DCEA`) are blends of White Sand and Deep Ocean, not standalone palette colors.

## Deviations from the Kit Snapshot

- Corner radii are tighter than the kit: `--radius-selector` `0.25rem`, `--radius-field` `0.375rem`, `--radius-box` `0.5rem`.
- Panel headings are sentence-case text in the UI font; the tracked uppercase mono eyebrow is gone.
- List search, the "My Games" toggle, and the "Free places" toggle are rendered now that Track A backs them with server-side discovery filters. Every visible control applies live, search is debounced, and updates replace the URL instead of pushing history; the submit button renders only without scripting. "Load more" is still not rendered; it returns with server-side paging.
- The rail ranks games as **Trending** over the rolling window Track A's read model states (`Trending · last 6 hours`), not as an unbounded "most requested" tally.
- Player rows in the detail view are separated by rules inside the Players panel instead of being filled cards inside a card.
- The detail view's Game panel does not repeat the kit's catalog total, which the navigation already carries. It states the Game's own type, platforms, and release year instead, so the panel disambiguates two Games of the same name. Its link keeps the implemented wording, "More rounds of this game".
- The navigation frame labels the Games sub-item "Followed". The implementation renders it as **My Games**, the term PRODUCT fixes for explicitly followed Games; only the frame's indentation and row metrics are adopted.
- The navigation frame's "Games" counter is the active local catalog, not the synchronized IGDB provider index, which is a search surface rather than a catalog this installation can total.
- "Your next games" and "Recently viewed" are implemented as uppercase navigation headings in Deep Ocean for light mode and Sea Foam for dark mode; they remain in the UI font rather than the frame's mono eyebrow.
- The older snapshot does not contain the `playing` lifecycle. Production UI extends the same row/detail/component vocabulary with a visible `PLAYING` state, a Host-only **Start game** action on `open`, explicit confirmation before starting, and **Close game** once `playing`. `playing` remains visible in active lists/navigation.
- `playing` keeps the normal participation states of `open`: a non-host viewer sees **Join** while a place is free, **Leave** once joined, and **Full** at the finite maximum, in the list row and on the detail page alike. The Host never sees Join or Leave. `closed` and `cancelled` rows stay static.
- The Host's **Capacity** card exists only on the detail page, only for the Host, and only while the request is `open` or `playing`. It is a compact form with a number input (`min` 0, `max` 999) and a plain **Save** button, kept apart from the destructive lifecycle actions and with no confirmation, because the change is reversible. The input shows the current maximum, or `0` for unlimited; `0` or a blank field means unlimited and a finite value is `2`–`999`. Its hint reads "0 means unlimited; finite limits start at 2." and a rejected value shows the error beside the field with the submitted text kept. The discovery list never offers an editor.

## Open

- According to BRANDING.md, light mode is equally supported, but it is not yet represented in the kit. The implemented light presentation is the Sea Mist canvas with White Sand surfaces and Tide Line borders described there; a future kit frame should follow it rather than the kit's original near-white sketch.
