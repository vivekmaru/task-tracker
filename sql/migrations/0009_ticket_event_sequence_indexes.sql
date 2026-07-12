-- +goose Up
CREATE INDEX idx_ticket_events_ticket_sequence ON ticket_events(ticket_id, event_sequence);
CREATE INDEX idx_ticket_events_attempt_sequence ON ticket_events(attempt_id, event_sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_ticket_sequence;
DROP INDEX IF EXISTS idx_ticket_events_attempt_sequence;
