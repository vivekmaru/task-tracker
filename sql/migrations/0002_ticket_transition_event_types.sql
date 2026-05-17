-- +goose Up
ALTER TABLE ticket_events
    DROP CONSTRAINT IF EXISTS ticket_events_type_check;

ALTER TABLE ticket_events
    ADD CONSTRAINT ticket_events_type_check
    CHECK (type IN ('created', 'proposed', 'claimed', 'heartbeat', 'checkpointed', 'updated', 'completed', 'failed', 'blocked', 'expired', 'ready', 'reopened', 'unblocked', 'review_requested', 'reviewed', 'archived'));

-- +goose Down
ALTER TABLE ticket_events
    DROP CONSTRAINT IF EXISTS ticket_events_type_check;

ALTER TABLE ticket_events
    ADD CONSTRAINT ticket_events_type_check
    CHECK (type IN ('created', 'proposed', 'claimed', 'heartbeat', 'checkpointed', 'updated', 'completed', 'failed', 'blocked', 'expired', 'reviewed', 'archived'));
