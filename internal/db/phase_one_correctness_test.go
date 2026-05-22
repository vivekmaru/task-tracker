package db

import (
	"os"
	"strings"
	"testing"
)

func TestPhaseOneCorrectnessNoDuplicateClaims(t *testing.T) {
	claimSQL := readSQLFile(t, "../../sql/queries/claims.sql")
	migrationSQL := readSQLFile(t, "../../sql/migrations/0001_initial_schema.sql")

	for _, want := range []string{
		"for update skip locked",
		"limit 1",
		"set status = 'in_progress'",
		"insert into attempts",
		"insert into ticket_events",
	} {
		if !strings.Contains(claimSQL, want) {
			t.Fatalf("claim query must contain %q", want)
		}
	}
	for _, want := range []string{
		"create unique index idx_attempts_running_by_ticket",
		"where status = 'running'",
	} {
		if !strings.Contains(migrationSQL, want) {
			t.Fatalf("migration must contain %q", want)
		}
	}
}

func TestPhaseOneCorrectnessEligibilityFilters(t *testing.T) {
	claimSQL := readSQLFile(t, "../../sql/queries/claims.sql")

	for _, want := range []string{
		"t.status = 'todo'",
		"t.type =",
		"t.tags &&",
		"any(t.allowed_harnesses)",
		"t.required_capabilities <@",
		"dep.status != 'done'",
		"retry_policy->>'max_attempts'",
	} {
		if !strings.Contains(claimSQL, want) {
			t.Fatalf("claim query must contain eligibility guard %q", want)
		}
	}
}

func TestPhaseOneCorrectnessClaimOrdersHighestPriorityFirst(t *testing.T) {
	claimSQL := readSQLFile(t, "../../sql/queries/claims.sql")
	migrationSQL := readSQLFile(t, "../../sql/migrations/0001_initial_schema.sql")

	if !strings.Contains(claimSQL, "order by t.priority asc, t.created_at asc") {
		t.Fatalf("claim query must order by priority ASC because priority 0 is highest")
	}
	if !strings.Contains(migrationSQL, "status, priority asc, created_at asc") {
		t.Fatalf("claim queue index must match priority ASC claim order")
	}
}

func TestPhaseOneCorrectnessLeaseExpiryAndBlockedWork(t *testing.T) {
	transitionsSQL := readSQLFile(t, "../../sql/queries/transitions.sql")

	for _, want := range []string{
		"-- name: expireattempt",
		"set status = 'expired'",
		"else 'todo'",
		"'expired'",
		"-- name: blockattempt",
		"set status = 'blocked'",
		"'blocked'",
	} {
		if !strings.Contains(transitionsSQL, want) {
			t.Fatalf("transition query must contain %q", want)
		}
	}
}

func TestPhaseOneCorrectnessTerminalAttemptsRejectInvalidTransitions(t *testing.T) {
	transitionsSQL := readSQLFile(t, "../../sql/queries/transitions.sql")

	for _, queryName := range []string{
		"-- name: completeattempt",
		"-- name: failattempt",
		"-- name: blockattempt",
		"-- name: cancelattempt",
		"-- name: expireattempt",
	} {
		start := strings.Index(transitionsSQL, queryName)
		if start < 0 {
			t.Fatalf("missing transition query %q", queryName)
		}
		next := strings.Index(transitionsSQL[start+len(queryName):], "-- name:")
		query := transitionsSQL[start:]
		if next >= 0 {
			query = transitionsSQL[start : start+len(queryName)+next]
		}
		if !strings.Contains(query, "and a.status = 'running'") {
			t.Fatalf("%s must guard against terminal re-transition", queryName)
		}
	}
}

func TestPhaseOneCorrectnessClaimIdempotencyReplay(t *testing.T) {
	claimSQL := readSQLFile(t, "../../sql/queries/claims.sql")

	for _, want := range []string{
		"insert into idempotency_keys",
		"'claim-next'",
		"request_hash",
		"jsonb_build_object",
		"'ticket_id'",
		"'attempt_id'",
	} {
		if !strings.Contains(claimSQL, want) {
			t.Fatalf("claim query must contain idempotency behavior %q", want)
		}
	}
}

func TestPhaseFourSearchQueryCoversExecutionHistory(t *testing.T) {
	searchSQL := readSQLFile(t, "../../sql/queries/search.sql")

	for _, want := range []string{
		"websearch_to_tsquery",
		"from tickets t",
		"from attempts a",
		"from ticket_events e",
		"from artifacts ar",
		"'ticket'::text as source",
		"'attempt'::text as source",
		"'event'::text as source",
		"'artifact'::text as source",
		"array_agg(distinct m.source",
		"string_agg(distinct left(m.match_text, 360)",
		"join tickets t on t.id = m.ticket_id",
		"where t.workspace_id = sqlc.arg('workspace_id')::uuid",
		"and t.project_id = sqlc.arg('project_id')::uuid",
	} {
		if !strings.Contains(searchSQL, want) {
			t.Fatalf("search query must contain %q", want)
		}
	}
}

func readSQLFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SQL file %s: %v", path, err)
	}
	return strings.ToLower(string(data))
}
