//go:build integration

package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

func TestCancelAttemptTransitionsTicketAndRecordsEvent(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)

	claim, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", ""))
	if err != nil {
		t.Fatalf("claim ticket: %v", err)
	}
	cancelled, err := fixture.runtime.Cancel(fixture.context, services.CancelAttemptRequest{
		AttemptID: claim.Attempt.ID,
		Reason:    "operator stopped run",
	})
	if err != nil {
		t.Fatalf("cancel attempt: %v", err)
	}
	if cancelled.AttemptStatus != services.AttemptStatusCancelled || cancelled.TicketStatus != services.TicketStatusTodo {
		t.Fatalf("unexpected cancellation result: %#v", cancelled)
	}

	attempt, err := fixture.runtime.Queries.GetAttempt(fixture.context, claim.Attempt.ID)
	if err != nil {
		t.Fatalf("get cancelled attempt: %v", err)
	}
	if attempt.Status != services.AttemptStatusCancelled {
		t.Fatalf("expected cancelled attempt, got %q", attempt.Status)
	}
	updatedTicket, err := fixture.runtime.Queries.GetTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("get ticket after cancellation: %v", err)
	}
	if updatedTicket.Status != services.TicketStatusTodo {
		t.Fatalf("expected todo ticket after cancellation, got %q", updatedTicket.Status)
	}
	events, err := fixture.runtime.Queries.ListTicketEventsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list ticket events: %v", err)
	}
	var cancelledEvents int
	for _, event := range events {
		if event.Type == "cancelled" {
			cancelledEvents++
		}
	}
	if cancelledEvents != 1 {
		t.Fatalf("expected one cancelled event, got %d in %#v", cancelledEvents, events)
	}
}

func TestHeartbeatExpiryRaceKeepsRenewedAttemptRunning(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	claim, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", ""))
	if err != nil {
		t.Fatalf("claim ticket: %v", err)
	}

	cutoff := time.Now().UTC()
	setAttemptLease(t, fixture, claim.Attempt.ID, cutoff.Add(-time.Minute))
	selected := listExpiredAttempts(t, fixture, cutoff)
	if len(selected) != 1 || selected[0].ID != claim.Attempt.ID {
		t.Fatalf("expected selected expired attempt %v, got %#v", claim.Attempt.ID, selected)
	}
	if _, err := fixture.runtime.Heartbeat(fixture.context, services.HeartbeatRequest{AttemptID: claim.Attempt.ID, Lease: time.Hour}); err != nil {
		t.Fatalf("renew attempt lease: %v", err)
	}

	_, err = fixture.runtime.Queries.ExpireAttempt(fixture.context, db.ExpireAttemptParams{
		AttemptID:        claim.Attempt.ID,
		CompletedAt:      pgtype.Timestamptz{Time: cutoff, Valid: true},
		ExpirationCutoff: pgtype.Timestamptz{Time: cutoff, Valid: true},
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected stale expiry to lose eligibility, got %v", err)
	}

	attempt, err := fixture.runtime.Queries.GetAttempt(fixture.context, claim.Attempt.ID)
	if err != nil {
		t.Fatalf("get renewed attempt: %v", err)
	}
	if attempt.Status != services.AttemptStatusRunning {
		t.Fatalf("expected renewed attempt to remain running, got %q", attempt.Status)
	}
	events, err := fixture.runtime.Queries.ListTicketEventsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list ticket events: %v", err)
	}
	if countEvents(events, "expired") != 0 {
		t.Fatalf("expected no expiry event after lease renewal, got %#v", events)
	}
}

