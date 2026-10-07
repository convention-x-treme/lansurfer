# Game Request Lobby Design

## Context

Issue #123 restructures the Game Request detail experience in several steps. This design covers only the first architectural step: replace the current request-local chat presentation with a participant-only **Lobby** that combines player chat and existing Game Request Activity for presentation.

The visual redesign of the full detail page is deliberately deferred. This step keeps the current page/card structure as much as practical and focuses on behavior, authorization, read-model boundaries, and realtime refresh behavior.

The term **Lobby** is the product and technical name for this read projection. We do not introduce or retain a separate "Game Request Timeline" concept.

## Goals

- Present player-authored chat and Game Request Activity in one chronological Lobby stream.
- Keep chat and Activity as separate persistent concepts and sources of truth.
- Make Lobby history readable by current participants after a request reaches `closed` or `cancelled`.
- Keep chat posting limited to current participants while the request is `open` or `playing`.
- Remove Activity from the generic Game Request detail projection.
- Remove the separate Activity card as part of this step.
- Keep the existing content-free SSE model and refresh one authorized Lobby fragment.
- Preserve the current Activity wording when Activity entries are rendered as Lobby system messages.
- Keep the Lobby card present for non-participants, but never expose Lobby content to them.

## Non-goals

This step does not:

- redesign the full Game Request detail page;
- implement the later Players 1/3 + Lobby 2/3 layout;
- move Host Actions into the hero;
- remove the Game card;
- add edit modals;
- redesign Session Information;
- add pagination;
- add a Lobby table, second Activity source, event store, global sequence, or new Lobby event;
- add WebSockets, SPA state, or a UI-only JSON API;
- create chat notifications.

Those changes remain separate follow-up work. The final visual polish happens after the functional restructuring steps.

## Terminology

**Lobby** is the participant-only presentation/read projection for one Game Request. It combines:

- player-authored rows from `game_request_messages`; and
- durable lifecycle/history rows from `game_request_activity`.

A read-model entry may be named `LobbyEntry` and the projection may be named `GameRequestLobby`.

**Chat** remains the player-authored write/persistence concept. Routes and code that mutate chat may keep "chat" terminology.

**Game Request Activity** remains the durable lifecycle/history and notification-handoff concept. It does not become chat and is not replaced by Lobby persistence.

Canonical documentation must be updated during implementation so `docs/PRODUCT.md`, `docs/ARCHITECTURE.md`, `docs/TERMINOLOGY.md`, and the relevant `docs/ui-kit/README.md` material consistently use Lobby for this presentation concept.

## Architecture

The recommended design is a dedicated Lobby read projection backed by one combined query.

```text
Game Request domain
├── Lobby read authorization
│   └── CanReadLobby(requestID, userID)
└── Chat write
    └── PostMessage / CanPostChat-equivalent rules

Read/query layer
└── GameRequestLobby
    ├── game_request_messages
    └── game_request_activity
             ↓
       newest 200 combined
             ↓
       returned oldest → newest

HTTP
├── GET  /game-requests/{id}/lobby
└── POST /game-requests/{id}/chat
```

The domain/service owns authorization rules. The query/read-model layer composes presentation data only.

The generic Game Request `Request` projection must no longer contain Activity. `ByID()` must not load participant-only Activity and rely on template hiding. Activity used for Lobby presentation is loaded only through the participant-authorized Lobby path.

No database migration is required for a Lobby-specific persistence structure.

## Authorization and lifecycle

Lobby read access and chat write access are separate concepts.

### Lobby read

`CanReadLobby(requestID, userID)` allows current participants, including the Host, to read the Lobby in all Game Request states:

| Status | Current participant may read Lobby |
| --- | --- |
| `open` | yes |
| `playing` | yes |
| `closed` | yes |
| `cancelled` | yes |

Membership is the authorization boundary. If a participant leaves, they lose Lobby access immediately. A later transition to `closed` or `cancelled` does not restore historical access to someone who is no longer a member.

For a direct unauthorized `GET /game-requests/{id}/lobby`, return **403 Forbidden**. The Game Request detail itself is already visible, so existence does not need to be hidden with 404.

### Chat write

Posting remains allowed only for current participants while the request is `open` or `playing`.

| Status | Current participant may post chat |
| --- | --- |
| `open` | yes |
| `playing` | yes |
| `closed` | no |
| `cancelled` | no |

Existing message validation, rate limiting, request-row locking, and mutation semantics remain intact.

## Read model and ordering

The Lobby read projection combines the two existing sources with `UNION ALL`.

The limit applies **after** the sources are combined:

1. combine Activity and chat rows for one Game Request;
2. sort newest-first;
3. select the newest **200 Lobby entries total**;
4. return/render those selected rows oldest-first.

It must not load "200 chat plus all Activity" or independently limit each source.

For equal timestamps, presentation order is deterministic:

`timestamp -> source discriminator -> stable source-local key`

Use a fixed discriminator order with **Activity before Chat**. Activity may use its existing `sequence` as its source-local stable key. Chat uses its stable message ID.

This tie-break exists only for deterministic presentation. It must not be described or modeled as a global causal sequence. `game_request_activity.sequence` remains Activity-local ordering.

The current Activity query does not need to remain as a generic detail query. The Lobby projection may select `sequence` because it needs the stable Activity-local key.

