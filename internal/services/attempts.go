package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

var ErrAttemptNotRunning = errors.New("attempt is not running")

type AttemptStore interface {
	HeartbeatAttempt(context.Context, db.HeartbeatAttemptParams) (db.HeartbeatAttemptRow, error)
	CheckpointAttempt(context.Context, db.CheckpointAttemptParams) (db.CheckpointAttemptRow, error)
	CompleteAttempt(context.Context, db.CompleteAttemptParams) (db.CompleteAttemptRow, error)
	FailAttempt(context.Context, db.FailAttemptParams) (db.FailAttemptRow, error)
	BlockAttempt(context.Context, db.BlockAttemptParams) (db.BlockAttemptRow, error)
	CancelAttempt(context.Context, db.CancelAttemptParams) (db.CancelAttemptRow, error)
	ExpireAttempt(context.Context, db.ExpireAttemptParams) (db.ExpireAttemptRow, error)
	CreateAttemptMetrics(context.Context, db.CreateAttemptMetricsParams) (db.AttemptMetric, error)
}

var _ AttemptStore = (*db.Queries)(nil)

type AttemptService struct {
	store AttemptStore
	now   func() time.Time
}

type AttemptOption func(*AttemptService)

func WithAttemptClock(clock func() time.Time) AttemptOption {
	return func(service *AttemptService) {
		service.now = clock
	}
}

