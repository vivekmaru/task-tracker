-- name: ClaimNextTicket :one
WITH candidate AS (
    SELECT t.id
    FROM tickets t
    WHERE t.workspace_id = $1
      AND t.project_id = $2
      AND t.status = 'todo'
      AND (sqlc.narg(ticket_type)::text IS NULL OR t.type = sqlc.narg(ticket_type)::text)
      AND (COALESCE(cardinality(sqlc.arg(tags)::text[]), 0) = 0 OR t.tags && sqlc.arg(tags)::text[])
      AND (
          COALESCE(cardinality(t.allowed_harnesses), 0) = 0
          OR sqlc.arg(harness)::text = ANY(t.allowed_harnesses)
      )
      AND (
          COALESCE(cardinality(t.required_capabilities), 0) = 0
          OR t.required_capabilities <@ sqlc.arg(capabilities)::text[]
      )
      AND NOT EXISTS (
          SELECT 1
          FROM ticket_dependencies d
          JOIN tickets dep ON dep.id = d.depends_on_ticket_id
          WHERE d.ticket_id = t.id
            AND dep.status != 'done'
      )
      AND (
          SELECT count(*)::integer
          FROM attempts a
          WHERE a.ticket_id = t.id
            AND a.status IN ('failed', 'expired')
      ) < COALESCE((t.retry_policy->>'max_attempts')::integer, 3)
    ORDER BY t.priority DESC, t.created_at ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
),
updated_ticket AS (
    UPDATE tickets t
    SET status = 'in_progress',
        updated_at = now()
    FROM candidate c
    WHERE t.id = c.id
    RETURNING t.id, t.workspace_id, t.project_id
),
created_attempt AS (
    INSERT INTO attempts (
        workspace_id,
        project_id,
        ticket_id,
        agent_id,
        harness,
        model,
        status,
        lease_expires_at,
        last_heartbeat_at,
        progress_percent,
        output
    )
    SELECT
        t.workspace_id,
        t.project_id,
        t.id,
        sqlc.arg(agent_id)::text,
        sqlc.arg(harness)::text,
        sqlc.arg(model)::text,
        'running',
        sqlc.arg(lease_expires_at)::timestamptz,
        sqlc.arg(last_heartbeat_at)::timestamptz,
        0,
        '{}'::jsonb
    FROM updated_ticket t
    RETURNING id, ticket_id
),
claimed_event AS (
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
        t.workspace_id,
        t.project_id,
        t.id,
        a.id,
        'claimed',
        'agent',
        sqlc.arg(agent_id)::text,
        jsonb_build_object(
            'attempt_id', a.id,
            'agent_id', sqlc.arg(agent_id)::text,
            'harness', sqlc.arg(harness)::text,
            'model', sqlc.arg(model)::text
        )
    FROM updated_ticket t
    JOIN created_attempt a ON a.ticket_id = t.id
    RETURNING id
),
stored_idempotency AS (
    INSERT INTO idempotency_keys (
        workspace_id,
        actor_id,
        key,
        route,
        request_hash,
        response_body,
        expires_at
    )
    SELECT
        t.workspace_id,
        sqlc.arg(agent_id)::text,
        sqlc.narg(idempotency_key)::text,
        'claim-next',
        sqlc.narg(request_hash)::text,
        jsonb_build_object(
            'ticket_id', t.id,
            'attempt_id', a.id
        ),
        sqlc.narg(idempotency_expires_at)::timestamptz
    FROM updated_ticket t
    JOIN created_attempt a ON a.ticket_id = t.id
    WHERE sqlc.narg(idempotency_key)::text IS NOT NULL
    RETURNING id
)
SELECT
    t.id AS ticket_id,
    a.id AS attempt_id
FROM updated_ticket t
JOIN created_attempt a ON a.ticket_id = t.id;
