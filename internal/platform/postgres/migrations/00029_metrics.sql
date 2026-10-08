-- +goose Up
-- One metrics-only bearer credential, stored as SHA-256 of 32 random bytes.
CREATE TABLE metrics_access_key (
    singleton boolean PRIMARY KEY CHECK (singleton),
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32)
);

-- Retained live activity remains authoritative. Preserve only anonymous counts
-- when account deletion removes it; never retain a user or request identifier.
-- Reconstructed history present at upgrade is included once as a lower bound.
-- Activity deleted before this migration and unrecorded prototype transitions
-- cannot be recovered. Later backfills are not new product events.
CREATE TABLE metrics_product_totals (
    kind text PRIMARY KEY CHECK (kind IN ('created', 'joined', 'left', 'cancelled', 'closed', 'started')),
    total bigint NOT NULL DEFAULT 0 CHECK (total >= 0)
);
INSERT INTO metrics_product_totals (kind, total)
SELECT k.kind, count(a.sequence)
FROM (VALUES ('created'), ('joined'), ('left'), ('cancelled'), ('closed'), ('started')) AS k (kind)
LEFT JOIN game_request_activity a ON a.kind = k.kind AND a.origin = 'backfill'
GROUP BY k.kind;

-- No insert-time global counter lock: unrelated requests must still commit
-- independently. Deletion and its anonymous aggregate commit together, so a
-- scrape's single statement sees each event in history OR in this aggregate.
-- ponytail: deletions serialize on fixed counter rows; batch if that matters.
-- +goose StatementBegin
CREATE FUNCTION metrics_preserve_activity_totals() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    removed record;
BEGIN
    BEGIN
        FOR removed IN
            SELECT kind, count(*) AS total FROM deleted_activity
            WHERE origin = 'live' AND kind <> 'request_updated'
            GROUP BY kind ORDER BY kind
        LOOP
            UPDATE metrics_product_totals SET total = total + removed.total WHERE kind = removed.kind;
        END LOOP;
    EXCEPTION WHEN OTHERS THEN
        -- Optional observability must not prevent an account deletion.
        RAISE WARNING 'metrics counter preservation failed: %', SQLSTATE;
    END;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER metrics_preserve_activity_totals
    AFTER DELETE ON game_request_activity
    REFERENCING OLD TABLE AS deleted_activity
    FOR EACH STATEMENT EXECUTE FUNCTION metrics_preserve_activity_totals();

-- +goose Down
DROP TRIGGER metrics_preserve_activity_totals ON game_request_activity;
DROP FUNCTION metrics_preserve_activity_totals();
DROP TABLE metrics_product_totals;
DROP TABLE metrics_access_key;
