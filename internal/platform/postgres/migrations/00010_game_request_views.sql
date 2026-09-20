-- +goose Up
-- game_request_views records that a player opened a Game Request's detail
-- page, backing the navigation's "Recently viewed" list. One row per
-- (player, request): re-opening a request moves it back to the top rather
-- than appending history, so the list stays a set of distinct requests and
-- needs no pruning to stay short.
--
-- This is deliberately not game_request_activity. That table is request-local
-- domain history and Track E's notification-intent handoff; a page view is
-- neither, and must never be delivered as a notification.
CREATE TABLE game_request_views (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    game_request_id uuid NOT NULL REFERENCES game_requests (id) ON DELETE CASCADE,
    viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, game_request_id)
);

CREATE INDEX game_request_views_recent_idx
    ON game_request_views (user_id, viewed_at DESC);

-- +goose Down
DROP TABLE game_request_views;
