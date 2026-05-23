package services

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestBuildObservabilityPayloadMapsTicketEventAttemptAndMetrics(t *testing.T) {
	occurredAt := time.Date(2026, 5, 23, 7, 30, 0, 0, time.UTC)
	startedAt := occurredAt.Add(-3 * time.Minute)
	completedAt := occurredAt.Add(-10 * time.Second)

	payload, err := BuildObservabilityPayload(ObservabilityPayloadInput{
		EventID:     testUUID(71),
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		TicketID:    testUUID(3),
		AttemptID:   testUUID(4),
		EventType:   EventAttemptCompleted,
		ActorType:   ActorAgent,
		ActorID:     "codex-worker",
		EventData:   json.RawMessage(`{"output_schema":"summary.v1"}`),
		OccurredAt:  occurredAt,
		Attempt: &db.Attempt{
			ID:              testUUID(4),
			AgentID:         "codex-worker",
			Harness:         "codex",
			Model:           "gpt-5",
			Status:          AttemptStatusSucceeded,
			ProgressPercent: 100,
			TraceID:         pgtype.Text{String: "trace-123", Valid: true},
			CheckpointRef:   pgtype.Text{String: "checkpoint-456", Valid: true},
			StartedAt:       pgtype.Timestamptz{Time: startedAt, Valid: true},
			CompletedAt:     pgtype.Timestamptz{Time: completedAt, Valid: true},
		},
		Metrics: &db.AttemptMetric{
			TokensIn:        1200,
			TokensOut:       345,
			CostUsd:         numeric(0.123456),
			DurationSeconds: numeric(170.25),
			RetryCount:      2,
		},
	})
	if err != nil {
		t.Fatalf("build observability payload: %v", err)
	}

	if payload.SchemaVersion != "forge.observability.v1" || payload.Signal != "ticket_event" || payload.Source != "forge" {
		t.Fatalf("unexpected envelope fields: %#v", payload)
	}
	if payload.Event.ID != "00000000-0000-0000-0000-000000000047" || payload.Event.Type != EventAttemptCompleted {
		t.Fatalf("unexpected event mapping: %#v", payload.Event)
	}
	if payload.WorkspaceID != "00000000-0000-0000-0000-000000000001" || payload.ProjectID != "00000000-0000-0000-0000-000000000002" || payload.TicketID != "00000000-0000-0000-0000-000000000003" {
		t.Fatalf("unexpected scope mapping: %#v", payload)
	}
	if payload.Attempt == nil || payload.Attempt.AgentID != "codex-worker" || payload.Attempt.Harness != "codex" || payload.Attempt.Model != "gpt-5" {
		t.Fatalf("expected attempt details, got %#v", payload.Attempt)
	}
	if payload.Attempt.TraceID != "trace-123" || payload.Attempt.CheckpointRef != "checkpoint-456" {
		t.Fatalf("expected trace fields, got %#v", payload.Attempt)
	}
	if payload.Metrics == nil || payload.Metrics.TokensIn != 1200 || payload.Metrics.TokensOut != 345 || payload.Metrics.TotalTokens != 1545 {
		t.Fatalf("expected token metrics, got %#v", payload.Metrics)
	}
	if payload.Metrics.CostUSD != 0.123456 || payload.Metrics.DurationSeconds != 170.25 || payload.Metrics.RetryCount != 2 {
		t.Fatalf("expected cost/duration metrics, got %#v", payload.Metrics)
	}
}

func TestBuildObservabilityPayloadOmitsMissingAttemptAndMetrics(t *testing.T) {
	payload, err := BuildObservabilityPayload(ObservabilityPayloadInput{
		EventID:     testUUID(72),
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		TicketID:    testUUID(3),
		EventType:   EventTicketUpdated,
		ActorType:   ActorHuman,
		EventData:   json.RawMessage(`{}`),
		OccurredAt:  time.Date(2026, 5, 23, 7, 45, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build observability payload: %v", err)
	}

	if payload.Attempt != nil {
		t.Fatalf("did not expect attempt section, got %#v", payload.Attempt)
	}
	if payload.Metrics != nil {
		t.Fatalf("did not expect metrics section, got %#v", payload.Metrics)
	}
	if payload.AttemptID != "" {
		t.Fatalf("did not expect attempt_id, got %q", payload.AttemptID)
	}
}
