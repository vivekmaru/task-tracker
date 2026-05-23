-- name: SearchTickets :many
WITH search_query AS (
    SELECT websearch_to_tsquery('english', sqlc.arg('query')::text) AS query
),
matches AS (
    SELECT
        t.id AS ticket_id,
        'ticket'::text AS source,
        concat_ws(' ', t.title, t.description) AS match_text,
        ts_rank_cd(
            to_tsvector('english', coalesce(t.title, '') || ' ' || coalesce(t.description, '')),
            sq.query
        ) AS rank
    FROM tickets t
    CROSS JOIN search_query sq
    WHERE t.workspace_id = sqlc.arg('workspace_id')::uuid
      AND t.project_id = sqlc.arg('project_id')::uuid
      AND to_tsvector('english', coalesce(t.title, '') || ' ' || coalesce(t.description, '')) @@ sq.query

    UNION ALL

    SELECT
        a.ticket_id,
        'attempt'::text AS source,
        concat_ws(' ', a.current_summary, a.output::text) AS match_text,
        ts_rank_cd(
            to_tsvector('english', coalesce(a.current_summary, '') || ' ' || a.output::text),
            sq.query
        ) AS rank
    FROM attempts a
    CROSS JOIN search_query sq
    WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
      AND a.project_id = sqlc.arg('project_id')::uuid
      AND to_tsvector('english', coalesce(a.current_summary, '') || ' ' || a.output::text) @@ sq.query

    UNION ALL

    SELECT
        e.ticket_id,
        'event'::text AS source,
        e.data::text AS match_text,
        ts_rank_cd(to_tsvector('english', e.data::text), sq.query) AS rank
    FROM ticket_events e
    CROSS JOIN search_query sq
    WHERE e.workspace_id = sqlc.arg('workspace_id')::uuid
      AND e.project_id = sqlc.arg('project_id')::uuid
      AND to_tsvector('english', e.data::text) @@ sq.query

    UNION ALL

    SELECT
        ar.ticket_id,
        'artifact'::text AS source,
        ar.name AS match_text,
        ts_rank_cd(to_tsvector('english', coalesce(ar.name, '')), sq.query) AS rank
    FROM artifacts ar
    CROSS JOIN search_query sq
    WHERE ar.workspace_id = sqlc.arg('workspace_id')::uuid
      AND ar.project_id = sqlc.arg('project_id')::uuid
      AND to_tsvector('english', coalesce(ar.name, '')) @@ sq.query
)
SELECT
    t.id,
    t.workspace_id,
    t.project_id,
    t.parent_id,
    t.root_id,
    t.source_attempt_id,
    t.source_artifact_id,
    t.title,
    t.description,
    t.type,
    t.status,
    t.priority,
    t.tags,
    t.acceptance_criteria,
    t.verification_commands,
    t.expected_artifacts,
    t.relevant_paths,
    t.required_tools,
    t.required_permissions,
    t.environment,
    t.input,
    t.input_schema,
    t.required_capabilities,
    t.allowed_harnesses,
    t.retry_policy,
    t.created_by,
    t.created_by_id,
    t.creation_reason,
    t.created_at,
    t.updated_at,
    array_agg(DISTINCT m.source ORDER BY m.source)::text[] AS match_sources,
    string_agg(DISTINCT left(m.match_text, 360), ' | ')::text AS snippet,
    max(m.rank)::real AS rank
FROM matches m
JOIN tickets t ON t.id = m.ticket_id
WHERE t.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.project_id = sqlc.arg('project_id')::uuid
GROUP BY t.id
ORDER BY rank DESC, t.updated_at DESC, t.id ASC
LIMIT sqlc.arg('limit')::integer
OFFSET sqlc.arg('offset')::integer;

