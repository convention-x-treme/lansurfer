# Game Request session information design

Date: 2026-10-03  
Issues: #102, #103  
Parent: #82  
Base: `main` after #104 / PR #120

## 1. Intent

Game Request session information gives a group a small, structured place to keep the connection details needed to actually play: server addresses, lobby names, meeting points, passwords, and similar values.

The feature stays deliberately narrow:

- generic key/value data, never game-specific schema;
- available only while a Game Request is active (`open` or `playing`);
- readable only by the Host and players who are currently joined;
- editable only by the Host;
- lightweight server-rendered HTMX UI inside the Game Request detail page;
- realtime refresh through the existing SSE model;
- no activity history, notification work, Discord synchronization, Web Push, or Trending effect.

Issue #102 introduces ordinary session-information entries. Issue #103 is designed together with it but implemented separately as an additive extension for visually sensitive values. #102 must not pre-implement secret behavior.

The security goal of #103 is presentation privacy against shoulder-surfing and screen sharing. It is not protection against an authorized user's browser/devtools, database administrators, backups, or a compromised LANsurfer server.

## 2. Product behavior

### 2.1 Audience and lifecycle

All session information, not only secret values, is participant-only.

While the request is `open` or `playing`:

- the Host can read, create, edit, and delete entries;
- currently joined participants can read entries;
- non-participants receive no keys or values from the server.

An active non-participant sees only a neutral detail-page hint:

> Join this Game Request to see session information.

The hint reveals only that the feature exists. It does not reveal entry count, keys, values, or whether any entry is secret.

When the request becomes `closed` or `cancelled`:

- session information is no longer shown;
- no session-information editor is available;
- all session-information mutations are rejected;
- rows remain stored until the Game Request itself is deleted.

Leaving a request removes future read access immediately. Rejoining an active request grants access to the current values again.

### 2.2 Entry model and limits

Each Game Request may have at most 12 session-information entries.

Each entry has:

- a stable internal ID;
- a key, maximum 40 Unicode code points;
- a value, maximum 300 Unicode code points;
- stable creation order;
- created/updated timestamps.

Keys and values are single-line plain text.

Before validation and persistence, surrounding whitespace is trimmed. A key or value that is empty after trimming is invalid. Carriage returns and line feeds are invalid.

Keys are unique within one Game Request case-insensitively. For example, `Server` and `server` conflict. Display casing is preserved.

Both key and value are editable. Renaming an entry keeps its stable ID and original position.

There is no manual reordering. New entries append; editing never moves an entry.

### 2.3 Activity and notifications

Session-information CRUD is coordination state, not Game Request activity.

A successful create, update, or delete:

- writes no `game_request_activity` row;
- creates no notification work;
- sends no Web Push;
- causes no Discord create/edit;
- does not affect Trending.

A semantic no-op update writes nothing and emits no realtime signal.

## 3. Persistence

### 3.1 #102 table

#102 adds a child table owned by the `gamerequest` module, conceptually:

```text
game_request_session_entries
- id              uuid primary key
- game_request_id uuid not null -> game_requests(id) on delete cascade
- key             text not null
- value           text not null
- sequence        bigint generated always as identity
- created_at      timestamptz not null
- updated_at      timestamptz not null
```

`sequence` is only a stable creation-order value. It is not a global event cursor and is never exposed as user-controlled ordering. Reads for one request use `ORDER BY sequence`.

The database enforces the durable invariants it can express directly:

- key length 1..40;
- value length 1..300;
- no CR/LF in either field;
- one case-insensitive key per Game Request through a unique index on the request plus `lower(key)`;
- child rows cascade when their Game Request is deleted.

The domain layer performs trimming before persistence and repeats user-facing validation so callers receive useful errors rather than raw constraint failures.

The 12-entry limit is enforced under the Game Request row lock because it is a request-level concurrency rule rather than a simple row constraint.

