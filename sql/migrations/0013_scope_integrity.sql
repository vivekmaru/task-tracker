-- +goose Up
-- Do not repair mismatches here. Operators must inspect and remediate the
-- affected rows before re-running this migration.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM tickets t JOIN projects p ON p.id = t.project_id WHERE p.workspace_id <> t.workspace_id
        UNION ALL
        SELECT 1 FROM tickets t JOIN tickets parent ON parent.id = t.parent_id WHERE parent.workspace_id <> t.workspace_id OR parent.project_id <> t.project_id
        UNION ALL
        SELECT 1 FROM tickets t JOIN tickets root ON root.id = t.root_id WHERE root.workspace_id <> t.workspace_id OR root.project_id <> t.project_id
        UNION ALL
        SELECT 1 FROM tickets t JOIN attempts a ON a.id = t.source_attempt_id WHERE a.workspace_id <> t.workspace_id OR a.project_id <> t.project_id
        UNION ALL
        SELECT 1 FROM tickets t JOIN artifacts a ON a.id = t.source_artifact_id WHERE a.workspace_id <> t.workspace_id OR a.project_id <> t.project_id
        UNION ALL
        SELECT 1 FROM ticket_dependencies d
            JOIN tickets t ON t.id = d.ticket_id
            JOIN tickets dependency ON dependency.id = d.depends_on_ticket_id
            WHERE t.workspace_id <> d.workspace_id OR t.project_id <> d.project_id
               OR dependency.workspace_id <> d.workspace_id OR dependency.project_id <> d.project_id
        UNION ALL
        SELECT 1 FROM attempts a JOIN tickets t ON t.id = a.ticket_id WHERE t.workspace_id <> a.workspace_id OR t.project_id <> a.project_id
        UNION ALL
        SELECT 1 FROM attempt_checkpoints c JOIN attempts a ON a.id = c.attempt_id
            WHERE a.workspace_id <> c.workspace_id OR a.project_id <> c.project_id OR a.ticket_id <> c.ticket_id
        UNION ALL
        SELECT 1 FROM ticket_events e
            JOIN tickets t ON t.id = e.ticket_id
            LEFT JOIN attempts a ON a.id = e.attempt_id
            WHERE t.workspace_id <> e.workspace_id OR t.project_id <> e.project_id
               OR (a.id IS NOT NULL AND (a.workspace_id <> e.workspace_id OR a.project_id <> e.project_id OR a.ticket_id <> e.ticket_id))
        UNION ALL
        SELECT 1 FROM artifacts a
            JOIN tickets t ON t.id = a.ticket_id
            LEFT JOIN attempts attempt ON attempt.id = a.attempt_id
            WHERE t.workspace_id <> a.workspace_id OR t.project_id <> a.project_id
               OR (attempt.id IS NOT NULL AND (attempt.workspace_id <> a.workspace_id OR attempt.project_id <> a.project_id OR attempt.ticket_id <> a.ticket_id))
        UNION ALL
        SELECT 1 FROM attempt_metrics m JOIN attempts a ON a.id = m.attempt_id
            WHERE a.workspace_id <> m.workspace_id OR a.project_id <> m.project_id
        UNION ALL
        SELECT 1 FROM agent_capabilities c JOIN projects p ON p.id = c.project_id WHERE p.workspace_id <> c.workspace_id
        UNION ALL
        SELECT 1 FROM webhook_subscriptions s JOIN projects p ON p.id = s.project_id WHERE p.workspace_id <> s.workspace_id
        UNION ALL
        SELECT 1 FROM webhook_deliveries d
            JOIN webhook_subscriptions s ON s.id = d.subscription_id
            JOIN ticket_events e ON e.id = d.event_id
            JOIN tickets t ON t.id = d.ticket_id
            LEFT JOIN attempts a ON a.id = d.attempt_id
            WHERE s.workspace_id <> d.workspace_id OR s.project_id <> d.project_id
               OR e.workspace_id <> d.workspace_id OR e.project_id <> d.project_id OR e.ticket_id <> d.ticket_id
               OR t.workspace_id <> d.workspace_id OR t.project_id <> d.project_id
               OR (a.id IS NOT NULL AND (a.workspace_id <> d.workspace_id OR a.project_id <> d.project_id OR a.ticket_id <> d.ticket_id))
    ) THEN
        RAISE EXCEPTION 'scope integrity preflight failed; run sql/scope-integrity-inventory.sql and remediate mismatched rows before retrying';
    END IF;
