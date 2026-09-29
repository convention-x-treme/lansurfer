# Issue #100 — Playing lifecycle for Game Requests

Date: 2026-09-30  
Status: design approved in brainstorming; implementation not started  
Issue: #100 `Add playing lifecycle to Game Requests`  
Parent: #82 `Add Game Request coordination and playing flow`

## Intent

LANsurfer currently stops at "players joined". This change introduces an explicit, durable transition from forming a group to actually playing.

The host deliberately starts a Game Request. That transition is the authoritative product signal that a game session really began. A started request remains active and visible until the host closes it.

This slice establishes the lifecycle foundation for #101–#104 without pulling their capacity, session-information, secret, or chat behavior into #100.

## Approved lifecycle

The lifecycle is:

```text
open -> playing -> closed
  \-> cancelled
```

The following transitions are valid:

| From | Action | To |
| --- | --- | --- |
| `open` | Start game | `playing` |
| `open` | Cancel | `cancelled` |
| `playing` | Close game | `closed` |

The following transitions are intentionally invalid:

- `open -> closed`
- `playing -> cancelled`
- any transition out of `closed`
- any transition out of `cancelled`
- repeated `playing -> playing`

Semantics:

- `open`: the group is forming and the session has not started.
- `playing`: the host explicitly confirmed that the session started.
- `closed`: a started session finished.
- `cancelled`: a request was abandoned before the session started.

The host may start a request:

- with no other participants;
- before the planned start time;
- regardless of whether a maximum player count is configured.

The planned start time remains planning information. It never gates the actual start action.

## Scope

### In scope

- add the explicit `playing` status;
- add a durable actual start timestamp;
- add a host-only Start game action with irreversible-action confirmation;
- restrict Cancel to `open`;
- restrict Close to `playing`;
- keep `playing` requests visible in active discovery;
- add a durable `started` Game Request activity event;
- feed `started` through existing realtime refresh behavior;
- synchronize Discord presentation to the `playing` state;
- send personal Web Push for `started` to current participants except the host/actor;
- update presentation so `open`, `playing`, `closed`, and `cancelled` are distinct;
- update canonical product/architecture documentation where the new durable behavior changes current truth.

### Out of scope

