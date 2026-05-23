-- +goose Up
ALTER TABLE ticket_events
    ADD COLUMN event_sequence bigint;

CREATE SEQUENCE ticket_events_event_sequence_seq
    OWNED BY ticket_events.event_sequence;

WITH ordered_events AS (
    SELECT
        id,
        row_number() OVER (ORDER BY created_at ASC, id ASC)::bigint AS sequence_value
    FROM ticket_events
)
UPDATE ticket_events
SET event_sequence = ordered_events.sequence_value
FROM ordered_events
WHERE ticket_events.id = ordered_events.id;

SELECT setval(
    'ticket_events_event_sequence_seq',
    COALESCE((SELECT max(event_sequence) FROM ticket_events), 0) + 1,
    false
);

ALTER TABLE ticket_events
    ALTER COLUMN event_sequence SET DEFAULT nextval('ticket_events_event_sequence_seq'),
    ALTER COLUMN event_sequence SET NOT NULL;

CREATE UNIQUE INDEX idx_ticket_events_event_sequence
    ON ticket_events(event_sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_ticket_events_event_sequence;
ALTER TABLE ticket_events
    ALTER COLUMN event_sequence DROP DEFAULT,
    DROP COLUMN IF EXISTS event_sequence;
DROP SEQUENCE IF EXISTS ticket_events_event_sequence_seq;
