package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

type MaintenanceStore interface {
	ListExpiredRunningAttempts(context.Context, db.ListExpiredRunningAttemptsParams) ([]db.Attempt, error)
	ExpireAttempt(context.Context, db.ExpireAttemptParams) (db.ExpireAttemptRow, error)
	DeleteExpiredIdempotencyKeys(context.Context) (int64, error)
}

var _ MaintenanceStore = (*db.Queries)(nil)

type MaintenanceWorker struct {
	store      MaintenanceStore
	now        func() time.Time
	batchLimit int32
}

type Option func(*MaintenanceWorker)

func WithClock(clock func() time.Time) Option {
	return func(worker *MaintenanceWorker) {
		worker.now = clock
	}
}

func WithBatchLimit(limit int32) Option {
	return func(worker *MaintenanceWorker) {
		worker.batchLimit = limit
	}
}

type MaintenanceResult struct {
	ExpiredAttempts        int
	SkippedExpiryRaces     int
	DeletedIdempotencyKeys int64
}

func NewMaintenanceWorker(store MaintenanceStore, opts ...Option) *MaintenanceWorker {
	worker := &MaintenanceWorker{
		store:      store,
		now:        time.Now,
		batchLimit: 100,
	}
	for _, opt := range opts {
		opt(worker)
	}
	if worker.batchLimit <= 0 {
		worker.batchLimit = 100
	}
	return worker
}

func (w *MaintenanceWorker) RunOnce(ctx context.Context) (MaintenanceResult, error) {
	now := w.now().UTC()
	expired, err := w.store.ListExpiredRunningAttempts(ctx, db.ListExpiredRunningAttemptsParams{
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
		BatchLimit: w.batchLimit,
	})
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("list expired running attempts: %w", err)
	}

	result := MaintenanceResult{}
	for _, attempt := range expired {
		_, err := w.store.ExpireAttempt(ctx, db.ExpireAttemptParams{
			AttemptID:        attempt.ID,
			CompletedAt:      pgtype.Timestamptz{Time: now, Valid: true},
			ExpirationCutoff: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				result.SkippedExpiryRaces++
				continue
			}
			return MaintenanceResult{}, fmt.Errorf("expire attempt %v: %w", attempt.ID, err)
		}
		result.ExpiredAttempts++
	}

	deleted, err := w.store.DeleteExpiredIdempotencyKeys(ctx)
	if err != nil {
		return MaintenanceResult{}, fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	result.DeletedIdempotencyKeys = deleted
	return result, nil
}
