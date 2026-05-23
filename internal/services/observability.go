package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	ObservabilitySchemaVersion = "forge.observability.v1"
	ObservabilitySource        = "forge"
	ObservabilitySignalEvent   = "ticket_event"

	EventAttemptHeartbeat    = "heartbeat"
	EventAttemptCheckpointed = "checkpointed"
	EventAttemptCompleted    = "completed"
	EventAttemptFailed       = "failed"
	EventAttemptBlocked      = "blocked"
	EventAttemptExpired      = "expired"
)

type ObservabilityPayloadInput struct {
	EventID     pgtype.UUID
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	TicketID    pgtype.UUID
	AttemptID   pgtype.UUID
	EventType   string
	ActorType   string
	ActorID     string
	EventData   json.RawMessage
	OccurredAt  time.Time
	Attempt     *db.Attempt
	Metrics     *db.AttemptMetric
}

type ObservabilityPayload struct {
	SchemaVersion string `json:"schema_version"`
	Source        string `json:"source"`
	Signal        string `json:"signal"`
	WorkspaceID   string `json:"workspace_id"`
	ProjectID     string `json:"project_id"`
	TicketID      string `json:"ticket_id"`
	AttemptID     string `json:"attempt_id,omitempty"`
	Event         struct {
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		ActorType  string          `json:"actor_type"`
		ActorID    string          `json:"actor_id,omitempty"`
		Data       json.RawMessage `json:"data"`
		OccurredAt time.Time       `json:"occurred_at"`
	} `json:"event"`
	Attempt *ObservabilityAttempt        `json:"attempt,omitempty"`
	Metrics *ObservabilityAttemptMetrics `json:"metrics,omitempty"`
}

type ObservabilityAttempt struct {
	ID              string     `json:"id"`
	AgentID         string     `json:"agent_id"`
	Harness         string     `json:"harness"`
	Model           string     `json:"model,omitempty"`
	Status          string     `json:"status"`
	ProgressPercent int32      `json:"progress_percent"`
	TraceID         string     `json:"trace_id,omitempty"`
	CheckpointRef   string     `json:"checkpoint_ref,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type ObservabilityAttemptMetrics struct {
	TokensIn        int64   `json:"tokens_in"`
	TokensOut       int64   `json:"tokens_out"`
	TotalTokens     int64   `json:"total_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	DurationSeconds float64 `json:"duration_seconds"`
	RetryCount      int32   `json:"retry_count"`
}

func BuildObservabilityPayload(input ObservabilityPayloadInput) (ObservabilityPayload, error) {
	data, err := normalizeJSONData(input.EventData)
	if err != nil {
		return ObservabilityPayload{}, err
	}

	payload := ObservabilityPayload{
		SchemaVersion: ObservabilitySchemaVersion,
		Source:        ObservabilitySource,
		Signal:        ObservabilitySignalEvent,
		WorkspaceID:   uuidText(input.WorkspaceID),
		ProjectID:     uuidText(input.ProjectID),
		TicketID:      uuidText(input.TicketID),
		AttemptID:     uuidText(input.AttemptID),
	}
	payload.Event.ID = uuidText(input.EventID)
	payload.Event.Type = input.EventType
	payload.Event.ActorType = input.ActorType
	payload.Event.ActorID = input.ActorID
	payload.Event.Data = data
	payload.Event.OccurredAt = input.OccurredAt.UTC()

	if input.Attempt != nil {
		payload.Attempt = observabilityAttempt(*input.Attempt)
	}
	if input.Metrics != nil {
		payload.Metrics = observabilityMetrics(*input.Metrics)
	}
	return payload, nil
}

func BuildObservabilityPayloadFromWebhookDelivery(delivery db.ClaimPendingWebhookDeliveriesRow, attempt *db.Attempt, metrics *db.AttemptMetric) (ObservabilityPayload, error) {
	var raw struct {
		EventType  string          `json:"event_type"`
		ActorType  string          `json:"actor_type"`
		ActorID    *string         `json:"actor_id"`
		Data       json.RawMessage `json:"data"`
		OccurredAt time.Time       `json:"created_at"`
	}
	if len(delivery.Payload) > 0 {
		if err := json.Unmarshal(delivery.Payload, &raw); err != nil {
			return ObservabilityPayload{}, fmt.Errorf("decode webhook event payload: %w", err)
		}
	}

	actorID := ""
	if raw.ActorID != nil {
		actorID = *raw.ActorID
	}
	return BuildObservabilityPayload(ObservabilityPayloadInput{
		EventID:     delivery.EventID,
		WorkspaceID: delivery.WorkspaceID,
		ProjectID:   delivery.ProjectID,
		TicketID:    delivery.TicketID,
		AttemptID:   delivery.AttemptID,
		EventType:   raw.EventType,
		ActorType:   raw.ActorType,
		ActorID:     actorID,
		EventData:   raw.Data,
		OccurredAt:  raw.OccurredAt,
		Attempt:     attempt,
		Metrics:     metrics,
	})
}

func observabilityAttempt(attempt db.Attempt) *ObservabilityAttempt {
	return &ObservabilityAttempt{
		ID:              uuidText(attempt.ID),
		AgentID:         attempt.AgentID,
		Harness:         attempt.Harness,
		Model:           attempt.Model,
		Status:          attempt.Status,
		ProgressPercent: attempt.ProgressPercent,
		TraceID:         textValue(attempt.TraceID),
		CheckpointRef:   textValue(attempt.CheckpointRef),
		StartedAt:       timePtr(attempt.StartedAt),
		CompletedAt:     timePtr(attempt.CompletedAt),
	}
}

func observabilityMetrics(metrics db.AttemptMetric) *ObservabilityAttemptMetrics {
	return &ObservabilityAttemptMetrics{
		TokensIn:        metrics.TokensIn,
		TokensOut:       metrics.TokensOut,
		TotalTokens:     metrics.TokensIn + metrics.TokensOut,
		CostUSD:         numericFloat(metrics.CostUsd),
		DurationSeconds: numericFloat(metrics.DurationSeconds),
		RetryCount:      metrics.RetryCount,
	}
}

func normalizeJSONData(data json.RawMessage) (json.RawMessage, error) {
	if len(data) == 0 || string(data) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode event data: %w", err)
	}
	return data, nil
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func uuidText(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", value.Bytes[0:4], value.Bytes[4:6], value.Bytes[6:8], value.Bytes[8:10], value.Bytes[10:16])
}