On the current post-#104 `main`, migration `00026_game_request_messages.sql` is already occupied. #102 therefore starts with the next free migration (currently expected to be `00027`), but implementation must still confirm the branch's actual next migration before writing it.

### 3.2 #103 additive change

#103 adds only:

```text
is_secret boolean not null default false
```

to the session-entry model plus corresponding domain/UI behavior.

#102 does not add this column in advance.

Secret values remain plaintext in PostgreSQL. LANsurfer does not introduce at-rest encryption or a new data-encryption-key requirement for this feature.

## 4. Domain and repository boundaries

Session information remains inside the existing `gamerequest` module. No new business module and no generic metadata subsystem are introduced.

The domain exposes narrow operations equivalent to:

```text
SessionInformation(requestID, viewerID)
CreateSessionEntry(requestID, actorID, key, value)
UpdateSessionEntry(requestID, entryID, actorID, key, value)
DeleteSessionEntry(requestID, entryID, actorID)
```

#103 extends create/update input with the secret flag without changing the audience model.

### 4.1 Reads

The session-information read path authorizes on every request. It must not rely on the fact that a surrounding detail page happened to be rendered earlier.

A read may return entry values only when the database snapshot shows both:

```text
request status is open or playing
AND viewer is a current game_request_member
```

The Host is already a request member by invariant, so no separate Host bypass is required.

The handler needs to distinguish at least these presentation states:

- active + authorized: render current entries;
- active + unauthorized: render the neutral join hint, with no entry data;
- terminal: render no session-information section.

The query should authorize and read through the same database snapshot/query path rather than first trusting browser state or a stale page-level membership flag.

### 4.2 Mutations and concurrency

Every create, update, and delete runs in a transaction that first locks the owning Game Request row with the same `SELECT ... FOR UPDATE` model already used for Join, Leave, lifecycle transitions, Host detail edits, and chat posting.

Under that lock, each mutation verifies:

- the request exists;
- the actor is the Host;
- the request is `open` or `playing`;
- for create, the request still has fewer than 12 entries;
- for update/delete, the entry belongs to that request.

Create and rename also rely on the database unique index as the final case-insensitive key invariant.

This shared lock serializes session-information mutations with Leave, Close, Cancel, and other request-local coordination writes. A successful mutation cannot race past a terminal lifecycle transition.

An update compares normalized input to the current stored state. If no field changed, it commits no row update and publishes no realtime signal.

## 5. HTTP and HTMX

Session information uses a focused handler/fragment rather than widening the general Game Request details mutation.

The intended route shape is:

```text
GET  /game-requests/{requestID}/session-information
POST /game-requests/{requestID}/session-information
POST /game-requests/{requestID}/session-information/{entryID}
POST /game-requests/{requestID}/session-information/{entryID}/delete
```

The GET returns the complete session-information section and performs its own authorization.

Successful create/update/delete responses render the same section, so an HTMX mutation replaces only that local area. Non-HTMX POSTs redirect back to the Game Request detail page.

Validation failures render the inline form with the submitted values and a field-level or form-level error. They do not discard the draft.

Expected user-facing failures include:

- empty key/value after trim;
- key/value over the limit;
- line breaks;
- duplicate key;
- 13th entry;
- non-Host mutation;
- inactive request;
- missing/deleted entry.

There is no upsert behavior for a missing entry.

## 6. Detail-page UX

Session information is its own compact detail-page section and appears before request-local chat.

For an authorized participant, ordinary entries read as compact key/value rows.

For the Host while the request is active:

- an Add control opens one inline create form;
- Edit replaces one row with an inline edit form;
- Save and Cancel are local to that form;
- Delete is an explicit destructive action with confirmation;
- at most one session-information create/edit form is open at a time.

Participants who are not the Host get no edit controls.

An authorized empty state should be quiet and useful, with an Add action for the Host. An active non-participant sees only the join hint. Terminal requests show no section.

### 6.1 Draft preservation

Realtime/full-detail refreshes must not casually destroy a Host's unsaved session-information draft.

