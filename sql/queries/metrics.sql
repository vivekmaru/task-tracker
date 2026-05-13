-- name: CreateAttemptMetrics :one
INSERT INTO attempt_metrics (
    attempt_id,
    workspace_id,
    project_id,
    tokens_in,
    tokens_out,
    cost_usd,
    duration_seconds,
    retry_count,
    agent_success_score,
    human_rating
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetAttemptMetrics :one
SELECT *
FROM attempt_metrics
WHERE attempt_id = $1;