func NewAttemptService(store AttemptStore, opts ...AttemptOption) *AttemptService {
	service := &AttemptService{
		store: store,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

type HeartbeatRequest struct {
	AttemptID pgtype.UUID
	Lease     time.Duration
}

type CheckpointRequest struct {
	AttemptID       pgtype.UUID
	Summary         string
	ProgressPercent int32
	FilesTouched    []string
	CommandsRun     []string
	NextStep        string
	Risk            string
}

type CheckpointResult struct {
	Checkpoint      db.AttemptCheckpoint
	ProgressPercent int32
}

type CompleteAttemptRequest struct {
	AttemptID    pgtype.UUID
	Output       map[string]any
	OutputSchema string
	Metrics      *AttemptMetricsRequest
}

type FailAttemptRequest struct {
	AttemptID       pgtype.UUID
	FailureReason   string
	FailureCategory string
	Output          map[string]any
	Metrics         *AttemptMetricsRequest
}

type BlockAttemptRequest struct {
	AttemptID       pgtype.UUID
	BlockerReason   string
	FailureCategory string
	Blocker         map[string]any
	Metrics         *AttemptMetricsRequest
}

type CancelAttemptRequest struct {
	AttemptID pgtype.UUID
	Reason    string
}

type ExpireAttemptRequest struct {
	AttemptID        pgtype.UUID
	ExpirationCutoff time.Time
}

type AttemptTransitionResult struct {
	AttemptID     pgtype.UUID
	TicketID      pgtype.UUID
	AttemptStatus string
	TicketStatus  string
}

type AttemptMetricsRequest struct {
	TokensIn        int64
	TokensOut       int64
	CostUSD         float64
	DurationSeconds float64
	RetryCount      int32
}

func (s *AttemptService) Heartbeat(ctx context.Context, req HeartbeatRequest) (db.Attempt, error) {
	if problems := validateHeartbeatRequest(req); len(problems) > 0 {
		return db.Attempt{}, ValidationError{Problems: problems}
	}

	now := s.now().UTC()
	row, err := s.store.HeartbeatAttempt(ctx, db.HeartbeatAttemptParams{
		AttemptID:      req.AttemptID,
		HeartbeatAt:    timestamptz(now),
		LeaseExpiresAt: timestamptz(now.Add(req.Lease)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Attempt{}, ErrAttemptNotRunning
		}
		return db.Attempt{}, fmt.Errorf("heartbeat attempt: %w", err)
	}

	return attemptFromHeartbeatRow(row), nil
}

func (s *AttemptService) Checkpoint(ctx context.Context, req CheckpointRequest) (CheckpointResult, error) {
	req = trimCheckpointRequest(req)
	if problems := validateCheckpointRequest(req); len(problems) > 0 {
		return CheckpointResult{}, ValidationError{Problems: problems}
	}

	row, err := s.store.CheckpointAttempt(ctx, db.CheckpointAttemptParams{
		AttemptID:       req.AttemptID,
		Summary:         req.Summary,
		ProgressPercent: req.ProgressPercent,
		FilesTouched:    req.FilesTouched,
		CommandsRun:     req.CommandsRun,
		NextStep:        optionalText(req.NextStep),
		Risk:            optionalText(req.Risk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CheckpointResult{}, ErrAttemptNotRunning
		}
		return CheckpointResult{}, fmt.Errorf("checkpoint attempt: %w", err)
	}

	return CheckpointResult{
		Checkpoint:      checkpointFromCheckpointRow(row),
		ProgressPercent: req.ProgressPercent,
	}, nil
}

func (s *AttemptService) Complete(ctx context.Context, req CompleteAttemptRequest) (AttemptTransitionResult, error) {
	if problems := validateAttemptID(req.AttemptID); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	if problems := validateAttemptMetrics(req.Metrics); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	output, err := encodeJSONObject(req.Output)
	if err != nil {
		return AttemptTransitionResult{}, fmt.Errorf("marshal completion output: %w", err)
	}

	row, err := s.store.CompleteAttempt(ctx, db.CompleteAttemptParams{
		AttemptID:    req.AttemptID,
		Output:       output,
		OutputSchema: optionalText(strings.TrimSpace(req.OutputSchema)),
		CompletedAt:  timestamptz(s.now().UTC()),
	})
	if err != nil {
		return AttemptTransitionResult{}, transitionError("complete attempt", err)
	}
	if err := s.recordAttemptMetrics(ctx, row.AttemptID, row.WorkspaceID, row.ProjectID, req.Metrics); err != nil {
		return AttemptTransitionResult{}, err
	}
	return transitionResult(row.AttemptID, row.TicketID, row.AttemptStatus, row.TicketStatus), nil
}

func (s *AttemptService) Fail(ctx context.Context, req FailAttemptRequest) (AttemptTransitionResult, error) {
	req.FailureReason = strings.TrimSpace(req.FailureReason)
	req.FailureCategory = strings.TrimSpace(req.FailureCategory)
	if problems := validateFailAttemptRequest(req); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	if problems := validateAttemptMetrics(req.Metrics); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	output, err := encodeJSONObject(req.Output)
	if err != nil {
		return AttemptTransitionResult{}, fmt.Errorf("marshal failure output: %w", err)
	}

	row, err := s.store.FailAttempt(ctx, db.FailAttemptParams{
		AttemptID:       req.AttemptID,
		FailureReason:   req.FailureReason,
		FailureCategory: optionalText(req.FailureCategory),
		Output:          output,
		CompletedAt:     timestamptz(s.now().UTC()),
	})
	if err != nil {
		return AttemptTransitionResult{}, transitionError("fail attempt", err)
	}
	if err := s.recordAttemptMetrics(ctx, row.AttemptID, row.WorkspaceID, row.ProjectID, req.Metrics); err != nil {
		return AttemptTransitionResult{}, err
	}
	return transitionResult(row.AttemptID, row.TicketID, row.AttemptStatus, row.TicketStatus), nil
}

func (s *AttemptService) Block(ctx context.Context, req BlockAttemptRequest) (AttemptTransitionResult, error) {
	req.BlockerReason = strings.TrimSpace(req.BlockerReason)
	req.FailureCategory = strings.TrimSpace(req.FailureCategory)
	if problems := validateBlockAttemptRequest(req); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	if problems := validateAttemptMetrics(req.Metrics); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	blocker, err := encodeJSONObject(req.Blocker)
	if err != nil {
		return AttemptTransitionResult{}, fmt.Errorf("marshal blocker: %w", err)
	}

	row, err := s.store.BlockAttempt(ctx, db.BlockAttemptParams{
		AttemptID:       req.AttemptID,
		BlockerReason:   req.BlockerReason,
		FailureCategory: optionalText(req.FailureCategory),
		Blocker:         blocker,
		CompletedAt:     timestamptz(s.now().UTC()),
	})
	if err != nil {
		return AttemptTransitionResult{}, transitionError("block attempt", err)
	}
	if err := s.recordAttemptMetrics(ctx, row.AttemptID, row.WorkspaceID, row.ProjectID, req.Metrics); err != nil {
		return AttemptTransitionResult{}, err
	}
	return transitionResult(row.AttemptID, row.TicketID, row.AttemptStatus, row.TicketStatus), nil
}

func (s *AttemptService) recordAttemptMetrics(ctx context.Context, attemptID, workspaceID, projectID pgtype.UUID, metrics *AttemptMetricsRequest) error {
	if metrics == nil {
		return nil
	}
	_, err := s.store.CreateAttemptMetrics(ctx, db.CreateAttemptMetricsParams{
		AttemptID:       attemptID,
		WorkspaceID:     workspaceID,
		ProjectID:       projectID,
		TokensIn:        metrics.TokensIn,
		TokensOut:       metrics.TokensOut,
		CostUsd:         numeric(metrics.CostUSD),
		DurationSeconds: numeric(metrics.DurationSeconds),
		RetryCount:      metrics.RetryCount,
	})
	if err != nil {
		return fmt.Errorf("record attempt metrics: %w", err)
	}
	return nil
}

func (s *AttemptService) Cancel(ctx context.Context, req CancelAttemptRequest) (AttemptTransitionResult, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if problems := validateAttemptID(req.AttemptID); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}

	row, err := s.store.CancelAttempt(ctx, db.CancelAttemptParams{
		AttemptID:   req.AttemptID,
		Reason:      optionalText(req.Reason),
		CompletedAt: timestamptz(s.now().UTC()),
	})
	if err != nil {
		return AttemptTransitionResult{}, transitionError("cancel attempt", err)
	}
	return transitionResult(row.AttemptID, row.TicketID, row.AttemptStatus, row.TicketStatus), nil
}

func (s *AttemptService) Expire(ctx context.Context, req ExpireAttemptRequest) (AttemptTransitionResult, error) {
	if problems := validateAttemptID(req.AttemptID); len(problems) > 0 {
		return AttemptTransitionResult{}, ValidationError{Problems: problems}
	}
	now := s.now().UTC()
	cutoff := req.ExpirationCutoff.UTC()
	if cutoff.IsZero() {
		cutoff = now
	}

	row, err := s.store.ExpireAttempt(ctx, db.ExpireAttemptParams{
		AttemptID:        req.AttemptID,
		CompletedAt:      timestamptz(now),
		ExpirationCutoff: timestamptz(cutoff),
	})
	if err != nil {
		return AttemptTransitionResult{}, transitionError("expire attempt", err)
	}
	return transitionResult(row.AttemptID, row.TicketID, row.AttemptStatus, row.TicketStatus), nil
}

func validateHeartbeatRequest(req HeartbeatRequest) []string {
	var problems []string
	problems = append(problems, validateAttemptID(req.AttemptID)...)
	if req.Lease <= 0 {
		problems = append(problems, "lease must be positive")
	}
	return problems
}

func validateCheckpointRequest(req CheckpointRequest) []string {
	var problems []string
	problems = append(problems, validateAttemptID(req.AttemptID)...)
	if req.Summary == "" {
		problems = append(problems, "summary is required")
	}
	if req.ProgressPercent < 0 || req.ProgressPercent > 100 {
		problems = append(problems, "progress_percent must be between 0 and 100")
	}
	return problems
}

func validateFailAttemptRequest(req FailAttemptRequest) []string {
	var problems []string
	problems = append(problems, validateAttemptID(req.AttemptID)...)
	if req.FailureReason == "" {
		problems = append(problems, "failure_reason is required")
	}
	return problems
}

func validateBlockAttemptRequest(req BlockAttemptRequest) []string {
	var problems []string
	problems = append(problems, validateAttemptID(req.AttemptID)...)
	if req.BlockerReason == "" {
		problems = append(problems, "blocker_reason is required")
	}
	return problems
}

func validateAttemptID(attemptID pgtype.UUID) []string {
	if !attemptID.Valid {
		return []string{"attempt_id is required"}
	}
	return nil
}

func trimCheckpointRequest(req CheckpointRequest) CheckpointRequest {
	req.Summary = strings.TrimSpace(req.Summary)
	req.NextStep = strings.TrimSpace(req.NextStep)
	req.Risk = strings.TrimSpace(req.Risk)
	req.FilesTouched = compactStringsPreserveEmpty(req.FilesTouched)
	req.CommandsRun = compactStringsPreserveEmpty(req.CommandsRun)
	return req
}

func attemptFromHeartbeatRow(row db.HeartbeatAttemptRow) db.Attempt {
	return db.Attempt{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		ProjectID:       row.ProjectID,
		TicketID:        row.TicketID,
		AgentID:         row.AgentID,
		Harness:         row.Harness,
		Model:           row.Model,
		Status:          row.Status,
		LeaseExpiresAt:  row.LeaseExpiresAt,
		LastHeartbeatAt: row.LastHeartbeatAt,
		ProgressPercent: row.ProgressPercent,
		CurrentSummary:  row.CurrentSummary,
		NextStep:        row.NextStep,
		Output:          row.Output,
		OutputSchema:    row.OutputSchema,
		FailureReason:   row.FailureReason,
		FailureCategory: row.FailureCategory,
		Blocker:         row.Blocker,
		TraceID:         row.TraceID,
		CheckpointRef:   row.CheckpointRef,
		StartedAt:       row.StartedAt,
		CompletedAt:     row.CompletedAt,
	}
}

func checkpointFromCheckpointRow(row db.CheckpointAttemptRow) db.AttemptCheckpoint {
	return db.AttemptCheckpoint{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		ProjectID:    row.ProjectID,
		TicketID:     row.TicketID,
		AttemptID:    row.AttemptID,
		Summary:      row.Summary,
		FilesTouched: row.FilesTouched,
		CommandsRun:  row.CommandsRun,
		NextStep:     row.NextStep,
		Risk:         row.Risk,
		CreatedAt:    row.CreatedAt,
	}
}

func transitionResult(attemptID, ticketID pgtype.UUID, attemptStatus, ticketStatus string) AttemptTransitionResult {
	return AttemptTransitionResult{
		AttemptID:     attemptID,
		TicketID:      ticketID,
		AttemptStatus: attemptStatus,
		TicketStatus:  ticketStatus,
	}
}

func transitionError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAttemptNotRunning
	}
	return fmt.Errorf("%s: %w", operation, err)
}
