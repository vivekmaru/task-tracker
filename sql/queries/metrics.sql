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
ON CONFLICT (attempt_id) DO UPDATE SET
    tokens_in = EXCLUDED.tokens_in,
    tokens_out = EXCLUDED.tokens_out,
    cost_usd = EXCLUDED.cost_usd,
    duration_seconds = EXCLUDED.duration_seconds,
    retry_count = EXCLUDED.retry_count,
    agent_success_score = EXCLUDED.agent_success_score,
    human_rating = EXCLUDED.human_rating
RETURNING *;

-- name: GetAttemptMetrics :one
SELECT *
FROM attempt_metrics
WHERE attempt_id = $1;

-- name: GetAnalyticsSummary :one
SELECT
    COUNT(a.id)::bigint AS attempt_count,
    COUNT(*) FILTER (WHERE a.status = 'succeeded')::bigint AS succeeded_attempts,
    COUNT(*) FILTER (WHERE a.status = 'failed')::bigint AS failed_attempts,
    COUNT(*) FILTER (WHERE a.status = 'blocked')::bigint AS blocked_attempts,
    COALESCE(SUM(m.tokens_in), 0)::bigint AS total_tokens_in,
    COALESCE(SUM(m.tokens_out), 0)::bigint AS total_tokens_out,
    COALESCE(SUM(m.cost_usd), 0)::numeric(12, 6) AS total_cost_usd,
    COALESCE(SUM(m.duration_seconds), 0)::numeric(12, 3) AS total_duration_secs,
    COALESCE(SUM(m.retry_count), 0)::bigint AS total_retries,
    COUNT(m.id)::bigint AS attempts_with_metrics
