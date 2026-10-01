-- +goose Up
-- A player can permanently delete their account, and the account takes every
-- record attributable to them with it: the Game Requests they created, their
-- memberships, and the activity they caused. These four foreign keys were
-- RESTRICT, which made DELETE FROM users fail as soon as a player had done
-- anything. Cascading keeps the deletion one atomic statement without one
-- module writing another's tables. Every other user-owned table already
-- cascades (sessions, identities, follows, views, push subscriptions).
ALTER TABLE game_requests
    DROP CONSTRAINT game_requests_creator_id_fkey,
    ADD CONSTRAINT game_requests_creator_id_fkey
        FOREIGN KEY (creator_id) REFERENCES users (id) ON DELETE CASCADE;

ALTER TABLE game_request_members
    DROP CONSTRAINT game_request_members_user_id_fkey,
    ADD CONSTRAINT game_request_members_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;

ALTER TABLE game_request_activity
    DROP CONSTRAINT game_request_activity_actor_id_fkey,
    ADD CONSTRAINT game_request_activity_actor_id_fkey
        FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE CASCADE;

ALTER TABLE notification_work
    DROP CONSTRAINT notification_work_actor_id_fkey,
    ADD CONSTRAINT notification_work_actor_id_fkey
        FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE notification_work
    DROP CONSTRAINT notification_work_actor_id_fkey,
    ADD CONSTRAINT notification_work_actor_id_fkey
        FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE RESTRICT;

ALTER TABLE game_request_activity
    DROP CONSTRAINT game_request_activity_actor_id_fkey,
    ADD CONSTRAINT game_request_activity_actor_id_fkey
        FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE RESTRICT;

ALTER TABLE game_request_members
    DROP CONSTRAINT game_request_members_user_id_fkey,
    ADD CONSTRAINT game_request_members_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT;

ALTER TABLE game_requests
    DROP CONSTRAINT game_requests_creator_id_fkey,
    ADD CONSTRAINT game_requests_creator_id_fkey
        FOREIGN KEY (creator_id) REFERENCES users (id) ON DELETE RESTRICT;