func TestLeaseExpiryRequeuesTicketOnce(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	claim, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", ""))
	if err != nil {
		t.Fatalf("claim ticket: %v", err)
	}

	cutoff := time.Now().UTC()
	setAttemptLease(t, fixture, claim.Attempt.ID, cutoff.Add(-time.Minute))
	selected := listExpiredAttempts(t, fixture, cutoff)
	if len(selected) != 1 || selected[0].ID != claim.Attempt.ID {
		t.Fatalf("expected selected expired attempt %v, got %#v", claim.Attempt.ID, selected)
	}
	expired, err := fixture.runtime.Queries.ExpireAttempt(fixture.context, db.ExpireAttemptParams{
		AttemptID:        claim.Attempt.ID,
		CompletedAt:      pgtype.Timestamptz{Time: cutoff, Valid: true},
		ExpirationCutoff: pgtype.Timestamptz{Time: cutoff, Valid: true},
	})
	if err != nil {
		t.Fatalf("expire selected attempt: %v", err)
	}
	if expired.AttemptStatus != services.AttemptStatusExpired || expired.TicketStatus != services.TicketStatusTodo {
		t.Fatalf("unexpected expiry result: %#v", expired)
	}
	events, err := fixture.runtime.Queries.ListTicketEventsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list ticket events: %v", err)
	}
	if countEvents(events, "expired") != 1 {
		t.Fatalf("expected exactly one expiry event, got %#v", events)
	}
}

func TestCreateTicketAtomicOnDependencyFailure(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	missingDependency := pgtype.UUID{Valid: true}
	missingDependency.Bytes[15] = 99

	_, err := fixture.runtime.CreateTicket(fixture.context, services.CreateTicketRequest{
		WorkspaceID:          workspace.ID,
		ProjectID:            project.ID,
		Title:                "Atomic ticket creation",
		Description:          "The ticket and its dependency must commit together.",
		Type:                 services.TicketTypeTask,
		AcceptanceCriteria:   []string{"No ticket survives a failed dependency insert"},
		VerificationCommands: []string{"go test -tags=integration ./internal/integration"},
		Dependencies:         []pgtype.UUID{missingDependency},
		CreatedBy:            services.ActorHuman,
	})
	if err == nil {
		t.Fatal("expected missing dependency to fail ticket creation")
	}
	tickets, err := fixture.runtime.ListTickets(fixture.context, services.ListTicketsRequest{
		WorkspaceID: workspace.ID,
		ProjectID:   project.ID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list tickets after failed create: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected failed create to roll back ticket, got %#v", tickets)
	}
}

func TestUpdateTicketAtomicOnEventFailure(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `
CREATE FUNCTION reject_integration_updated_events() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.type = 'updated' THEN
        RAISE EXCEPTION 'reject updated event';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER reject_integration_updated_events
BEFORE INSERT ON ticket_events
FOR EACH ROW EXECUTE FUNCTION reject_integration_updated_events();`); err != nil {
		t.Fatalf("install update event failure trigger: %v", err)
	}

	updatedTitle := "This update must roll back"
	_, err := fixture.runtime.UpdateTicket(fixture.context, services.UpdateTicketRequest{
		TicketID:  ticket.ID,
		Title:     &updatedTitle,
		ActorType: services.ActorHuman,
		ActorID:   "integration-operator",
	})
	if err == nil {
		t.Fatal("expected update event trigger to reject the transaction")
	}
	stored, err := fixture.runtime.GetTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("get ticket after failed update: %v", err)
	}
	if stored.Title != ticket.Title {
		t.Fatalf("expected original title %q after rollback, got %q", ticket.Title, stored.Title)
	}
	events, err := fixture.runtime.ListTicketEventsByTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("list ticket events after failed update: %v", err)
	}
	if countEvents(events, "updated") != 0 {
		t.Fatalf("expected no update event after rollback, got %#v", events)
	}
}

