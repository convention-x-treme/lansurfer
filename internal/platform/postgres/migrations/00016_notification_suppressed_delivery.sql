-- +goose Up
-- `suppressed` is a general terminal delivery state: an originally
-- materialized delivery is intentionally not sent because current
-- eligibility no longer permits it (the target/user/destination became
-- ineligible after materialization). It is not a provider failure, is never
-- automatically retried, and is retained for diagnostics like the other
-- terminal problem states.
ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain', 'suppressed'));

DROP INDEX notification_delivery_problem_idx;
CREATE INDEX notification_delivery_problem_idx
    ON notification_delivery (status) WHERE status IN ('exhausted', 'permanent', 'uncertain', 'suppressed');

-- +goose Down
DROP INDEX notification_delivery_problem_idx;
CREATE INDEX notification_delivery_problem_idx
    ON notification_delivery (status) WHERE status IN ('exhausted', 'permanent', 'uncertain');

ALTER TABLE notification_delivery DROP CONSTRAINT notification_delivery_status_check;
ALTER TABLE notification_delivery ADD CONSTRAINT notification_delivery_status_check
    CHECK (status IN ('pending', 'claimed', 'succeeded', 'permanent', 'exhausted', 'uncertain'));
