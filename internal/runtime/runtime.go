package runtime

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/jobs"
	"github.com/vivek/agent-task-tracker/internal/services"
)

type Runtime struct {
	Pool        *pgxpool.Pool
	Queries     *db.Queries
	Tickets     *services.TicketService
	Claims      *services.ClaimService
	Attempts    *services.AttemptService
	Maintenance *jobs.MaintenanceWorker
}

func Open(ctx context.Context, cfg config.Config) (*Runtime, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	rt := New(db.New(pool))
	rt.Pool = pool
	return rt, nil
}

func New(queries *db.Queries) *Runtime {
	return &Runtime{
		Queries:     queries,
		Tickets:     services.NewTicketService(queries),
		Claims:      services.NewClaimService(queries),
		Attempts:    services.NewAttemptService(queries),
		Maintenance: jobs.NewMaintenanceWorker(queries),
	}
}

func (r *Runtime) Close() {
	if r != nil && r.Pool != nil {
		r.Pool.Close()
	}
}