FROM attempts a
LEFT JOIN attempt_metrics m ON m.attempt_id = a.id
WHERE (sqlc.narg(workspace_id)::uuid IS NULL OR a.workspace_id = sqlc.narg(workspace_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR a.project_id = sqlc.narg(project_id));

-- name: GetAnalyticsByModel :many
SELECT
    COALESCE(NULLIF(a.model, ''), '(unknown)')::text AS model,
    COUNT(a.id)::bigint AS attempt_count,
    COUNT(*) FILTER (WHERE a.status = 'succeeded')::bigint AS succeeded_attempts,
    COUNT(*) FILTER (WHERE a.status = 'failed')::bigint AS failed_attempts,
    COUNT(*) FILTER (WHERE a.status = 'blocked')::bigint AS blocked_attempts,
    COALESCE(SUM(m.tokens_in), 0)::bigint AS total_tokens_in,
    COALESCE(SUM(m.tokens_out), 0)::bigint AS total_tokens_out,
    COALESCE(SUM(m.cost_usd), 0)::numeric(12, 6) AS total_cost_usd,
    COALESCE(SUM(m.duration_seconds), 0)::numeric(12, 3) AS total_duration_secs,
    COALESCE(SUM(m.retry_count), 0)::bigint AS total_retries,
    COUNT(m.id)::bigint AS attempts_with_metrics
FROM attempts a
LEFT JOIN attempt_metrics m ON m.attempt_id = a.id
WHERE (sqlc.narg(workspace_id)::uuid IS NULL OR a.workspace_id = sqlc.narg(workspace_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR a.project_id = sqlc.narg(project_id))
GROUP BY COALESCE(NULLIF(a.model, ''), '(unknown)')
ORDER BY attempt_count DESC, model ASC;

-- name: GetAnalyticsByHarness :many
SELECT
    COALESCE(NULLIF(a.harness, ''), '(unknown)')::text AS harness,
    COUNT(a.id)::bigint AS attempt_count,
    COUNT(*) FILTER (WHERE a.status = 'succeeded')::bigint AS succeeded_attempts,
    COUNT(*) FILTER (WHERE a.status = 'failed')::bigint AS failed_attempts,
    COUNT(*) FILTER (WHERE a.status = 'blocked')::bigint AS blocked_attempts,
    COALESCE(SUM(m.tokens_in), 0)::bigint AS total_tokens_in,
    COALESCE(SUM(m.tokens_out), 0)::bigint AS total_tokens_out,
    COALESCE(SUM(m.cost_usd), 0)::numeric(12, 6) AS total_cost_usd,
    COALESCE(SUM(m.duration_seconds), 0)::numeric(12, 3) AS total_duration_secs,
    COALESCE(SUM(m.retry_count), 0)::bigint AS total_retries,
    COUNT(m.id)::bigint AS attempts_with_metrics
FROM attempts a
LEFT JOIN attempt_metrics m ON m.attempt_id = a.id
WHERE (sqlc.narg(workspace_id)::uuid IS NULL OR a.workspace_id = sqlc.narg(workspace_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR a.project_id = sqlc.narg(project_id))
GROUP BY COALESCE(NULLIF(a.harness, ''), '(unknown)')
ORDER BY attempt_count DESC, harness ASC;

-- name: GetAnalyticsByStatus :many
SELECT
    a.status::text AS status,
    COUNT(a.id)::bigint AS attempt_count,
    COUNT(*) FILTER (WHERE a.status = 'succeeded')::bigint AS succeeded_attempts,
    COUNT(*) FILTER (WHERE a.status = 'failed')::bigint AS failed_attempts,
    COUNT(*) FILTER (WHERE a.status = 'blocked')::bigint AS blocked_attempts,
    COALESCE(SUM(m.tokens_in), 0)::bigint AS total_tokens_in,
    COALESCE(SUM(m.tokens_out), 0)::bigint AS total_tokens_out,
    COALESCE(SUM(m.cost_usd), 0)::numeric(12, 6) AS total_cost_usd,
    COALESCE(SUM(m.duration_seconds), 0)::numeric(12, 3) AS total_duration_secs,
    COALESCE(SUM(m.retry_count), 0)::bigint AS total_retries,
    COUNT(m.id)::bigint AS attempts_with_metrics
FROM attempts a
LEFT JOIN attempt_metrics m ON m.attempt_id = a.id
WHERE (sqlc.narg(workspace_id)::uuid IS NULL OR a.workspace_id = sqlc.narg(workspace_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR a.project_id = sqlc.narg(project_id))
GROUP BY a.status
ORDER BY attempt_count DESC, status ASC;

-- name: GetAnalyticsByAgent :many
SELECT
    COALESCE(NULLIF(a.agent_id, ''), '(unknown)')::text AS agent_id,
    COUNT(a.id)::bigint AS attempt_count,
    COUNT(*) FILTER (WHERE a.status = 'succeeded')::bigint AS succeeded_attempts,
    COUNT(*) FILTER (WHERE a.status = 'failed')::bigint AS failed_attempts,
    COUNT(*) FILTER (WHERE a.status = 'blocked')::bigint AS blocked_attempts,
    COALESCE(SUM(m.tokens_in), 0)::bigint AS total_tokens_in,
    COALESCE(SUM(m.tokens_out), 0)::bigint AS total_tokens_out,
    COALESCE(SUM(m.cost_usd), 0)::numeric(12, 6) AS total_cost_usd,
    COALESCE(SUM(m.duration_seconds), 0)::numeric(12, 3) AS total_duration_secs,
    COALESCE(SUM(m.retry_count), 0)::bigint AS total_retries,
    COUNT(m.id)::bigint AS attempts_with_metrics
FROM attempts a
LEFT JOIN attempt_metrics m ON m.attempt_id = a.id
WHERE (sqlc.narg(workspace_id)::uuid IS NULL OR a.workspace_id = sqlc.narg(workspace_id))
  AND (sqlc.narg(project_id)::uuid IS NULL OR a.project_id = sqlc.narg(project_id))
GROUP BY COALESCE(NULLIF(a.agent_id, ''), '(unknown)')
ORDER BY attempt_count DESC, agent_id ASC;
