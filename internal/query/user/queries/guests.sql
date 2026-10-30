-- name: ListGuests :many
SELECT u.id::text AS id, u.game_nick, u.role, u.created_at, u.updated_at
FROM users u
WHERE EXISTS (
    SELECT 1 FROM user_identities i WHERE i.user_id = u.id AND i.provider_kind = 'guest'
)
ORDER BY u.created_at, u.id;
