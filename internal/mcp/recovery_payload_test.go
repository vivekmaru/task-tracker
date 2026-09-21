package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestClaimHistoryPreservesRecoveryDetails(t *testing.T) {
	bundle := services.ClaimContextBundle{PriorAttempts: []db.Attempt{{
		FailureReason:   pgtype.Text{String: "Test database unavailable", Valid: true},
		FailureCategory: pgtype.Text{String: "environment_failed", Valid: true},
		CurrentSummary:  pgtype.Text{String: "Reproduced the timeout", Valid: true},
		NextStep:        pgtype.Text{String: "Restore database", Valid: true},
		Output:          []byte(`{"exit_code":1}`),
	}}}
	payload, err := json.Marshal(claimContextPayload(bundle))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"failure_reason":"Test database unavailable"`, `"failure_category":"environment_failed"`, `"current_summary":"Reproduced the timeout"`, `"next_step":"Restore database"`, `"output":{"exit_code":1}`} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("missing recovery detail %s", want)
		}
	}
}
