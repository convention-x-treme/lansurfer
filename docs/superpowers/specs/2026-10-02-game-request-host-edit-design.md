# Game Request Host Edit Design

Status: approved follow-up design for Issue #101 / PR #118; implementation pending

Issue: #101  
PR: #118  
Branch: `feat/issue-101-dynamic-capacity`

## 1. Intent

Replace the capacity-only Host editor introduced by #101 with one coherent **Edit Game Request** flow on the existing detail page.

The Host should be able to correct the mutable coordination details of a Game Request without recreating it:

- maximum players;
- planned start time;
- description.

The selected Game is the identity of the request and is never editable. The authoritative actual start time `started_at` is also never editable.

This follow-up preserves #101's concurrency, realtime, Activity, Discord, Web Push, and Trending decisions while generalizing the capacity-only update into one atomic request-details update.

## 2. Editable fields by lifecycle state

### Open

While `open`, the Host may edit:

- Max players;
- planned start time (`starts_at`);
- description.

### Playing

While `playing`, the Host may edit:

- Max players;
- description.

The planned start time is no longer editable after **Start game**. It remains visible as planning/history information, but an attempted manipulated request that changes it while `playing` must be rejected server-side.

### Closed / cancelled

No request-detail editing is allowed.

### Never editable

The update input does not contain:

- Game / `game_id`;
- Host / creator;
- lifecycle status;
- `started_at`.

Those fields cannot be changed accidentally because they are not part of the update contract.

## 3. Detail-page interaction

The existing separate Host **Capacity** card is replaced by a general **Edit** action on the Game Request detail page.

The edit experience stays on the same detail route:

- normal detail: `GET /game-requests/{requestID}`;
- edit mode: `GET /game-requests/{requestID}?edit=1`;
- Cancel returns to the normal detail route;
- Save posts the complete editable detail form and returns to the normal detail view.

No separate edit page and no SPA/client-side domain state are introduced.

The current Capacity control in PR #118 is **moved into the shared editor rather than redesigned**. Preserve its working behavior and visual language:

- blank or `0` means unlimited;
- finite limits are `2..999`;
- field-local validation;
- rejected input remains visible after server re-render;
- reversible edits need no confirmation dialog.

The existing Create controls/patterns for planned time and description should be reused where practical rather than creating a second time/description vocabulary.

The Game remains visible in the detail UI but is not rendered as an editable form control.

## 4. Domain API

Replace the capacity-only command with one details update.

Conceptually:

```go
type UpdateDetailsInput struct {
    MaxPlayers  *int32
    StartsAt    time.Time
    Description string
}

func (s *Service) UpdateDetails(
    ctx context.Context,
    requestID string,
    userID string,
    input UpdateDetailsInput,
) error
```

The exact type may carry the current `StartsAt` value even while playing so one form/parser shape can be reused, but the persistence layer must enforce that `starts_at` is unchanged once the request is `playing`.

The service retains value-shape validation:

- finite Max players is at least 2;
- description is trimmed and at most 1000 runes;
- planned start is a valid non-zero absolute instant.

The existing browser upper bound of 999 remains a transport constraint.

## 5. Atomic persistence and locking

`UpdateDetails` uses the same `game_requests ... FOR UPDATE` row lock already used by Join, Leave, lifecycle transitions, and the #101 capacity update.

Under that lock the Store validates:

- actor is the Host;
- status is `open` or `playing`;
- finite capacity is not below current membership;
- when `playing`, requested `starts_at` equals the stored value;
- which editable fields actually changed.

A Save is atomic across all changed fields.

A semantic no-op writes nothing:

- no `game_requests` update;
- no `updated_at` change;
- no Activity;
- no SSE event;
- no notification work;
- no Discord edit.

The Game and `started_at` are not columns in the update statement.

## 6. Durable Activity

Replace the branch-only `capacity_changed` event with one general durable kind:

`request_updated`

PR #118 has not merged, so no backward-compatibility path for deployed `capacity_changed` rows is required. The branch migration should be edited in place rather than adding a production migration whose only purpose is translating an unshipped schema.

One successful Save creates at most one `request_updated` Activity row.

### Typed change data

Retain the current typed capacity metadata:

- `previous_max_players integer NULL`
- `new_max_players integer NULL`

Add:

- `previous_starts_at timestamptz NULL`
- `new_starts_at timestamptz NULL`
- `description_changed boolean NOT NULL DEFAULT false`

Capacity `NULL` continues to mean unlimited.

For `request_updated`, the row must describe at least one real change:

- capacity old/new are distinct; and/or
- planned start old/new are distinct; and/or
- `description_changed = true`.

Unchanged dimensions leave their before/after columns NULL.

