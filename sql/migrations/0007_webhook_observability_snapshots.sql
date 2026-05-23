-- +goose Up
CREATE OR REPLACE FUNCTION enqueue_webhook_deliveries_for_ticket_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO webhook_deliveries (
        subscription_id,
        event_id,
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        payload,
        max_attempts
    )
    SELECT
        s.id,
        NEW.id,
        NEW.workspace_id,
        NEW.project_id,
        NEW.ticket_id,
        NEW.attempt_id,
        jsonb_build_object(
            'event_id', NEW.id,
            'event_type', NEW.type,
            'workspace_id', NEW.workspace_id,
            'project_id', NEW.project_id,
            'ticket_id', NEW.ticket_id,
            'attempt_id', NEW.attempt_id,
            'actor_type', NEW.actor_type,
            'actor_id', NEW.actor_id,
            'data', NEW.data,
            'created_at', NEW.created_at,
            'attempt', CASE
                WHEN a.id IS NULL THEN NULL
                ELSE jsonb_build_object(
                    'id', a.id,
                    'agent_id', a.agent_id,
                    'harness', a.harness,
                    'model', NULLIF(a.model, ''),
                    'status', a.status,
                    'progress_percent', a.progress_percent,
                    'trace_id', a.trace_id,
                    'checkpoint_ref', a.checkpoint_ref,
                    'started_at', a.started_at,
                    'completed_at', a.completed_at
                )
            END,
            'metrics', CASE
                WHEN m.id IS NULL THEN NULL
                ELSE jsonb_build_object(
                    'tokens_in', m.tokens_in,
                    'tokens_out', m.tokens_out,
                    'total_tokens', m.tokens_in + m.tokens_out,
                    'cost_usd', m.cost_usd,
                    'duration_seconds', m.duration_seconds,
                    'retry_count', m.retry_count
                )
            END
        ),
        s.max_attempts
    FROM webhook_subscriptions s
    LEFT JOIN attempts a ON a.id = NEW.attempt_id
    LEFT JOIN attempt_metrics m ON m.attempt_id = NEW.attempt_id
    WHERE s.workspace_id = NEW.workspace_id
      AND s.project_id = NEW.project_id
      AND s.active
      AND (
          cardinality(s.event_types) = 0
          OR NEW.type = ANY(s.event_types)
      )
    ON CONFLICT (subscription_id, event_id) DO NOTHING;

    RETURN NEW;
END;
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION enqueue_webhook_deliveries_for_ticket_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO webhook_deliveries (
        subscription_id,
        event_id,
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        payload,
        max_attempts
    )
    SELECT
        s.id,
        NEW.id,
        NEW.workspace_id,
        NEW.project_id,
        NEW.ticket_id,
        NEW.attempt_id,
        jsonb_build_object(
            'event_id', NEW.id,
            'event_type', NEW.type,
            'workspace_id', NEW.workspace_id,
            'project_id', NEW.project_id,
            'ticket_id', NEW.ticket_id,
            'attempt_id', NEW.attempt_id,
            'actor_type', NEW.actor_type,
            'actor_id', NEW.actor_id,
            'data', NEW.data,
            'created_at', NEW.created_at
        ),
        s.max_attempts
    FROM webhook_subscriptions s
    WHERE s.workspace_id = NEW.workspace_id
      AND s.project_id = NEW.project_id
      AND s.active
      AND (
          cardinality(s.event_types) = 0
          OR NEW.type = ANY(s.event_types)
      )
    ON CONFLICT (subscription_id, event_id) DO NOTHING;

    RETURN NEW;
END;
$$;
