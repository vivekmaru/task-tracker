package db

import (
	"os"
	"strings"
	"testing"
)

func TestAttemptMutationQueriesWriteEvents(t *testing.T) {
	data, err := os.ReadFile("../../sql/queries/attempts.sql")
	if err != nil {
		t.Fatalf("read attempts queries: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"-- name: heartbeatattempt",
		"'heartbeat'",
		"insert into ticket_events",
		"lease_expires_at",
		"-- name: checkpointattempt",
		"'checkpointed'",
		"insert into attempt_checkpoints",
		"progress_percent",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected attempt query file to contain %q", want)
		}
	}
}
