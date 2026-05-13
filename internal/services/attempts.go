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

func validateHeartbeatRequest(req HeartbeatRequest) []string {
	var problems []string
	if !req.AttemptID.Valid {
		problems = append(problems, "attempt_id is required")
	}
	if req.Lease <= 0 {
		problems = append(problems, "lease must be positive")
	}
	return problems
}

func validateCheckpointRequest(req CheckpointRequest) []string {
	var problems []string
	if !req.AttemptID.Valid {
		problems = append(problems, "attempt_id is required")
	}
	if req.Summary == "" {
		problems = append(problems, "summary is required")
	}
	if req.ProgressPercent < 0 || req.ProgressPercent > 100 {
		problems = append(problems, "progress_percent must be between 0 and 100")
	}
	return problems
}

func trimCheckpointRequest(req CheckpointRequest) CheckpointRequest {
	req.Summary = strings.TrimSpace(req.Summary)
	req.NextStep = strings.TrimSpace(req.NextStep)
	req.Risk = strings.TrimSpace(req.Risk)
	req.FilesTouched = compactStrings(req.FilesTouched)
	req.CommandsRun = compactStrings(req.CommandsRun)
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