For every non-`request_updated` Activity kind, all update metadata columns remain NULL/false.

Description text itself is not copied into Activity history.

## 7. Activity presentation

History should be concrete when one dimension changed:

- `Alex changed capacity from 4 to 6`
- `Alex changed the planned start from <old> to <new>`
- `Alex updated the description`

When multiple dimensions changed in one Save, show one concise combined entry, for example:

- `Alex updated capacity and description`
- `Alex updated capacity, planned start, and description`

The planned-start before/after timestamps are stored as absolute instants and rendered with the same browser-local time model used elsewhere in LANsurfer.

## 8. Realtime and delivery

A real update publishes one domain/SSE event:

`GameRequestUpdated`

The existing SSE/HTMX refresh path re-reads the authoritative request snapshot. No update payload containing field values is required.

`request_updated` remains durable notification handoff work so Discord edits the one synchronized Game Request message using current state.

For every request-details update:

- Discord: synchronize current snapshot;
- Web Push: zero targets;
- Trending: no contribution.

Do not add `request_updated` to the service-worker PushKind wire vocabulary.

## 9. Migration handling on the unmerged branch

Before implementation, synchronize the feature worktree with current `origin/main`.

Use an explicit fetch + merge rather than configuration-dependent plain pull:

```bash
git fetch origin
git merge origin/main
```

Resolve all conflicts and restore a green baseline before changing the edit flow.

Because PR #118 is unmerged, modify its dynamic-capacity migration rather than layering a second migration merely to rename/reshape unshipped Activity semantics.

After merging `origin/main`:

- inspect the latest migration number on main;
- if the branch's dynamic-capacity migration number now conflicts with main, rename it to the next free version before editing it;
- keep the migration forward-only;
- do not migrate any deployed `capacity_changed` data because none exists on main.

## 10. Validation and errors

Preserve the current Capacity rules and messages where applicable.

Add detail-edit validation for:

- invalid planned date/time;
- description over 1000 runes;
- trying to alter planned time while playing;
- inactive request;
- non-Host edit.

Field-specific errors re-render the edit form and preserve submitted values.

A manipulated form cannot alter Game or `started_at` because the command does not accept them.

## 11. Tests

Cover at minimum:

### Domain / service
- capacity only;
- start time only;
- description only;
- multiple fields;
- complete no-op publishes no event;
- invalid finite capacity;
- invalid description/start input;
- failed repository update publishes no event.

### PostgreSQL
- open Host can change all three editable fields;
- playing Host can change capacity + description;
- playing planned-time change is rejected with no partial write;
- inactive/non-Host edit rejected;
- Game unchanged;
- `started_at` unchanged;
- capacity cannot fall below membership;
- Join/update race preserves membership/capacity invariant;
- one Save yields exactly one `request_updated` Activity;
- typed capacity old/new values correct;
- typed planned-time old/new values correct;
- description records only `description_changed`;
- metadata constraint rejects invalid unrelated metadata;
- no-op changes nothing.

### Web/UI
- Host sees Edit only in `open|playing`;
- non-Host and inactive requests do not;
- edit mode remains on detail page;
- Game is visible but not editable;
- open edit contains capacity + planned start + description;
- playing edit contains capacity + description and non-editable planned start;
- Cancel returns to normal detail;
- validation preserves submitted field values;
- the existing refined Capacity control behavior is preserved inside the editor.

### Realtime / notification
- one `GameRequestUpdated` SSE event for one real Save;
- no event for no-op;
- one `request_updated` notification work item;
- Discord edits the existing synchronized message with the current capacity/start/description snapshot;
- Web Push has zero targets;
- Trending is unchanged.

### Browser
- Host opens inline edit mode, changes details, saves, and returns to normal detail;
- open planned-time editing works;
- after Start, Capacity and Description remain editable but planned time is not;
- participant Join/Leave/Full behavior from #101 remains intact.

## 12. Canonical documentation impact

Update:

- `docs/PRODUCT.md`: Host edits request coordination details; lifecycle-specific editable fields; Game immutable.
- `docs/ARCHITECTURE.md`: atomic `UpdateDetails`, shared row lock, `request_updated` typed metadata, delivery behavior.
- `docs/TERMINOLOGY.md`: Edit Game Request / mutable request details and updated Activity wording.
- `docs/ui-kit/README.md`: replace Capacity-only card description with inline Edit flow while preserving Capacity-control UX.

## 13. Non-goals

This follow-up does not add:

- Game replacement;
- Host transfer;
- actual-start editing;
- editing closed/cancelled requests;
- generic patch/JSON metadata;
- revision history for full descriptions;
- Web Push for edits;
- new realtime transport;
- separate edit page;
- #102 session information;
- #103 secrets;
- #104 chat.
