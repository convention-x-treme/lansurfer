-- +goose Up
-- Track A: the LANsurfer installation itself is the 1.0 event context, so a
-- Game Request no longer belongs to an Event. Existing requests keep their id,
-- creator, members, status, description, and timestamps; only the Event
-- association is removed.
--
-- Legacy Event data is deliberately left untouched in the `events` table. It is
-- no longer read by the application and no longer referenced by any row, but
-- dropping it would destroy operator data that a 1.0 upgrade never promised to
-- discard. A later release may remove the table once operators have exported it.
DROP INDEX game_requests_active_event_idx;
ALTER TABLE game_requests DROP COLUMN event_id;
CREATE INDEX game_requests_open_idx
    ON game_requests (starts_at, created_at) WHERE status = 'open';

-- Durable request-local activity. This is the chronological record of
-- successful lifecycle changes, and it is also Track A's transactional handoff
-- point to Track E: a row exists exactly when the domain change it describes
-- committed, so a rolled-back mutation leaves no notification intent behind.
-- `sequence` gives Track E a stable replay cursor and a total order.
CREATE TABLE game_request_activity (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('created', 'joined', 'left', 'closed', 'cancelled')),
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_request_activity_request_idx
    ON game_request_activity (game_request_id, sequence DESC);
-- Trending reads a rolling window, so the window predicate leads the index.
CREATE INDEX game_request_activity_recent_idx
    ON game_request_activity (occurred_at DESC, kind);

-- Prototype requests predate durable activity. Their creations and joins are
-- reconstructed from the membership rows that recorded them.
INSERT INTO game_request_activity (game_request_id, actor_id, kind, occurred_at)
SELECT m.game_request_id, m.user_id,
       CASE WHEN m.user_id = gr.creator_id THEN 'created' ELSE 'joined' END,
       CASE WHEN m.user_id = gr.creator_id THEN gr.created_at ELSE m.joined_at END
FROM game_request_members m
JOIN game_requests gr ON gr.id = m.game_request_id
ORDER BY 4, m.game_request_id;

-- A request that already left the open state ends its history with that change.
-- Only the creator can close or cancel, so the creator is the recorded actor.
INSERT INTO game_request_activity (game_request_id, actor_id, kind, occurred_at)
SELECT gr.id, gr.creator_id,
       CASE gr.status WHEN 'closed' THEN 'closed' ELSE 'cancelled' END,
       gr.updated_at
FROM game_requests gr
WHERE gr.status <> 'open'
ORDER BY gr.updated_at, gr.id;

-- +goose Down
DROP TABLE game_request_activity;
DROP INDEX game_requests_open_idx;
-- The original Event association cannot be restored; the column comes back
-- nullable so the previous schema shape exists again.
ALTER TABLE game_requests ADD COLUMN event_id uuid REFERENCES events (id) ON DELETE RESTRICT;
CREATE INDEX game_requests_active_event_idx
    ON game_requests (event_id, starts_at, created_at) WHERE status = 'open';
