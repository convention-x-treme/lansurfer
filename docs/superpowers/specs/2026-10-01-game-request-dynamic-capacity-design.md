# Game Request Dynamic Participation and Capacity Design

Status: design approved in brainstorming; stable 1.0 scope; implementation not started

Issue: #101  
Parent: #82  
Depends on: #100

## 1. Intent

Extend active Game Requests so participation can continue after the Host starts the game.

A Game Request in `playing` remains an active coordination surface. Players may continue to join and leave while capacity permits, and the Host may change the optional maximum player count while the request is `open` or `playing`.

The design must preserve LANsurfer's existing modular-monolith boundaries, PostgreSQL consistency model, request-local durable activity history, SSE refresh model, and synchronized Discord message behavior.

## 2. Product behavior

### 2.1 Active participation

Join and leave are allowed while a request is either:

- `open`
- `playing`

Join and leave remain rejected for:

- `closed`
- `cancelled`

The Host remains a member and cannot leave their own request. Host ownership remains non-transferable in this scope.

### 2.2 Joinability

Joinability is derived exclusively from:

1. request lifecycle state;
2. current participant count;
3. optional `max_players`.

No separate "accepting players" toggle is introduced.

A request is joinable when:

- status is `open` or `playing`; and
- the viewer is not already a member; and
- either `max_players IS NULL` or current participants are below `max_players`.

A request without a maximum is unlimited while active.

When the configured maximum is reached, further joins are rejected automatically. When a participant leaves, capacity becomes available automatically. Increasing the maximum or removing it immediately makes additional joins possible.

### 2.3 Capacity editing

Only the Host may change `max_players`.

The Host may edit capacity while the request is:

- `open`
- `playing`

Capacity changes are rejected for `closed` and `cancelled` requests.

The editor exists only on the Game Request detail page. Discovery/list rows expose the current capacity and joinability but do not provide an inline capacity editor.

The Host may:

- set a finite maximum;
- increase or decrease an existing maximum;
- remove an existing maximum and return the request to unlimited.

Submitting the same semantic value is a no-op: no database update, no durable activity entry, no realtime event, and no Discord synchronization work.

### 2.4 Capacity value rules

`max_players` remains optional.

For new or edited finite limits:

- `0` or an empty input means unlimited and maps to `NULL`;
- finite values must be at least `2`;
- the existing UI upper bound of `999` remains;
- a finite maximum must never be below the current participant count.

The Host counts as the first participant. Therefore `2` is the smallest meaningful finite limit.

Historical requests that already contain `max_players = 1` are not rewritten. Their original value is preserved because the original intent is unknown. They remain readable and usable according to current state, but any future capacity edit must submit either unlimited or a finite value `>= 2`.

No migration backfills or normalizes legacy `1` values.

## 3. Domain and persistence design

### 3.1 Existing locking model

The existing Game Request row lock remains the consistency boundary.

Join, leave, lifecycle transitions, and capacity updates serialize through the same locked `game_requests` row.

This preserves the current concurrency guarantees without introducing a new locking mechanism, trigger-based workflow, separate service, queue, or coordination table.

### 3.2 Repository/service shape

Add an explicit capacity mutation to the Game Request domain rather than overloading lifecycle status mutation.

Conceptually:

`Service.UpdateCapacity(ctx, requestID, userID, maxPlayers)`

The repository gains a corresponding targeted write.

The service/domain validates value-shape rules that do not depend on current persisted state:

- unlimited is valid;
- finite values below `2` are invalid.

The PostgreSQL repository validates state-dependent invariants while holding the Game Request row lock:

- request exists;
- actor is the Host;
- status is `open` or `playing`;
- the requested finite maximum is not below current participant count;
- the requested value differs semantically from the current value.

The write and its durable activity record occur in the same transaction.

### 3.3 Concurrency semantics

All membership and capacity decisions use the same locked request row.

This gives deterministic behavior for racing operations:

- two joins competing for the final free place cannot overfill the request;
- join versus capacity reduction resolves according to lock order;
- capacity reduction versus leave resolves according to lock order;
- no operation may commit a state where participant count exceeds the finite maximum.

Example:

- If Join acquires the lock first and increases membership, a following capacity reduction must validate against the higher count and may be rejected.
- If a valid capacity reduction acquires the lock first, a following Join observes the new lower limit and may be rejected as full.