## Presentation

The existing chat card becomes a **Lobby** card. The separate Activity card is removed in the same change.

There is no Lobby count in the heading.

For authorized participants, the Lobby renders one chronological stream:

- Activity entries render as visually distinct system messages.
- Chat entries render with their existing author/time/body presentation.
- Activity message wording is reused from the current Activity implementation without copy changes.
- The empty state is a Lobby empty state, not an Activity or chat-specific count state.

For participants in `closed` or `cancelled`, the Lobby remains readable but the chat composer is not available.

For non-participants, the Lobby card stays in the page layout but contains no Lobby data:

- `open` / `playing`: **"Join this Game Request to access the Lobby."**
- `closed` / `cancelled`: **"The Lobby is available to participants only."**

The locked presentation is not an authorization mechanism. The server must not return Lobby contents to a non-participant.

This step intentionally does not perform the later visual redesign or final layout polish.

## HTTP behavior

Replace the current chat read route with:

```text
GET /game-requests/{id}/lobby
```

Remove:

```text
GET /game-requests/{id}/chat
```

There is no redirect or compatibility route for the old GET endpoint.

Keep:

```text
POST /game-requests/{id}/chat
```

The POST remains a chat mutation. A non-HTMX successful POST redirects back to the Game Request detail page as today.

For HTMX, a successful chat POST may return the freshly authorized Lobby fragment so the mutation's visible result is immediate, while the route itself remains a chat mutation rather than a read endpoint.

Use one stable DOM target for the Lobby, for example `#game-request-lobby`, so successful chat mutations and realtime refreshes target the same fragment.

## Realtime

Do not create a Lobby/Timeline durable event.

Keep the existing content-free realtime architecture:

- `GameRequestChatMessage` remains a content-free signal.
- Chat signal delivery authorization changes from the current active-chat read rule to `CanReadLobby`.
- A received chat signal refreshes `GET /game-requests/{id}/lobby`.
- Existing Game Request lifecycle/update signals also refresh the same Lobby fragment when the current page has an authorized Lobby.
- Existing detail refresh behavior for other parts of the page remains separate.
- SSE carries identifiers/signals only; Lobby content is always fetched through the independently authorized GET.

After the viewer leaves, the detail refresh must transition the Lobby card to its locked state, and the browser must not continue fetching private Lobby content for that viewer.

## Error handling

Keep current chat mutation behavior for validation, rate limits, and action errors.

Infrastructure failures remain 5xx and are logged.

For Lobby reads:

- not found remains the existing not-found behavior;
- authenticated non-participant access returns 403;
- infrastructure/query failures return 5xx and are logged.

Do not convert backend failures into successful empty Lobby responses.

## Testing

Tests should cover at least:

- current participant can read Lobby in `open`, `playing`, `closed`, and `cancelled`;
- non-participant cannot read Lobby in any status;
- leaving removes Lobby read access;
- later closing/cancelling does not restore access to a former participant;
- chat posting remains limited to current participants in `open` and `playing`;
- chat posting is rejected in `closed` and `cancelled`;
- combined query returns at most 200 entries total, not 200 per source;
- selected entries render oldest-first;
- equal timestamps use the deterministic source discriminator and source-local key;
- Activity wording in Lobby matches the current Activity wording;
- generic `ByID()` no longer contains or loads Activity;
- the separate Activity card is gone;
- `GET /chat` is removed;
- `GET /lobby` enforces authorization and returns 403 to non-participants;
- locked Lobby copy differs correctly between active and terminal request states;
- HTMX chat mutation updates the Lobby fragment;
- chat signals and lifecycle/update signals refresh the same Lobby fragment;
- realtime chat-signal authorization uses Lobby read permission, including terminal requests;
- no Lobby persistence/event or chat-notification behavior is introduced.

## Documentation updates during implementation

Implementation must update the canonical project documentation to record the approved model:

- `docs/PRODUCT.md`: Lobby audience, lifecycle read/write behavior, and locked non-participant presentation.
- `docs/ARCHITECTURE.md`: CQRS-light Lobby projection, separate sources of truth, authorization boundary, combined 200-entry query, and content-free realtime refresh.
- `docs/TERMINOLOGY.md`: define **Lobby** and keep **Game Request Activity** and **Chat** as distinct underlying concepts.
- `docs/ui-kit/README.md`: update relevant Game Request detail/Lobby component guidance without doing the later visual redesign.

The obsolete "Game Request Timeline" wording must not be introduced.

## Acceptance summary

This design is complete when the implementation can satisfy all of the following:

1. A current participant sees one Lobby containing chat and Activity system messages.
2. A non-participant sees only the appropriate locked Lobby card.
3. Closed/cancelled participants can read history but cannot post.
4. Former participants cannot read the Lobby.
5. The Lobby contains at most the newest 200 combined entries in deterministic chronological presentation order.
6. Activity no longer lives in the generic detail projection or a separate Activity card.
7. `GET /lobby` is the only Lobby read endpoint and is independently authorized.
8. Realtime signals refresh the Lobby without carrying Lobby content or introducing a new Lobby event.
9. Existing Activity wording and existing chat persistence/write behavior are preserved.
10. No unrelated Game Request detail-page visual redesign is included in this step.
