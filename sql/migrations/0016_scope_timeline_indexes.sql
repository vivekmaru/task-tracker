-- +goose Up
-- Both scope filters are optional. Keep separate ordered paths for workspace-only,
-- project-only, and workspace-plus-project feeds (ascending scans work too).
CREATE INDEX idx_ticket_events_scope_sequence ON ticket_events(workspace_id, project_id, event_sequence DESC);
CREATE INDEX idx_ticket_events_workspace_sequence ON ticket_events(workspace_id, event_sequence);
CREATE INDEX idx_ticket_events_project_sequence ON ticket_events(project_id, event_sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_project_sequence;
DROP INDEX IF EXISTS idx_ticket_events_workspace_sequence;
DROP INDEX IF EXISTS idx_ticket_events_scope_sequence;