The browser preserves the current inline editor mode, dirty/rejected fields, and focus across a refresh when the refreshed server state still permits that editor. This follows the existing field-level preservation principle used by the Host Game Request detail editor rather than treating an old copy of the whole DOM as authoritative.

The server still wins when the editor is no longer legal. In particular, if the request became `closed` or `cancelled`, or the viewer is no longer authorized, the editor disappears and the local draft is discarded.

## 7. Realtime

### 7.1 Signal

A real session-information mutation publishes one content-free domain event, conceptually:

```text
GameRequestSessionInfoChanged
- requestID
```

The event carries no key, value, entry ID, entry count, or secret flag.

The browser receives it as a dedicated SSE event and refetches only the session-information section.

### 7.2 Participant-authorized delivery

The implementation follows the participant-authorized realtime pattern established by #104 chat rather than broadcasting this signal globally.

For a matching detail-page request, the realtime handler checks current session-information access per signal. It sends the signal only when the viewer is currently authorized for that active request.

Because #104 currently scopes the detail stream with the chat-specific `chatRequestId` query parameter, #102 should make that scope neutral (for example `detailRequestId`) and use the same selected request for both chat and session-information signals. Do not add parallel chat/session query parameters for the same detail page.

The dedicated HTTP read endpoint still performs its own authorization. SSE filtering is defense in depth and metadata minimization, not the authorization boundary for values.

Leaving therefore stops future session-information signals as well as future value reads.

General Game Request events such as Join, Leave, Close, or Cancel may still cause the existing full-detail refresh. That refresh must render the session-information section from the viewer's new current authorization state.

## 8. #103 secret presentation

### 8.1 Meaning of secret

A secret entry has exactly the same server-side audience as a normal entry: Host plus currently joined participants while the request is active.

`secret` changes presentation, not authorization.

It is intended for values such as:

- server passwords;
- lobby passwords;
- private connection tokens.

### 8.2 Rendering and copy

For an authorized viewer, a secret value is present in the server-rendered HTML but visually masked by default, for example as a read-only password input.

Controls provide:

- Copy: copies the value without making it visible;
- Reveal: changes the control to visible text;
- Hide: masks it immediately.

Reveal automatically returns to the masked state after 20 seconds. Manual Hide cancels the current reveal immediately. A fragment/full-detail refresh renders the secret masked again.

If clipboard access fails, the UI reports the failure without automatically revealing the value; the user can choose Reveal explicitly.

Each secret's reveal state is local presentation state only and is never persisted or sent to the server.

### 8.3 Host editing

A Host editing an existing secret receives the current value prefilled in a password-style field.

The edit form provides the same Reveal/Hide behavior and 20-second auto-mask. The Host may overwrite the value normally.

The Host may change an existing entry from normal to secret or secret to normal without changing its ID or ordering.

### 8.4 Explicit security boundary

Secret values must never be copied into:

- Game Request activity;
- notification work or payloads;
- Discord messages;
- Web Push;
- discovery/list surfaces;
- logs;
- metrics;
- SSE payloads;
- unrelated diagnostics.

The feature does not claim to hide a secret from:

- an authorized viewer inspecting browser DOM/devtools;
- a database administrator;
- database backups;
- a compromised application server.

No encryption-at-rest subsystem is added for #103.

## 9. Interaction with request-local chat

#104 is now part of the baseline.

Session information should reuse its established principles where they fit:

- request-local data remains owned by `gamerequest`;
- active membership controls reading;
- writes serialize under the Game Request row lock;
- realtime signals are content-free and participant-authorized;
- the browser refetches server-rendered HTML after a signal.

Session information does not reuse chat storage, rate limiting, message history, or chat-specific endpoints.

On the detail page, session information appears before Lobby chat because connection/session details are primary coordination data and chat is the conversation around that session.

## 10. Testing

### 10.1 #102 domain/service tests

Cover at least:

