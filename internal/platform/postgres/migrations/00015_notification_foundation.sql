-- +goose Up
-- A singleton marker: at most one row, and it can only ever be `true`. Its
-- presence means the one-time baseline materialization has already run, so
-- a later restart never re-scans and never risks delivering pre-upgrade
-- history. See internal/notification/postgres/store.go InitializeBaseline.
CREATE TABLE notification_baseline (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    initialized_at timestamptz NOT NULL DEFAULT now()
);

-- One row per game_request_activity row the notification module has consumed. The UNIQUE
-- constraint on activity_sequence is what makes repeated discovery harmless:
-- INSERT ... ON CONFLICT (activity_sequence) DO NOTHING is the only
-- durability guarantee this module needs, in place of a global cursor.
-- game_request_id, actor_id, kind, and occurred_at are snapshotted from
-- game_request_activity at materialization time so later reads never need
-- to join back into gamerequest's tables.
CREATE TABLE notification_work (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    activity_sequence bigint NOT NULL UNIQUE
        REFERENCES game_request_activity (sequence) ON DELETE CASCADE,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    actor_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('created', 'joined', 'left', 'closed', 'cancelled')),
    occurred_at timestamptz NOT NULL,
    baseline boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_work_game_request_idx ON notification_work (game_request_id);

-- Zero or more delivery targets per work item. UNIQUE (work_id, transport,
-- target_key) prevents a crash-and-retry of materialization from doubling up
-- deliveries for the same target.
CREATE TABLE notification_delivery (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    work_id bigint NOT NULL REFERENCES notification_work (id) ON DELETE CASCADE,
    transport text NOT NULL,
    target_key text NOT NULL,
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain')),
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    claimed_until timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (work_id, transport, target_key)
);

-- Two narrow partial indexes back the claim query's two due-conditions
-- (never-claimed vs. lease-expired) instead of one index that would only
-- help one branch.
CREATE INDEX notification_delivery_pending_due_idx
    ON notification_delivery (next_attempt_at) WHERE status = 'pending';
CREATE INDEX notification_delivery_claimed_lease_idx
    ON notification_delivery (claimed_until) WHERE status = 'claimed';
-- Authorized diagnostics list exactly these three terminal problem states.
CREATE INDEX notification_delivery_problem_idx
    ON notification_delivery (status) WHERE status IN ('exhausted', 'permanent', 'uncertain');

-- +goose Down
DROP TABLE notification_delivery;
DROP TABLE notification_work;
DROP TABLE notification_baseline;
