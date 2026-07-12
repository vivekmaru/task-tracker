//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/runtime"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/testsupport"
)

func TestHarnessCreatesIndependentDatabasesConcurrently(t *testing.T) {
	rootURL, err := testsupport.TestDatabaseURL()
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	databases := make(chan *testsupport.Database, 2)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			database, err := testsupport.CreateDatabase(context.Background(), rootURL)
			if err != nil {
				errs <- err
				return
			}
			databases <- database
		}()
	}
	close(start)
	group.Wait()
	close(databases)
	close(errs)

	for err := range errs {
		t.Fatalf("create concurrent test database: %v", err)
	}
	var created []*testsupport.Database
	for database := range databases {
		created = append(created, database)
	}
	if len(created) != 2 || created[0].Name == created[1].Name {
		t.Fatalf("expected two distinct test databases, got %#v", created)
	}
	t.Cleanup(func() {
		for _, database := range created {
			if err := database.Close(context.Background()); err != nil {
				t.Errorf("drop test database %q: %v", database.Name, err)
			}
		}
	})
}

func TestMigrationRunnerStartsFromZeroAndRuntimeCreatesScope(t *testing.T) {
	fixture := newFixture(t)

	repeated, err := fixture.database.ApplyMigrations(fixture.context)
	if err != nil {
		t.Fatalf("repeat migrations: %v", err)
	}
	if len(repeated.Applied) != 0 || len(repeated.Skipped) == 0 {
		t.Fatalf("expected an idempotent migration rerun, got %#v", repeated)
	}

	workspace, project := createScope(t, fixture.runtime, fixture.context)
	if workspace.ID != project.WorkspaceID {
		t.Fatalf("project scope mismatch: workspace=%v project=%v", workspace.ID, project.WorkspaceID)
	}
}

func TestConcurrentClaimNextCreatesOneRunningAttempt(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, agentID := range []string{"integration-agent-a", "integration-agent-b"} {
		agentID := agentID
		go func() {
			<-start
			_, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, agentID, ""))
			results <- err
		}()
	}
	close(start)

	var successful int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successful++
		case errors.Is(err, services.ErrNoClaimableTickets):
		default:
			t.Fatalf("claim next: %v", err)
		}
	}
	if successful != 1 {
		t.Fatalf("expected one successful claim, got %d", successful)
	}

	attempts, err := fixture.runtime.Queries.ListAttemptsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Status != services.AttemptStatusRunning {
		t.Fatalf("expected one running attempt, got %#v", attempts)
	}
}

func TestClaimReplayAndCompleteUseRealPostgreSQL(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)

	first, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", "claim-replay"))
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	replayed, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", "claim-replay"))
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if first.Attempt.ID != replayed.Attempt.ID || first.Ticket.ID != replayed.Ticket.ID {
		t.Fatalf("idempotency replay returned different claim: first=%v replay=%v", first.Attempt.ID, replayed.Attempt.ID)
	}

	attempts, err := fixture.runtime.Queries.ListAttemptsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one running attempt after replay, got %d", len(attempts))
	}

	completed, err := fixture.runtime.Complete(fixture.context, services.CompleteAttemptRequest{
		AttemptID: first.Attempt.ID,
		Output:    map[string]any{"summary": "integration complete"},
	})
	if err != nil {
		t.Fatalf("complete attempt: %v", err)
	}
	if completed.AttemptStatus != services.AttemptStatusSucceeded || completed.TicketStatus != services.TicketStatusDone {
		t.Fatalf("unexpected terminal transition: %#v", completed)
	}
}

func createScope(t *testing.T, rt *runtime.Runtime, ctx context.Context) (db.Workspace, db.Project) {
	t.Helper()
	workspace, err := rt.CreateWorkspace(ctx, "integration-workspace")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := rt.CreateProject(ctx, workspace.ID, "integration-project")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return workspace, project
}

func createClaimableTicket(t *testing.T, rt *runtime.Runtime, ctx context.Context, workspaceID, projectID pgtype.UUID) db.Ticket {
	t.Helper()
	ticket, err := rt.CreateTicket(ctx, services.CreateTicketRequest{
		WorkspaceID:          workspaceID,
		ProjectID:            projectID,
		Title:                "Run PostgreSQL integration claim",
		Description:          "Exercise the real claim path against a fresh PostgreSQL database.",
		Type:                 services.TicketTypeTask,
		AcceptanceCriteria:   []string{"Exactly one agent claims this ticket"},
		VerificationCommands: []string{"go test -tags=integration ./internal/integration"},
		CreatedBy:            services.ActorHuman,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	return ticket
}

func claimRequest(workspaceID, projectID pgtype.UUID, agentID, idempotencyKey string) services.ClaimNextRequest {
	return services.ClaimNextRequest{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		AgentID:        agentID,
		Harness:        "codex",
		Model:          "integration",
		Lease:          time.Minute,
		IdempotencyKey: idempotencyKey,
	}
}
