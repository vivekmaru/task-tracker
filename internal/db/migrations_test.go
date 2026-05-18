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

func readInitialMigration(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(initialMigration)
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	return strings.ToLower(string(data))
}
