package runtime

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
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

func (r *Runtime) CreateTicket(ctx context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	return r.Tickets.CreateTicket(ctx, req)
}

func (r *Runtime) ProposeTicket(ctx context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	return r.Tickets.ProposeTicket(ctx, req)
}

func (r *Runtime) ClaimNext(ctx context.Context, req services.ClaimNextRequest) (services.ClaimNextResult, error) {
	return r.Claims.ClaimNext(ctx, req)
}

func (r *Runtime) Heartbeat(ctx context.Context, req services.HeartbeatRequest) (db.Attempt, error) {
	return r.Attempts.Heartbeat(ctx, req)
}

func (r *Runtime) Checkpoint(ctx context.Context, req services.CheckpointRequest) (services.CheckpointResult, error) {
	return r.Attempts.Checkpoint(ctx, req)
}

func (r *Runtime) Complete(ctx context.Context, req services.CompleteAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.Attempts.Complete(ctx, req)
}

func (r *Runtime) Fail(ctx context.Context, req services.FailAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.Attempts.Fail(ctx, req)
}

func (r *Runtime) Block(ctx context.Context, req services.BlockAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.Attempts.Block(ctx, req)
}

func (r *Runtime) Cancel(ctx context.Context, req services.CancelAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.Attempts.Cancel(ctx, req)
}

func (r *Runtime) ListTickets(ctx context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	return r.Tickets.ListTickets(ctx, req)
}

func (r *Runtime) GetTicket(ctx context.Context, id pgtype.UUID) (db.Ticket, error) {
	return r.Queries.GetTicket(ctx, id)
}

func (r *Runtime) GetAttempt(ctx context.Context, id pgtype.UUID) (db.Attempt, error) {
	return r.Queries.GetAttempt(ctx, id)
}
