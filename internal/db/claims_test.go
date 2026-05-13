package db

import (
	"os"
	"strings"
	"testing"
)

func TestClaimNextQueryKeepsCorrectnessInPostgres(t *testing.T) {
	data, err := os.ReadFile("../../sql/queries/claims.sql")
	if err != nil {
		t.Fatalf("read claim query: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"for update skip locked",
		"t.status = 'todo'",
		"dep.status != 'done'",
		"t.required_capabilities <@",
		"any(t.allowed_harnesses)",
		"insert into attempts",
		"insert into ticket_events",
		"set status = 'in_progress'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected claim query to contain %q", want)
		}
	}
}
