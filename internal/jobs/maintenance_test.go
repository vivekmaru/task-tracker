package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestMaintenanceWorkerExpiresAttemptsAndCleansIdempotency(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	attemptID := testUUID(41)
	store := &fakeMaintenanceStore{
		expiredAttempts: []db.Attempt{{ID: attemptID}},
		deletedKeys:     3,
	}
	worker := NewMaintenanceWorker(store, WithClock(func() time.Time { return now }), WithBatchLimit(25))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run maintenance: %v", err)
	}

	if result.ExpiredAttempts != 1 {
		t.Fatalf("expected one expired attempt, got %d", result.ExpiredAttempts)
	}
	if result.DeletedIdempotencyKeys != 3 {
		t.Fatalf("expected three deleted idempotency keys, got %d", result.DeletedIdempotencyKeys)
	}
	listParams := store.listParams[0]
	if !listParams.Now.Time.Equal(now) {
		t.Fatalf("expected list time %v, got %v", now, listParams.Now.Time)
	}
	if listParams.BatchLimit != 25 {
		t.Fatalf("expected batch limit 25, got %d", listParams.BatchLimit)
	}
	if store.expireParams[0].AttemptID != attemptID {
		t.Fatalf("expected expired attempt %v, got %v", attemptID, store.expireParams[0].AttemptID)
	}
}

type fakeMaintenanceStore struct {
	listParams      []db.ListExpiredRunningAttemptsParams
	expiredAttempts []db.Attempt
	expireParams    []db.ExpireAttemptParams
	deletedKeys     int64
}

func (s *fakeMaintenanceStore) ListExpiredRunningAttempts(_ context.Context, params db.ListExpiredRunningAttemptsParams) ([]db.Attempt, error) {
	s.listParams = append(s.listParams, params)
	return s.expiredAttempts, nil
}

func (s *fakeMaintenanceStore) ExpireAttempt(_ context.Context, params db.ExpireAttemptParams) (db.ExpireAttemptRow, error) {
	s.expireParams = append(s.expireParams, params)
	return db.ExpireAttemptRow{
		AttemptID:     params.AttemptID,
		AttemptStatus: "expired",
		TicketStatus:  "todo",
	}, nil
}

func (s *fakeMaintenanceStore) DeleteExpiredIdempotencyKeys(_ context.Context) (int64, error) {
	return s.deletedKeys, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