- surrounding whitespace is trimmed;
- empty key/value rejected;
- 40/300-code-point boundaries;
- CR/LF rejected;
- case-insensitive duplicate keys rejected;
- rename preserves entry identity/order;
- semantic no-op update publishes no event;
- real create/update/delete publishes exactly one session-info event.

### 10.2 #102 PostgreSQL integration tests

Cover at least:

- Host can create/update/delete while `open`;
- the same mutations work while `playing`;
- non-Host mutation is rejected;
- currently joined participant can read;
- active non-participant receives no entries;
- terminal request receives no entries and rejects mutation;
- leaving removes future read access;
- rejoining restores current read access;
- 12-entry limit under the request row lock;
- concurrent creates cannot exceed 12;
- case-insensitive uniqueness is enforced by the database;
- create/rename vs lifecycle transitions serialize correctly;
- request deletion cascades entries;
- ordering remains stable after edits.

### 10.3 #102 handler/template/browser tests

Cover at least:

- Host inline Add/Edit/Delete;
- one editor at a time;
- participant read-only rendering;
- non-participant join hint contains no entry metadata;
- terminal section absence;
- validation error retains submitted input;
- dedicated session-info SSE refetch;
- participant authorization of SSE delivery;
- no-op emits no SSE;
- unsaved inline draft survives an allowed live/full-detail refresh;
- lifecycle/authorization loss removes the editor despite a local draft;
- session-information section appears before chat on the current post-#104 detail layout.

### 10.4 #103 tests

Add targeted coverage for:

- secret flag create/edit/toggle;
- secret value masked by default;
- Copy works without Reveal;
- Reveal and manual Hide;
- automatic re-mask after 20 seconds;
- refresh returns a revealed value to masked state;
- secret edit form is prefilled but masked;
- leaving removes future value reads and future session-info signals;
- normal and secret entries have the same authorized audience;
- secret plaintext does not appear in activity, notifications, Discord rendering, SSE payloads, logs/diagnostic structures, or discovery/list rendering.

## 11. Documentation and issue alignment

The implementation changes durable product wording and must harvest the final decisions before merge.

### PRODUCT.md

Change the current wording so that:

- all session information is available only to the Host and currently joined participants while active;
- secret marks a value for visual privacy, not a narrower audience;
- secret presentation is masked by default with copy/reveal behavior;
- session-information mutations are not activity/notification events.

### ARCHITECTURE.md

Document:

- the session-entry child table and `gamerequest` ownership;
- active-membership read authorization;
- Host mutations under the shared request row lock;
- case-insensitive per-request keys and stable sequence ordering;
- absence from activity/notification work;
- content-free, participant-authorized realtime signaling;
- plaintext-at-rest decision and its threat-model boundary for #103.

### TERMINOLOGY.md

Add durable vocabulary for:

- Session information: request-local generic key/value coordination data visible only to current participants while active;
- Secret session information: the same participant audience, rendered with shoulder-surfing/screen-sharing protections.

### Issues #102 and #103

Align acceptance criteria with this design, especially the change from "only secrets are participant-only" to "all session information is participant-only."

## 12. Delivery split

The design is intentionally shared, but implementation and review stay separate.

### PR 1 — #102 structured session information

Includes:

- session-entry persistence without `is_secret`;
- participant-only reads;
- Host CRUD;
- inline UI;
- participant-authorized realtime;
- draft preservation;
- tests;
- durable documentation updates that are already true for normal session information.

It must not implement masking, Copy/Reveal, or a secret flag.

### PR 2 — #103 secret values

Builds on merged #102 and adds:

- `is_secret`;
- masked rendering;
- Copy without reveal;
- Reveal/Hide;
- 20-second auto-mask;
- secret editing/toggling;
- secret-specific leakage tests;
- final product/architecture/terminology wording for the presentation-privacy threat model.

This split keeps #102 reviewable and gives #103's privacy-sensitive behavior its own review while avoiding a later model rewrite.
