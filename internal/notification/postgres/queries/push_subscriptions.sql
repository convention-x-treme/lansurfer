-- name: UpsertPushSubscription :one
INSERT INTO notification_push_subscription
    (user_id, label, device_id_hash, endpoint_fingerprint, endpoint_ciphertext, p256dh_ciphertext, auth_ciphertext)
VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(label),
    sqlc.arg(device_id_hash),
    sqlc.arg(endpoint_fingerprint),
    sqlc.arg(endpoint_ciphertext),
    sqlc.arg(p256dh_ciphertext),
    sqlc.arg(auth_ciphertext)
)
ON CONFLICT (endpoint_fingerprint) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    label = EXCLUDED.label,
    device_id_hash = EXCLUDED.device_id_hash,
    endpoint_ciphertext = EXCLUDED.endpoint_ciphertext,
    p256dh_ciphertext = EXCLUDED.p256dh_ciphertext,
    auth_ciphertext = EXCLUDED.auth_ciphertext,
    updated_at = now()
RETURNING id, created_at, updated_at;

-- name: ListPushSubscriptionsByUser :many
SELECT id, label, device_id_hash, endpoint_fingerprint, created_at, updated_at
FROM notification_push_subscription
WHERE user_id = sqlc.arg(user_id)::uuid
ORDER BY created_at;

-- name: DeletePushSubscription :execrows
DELETE FROM notification_push_subscription
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)::uuid;

-- name: DeletePushSubscriptionByDeviceHash :execrows
DELETE FROM notification_push_subscription
WHERE user_id = sqlc.arg(user_id)::uuid AND device_id_hash = sqlc.arg(device_id_hash);

-- name: GetPushPreference :one
SELECT web_push_enabled, new_request_audience
FROM notification_push_preference
WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: UpsertPushPreference :exec
INSERT INTO notification_push_preference (user_id, web_push_enabled, new_request_audience)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(web_push_enabled), sqlc.arg(new_request_audience))
ON CONFLICT (user_id) DO UPDATE SET
    web_push_enabled = EXCLUDED.web_push_enabled,
    new_request_audience = EXCLUDED.new_request_audience,
    updated_at = now();
