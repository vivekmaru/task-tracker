package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestHeartbeatAttemptExtendsLease(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	attemptID := testUUID(21)
	store := &fakeAttemptStore{
		heartbeat: db.HeartbeatAttemptRow{
			ID:              attemptID,
			Status:          AttemptStatusRunning,
			LeaseExpiresAt:  timestamptz(now.Add(15 * time.Minute)),
			LastHeartbeatAt: timestamptz(now),
		},
	}
	service := NewAttemptService(store, WithAttemptClock(func() time.Time { return now }))

	attempt, err := service.Heartbeat(context.Background(), HeartbeatRequest{
		AttemptID: attemptID,
		Lease:     15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	params := store.heartbeatParams[0]
	if params.AttemptID != attemptID {
		t.Fatalf("expected attempt id %v, got %v", attemptID, params.AttemptID)
	}
	if !params.HeartbeatAt.Time.Equal(now) {
		t.Fatalf("expected heartbeat at %v, got %v", now, params.HeartbeatAt.Time)
	}
	if !params.LeaseExpiresAt.Time.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expected lease expiry %v, got %v", now.Add(15*time.Minute), params.LeaseExpiresAt.Time)
	}
	if attempt.ID != attemptID || attempt.Status != AttemptStatusRunning {
		t.Fatalf("unexpected heartbeat result: %#v", attempt)
	}
}

func TestCheckpointAttemptRecordsProgress(t *testing.T) {
	attemptID := testUUID(22)
	checkpointID := testUUID(23)
	store := &fakeAttemptStore{
		checkpoint: db.CheckpointAttemptRow{
			ID:           checkpointID,
			AttemptID:    attemptID,
			Summary:      "Found failing middleware branch",
			FilesTouched: []string{"internal/auth/middleware.go"},
			CommandsRun:  []string{"go test ./internal/auth"},
			NextStep:     pgtype.Text{String: "Patch retry branch", Valid: true},
			Risk:         pgtype.Text{String: "Needs auth regression test", Valid: true},
		},
	}
	service := NewAttemptService(store)

	checkpoint, err := service.Checkpoint(context.Background(), CheckpointRequest{
		AttemptID:       attemptID,
		Summary:         " Found failing middleware branch ",
		ProgressPercent: 40,
		FilesTouched:    []string{" internal/auth/middleware.go ", ""},
		CommandsRun:     []string{" go test ./internal/auth "},
		NextStep:        " Patch retry branch ",
		Risk:            " Needs auth regression test ",
	})
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	params := store.checkpointParams[0]
	if params.Summary != "Found failing middleware branch" {
		t.Fatalf("expected trimmed summary, got %q", params.Summary)
	}
	if len(params.FilesTouched) != 1 || params.FilesTouched[0] != "internal/auth/middleware.go" {
		t.Fatalf("expected compacted files, got %#v", params.FilesTouched)
	}
	if !params.NextStep.Valid || params.NextStep.String != "Patch retry branch" {
		t.Fatalf("expected next step, got %#v", params.NextStep)
	}
	if checkpoint.Checkpoint.ID != checkpointID || checkpoint.ProgressPercent != 40 {
		t.Fatalf("unexpected checkpoint result: %#v", checkpoint)
	}
}

func TestAttemptMutationValidationAndNoRunningAttempt(t *testing.T) {
	service := NewAttemptService(&fakeAttemptStore{heartbeatErr: pgx.ErrNoRows})

	_, err := service.Heartbeat(context.Background(), HeartbeatRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	_, err = service.Heartbeat(context.Background(), HeartbeatRequest{
		AttemptID: testUUID(24),
		Lease:     time.Minute,
	})
	if !errors.Is(err, ErrAttemptNotRunning) {
		t.Fatalf("expected attempt not running error, got %v", err)
	}
}

type fakeAttemptStore struct {
	heartbeatParams  []db.HeartbeatAttemptParams
	heartbeat        db.HeartbeatAttemptRow
	heartbeatErr     error
	checkpointParams []db.CheckpointAttemptParams
	checkpoint       db.CheckpointAttemptRow
	checkpointErr    error
}

func (s *fakeAttemptStore) HeartbeatAttempt(_ context.Context, params db.HeartbeatAttemptParams) (db.HeartbeatAttemptRow, error) {
	s.heartbeatParams = append(s.heartbeatParams, params)
	return s.heartbeat, s.heartbeatErr
}

func (s *fakeAttemptStore) CheckpointAttempt(_ context.Context, params db.CheckpointAttemptParams) (db.CheckpointAttemptRow, error) {
	s.checkpointParams = append(s.checkpointParams, params)
	return s.checkpoint, s.checkpointErr
}