END $$;

ALTER TABLE projects ADD CONSTRAINT projects_workspace_id_id_key UNIQUE (workspace_id, id);
ALTER TABLE tickets ADD CONSTRAINT tickets_scope_id_key UNIQUE (workspace_id, project_id, id);
ALTER TABLE attempts ADD CONSTRAINT attempts_scope_id_key UNIQUE (workspace_id, project_id, id);
ALTER TABLE attempts ADD CONSTRAINT attempts_ticket_scope_id_key UNIQUE (workspace_id, project_id, ticket_id, id);
ALTER TABLE artifacts ADD CONSTRAINT artifacts_scope_id_key UNIQUE (workspace_id, project_id, id);
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_ticket_scope_id_key UNIQUE (workspace_id, project_id, ticket_id, id);
ALTER TABLE webhook_subscriptions ADD CONSTRAINT webhook_subscriptions_scope_id_key UNIQUE (workspace_id, project_id, id);

ALTER TABLE tickets
    ADD CONSTRAINT tickets_project_scope_fk FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT tickets_parent_scope_fk FOREIGN KEY (workspace_id, project_id, parent_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE SET NULL NOT VALID,
    ADD CONSTRAINT tickets_root_scope_fk FOREIGN KEY (workspace_id, project_id, root_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE SET NULL NOT VALID,
    ADD CONSTRAINT tickets_source_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, source_attempt_id) REFERENCES attempts(workspace_id, project_id, id) ON DELETE SET NULL NOT VALID,
    ADD CONSTRAINT tickets_source_artifact_scope_fk FOREIGN KEY (workspace_id, project_id, source_artifact_id) REFERENCES artifacts(workspace_id, project_id, id) ON DELETE SET NULL NOT VALID;

ALTER TABLE ticket_dependencies
    ADD CONSTRAINT ticket_dependencies_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT ticket_dependencies_dependency_scope_fk FOREIGN KEY (workspace_id, project_id, depends_on_ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID;

ALTER TABLE attempts ADD CONSTRAINT attempts_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE attempt_checkpoints
    ADD CONSTRAINT attempt_checkpoints_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT attempt_checkpoints_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id, attempt_id) REFERENCES attempts(workspace_id, project_id, ticket_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE ticket_events
    ADD CONSTRAINT ticket_events_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT ticket_events_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id, attempt_id) REFERENCES attempts(workspace_id, project_id, ticket_id, id) ON DELETE SET NULL NOT VALID;
ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT artifacts_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id, attempt_id) REFERENCES attempts(workspace_id, project_id, ticket_id, id) ON DELETE SET NULL NOT VALID;
ALTER TABLE attempt_metrics ADD CONSTRAINT attempt_metrics_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, attempt_id) REFERENCES attempts(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE agent_capabilities ADD CONSTRAINT agent_capabilities_project_scope_fk FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE webhook_subscriptions ADD CONSTRAINT webhook_subscriptions_project_scope_fk FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_subscription_scope_fk FOREIGN KEY (workspace_id, project_id, subscription_id) REFERENCES webhook_subscriptions(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT webhook_deliveries_event_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id, event_id) REFERENCES ticket_events(workspace_id, project_id, ticket_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT webhook_deliveries_ticket_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id) REFERENCES tickets(workspace_id, project_id, id) ON DELETE CASCADE NOT VALID,
    ADD CONSTRAINT webhook_deliveries_attempt_scope_fk FOREIGN KEY (workspace_id, project_id, ticket_id, attempt_id) REFERENCES attempts(workspace_id, project_id, ticket_id, id) ON DELETE SET NULL NOT VALID;

ALTER TABLE tickets VALIDATE CONSTRAINT tickets_project_scope_fk;
ALTER TABLE tickets VALIDATE CONSTRAINT tickets_parent_scope_fk;
ALTER TABLE tickets VALIDATE CONSTRAINT tickets_root_scope_fk;
ALTER TABLE tickets VALIDATE CONSTRAINT tickets_source_attempt_scope_fk;
ALTER TABLE tickets VALIDATE CONSTRAINT tickets_source_artifact_scope_fk;
ALTER TABLE ticket_dependencies VALIDATE CONSTRAINT ticket_dependencies_ticket_scope_fk;
ALTER TABLE ticket_dependencies VALIDATE CONSTRAINT ticket_dependencies_dependency_scope_fk;
ALTER TABLE attempts VALIDATE CONSTRAINT attempts_ticket_scope_fk;
ALTER TABLE attempt_checkpoints VALIDATE CONSTRAINT attempt_checkpoints_ticket_scope_fk;
ALTER TABLE attempt_checkpoints VALIDATE CONSTRAINT attempt_checkpoints_attempt_scope_fk;
ALTER TABLE ticket_events VALIDATE CONSTRAINT ticket_events_ticket_scope_fk;
ALTER TABLE ticket_events VALIDATE CONSTRAINT ticket_events_attempt_scope_fk;
ALTER TABLE artifacts VALIDATE CONSTRAINT artifacts_ticket_scope_fk;
ALTER TABLE artifacts VALIDATE CONSTRAINT artifacts_attempt_scope_fk;
ALTER TABLE attempt_metrics VALIDATE CONSTRAINT attempt_metrics_attempt_scope_fk;
ALTER TABLE agent_capabilities VALIDATE CONSTRAINT agent_capabilities_project_scope_fk;
ALTER TABLE webhook_subscriptions VALIDATE CONSTRAINT webhook_subscriptions_project_scope_fk;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_subscription_scope_fk;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_event_scope_fk;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_ticket_scope_fk;
ALTER TABLE webhook_deliveries VALIDATE CONSTRAINT webhook_deliveries_attempt_scope_fk;

-- +goose Down
ALTER TABLE webhook_deliveries
    DROP CONSTRAINT IF EXISTS webhook_deliveries_attempt_scope_fk,
    DROP CONSTRAINT IF EXISTS webhook_deliveries_ticket_scope_fk,
    DROP CONSTRAINT IF EXISTS webhook_deliveries_event_scope_fk,
    DROP CONSTRAINT IF EXISTS webhook_deliveries_subscription_scope_fk;
ALTER TABLE webhook_subscriptions DROP CONSTRAINT IF EXISTS webhook_subscriptions_project_scope_fk;
ALTER TABLE agent_capabilities DROP CONSTRAINT IF EXISTS agent_capabilities_project_scope_fk;
ALTER TABLE attempt_metrics DROP CONSTRAINT IF EXISTS attempt_metrics_attempt_scope_fk;
ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_attempt_scope_fk, DROP CONSTRAINT IF EXISTS artifacts_ticket_scope_fk;
ALTER TABLE ticket_events DROP CONSTRAINT IF EXISTS ticket_events_attempt_scope_fk, DROP CONSTRAINT IF EXISTS ticket_events_ticket_scope_fk;
ALTER TABLE attempt_checkpoints DROP CONSTRAINT IF EXISTS attempt_checkpoints_attempt_scope_fk, DROP CONSTRAINT IF EXISTS attempt_checkpoints_ticket_scope_fk;
ALTER TABLE attempts DROP CONSTRAINT IF EXISTS attempts_ticket_scope_fk;
ALTER TABLE ticket_dependencies DROP CONSTRAINT IF EXISTS ticket_dependencies_dependency_scope_fk, DROP CONSTRAINT IF EXISTS ticket_dependencies_ticket_scope_fk;
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_source_artifact_scope_fk, DROP CONSTRAINT IF EXISTS tickets_source_attempt_scope_fk, DROP CONSTRAINT IF EXISTS tickets_root_scope_fk, DROP CONSTRAINT IF EXISTS tickets_parent_scope_fk, DROP CONSTRAINT IF EXISTS tickets_project_scope_fk;
ALTER TABLE webhook_subscriptions DROP CONSTRAINT IF EXISTS webhook_subscriptions_scope_id_key;
ALTER TABLE ticket_events DROP CONSTRAINT IF EXISTS ticket_events_ticket_scope_id_key;
ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_scope_id_key;
ALTER TABLE attempts DROP CONSTRAINT IF EXISTS attempts_ticket_scope_id_key, DROP CONSTRAINT IF EXISTS attempts_scope_id_key;
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_scope_id_key;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_workspace_id_id_key;
