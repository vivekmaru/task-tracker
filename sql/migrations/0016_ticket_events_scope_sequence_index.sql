-- ⚡ Bolt optimization: Add a composite index on (workspace_id, project_id, event_sequence DESC)
-- This speeds up queries like ListRecentTicketEvents that filter by scope and sort by sequence descending,
-- preventing full table scans on the rapidly growing ticket_events table.
-- +goose Up
CREATE INDEX idx_ticket_events_scope_sequence ON ticket_events(workspace_id, project_id, event_sequence DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_scope_sequence;
