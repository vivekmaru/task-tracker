package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const initialMigration = "../../sql/migrations/0001_initial_schema.sql"

func TestInitialMigrationDefinesCoreTables(t *testing.T) {
	sql := readInitialMigration(t)

	for _, want := range []string{
		"-- +goose up",
		"-- +goose down",
		"create table workspaces",
		"create table projects",
		"create table tickets",
		"create table ticket_dependencies",
		"create table attempts",
		"create table attempt_checkpoints",
		"create table ticket_events",
		"create table artifacts",
		"create table idempotency_keys",
		"create table agent_capabilities",
		"create table api_keys",
		"create table attempt_metrics",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected migration to contain %q", want)
		}
	}
}

func TestForwardMigrationAddsWorkspaceMembers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0005_workspace_members.sql"))
	if err != nil {
		t.Fatalf("read workspace members migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"create table workspace_members",
		"workspace_id uuid not null references workspaces(id) on delete cascade",
		"actor_type text not null",
		"actor_id text not null",
		"role text not null",
		"primary key (workspace_id, actor_type, actor_id)",
		"check (actor_type in ('human', 'agent', 'system'))",
		"check (role in ('owner', 'admin', 'member', 'viewer'))",
		"idx_workspace_members_actor",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected workspace members migration to contain %q", want)
		}
	}
}

func TestInitialMigrationDefinesTicketAndAttemptFields(t *testing.T) {
	sql := readInitialMigration(t)

	for _, want := range []string{
		"acceptance_criteria text[]",
		"verification_commands jsonb",
		"expected_artifacts text[]",
		"required_tools text[]",
		"required_permissions text[]",
		"retry_policy jsonb",
		"allowed_harnesses text[]",
		"required_capabilities text[]",
		"lease_expires_at timestamptz",
		"last_heartbeat_at timestamptz",
		"progress_percent integer",
		"failure_category text",
		"blocker jsonb",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected migration to contain %q", want)
		}
	}
}

func TestInitialMigrationDefinesClaimAndDependencyIndexes(t *testing.T) {
	sql := readInitialMigration(t)

	for _, want := range []string{
		"idx_tickets_claim_queue",
		"idx_tickets_tags",
		"idx_ticket_dependencies_ticket_id",
		"idx_attempts_running_by_ticket",
		"idx_idempotency_keys_lookup",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected migration to contain %q", want)
		}
	}
}

func TestForwardMigrationAddsHumanTransitionEventTypes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0002_ticket_transition_event_types.sql"))
	if err != nil {
		t.Fatalf("read event-type migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"drop constraint if exists ticket_events_type_check",
		"ready",
		"reopened",
		"unblocked",
		"review_requested",
		"reviewed",
		"archived",
		"downgraded_type",
		"type = 'updated'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected event-type migration to contain %q", want)
		}
	}
}

func TestForwardMigrationAddsFullTextSearchIndexes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0003_full_text_search.sql"))
	if err != nil {
		t.Fatalf("read search migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"using gin",
		"idx_tickets_search_vector",
		"idx_attempts_search_vector",
		"idx_ticket_events_search_vector",
		"idx_artifacts_search_vector",
		"to_tsvector('english'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected search migration to contain %q", want)
		}
	}
}

func TestForwardMigrationAddsWebhookDeliveryQueue(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0004_webhook_deliveries.sql"))
	if err != nil {
		t.Fatalf("read webhook migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"create table webhook_subscriptions",
		"create table webhook_deliveries",
		"unique (subscription_id, event_id)",
		"create trigger trg_enqueue_webhook_deliveries_for_ticket_event",
		"after insert on ticket_events",
		"insert into webhook_deliveries",
		"jsonb_build_object",
		"new.type = any(s.event_types)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected webhook migration to contain %q", want)
		}
	}
}

func TestForwardMigrationSnapshotsWebhookObservabilityPayloads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0007_webhook_observability_snapshots.sql"))
	if err != nil {
		t.Fatalf("read webhook snapshot migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"create or replace function enqueue_webhook_deliveries_for_ticket_event",
		"'attempt', case",
		"'metrics', case",
		"left join attempts a on a.id = new.attempt_id",
		"left join attempt_metrics m on m.attempt_id = new.attempt_id",
		"tokens_in",
		"duration_seconds",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected webhook snapshot migration to contain %q", want)
		}
	}
}

func TestForwardMigrationAddsTicketEventSequence(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", "0006_ticket_event_sequence.sql"))
	if err != nil {
		t.Fatalf("read event sequence migration: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"alter table ticket_events",
		"add column event_sequence bigint",
		"ticket_events_event_sequence_seq",
		"idx_ticket_events_event_sequence",
		"order by created_at asc, id asc",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected event sequence migration to contain %q", want)
		}
	}
}

func readInitialMigration(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(initialMigration)
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	return strings.ToLower(string(data))
}
