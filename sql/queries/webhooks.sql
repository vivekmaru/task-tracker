-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscriptions (
    workspace_id,
    project_id,
    endpoint_url,
    secret,
    event_types,
    active,
    max_attempts,
    description
)
VALUES (
    sqlc.arg(workspace_id),
    sqlc.arg(project_id),
    sqlc.arg(endpoint_url),
    sqlc.narg(secret),
    sqlc.arg(event_types),
    sqlc.arg(active),
    sqlc.arg(max_attempts),
    sqlc.arg(description)
)
RETURNING *;

-- name: ListWebhookDeliveriesByEvent :many
SELECT *
FROM webhook_deliveries
WHERE event_id = sqlc.arg(event_id)
ORDER BY created_at ASC;

-- name: ClaimPendingWebhookDeliveries :many
WITH candidates AS (
    SELECT id
    FROM webhook_deliveries
    WHERE status IN ('pending', 'delivering')
      AND attempt_count < max_attempts
      AND next_attempt_at <= sqlc.arg(now)::timestamptz
      AND (
          status <> 'delivering'
          OR locked_until IS NULL
          OR locked_until <= sqlc.arg(now)::timestamptz
      )
    ORDER BY created_at ASC
    LIMIT sqlc.arg(batch_limit)::integer
    FOR UPDATE SKIP LOCKED
)
UPDATE webhook_deliveries d
SET status = 'delivering',
    locked_until = sqlc.arg(locked_until)::timestamptz,
    updated_at = now()
FROM candidates c
JOIN webhook_deliveries claimed ON claimed.id = c.id
JOIN webhook_subscriptions s ON s.id = claimed.subscription_id
WHERE d.id = c.id
RETURNING
    d.id,
    d.subscription_id,
    d.event_id,
    d.workspace_id,
    d.project_id,
    d.ticket_id,
    d.attempt_id,
    d.status,
    d.payload,
    d.attempt_count,
    d.max_attempts,
    d.next_attempt_at,
    d.locked_until,
    d.last_attempt_at,
    d.delivered_at,
    d.response_status,
    d.response_body,
    d.error,
    d.created_at,
    d.updated_at,
    s.endpoint_url,
    s.secret;

-- name: MarkWebhookDeliverySucceeded :one
UPDATE webhook_deliveries
SET status = 'succeeded',
    attempt_count = sqlc.arg(attempt_count)::integer,
    locked_until = NULL,
    last_attempt_at = sqlc.arg(attempted_at)::timestamptz,
    delivered_at = sqlc.arg(attempted_at)::timestamptz,
    response_status = sqlc.narg(response_status)::integer,
    response_body = sqlc.narg(response_body)::text,
    error = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING *;

-- name: MarkWebhookDeliveryFailed :one
UPDATE webhook_deliveries
SET status = CASE
        WHEN sqlc.arg(attempt_count)::integer >= max_attempts THEN 'failed'
        ELSE 'pending'
    END,
    attempt_count = sqlc.arg(attempt_count)::integer,
    locked_until = NULL,
    last_attempt_at = sqlc.arg(attempted_at)::timestamptz,
    response_status = sqlc.narg(response_status)::integer,
    response_body = sqlc.narg(response_body)::text,
    error = sqlc.arg(error)::text,
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz,
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
RETURNING *;