- joining or leaving while `playing` (#101);
- changing capacity while `playing` (#101);
- structured session information (#102);
- secret session values (#103);
- request-local chat (#104);
- Prometheus metric implementation (#83);
- a generic workflow/state-machine framework;
- generic notification preferences for lifecycle events;
- a new realtime channel or transport.

## Persistence model

Add nullable `started_at` to `game_requests`.

For requests created after this feature is deployed:

- `open` => `started_at IS NULL`
- `cancelled` => `started_at IS NULL`
- `playing` => `started_at IS NOT NULL`
- `closed` => `started_at IS NOT NULL`

The `open -> playing` transaction sets `status = 'playing'` and `started_at = now()` together. `started_at` is immutable after that point.

### Existing historical closed requests

Existing `closed` rows predate an explicit start signal. Their real start time cannot be reconstructed reliably.

The migration must not invent one from `closed_at`, `updated_at`, planned time, or activity timestamps. Historical closed requests may therefore remain `closed` with `started_at IS NULL` as legacy data.

From the feature's deployment forward, application/domain behavior guarantees that newly closed requests passed through `playing` and therefore have a real `started_at`.

Any future aggregate "games started" query must use the explicit start signal (`started_at IS NOT NULL` and/or the `started` activity), not infer starts from legacy `closed` rows.

Database constraints should enforce all invariants that can be enforced without falsifying legacy history. At minimum:

- `playing` requires non-null `started_at`;
- `open` and `cancelled` require null `started_at`.

New domain transitions guarantee that new `closed` rows have non-null `started_at`.

## Game Request domain behavior

Introduce an explicit Start Game Request use case in the `gamerequest` capability.

The service/domain layer, not the HTTP handler, owns these checks:

1. the request exists;
2. actor is the request creator/host;
3. current status is exactly `open`.

No participant-count or planned-time condition is checked.

On success, one database transaction:

1. locks/serializes against the request using the existing lifecycle mutation pattern;
2. changes status to `playing`;
3. sets `started_at`;
4. appends the `started` `game_request_activity` row with the host as actor.

If any part fails, neither the state change nor the activity row commits.

The existing Cancel use case accepts only `open`.  
The existing Close use case accepts only `playing`.

Invalid state transitions use the existing domain-error style and remain transport-independent.

## Activity history

`game_request_activity` remains the single durable request-local history and notification handoff.

Add the deliberate lifecycle kind:

```text
started
```

The visible Activity section renders it as a normal chronological lifecycle entry using the host's public Game Nick, conceptually:

```text
20:42  Alex started the game
```

Use one timestamp value inside the start transaction for both `game_requests.started_at` and the `started` activity's occurrence time. The Game Request's `started_at` remains the source for current lifecycle state; the activity row is historical evidence and the existing notification handoff.

No second event/history table is introduced.

## Notification behavior

Because `game_request_activity` is already the transactional notification handoff, `started` is a supported notification activity kind.

### Discord

A `started` activity schedules the existing Discord synchronization behavior.

Discord rendering reads the current Game Request state, so the existing synchronized message is edited to represent the request as `playing`. It does not create a new message per lifecycle event.

The message must show a readable Playing status consistent with the web UI and existing brand rules.

### Personal Web Push

A `started` activity sends personal Web Push to the current request participants, excluding the activity actor/host.

This intentionally lets participants know that the group is starting even if LANsurfer is not currently open.

Existing rules remain:

- the host/actor does not receive their own personal push;
- visible same-origin LANsurfer windows suppress the OS notification in the service worker;
- Push failure cannot roll back the start transaction;
- current device/subscription eligibility is rechecked by the existing notification pipeline;
- no participant roster or new private content is added to the payload.

The existing Push payload shape remains unchanged except that `started` becomes an accepted activity kind. The current service worker must render a concise started-state prefix/message. Existing activity kinds keep their current behavior.

## Discovery and read models

A `playing` request is still active and must remain visible wherever the product means "active/current Game Requests".

Queries and read models that currently equate "active" with `status = open` must be reviewed individually:

- discovery/list visibility: include `open` and `playing`;
- detail access: continue to show all supported states as today;
- join/leave eligibility: do not broaden in #100;
- capacity rules: do not broaden in #100.

This distinction is important: visibility of a `playing` request does not imply that joining/leaving while playing is implemented yet. #101 owns that behavior.

Any trending/current-request query that is defined in terms of active visible requests should treat `playing` as active unless its existing product definition explicitly refers only to groups still forming.

## Web UI

### Start action

Only the host sees **Start game** while the request is `open`.

The action requires an explicit confirmation because it is irreversible. The confirmation copy must communicate that:

- the request will become an active playing session;
- it can no longer be cancelled afterward;
- ending the session uses Close game.

After confirmation, use the existing POST/HTMX server-rendered interaction style. No client-side state model is introduced.

### Playing presentation

After a successful start:

- status presentation changes from **Looking for players** / `open` to **Playing**;
- Start game disappears;
- Cancel disappears;
- the host receives **Close game**;
- non-host action availability remains governed by the current #100 rules;
- Activity shows the started event.

Lists/cards visibly distinguish `playing` from `open`.

The stable labels are:

- `open` — Looking for players
- `playing` — Playing
- `closed` — Closed
- `cancelled` — Cancelled

## Realtime behavior

No new realtime mechanism is introduced.

The successful start publishes through the existing Game Request realtime/SSE refresh path so already-open detail and discovery views can re-render the current request state and Activity without a full page reload.

Later closure from `playing` continues to use the same model.

The implementation must preserve the current separation:

- PostgreSQL transaction = durable domain state + durable activity/notification intent;
- SSE = best-effort live browser refresh;
- notification worker = durable external notification delivery.

## Error handling and concurrency

The transition must follow existing lifecycle mutation locking/serialization so concurrent host actions cannot produce two starts or conflicting terminal transitions.

Representative races to handle deterministically:

- two Start game submissions => one succeeds, the other receives invalid-current-state behavior;
- Start versus Cancel => exactly one valid transition commits;
- a Close submission observed while the request is still `open` is rejected; a Close submitted after the Start transition has committed is a valid `playing -> closed` transition.

No duplicate `started` activity may be created for one request.

A notification-delivery failure happens after the committed domain transition and must never reverse or fail the successful Start game action.

## Testing strategy

### Domain/unit tests

Cover:

- host can start an `open` request;
- host can start with no other participants;
- host can start before planned time;
- non-host cannot start;
- repeated start is rejected;
- closed/cancelled/playing requests cannot be started;
- Cancel accepts only `open`;
- Close accepts only `playing`;
- direct `open -> closed` is rejected;
- `playing -> cancelled` is rejected.

### PostgreSQL/integration tests

Cover:

- `status=playing`, `started_at`, and `started` activity commit atomically;
- rollback cannot leave state without activity or activity without state;
- concurrency produces exactly one successful start and one `started` row;
- database constraints enforce the non-legacy started-at invariants;
- migration preserves pre-existing closed rows without inventing start timestamps;
- active discovery includes `playing`;
- join/leave behavior is not accidentally broadened to `playing`.

### Notification tests

Cover:

- `started` is recognized by the notification consumer;
- current participants are targeted;
- host/actor is excluded;
- Discord sync renders current `playing` state and edits the existing message;
- Push payload accepts `started`;
- existing activity kinds are unchanged;
- notification failure cannot affect the committed Game Request transition.

### Browser/PWA tests

Use browser tests only where browser behavior matters:

- Start game confirmation is required;
- successful start updates the visible UI from Start game to Playing/Close game without full reload where the existing HTMX/SSE behavior supports it;
- service worker accepts/renders a `started` Push;
- visible-window suppression still suppresses the OS notification.

Do not turn the browser suite into a duplicate of domain/integration coverage.

### Repository verification

Run the relevant repository workflows after implementation, expected to include:

- `task generate`
- `task test`
- `task test:integration`
- `task test:browser` when the interaction change is covered there
- `task test:pwa` for the Push/service-worker contract
- `task lint`
- `task build`

## Documentation impact

Implementation changes durable product behavior and technical contracts.

Before merge:

- update `docs/PRODUCT.md` with the `playing` lifecycle and actual-start semantics;
- update `docs/ARCHITECTURE.md` for the new lifecycle/activity/notification kind and remove stale wording that says only create/join/leave/close/cancel activity exists;
- update `docs/TERMINOLOGY.md` if it enumerates Game Request status vocabulary;
- update issue #100 / PR description to record that `started` Activity + Discord + Web Push were deliberately pulled into this slice;
- keep #101–#104 scoped to their existing follow-up responsibilities.

This working spec is temporary branch material under the repository workflow. Durable truth must be harvested into the canonical documents before final merge.

## Acceptance summary

#100 is complete when:

- host-only `open -> playing` works with explicit confirmation;
- starting is allowed alone and before planned time;
- actual `started_at` is durable and immutable;
- `playing` remains visible as an active request;
- `open -> closed` and `playing -> cancelled` are impossible for new transitions;
- `playing -> closed` is the only successful finish path after start;
- one atomic `started` activity is recorded;
- realtime refresh shows the transition;
- Activity history renders the start;
- Discord synchronizes the existing message to Playing;
- current participants except the host receive the started Web Push subject to existing visible-window suppression;
- #101–#104 behavior has not been pulled into this implementation.
