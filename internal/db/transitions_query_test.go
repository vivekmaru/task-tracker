package db

import (
	"os"
	"strings"
	"testing"
)

func TestTerminalTransitionQueriesWriteAttemptTicketAndEvents(t *testing.T) {
	data, err := os.ReadFile("../../sql/queries/transitions.sql")
	if err != nil {
		t.Fatalf("read transition queries: %v", err)
	}
	sql := strings.ToLower(string(data))

	for _, want := range []string{
		"-- name: completeattempt",
		"-- name: failattempt",
		"-- name: blockattempt",
		"-- name: cancelattempt",
		"-- name: expireattempt",
		"and a.status = 'running'",
		"update tickets",
		"insert into ticket_events",
		"'succeeded'",
		"'failed'",
		"'blocked'",
		"'cancelled'",
		"'expired'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected transition query file to contain %q", want)
		}
	}
}
