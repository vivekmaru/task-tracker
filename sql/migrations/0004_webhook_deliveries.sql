-- +goose Up
CREATE TABLE webhook_subscriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    endpoint_url text NOT NULL,
    secret text,
    event_types text[] NOT NULL DEFAULT '{}'::text[],
    active boolean NOT NULL DEFAULT true,
    max_attempts integer NOT NULL DEFAULT 3,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (endpoint_url ~ '^https?://'),
    CHECK (max_attempts > 0)
);

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id uuid NOT NULL REFERENCES ticket_events(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    attempt_id uuid REFERENCES attempts(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'pending',
    payload jsonb NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 3,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    locked_until timestamptz,
    last_attempt_at timestamptz,
    delivered_at timestamptz,
    response_status integer,
    response_body text,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, event_id),
    CHECK (status IN ('pending', 'delivering', 'succeeded', 'failed')),
    CHECK (attempt_count >= 0),
    CHECK (max_attempts > 0)
);

CREATE INDEX idx_webhook_subscriptions_scope
    ON webhook_subscriptions(workspace_id, project_id)
    WHERE active;
CREATE INDEX idx_webhook_deliveries_pending
    ON webhook_deliveries(status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'delivering');
CREATE INDEX idx_webhook_deliveries_event_id ON webhook_deliveries(event_id);

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

CREATE TRIGGER trg_enqueue_webhook_deliveries_for_ticket_event
AFTER INSERT ON ticket_events
FOR EACH ROW
EXECUTE FUNCTION enqueue_webhook_deliveries_for_ticket_event();

-- +goose Down
DROP TRIGGER IF EXISTS trg_enqueue_webhook_deliveries_for_ticket_event ON ticket_events;
DROP FUNCTION IF EXISTS enqueue_webhook_deliveries_for_ticket_event();
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_subscriptions;
