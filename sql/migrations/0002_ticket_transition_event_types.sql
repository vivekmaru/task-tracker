-- +goose Up
ALTER TABLE ticket_events
    DROP CONSTRAINT IF EXISTS ticket_events_type_check;

ALTER TABLE ticket_events
    ADD CONSTRAINT ticket_events_type_check
    CHECK (type IN ('created', 'proposed', 'claimed', 'heartbeat', 'checkpointed', 'updated', 'completed', 'failed', 'blocked', 'expired', 'ready', 'reopened', 'unblocked', 'review_requested', 'reviewed', 'archived'));

-- +goose Down
ALTER TABLE ticket_events
    DROP CONSTRAINT IF EXISTS ticket_events_type_check;

UPDATE ticket_events
SET data = jsonb_set(data, '{downgraded_type}', to_jsonb(type), true),
    type = 'updated'
WHERE type IN ('ready', 'reopened', 'unblocked', 'review_requested');

ALTER TABLE ticket_events
    ADD CONSTRAINT ticket_events_type_check
    CHECK (type IN ('created', 'proposed', 'claimed', 'heartbeat', 'checkpointed', 'updated', 'completed', 'failed', 'blocked', 'expired', 'reviewed', 'archived'));