No optimistic retry layer or separate advisory lock is needed.

## 4. Durable activity model

### 4.1 New activity kind

Add a new durable Game Request activity kind:

`capacity_changed`

This is a meaningful request-local change and therefore belongs in `game_request_activity`.

It is written in the same transaction as the capacity update.

### 4.2 Typed change metadata

Do not introduce generic JSON activity metadata.

Extend `game_request_activity` with two nullable typed columns:

- `previous_max_players integer NULL`
- `new_max_players integer NULL`

For `capacity_changed`:

- each column stores the corresponding finite value;
- `NULL` means unlimited.

Examples:

- `4 -> 6`: previous `4`, new `6`
- `6 -> unlimited`: previous `6`, new `NULL`
- `unlimited -> 8`: previous `NULL`, new `8`

For every other activity kind, both fields must remain `NULL`.

The migration should enforce this relationship with a check constraint so these columns cannot silently become generic payload fields.

### 4.3 Activity history presentation

The request-local Activity section renders concrete changes, for example:

- `Alex changed capacity from 4 to 6`
- `Alex changed capacity from 6 to unlimited`
- `Alex changed capacity from unlimited to 8`

No-op submissions create no history entry.

## 5. Realtime and external delivery

### 5.1 SSE / web UI

A successful capacity change publishes a normal Game Request change event through the existing in-process event publisher.

The existing SSE/HTMX refresh model updates:

- request detail;
- discovery/list row;
- capacity/free-place state;
- join/leave controls;
- relevant active navigation state.

No new realtime transport is introduced.

### 5.2 Discord

`capacity_changed` enters the existing durable notification handoff so the synchronized Discord Game Request message can immediately reflect the current capacity snapshot.

Discord continues to render current Game Request state rather than historical change payloads.

No separate Discord event message is created.

### 5.3 Web Push

Capacity changes do not create personal Web Push deliveries.

The notification pipeline must explicitly treat `capacity_changed` as valid durable work that may require Discord synchronization while producing zero personal Push targets.

This must not change recipient behavior for existing activity kinds.

### 5.4 Trending

`capacity_changed` does not affect Trending.

Trending continues to use the established activity set and rolling-window semantics. Host configuration changes are not treated as popularity/activity signals.

## 6. UI behavior

### 6.1 Discovery/list

Both `open` and `playing` requests remain active.

For non-host viewers:

- if already joined: show Leave;
- if not joined and capacity is available: show Join;
- if not joined and finite capacity is reached: show Full.

This behavior applies equally to `open` and `playing`.

The list continues to show current participant count and finite maximum where present.

The existing "Free places" filter treats both active lifecycle states consistently.

### 6.2 Detail page

The detail page shows current capacity and joinability.

For the Host only, when status is `open` or `playing`, show a compact capacity editor.

The editor:

- accepts blank or `0` as unlimited;
- accepts finite values `2..999`;
- displays field-local validation errors;
- does not require a confirmation dialog because changing capacity is reversible;
- does not appear on closed or cancelled requests.

For non-host participants and viewers, capacity remains read-only.

## 7. Errors and validation

Use domain-specific errors rather than generic handler strings for state/business failures.

Expected cases include:

- invalid finite value, including `1`: existing `ErrInvalidMaxPlayers`;
- finite maximum below current participant count: add a dedicated error such as `ErrCapacityBelowMembers`;
- non-Host capacity edit: `ErrNotCreator`;
- capacity edit on inactive request: a dedicated active-state error or an existing error only if its wording remains accurate for both `open` and `playing`;
- join when full: `ErrFull`;
- join when already a member: `ErrAlreadyMember`;
- leave when not a member: `ErrNotMember`;
- Host leave: `ErrCreatorCannotLeave`.

Handlers map validation failures to the existing 4xx conventions and keep capacity-specific form errors adjacent to the capacity field.

## 8. Database migration

Create the next forward migration after `00023_game_request_playing.sql`.

The migration must:

1. add nullable `previous_max_players` and `new_max_players` to `game_request_activity`;
2. extend the activity kind constraint with `capacity_changed`;
3. add a check constraint tying metadata columns to `capacity_changed`;
4. extend `notification_work.kind` to accept `capacity_changed`;
5. preserve all existing Game Request rows unchanged;
6. perform no normalization/backfill of legacy `max_players = 1`.

