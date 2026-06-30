-- +goose Up
CREATE INDEX idx_ticket_events_ticket_seq ON ticket_events(ticket_id, event_sequence);
CREATE INDEX idx_ticket_events_attempt_seq ON ticket_events(attempt_id, event_sequence);
CREATE INDEX idx_ticket_events_type_seq ON ticket_events(workspace_id, project_id, type, event_sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_type_seq;
DROP INDEX IF EXISTS idx_ticket_events_attempt_seq;
DROP INDEX IF EXISTS idx_ticket_events_ticket_seq;
