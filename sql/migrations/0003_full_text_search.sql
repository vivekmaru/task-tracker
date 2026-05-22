-- +goose Up
CREATE INDEX idx_tickets_search_vector
    ON tickets USING gin (
        to_tsvector('english', coalesce(title, '') || ' ' || coalesce(description, ''))
    );

CREATE INDEX idx_attempts_search_vector
    ON attempts USING gin (
        to_tsvector('english', coalesce(current_summary, '') || ' ' || output::text)
    );

CREATE INDEX idx_ticket_events_search_vector
    ON ticket_events USING gin (
        to_tsvector('english', data::text)
    );

CREATE INDEX idx_artifacts_search_vector
    ON artifacts USING gin (
        to_tsvector('english', coalesce(name, ''))
    );

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_search_vector;
DROP INDEX IF EXISTS idx_ticket_events_search_vector;
DROP INDEX IF EXISTS idx_attempts_search_vector;
DROP INDEX IF EXISTS idx_tickets_search_vector;
