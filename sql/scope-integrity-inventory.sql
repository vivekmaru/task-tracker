-- Read-only scope mismatch inventory. It returns relationship names, counts,
-- and sample IDs only. Run before 0013 if the migration preflight fails.
WITH mismatches AS (
    SELECT 'ticket_project' AS relationship, t.id AS row_id FROM tickets t JOIN projects p ON p.id = t.project_id WHERE p.workspace_id <> t.workspace_id
    UNION ALL SELECT 'ticket_parent', t.id FROM tickets t JOIN tickets parent ON parent.id = t.parent_id WHERE parent.workspace_id <> t.workspace_id OR parent.project_id <> t.project_id
    UNION ALL SELECT 'ticket_root', t.id FROM tickets t JOIN tickets root ON root.id = t.root_id WHERE root.workspace_id <> t.workspace_id OR root.project_id <> t.project_id
    UNION ALL SELECT 'ticket_source_attempt', t.id FROM tickets t JOIN attempts a ON a.id = t.source_attempt_id WHERE a.workspace_id <> t.workspace_id OR a.project_id <> t.project_id
    UNION ALL SELECT 'ticket_source_artifact', t.id FROM tickets t JOIN artifacts a ON a.id = t.source_artifact_id WHERE a.workspace_id <> t.workspace_id OR a.project_id <> t.project_id
    UNION ALL SELECT 'dependency_endpoint', d.ticket_id FROM ticket_dependencies d JOIN tickets t ON t.id = d.ticket_id JOIN tickets dependency ON dependency.id = d.depends_on_ticket_id WHERE t.workspace_id <> d.workspace_id OR t.project_id <> d.project_id OR dependency.workspace_id <> d.workspace_id OR dependency.project_id <> d.project_id
    UNION ALL SELECT 'attempt_ticket', a.id FROM attempts a JOIN tickets t ON t.id = a.ticket_id WHERE t.workspace_id <> a.workspace_id OR t.project_id <> a.project_id
    UNION ALL SELECT 'checkpoint_attempt', c.id FROM attempt_checkpoints c JOIN attempts a ON a.id = c.attempt_id WHERE a.workspace_id <> c.workspace_id OR a.project_id <> c.project_id OR a.ticket_id <> c.ticket_id
    UNION ALL SELECT 'event_ticket_or_attempt', e.id FROM ticket_events e JOIN tickets t ON t.id = e.ticket_id LEFT JOIN attempts a ON a.id = e.attempt_id WHERE t.workspace_id <> e.workspace_id OR t.project_id <> e.project_id OR (a.id IS NOT NULL AND (a.workspace_id <> e.workspace_id OR a.project_id <> e.project_id OR a.ticket_id <> e.ticket_id))
    UNION ALL SELECT 'artifact_ticket_or_attempt', a.id FROM artifacts a JOIN tickets t ON t.id = a.ticket_id LEFT JOIN attempts attempt ON attempt.id = a.attempt_id WHERE t.workspace_id <> a.workspace_id OR t.project_id <> a.project_id OR (attempt.id IS NOT NULL AND (attempt.workspace_id <> a.workspace_id OR attempt.project_id <> a.project_id OR attempt.ticket_id <> a.ticket_id))
    UNION ALL SELECT 'attempt_metric', m.id FROM attempt_metrics m JOIN attempts a ON a.id = m.attempt_id WHERE a.workspace_id <> m.workspace_id OR a.project_id <> m.project_id
    UNION ALL SELECT 'webhook_delivery', d.id FROM webhook_deliveries d JOIN webhook_subscriptions s ON s.id = d.subscription_id JOIN ticket_events e ON e.id = d.event_id JOIN tickets t ON t.id = d.ticket_id LEFT JOIN attempts a ON a.id = d.attempt_id WHERE s.workspace_id <> d.workspace_id OR s.project_id <> d.project_id OR e.workspace_id <> d.workspace_id OR e.project_id <> d.project_id OR e.ticket_id <> d.ticket_id OR t.workspace_id <> d.workspace_id OR t.project_id <> d.project_id OR (a.id IS NOT NULL AND (a.workspace_id <> d.workspace_id OR a.project_id <> d.project_id OR a.ticket_id <> d.ticket_id))
)
SELECT relationship, count(*) AS mismatch_count, array_agg(row_id ORDER BY row_id)[1:20] AS sample_ids
FROM mismatches
GROUP BY relationship
ORDER BY relationship;
