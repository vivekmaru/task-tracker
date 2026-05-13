package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestCompleteAttemptTransitionsTicketDone(t *testing.T) {
	now := time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)
	attemptID := testUUID(31)
	store := &fakeAttemptStore{
		complete: db.CompleteAttemptRow{
			AttemptID:     attemptID,
			TicketID:      testUUID(32),
			AttemptStatus: AttemptStatusSucceeded,
			TicketStatus:  TicketStatusDone,
		},
	}
	service := NewAttemptService(store, WithAttemptClock(func() time.Time { return now }))

	result, err := service.Complete(context.Background(), CompleteAttemptRequest{
		AttemptID: attemptID,
		Output:    map[string]any{"summary": "fixed auth retry"},
	})
	if err != nil {
		t.Fatalf("complete attempt: %v", err)
	}

	params := store.completeParams[0]
	if params.AttemptID != attemptID {
		t.Fatalf("expected attempt id %v, got %v", attemptID, params.AttemptID)
	}
	if string(params.Output) != `{"summary":"fixed auth retry"}` {
		t.Fatalf("expected output JSON, got %s", string(params.Output))
	}
	if !params.CompletedAt.Time.Equal(now) {
		t.Fatalf("expected completed_at %v, got %v", now, params.CompletedAt.Time)
	}
	if result.AttemptStatus != AttemptStatusSucceeded || result.TicketStatus != TicketStatusDone {
		t.Fatalf("unexpected transition result: %#v", result)
	}
}

func TestFailAndBlockAttemptsRequireOperationalDetail(t *testing.T) {
	service := NewAttemptService(&fakeAttemptStore{})

	_, err := service.Fail(context.Background(), FailAttemptRequest{AttemptID: testUUID(33)})
	if err == nil {
		t.Fatal("expected fail validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) || !containsString(validationErr.Problems, "failure_reason is required") {
		t.Fatalf("expected failure reason validation, got %v", err)
	}

	_, err = service.Block(context.Background(), BlockAttemptRequest{AttemptID: testUUID(33)})
	if err == nil {
		t.Fatal("expected block validation error")
	}
	if !errors.As(err, &validationErr) || !containsString(validationErr.Problems, "blocker_reason is required") {
		t.Fatalf("expected blocker reason validation, got %v", err)
	}
}

func TestTerminalTransitionRejectsNonRunningAttempt(t *testing.T) {
	service := NewAttemptService(&fakeAttemptStore{completeErr: pgx.ErrNoRows})

	_, err := service.Complete(context.Background(), CompleteAttemptRequest{AttemptID: testUUID(34)})
	if !errors.Is(err, ErrAttemptNotRunning) {
		t.Fatalf("expected attempt not running, got %v", err)
	}
}

func TestCancelAndExpireTransitionsAreExposed(t *testing.T) {
	attemptID := testUUID(35)
	store := &fakeAttemptStore{
		cancel: db.CancelAttemptRow{
			AttemptID:     attemptID,
			TicketID:      testUUID(36),
			AttemptStatus: AttemptStatusCancelled,
			TicketStatus:  TicketStatusTodo,
		},
		expire: db.ExpireAttemptRow{
			AttemptID:     attemptID,
			TicketID:      testUUID(36),
			AttemptStatus: AttemptStatusExpired,
			TicketStatus:  TicketStatusTodo,
		},
	}
	service := NewAttemptService(store)

	cancelled, err := service.Cancel(context.Background(), CancelAttemptRequest{AttemptID: attemptID, Reason: "operator stopped run"})
	if err != nil {
		t.Fatalf("cancel attempt: %v", err)
	}
	expired, err := service.Expire(context.Background(), ExpireAttemptRequest{AttemptID: attemptID})
	if err != nil {
		t.Fatalf("expire attempt: %v", err)
	}

	if cancelled.AttemptStatus != AttemptStatusCancelled || expired.AttemptStatus != AttemptStatusExpired {
		t.Fatalf("unexpected cancel/expire results: %#v %#v", cancelled, expired)
	}
}
