-- name: CreateIdempotencyKey :one
INSERT INTO idempotency_keys (
    workspace_id,
    actor_id,
    key,
    route,
    request_hash,
    response_body,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT *
FROM idempotency_keys
WHERE workspace_id = $1
  AND actor_id = $2
  AND route = $3
  AND key = $4;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys
WHERE expires_at < now();
