-- +goose Up
CREATE INDEX idx_ticket_events_scope_sequence ON ticket_events(workspace_id, project_id, event_sequence DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_scope_sequence;
