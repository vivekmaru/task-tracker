package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if !store.expireParams[0].ExpirationCutoff.Time.Equal(now) {
		t.Fatalf("expected expiry cutoff %v, got %v", now, store.expireParams[0].ExpirationCutoff.Time)
	}
	if len(store.webhookCutoffs) != 0 {
		t.Fatalf("expected retention cleanup to stay disabled by default, got %v", store.webhookCutoffs)
	}
}

func TestMaintenanceWorkerRemovesOnlyTerminalWebhookDeliveriesAfterRetention(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{deletedWebhookDeliveries: 4}
	worker := NewMaintenanceWorker(store, WithClock(func() time.Time { return now }), WithWebhookRetention(72*time.Hour))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run maintenance: %v", err)
	}
	if result.DeletedWebhookDeliveries != 4 {
		t.Fatalf("expected four deleted webhook deliveries, got %d", result.DeletedWebhookDeliveries)
	}
	if len(store.webhookCutoffs) != 1 || !store.webhookCutoffs[0].Time.Equal(now.Add(-72*time.Hour)) {
		t.Fatalf("expected retention cutoff %v, got %v", now.Add(-72*time.Hour), store.webhookCutoffs)
	}
}

func TestMaintenanceWorkerSkipsLostExpiryRaces(t *testing.T) {
	store := &fakeMaintenanceStore{
		expiredAttempts: []db.Attempt{{ID: testUUID(42)}, {ID: testUUID(43)}},
		expireErrors:    []error{pgx.ErrNoRows, nil},
	}
	worker := NewMaintenanceWorker(store)

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run maintenance: %v", err)
	}
	if result.ExpiredAttempts != 1 || result.SkippedExpiryRaces != 1 {
		t.Fatalf("unexpected expiry result: %#v", result)
	}
	if len(store.expireParams) != 2 {
		t.Fatalf("expected both selected attempts to be handled, got %d", len(store.expireParams))
	}
}

func TestMaintenanceWorkerReturnsActualExpiryErrors(t *testing.T) {
	store := &fakeMaintenanceStore{
		expiredAttempts: []db.Attempt{{ID: testUUID(44)}},
		expireErrors:    []error{errors.New("database unavailable")},
	}
	worker := NewMaintenanceWorker(store)

	_, err := worker.RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("expected database error, got %v", err)
	}
}

type fakeMaintenanceStore struct {
	listParams               []db.ListExpiredRunningAttemptsParams
	expiredAttempts          []db.Attempt
	expireParams             []db.ExpireAttemptParams
	expireErrors             []error
	expireCall               int
	deletedKeys              int64
	webhookCutoffs           []pgtype.Timestamptz
	deletedWebhookDeliveries int64
}

func (s *fakeMaintenanceStore) ListExpiredRunningAttempts(_ context.Context, params db.ListExpiredRunningAttemptsParams) ([]db.Attempt, error) {
	s.listParams = append(s.listParams, params)
	return s.expiredAttempts, nil
}

func (s *fakeMaintenanceStore) ExpireAttempt(_ context.Context, params db.ExpireAttemptParams) (db.ExpireAttemptRow, error) {
	s.expireParams = append(s.expireParams, params)
	err := error(nil)
	if s.expireCall < len(s.expireErrors) {
		err = s.expireErrors[s.expireCall]
	}
	s.expireCall++
	if err != nil {
		return db.ExpireAttemptRow{}, err
	}
	return db.ExpireAttemptRow{
		AttemptID:     params.AttemptID,
		AttemptStatus: "expired",
		TicketStatus:  "todo",
	}, nil
}

func (s *fakeMaintenanceStore) DeleteExpiredIdempotencyKeys(_ context.Context) (int64, error) {
	return s.deletedKeys, nil
}

func (s *fakeMaintenanceStore) DeleteTerminalWebhookDeliveries(_ context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	s.webhookCutoffs = append(s.webhookCutoffs, cutoff)
	return s.deletedWebhookDeliveries, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