func TestDecomposeAtomicOnChildEventFailure(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	parent := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `CREATE FUNCTION reject_integration_proposed_events() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.type = 'proposed' THEN RAISE EXCEPTION 'reject proposed event'; END IF; RETURN NEW; END; $$`); err != nil {
		t.Fatalf("install proposal failure function: %v", err)
	}
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `CREATE TRIGGER reject_integration_proposed_events BEFORE INSERT ON ticket_events FOR EACH ROW EXECUTE FUNCTION reject_integration_proposed_events()`); err != nil {
		t.Fatalf("install proposal failure trigger: %v", err)
	}
	_, err := fixture.runtime.DecomposeTicket(fixture.context, services.DecomposeTicketRequest{
		WorkspaceID: workspace.ID, ProjectID: project.ID, ParentID: parent.ID,
		Mode: services.DecomposeModePropose, CreatedBy: services.ActorAgent, CreatedByID: "planner", CreationReason: "integration transaction test",
		Children: []services.DecomposeChildRequest{{Key: "one", Title: "First child", Description: "First child must not persist.", Type: services.TicketTypeTask, AcceptanceCriteria: []string{"No partial child"}, VerificationCommands: []string{"go test ./..."}}},
	})
	if err == nil {
		t.Fatal("expected child event failure")
	}
	tickets, err := fixture.runtime.ListTickets(fixture.context, services.ListTicketsRequest{WorkspaceID: workspace.ID, ProjectID: project.ID, Limit: 10})
	if err != nil {
		t.Fatalf("list tickets: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ID != parent.ID {
		t.Fatalf("expected only parent after rollback, got %#v", tickets)
	}
}

func TestTerminalMetricsAtomicOnMetricsFailure(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	claim, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "integration-agent", ""))
	if err != nil {
		t.Fatalf("claim ticket: %v", err)
	}
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `CREATE FUNCTION reject_integration_metrics() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reject attempt metrics'; END; $$`); err != nil {
		t.Fatalf("install metrics failure function: %v", err)
	}
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `CREATE TRIGGER reject_integration_metrics BEFORE INSERT ON attempt_metrics FOR EACH ROW EXECUTE FUNCTION reject_integration_metrics()`); err != nil {
		t.Fatalf("install metrics failure trigger: %v", err)
	}
	_, err = fixture.runtime.Complete(fixture.context, services.CompleteAttemptRequest{AttemptID: claim.Attempt.ID, Output: map[string]any{"summary": "must roll back"}, Metrics: &services.AttemptMetricsRequest{TokensIn: 1}})
	if err == nil {
		t.Fatal("expected metrics failure")
	}
	attempt, err := fixture.runtime.GetAttempt(fixture.context, claim.Attempt.ID)
	if err != nil {
		t.Fatalf("get attempt: %v", err)
	}
	if attempt.Status != services.AttemptStatusRunning {
		t.Fatalf("expected running attempt after rollback, got %q", attempt.Status)
	}
	stored, err := fixture.runtime.GetTicket(fixture.context, ticket.ID)
	if err != nil {
		t.Fatalf("get ticket: %v", err)
	}
	if stored.Status != services.TicketStatusInProgress {
		t.Fatalf("expected in_progress ticket after rollback, got %q", stored.Status)
	}
}

func setAttemptLease(t *testing.T, fixture *fixture, attemptID pgtype.UUID, leaseExpiresAt time.Time) {
	t.Helper()
	if _, err := fixture.runtime.Pool.Exec(fixture.context, "UPDATE attempts SET lease_expires_at = $1 WHERE id = $2", leaseExpiresAt, attemptID); err != nil {
		t.Fatalf("set attempt lease: %v", err)
	}
}

func listExpiredAttempts(t *testing.T, fixture *fixture, cutoff time.Time) []db.Attempt {
	t.Helper()
	attempts, err := fixture.runtime.Queries.ListExpiredRunningAttempts(fixture.context, db.ListExpiredRunningAttemptsParams{
		Now:        pgtype.Timestamptz{Time: cutoff, Valid: true},
		BatchLimit: 10,
	})
	if err != nil {
		t.Fatalf("list expired attempts: %v", err)
	}
	return attempts
}

func countEvents(events []db.TicketEvent, eventType string) int {
	var count int
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
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
