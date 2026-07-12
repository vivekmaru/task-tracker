-- name: CompleteAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET status = 'succeeded',
        progress_percent = 100,
        output = sqlc.arg(output)::jsonb,
        output_schema = sqlc.narg(output_schema)::text,
        completed_at = sqlc.arg(completed_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
updated_ticket AS (
    UPDATE tickets t
    SET status = CASE
            WHEN COALESCE((t.retry_policy->>'requires_review_on_success')::boolean, false)
                THEN 'needs_review'
            ELSE 'done'
        END,
        updated_at = now()
    FROM updated_attempt a
    WHERE t.id = a.ticket_id
    RETURNING t.id, t.status
),
completed_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'completed',
        'agent',
        a.agent_id,
        jsonb_build_object('ticket_status', t.status)
    FROM updated_attempt a
    JOIN updated_ticket t ON t.id = a.ticket_id
    RETURNING id
)
SELECT
    a.id AS attempt_id,
    a.workspace_id,
    a.project_id,
    a.ticket_id,
    a.status AS attempt_status,
    t.status AS ticket_status
FROM updated_attempt a
JOIN updated_ticket t ON t.id = a.ticket_id;

-- name: FailAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET status = 'failed',
        failure_reason = sqlc.arg(failure_reason)::text,
        failure_category = sqlc.narg(failure_category)::text,
        output = sqlc.arg(output)::jsonb,
        completed_at = sqlc.arg(completed_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
updated_ticket AS (
    UPDATE tickets t
    SET status = CASE
            WHEN COALESCE(t.retry_policy->>'on_failure', 'return_to_todo') = 'mark_failed'
                THEN 'failed'
            WHEN COALESCE(t.retry_policy->>'on_failure', 'return_to_todo') = 'needs_review'
                THEN 'needs_review'
            WHEN (
                SELECT count(*)::integer
                FROM attempts counted_attempts
                WHERE counted_attempts.ticket_id = t.id
                  AND counted_attempts.status IN ('failed', 'expired')
            ) >= COALESCE((t.retry_policy->>'max_attempts')::integer, 3)
                THEN 'failed'
            ELSE 'todo'
        END,
        updated_at = now()
    FROM updated_attempt a
    WHERE t.id = a.ticket_id
    RETURNING t.id, t.status
),
failed_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'failed',
        'agent',
        a.agent_id,
        jsonb_build_object(
            'ticket_status', t.status,
            'failure_category', a.failure_category
        )
    FROM updated_attempt a
    JOIN updated_ticket t ON t.id = a.ticket_id
    RETURNING id
)
SELECT
    a.id AS attempt_id,
    a.workspace_id,
    a.project_id,
    a.ticket_id,
    a.status AS attempt_status,
    t.status AS ticket_status
FROM updated_attempt a
JOIN updated_ticket t ON t.id = a.ticket_id;

-- name: BlockAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET status = 'blocked',
        failure_reason = sqlc.arg(blocker_reason)::text,
        failure_category = sqlc.narg(failure_category)::text,
        blocker = sqlc.arg(blocker)::jsonb,
        completed_at = sqlc.arg(completed_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
updated_ticket AS (
    UPDATE tickets t
    SET status = 'blocked',
        updated_at = now()
    FROM updated_attempt a
    WHERE t.id = a.ticket_id
    RETURNING t.id, t.status
),
blocked_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'blocked',
        'agent',
        a.agent_id,
        jsonb_build_object(
            'ticket_status', t.status,
            'failure_category', a.failure_category
        )
    FROM updated_attempt a
    JOIN updated_ticket t ON t.id = a.ticket_id
    RETURNING id
)
SELECT
    a.id AS attempt_id,
    a.workspace_id,
    a.project_id,
    a.ticket_id,
    a.status AS attempt_status,
    t.status AS ticket_status
FROM updated_attempt a
JOIN updated_ticket t ON t.id = a.ticket_id;

-- name: CancelAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET status = 'cancelled',
        failure_reason = sqlc.narg(reason)::text,
        completed_at = sqlc.arg(completed_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
updated_ticket AS (
    UPDATE tickets t
    SET status = 'todo',
        updated_at = now()
    FROM updated_attempt a
    WHERE t.id = a.ticket_id
    RETURNING t.id, t.status
),
cancelled_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'cancelled',
        'agent',
        a.agent_id,
        jsonb_build_object('ticket_status', t.status)
    FROM updated_attempt a
    JOIN updated_ticket t ON t.id = a.ticket_id
    RETURNING id
)
SELECT
    a.id AS attempt_id,
    a.workspace_id,
    a.project_id,
    a.ticket_id,
    a.status AS attempt_status,
    t.status AS ticket_status
FROM updated_attempt a
JOIN updated_ticket t ON t.id = a.ticket_id;

-- name: ExpireAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET status = 'expired',
        failure_reason = 'lease expired',
        completed_at = sqlc.arg(completed_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
      AND a.lease_expires_at < sqlc.arg(expiration_cutoff)::timestamptz
    RETURNING *
),
updated_ticket AS (
    UPDATE tickets t
    SET status = CASE
            WHEN (
                SELECT count(*)::integer
                FROM attempts counted_attempts
                WHERE counted_attempts.ticket_id = t.id
                  AND counted_attempts.status IN ('failed', 'expired')
            ) >= COALESCE((t.retry_policy->>'max_attempts')::integer, 3)
                THEN 'failed'
            ELSE 'todo'
        END,
        updated_at = now()
    FROM updated_attempt a
    WHERE t.id = a.ticket_id
    RETURNING t.id, t.status
),
expired_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'expired',
        'system',
        a.agent_id,
        jsonb_build_object('ticket_status', t.status)
    FROM updated_attempt a
    JOIN updated_ticket t ON t.id = a.ticket_id
    RETURNING id
)
SELECT
    a.id AS attempt_id,
    a.workspace_id,
    a.project_id,
    a.ticket_id,
    a.status AS attempt_status,
    t.status AS ticket_status
FROM updated_attempt a
JOIN updated_ticket t ON t.id = a.ticket_id;
