-- +goose Up
CREATE OR REPLACE FUNCTION suppress_implicit_heartbeat_webhook_deliveries()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.payload->>'event_type' = 'heartbeat' AND EXISTS (
        SELECT 1 FROM webhook_subscriptions s
        WHERE s.id = NEW.subscription_id
          AND cardinality(s.event_types) = 0
    ) THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_suppress_implicit_heartbeat_webhook_deliveries
BEFORE INSERT ON webhook_deliveries
FOR EACH ROW EXECUTE FUNCTION suppress_implicit_heartbeat_webhook_deliveries();

-- +goose Down
DROP TRIGGER IF EXISTS trg_suppress_implicit_heartbeat_webhook_deliveries ON webhook_deliveries;
DROP FUNCTION IF EXISTS suppress_implicit_heartbeat_webhook_deliveries();