-- name: SearchRelatedTickets :many
WITH source_ticket AS (
    SELECT
        t.id,
        t.workspace_id,
        t.project_id,
        concat_ws(
            ' ',
            t.title,
            t.description,
            array_to_string(t.tags, ' '),
            array_to_string(t.acceptance_criteria, ' '),
            array_to_string(t.relevant_paths, ' ')
        ) AS search_text
    FROM tickets t
    WHERE t.id = sqlc.arg('ticket_id')::uuid
),
search_query AS (
    SELECT COALESCE(string_agg(query_text, ' | ' ORDER BY query_text)::tsquery, ''::tsquery) AS query
    FROM (
        SELECT DISTINCT plainto_tsquery('english', lexeme)::text AS query_text
        FROM source_ticket st
        CROSS JOIN LATERAL unnest(tsvector_to_array(to_tsvector('english', st.search_text))) AS lexeme
        WHERE plainto_tsquery('english', lexeme) <> ''::tsquery
        ORDER BY query_text
        LIMIT 64
    ) terms
),
matches AS (
    SELECT
        t.id AS ticket_id,
        NULL::uuid AS attempt_id,
        'ticket'::text AS source,
        concat_ws(' ', t.title, t.description) AS match_text,
        ts_rank_cd(
            to_tsvector('english', coalesce(t.title, '') || ' ' || coalesce(t.description, '')),
            sq.query
        ) AS rank
    FROM tickets t
    JOIN source_ticket st ON st.workspace_id = t.workspace_id AND st.project_id = t.project_id
    CROSS JOIN search_query sq
    WHERE t.id <> st.id
      AND to_tsvector('english', coalesce(t.title, '') || ' ' || coalesce(t.description, '')) @@ sq.query

    UNION ALL

    SELECT
        a.ticket_id,
        a.id AS attempt_id,
        'attempt'::text AS source,
        concat_ws(' ', a.current_summary, a.output::text) AS match_text,
        ts_rank_cd(
            to_tsvector('english', coalesce(a.current_summary, '') || ' ' || a.output::text),
            sq.query
        ) AS rank
    FROM attempts a
    JOIN source_ticket st ON st.workspace_id = a.workspace_id AND st.project_id = a.project_id
    CROSS JOIN search_query sq
    WHERE a.ticket_id <> st.id
      AND to_tsvector('english', coalesce(a.current_summary, '') || ' ' || a.output::text) @@ sq.query

    UNION ALL

    SELECT
        e.ticket_id,
        e.attempt_id,
        'event'::text AS source,
        e.data::text AS match_text,
        ts_rank_cd(to_tsvector('english', e.data::text), sq.query) AS rank
    FROM ticket_events e
    JOIN source_ticket st ON st.workspace_id = e.workspace_id AND st.project_id = e.project_id
    CROSS JOIN search_query sq
    WHERE e.ticket_id <> st.id
      AND to_tsvector('english', e.data::text) @@ sq.query

    UNION ALL

    SELECT
        ar.ticket_id,
        ar.attempt_id,
        'artifact'::text AS source,
        ar.name AS match_text,
        ts_rank_cd(to_tsvector('english', coalesce(ar.name, '')), sq.query) AS rank
    FROM artifacts ar
    JOIN source_ticket st ON st.workspace_id = ar.workspace_id AND st.project_id = ar.project_id
    CROSS JOIN search_query sq
    WHERE ar.ticket_id <> st.id
      AND to_tsvector('english', coalesce(ar.name, '')) @@ sq.query
)
SELECT
    t.id,
    t.workspace_id,
    t.project_id,
    t.parent_id,
    t.root_id,
    t.source_attempt_id,
    t.source_artifact_id,
    t.title,
    t.description,
    t.type,
    t.status,
    t.priority,
    t.tags,
    t.acceptance_criteria,
    t.verification_commands,
    t.expected_artifacts,
    t.relevant_paths,
    t.required_tools,
    t.required_permissions,
    t.environment,
    t.input,
    t.input_schema,
    t.required_capabilities,
    t.allowed_harnesses,
    t.retry_policy,
    t.created_by,
    t.created_by_id,
    t.creation_reason,
    t.created_at,
    t.updated_at,
    array_agg(DISTINCT m.source ORDER BY m.source)::text[] AS match_sources,
    array_remove(array_agg(DISTINCT m.attempt_id), NULL)::uuid[] AS attempt_ids,
    string_agg(DISTINCT left(m.match_text, 360), ' | ')::text AS snippet,
    max(m.rank)::real AS rank
FROM matches m
JOIN tickets t ON t.id = m.ticket_id
JOIN source_ticket st ON st.workspace_id = t.workspace_id AND st.project_id = t.project_id
WHERE t.id <> st.id
GROUP BY t.id
ORDER BY rank DESC, t.updated_at DESC, t.id ASC
LIMIT sqlc.arg('limit')::integer
OFFSET sqlc.arg('offset')::integer;