Do not add a database-level `max_players >= 2` constraint to `game_requests`, because that would make legacy `1` rows invalid. Enforce the new finite-value rule on new/edited input instead.

The down migration is developer-only and may remove `capacity_changed` work/activity before restoring the previous constraints and dropping the metadata columns.

## 9. Testing strategy

### 9.1 Domain/unit tests

Cover:

- service publishes a capacity-change realtime event only after success;
- repository failure publishes nothing;
- invalid finite values are rejected;
- no-op behavior does not publish.

### 9.2 PostgreSQL integration tests

Cover at minimum:

- Join succeeds while `playing` when capacity permits;
- Leave succeeds while `playing`;
- Join/Leave remain rejected when `closed` or `cancelled`;
- finite capacity is enforced for both `open` and `playing`;
- unlimited remains joinable while active;
- Host may update capacity in `open`;
- Host may update capacity in `playing`;
- non-Host update is rejected;
- capacity update on inactive request is rejected;
- finite value below current membership is rejected;
- `1` is rejected as a new/edited finite value;
- a pre-existing legacy row with `max_players = 1` remains readable;
- same-value update is a no-op with no activity row;
- finite-to-finite activity stores old/new values;
- finite-to-unlimited stores new `NULL`;
- unlimited-to-finite stores previous `NULL`;
- activity metadata constraint rejects metadata on unrelated kinds;
- two concurrent joins cannot exceed capacity;
- concurrent Join versus capacity reduction preserves invariants;
- concurrent Leave versus capacity reduction preserves invariants.

### 9.3 Query/presentation tests

Cover:

- `playing` rows expose Join/Leave controls according to membership and capacity;
- full/available presentation works for `playing`;
- "Free places" includes joinable `playing` requests and excludes full ones;
- Activity history renders concrete old/new values;
- Trending count is unaffected by `capacity_changed`.

### 9.4 Notification tests

Cover:

- `capacity_changed` is discoverable/materializable as durable notification work;
- Discord synchronization renders the updated capacity snapshot;
- Web Push creates no personal deliveries for `capacity_changed`;
- existing activity kinds retain their current Push behavior.

### 9.5 Browser tests

Cover the visible flow:

1. Host creates a limited request.
2. Host changes the limit on the detail page.
3. Another player can join when capacity becomes available.
4. Reaching capacity shows Full and disables Join.
5. A participant leaves and a place becomes available again.
6. Host switches to unlimited and Join becomes available.
7. The flow continues to work after the request transitions to `playing`.

## 10. Canonical documentation impact

Update durable documentation where #101 changes implemented product truth:

- `docs/PRODUCT.md`
  - Join/Leave remain available while `playing`.
  - Capacity is Host-editable while `open` and `playing`.
  - Unlimited remains supported.
  - New finite values are at least `2`.
- `docs/ARCHITECTURE.md`
  - Join/Leave/Capacity share the request-row locking consistency boundary.
  - `capacity_changed` is durable activity and notification handoff work.
  - Capacity change has Discord sync but no Web Push.
- `docs/TERMINOLOGY.md`
  - Capacity, Full, Free place, Unlimited.
- relevant UI-kit documentation
  - Host capacity editor lives on the detail page.
  - `playing` retains normal Join/Leave/Full states.

Current scheduling, dependency status, implementation progress, and validation evidence remain in GitHub Issues/PRs rather than canonical architecture docs.

## 11. Explicit non-goals

#101 does not implement:

- waitlists;
- Host approval for joins;
- manual accepting-players toggle;
- Host transfer;
- team/slot/role assignment;
- structured session information (#102);
- secret session values (#103);
- request-local chat (#104);
- generic activity payload/metadata infrastructure;
- a separate capacity subsystem;
- triggers for membership/capacity orchestration;
- a second notification outbox;
- a new realtime transport.

## 12. Success criteria

The design is successful when:

- `playing` behaves as an active participation state;
- membership can change safely during play;
- capacity can change safely during `open` and `playing`;
- concurrency cannot overfill a request or reduce capacity below membership;
- capacity history records the actual before/after values;
- Discord stays synchronized without generating noisy personal Push;
- Trending semantics remain unchanged;
- legacy `max_players = 1` data remains intact;
- #102–#104 remain independent follow-up slices.
