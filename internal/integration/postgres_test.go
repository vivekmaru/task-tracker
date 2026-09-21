//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	// Even at the retry limit, a fenced-out expiry must not fail the ticket.
	if _, err := fixture.runtime.Pool.Exec(fixture.context, `UPDATE tickets SET retry_policy = '{"max_attempts":1}'::jsonb WHERE id = $1`, ticket.ID); err != nil {
		t.Fatalf("set retry policy: %v", err)
	}
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
	storedTicket, err := fixture.runtime.GetTicket(fixture.context, ticket.ID)
	if err != nil || storedTicket.Status != services.TicketStatusInProgress {
		t.Fatalf("fenced expiry must leave ticket in progress: status=%q err=%v", storedTicket.Status, err)
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

func TestRetryExhaustionCountsCurrentAttempt(t *testing.T) {
	for _, test := range []struct {
		name        string
		retryPolicy string
		outcomes    []string
		finalStatus string
	}{
		{
			name: "default_three_failures", outcomes: []string{"failed", "failed", "failed"},
			finalStatus: services.TicketStatusFailed,
		},
		{
			name: "missing_policy_fields_default_to_three", retryPolicy: `{}`,
			outcomes: []string{"failed", "failed", "failed"}, finalStatus: services.TicketStatusFailed,
		},
		{
			name: "expiry_max_one", retryPolicy: `{"max_attempts":1}`,
			outcomes: []string{"expired"}, finalStatus: services.TicketStatusFailed,
		},
		{
			name: "default_three_expiries", outcomes: []string{"expired", "expired", "expired"},
			finalStatus: services.TicketStatusFailed,
		},
		{
			name: "mixed_ending_in_failure", outcomes: []string{"failed", "expired", "failed"},
			finalStatus: services.TicketStatusFailed,
		},
		{
			name: "mixed_ending_in_expiry", outcomes: []string{"expired", "failed", "expired"},
			finalStatus: services.TicketStatusFailed,
		},
		{
			name: "cancellation_does_not_consume_failure_budget", retryPolicy: `{"max_attempts":1}`,
			outcomes: []string{"cancelled", "failed"}, finalStatus: services.TicketStatusFailed,
		},
		{
			name: "mark_failed_before_exhaustion", retryPolicy: `{"max_attempts":3,"on_failure":"mark_failed"}`,
			outcomes: []string{"failed"}, finalStatus: services.TicketStatusFailed,
		},
		{
			name: "needs_review_before_exhaustion", retryPolicy: `{"max_attempts":3,"on_failure":"needs_review"}`,
			outcomes: []string{"failed"}, finalStatus: services.TicketStatusNeedsReview,
		},
		{
			name: "needs_review_overrides_exhaustion", retryPolicy: `{"max_attempts":1,"on_failure":"needs_review"}`,
			outcomes: []string{"failed"}, finalStatus: services.TicketStatusNeedsReview,
		},
		{
			name: "return_to_todo_still_exhausts", retryPolicy: `{"max_attempts":1,"on_failure":"return_to_todo"}`,
			outcomes: []string{"failed"}, finalStatus: services.TicketStatusFailed,
		},
		// Expiry retains its existing budget-only behavior; on_failure applies to explicit failures.
		{
			name: "expiry_ignores_mark_failed", retryPolicy: `{"max_attempts":2,"on_failure":"mark_failed"}`,
			outcomes: []string{"expired", "expired"}, finalStatus: services.TicketStatusFailed,
		},
		{
			name: "expiry_ignores_needs_review", retryPolicy: `{"max_attempts":1,"on_failure":"needs_review"}`,
			outcomes: []string{"expired"}, finalStatus: services.TicketStatusFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			workspace, project := createScope(t, fixture.runtime, fixture.context)
			ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
			if test.retryPolicy != "" {
				if _, err := fixture.runtime.Pool.Exec(fixture.context, "UPDATE tickets SET retry_policy = $1::jsonb WHERE id = $2", test.retryPolicy, ticket.ID); err != nil {
					t.Fatalf("set retry policy: %v", err)
				}
			}

			for index, outcome := range test.outcomes {
				claim, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "retry-agent", ""))
				if err != nil {
					t.Fatalf("claim attempt %d: %v", index+1, err)
				}
				if claim.Ticket.ID != ticket.ID {
					t.Fatalf("claimed another ticket: %v", claim.Ticket.ID)
				}
				cutoff := time.Now().UTC()
				if outcome == services.AttemptStatusExpired {
					setAttemptLease(t, fixture, claim.Attempt.ID, cutoff.Add(-time.Minute))
				}
				transition := func() (services.AttemptTransitionResult, error) {
					switch outcome {
					case services.AttemptStatusFailed:
						return fixture.runtime.Fail(fixture.context, services.FailAttemptRequest{
							AttemptID: claim.Attempt.ID, FailureReason: "retry regression",
						})
					case services.AttemptStatusExpired:
						return fixture.runtime.Attempts.Expire(fixture.context, services.ExpireAttemptRequest{
							AttemptID: claim.Attempt.ID, ExpirationCutoff: cutoff,
						})
					default:
						return fixture.runtime.Cancel(fixture.context, services.CancelAttemptRequest{
							AttemptID: claim.Attempt.ID, Reason: "does not consume a retry",
						})
					}
				}
				result, err := transition()
				if err != nil {
					t.Fatalf("%s attempt %d: %v", outcome, index+1, err)
				}
				wantStatus := services.TicketStatusTodo
				if index == len(test.outcomes)-1 {
					wantStatus = test.finalStatus
				}
				if result.AttemptID != claim.Attempt.ID || result.TicketID != ticket.ID || result.AttemptStatus != outcome || result.TicketStatus != wantStatus {
					t.Fatalf("attempt %d: want %s/ticket %s, got %#v", index+1, outcome, wantStatus, result)
				}
				if _, err := transition(); !errors.Is(err, services.ErrAttemptNotRunning) {
					t.Fatalf("duplicate transition must not consume another retry: %v", err)
				}
				storedTicket, err := fixture.runtime.GetTicket(fixture.context, ticket.ID)
				if err != nil || storedTicket.Status != wantStatus {
					t.Fatalf("attempt %d: stored ticket status = %q, want %q, err=%v", index+1, storedTicket.Status, wantStatus, err)
				}
				storedAttempt, err := fixture.runtime.GetAttempt(fixture.context, claim.Attempt.ID)
				if err != nil || storedAttempt.Status != outcome || !storedAttempt.CompletedAt.Valid {
					t.Fatalf("attempt %d not persisted as completed %s: %#v, err=%v", index+1, outcome, storedAttempt, err)
				}
				events, err := fixture.runtime.Queries.ListTicketEventsByTicket(fixture.context, ticket.ID)
				if err != nil {
					t.Fatalf("list retry events: %v", err)
				}
				var terminalEvents int
				for _, event := range events {
					if event.AttemptID != claim.Attempt.ID || event.Type != outcome {
						continue
					}
					terminalEvents++
					var data struct {
						TicketStatus string `json:"ticket_status"`
					}
					if err := json.Unmarshal(event.Data, &data); err != nil || data.TicketStatus != wantStatus {
						t.Fatalf("attempt %d: event status must match %q: %s, err=%v", index+1, wantStatus, event.Data, err)
					}
				}
				if terminalEvents != 1 {
					t.Fatalf("attempt %d: expected one terminal event, got %d", index+1, terminalEvents)
				}
			}

			if _, err := fixture.runtime.ClaimNext(fixture.context, claimRequest(workspace.ID, project.ID, "retry-agent", "")); !errors.Is(err, services.ErrNoClaimableTickets) {
				t.Fatalf("terminal ticket must not be claimable: %v", err)
			}
			attempts, err := fixture.runtime.Queries.ListAttemptsByTicket(fixture.context, ticket.ID)
			if err != nil || len(attempts) != len(test.outcomes) {
				t.Fatalf("expected exactly %d attempts, got %d, err=%v", len(test.outcomes), len(attempts), err)
			}
		})
	}
}

func TestRetryCyclesResetOnHumanRecovery(t *testing.T) {
	for _, test := range []struct {
		name        string
		retryPolicy string
		outcomes    []string
		needsReview bool
	}{
		{name: "default_three_failures", outcomes: []string{"failed", "failed", "failed"}},
		{name: "default_three_expiries", outcomes: []string{"expired", "expired", "expired"}},
		{name: "mixed_ending_in_failure", outcomes: []string{"failed", "expired", "failed"}},
		{name: "missing_fields_mixed_ending_in_expiry", retryPolicy: `{}`, outcomes: []string{"expired", "failed", "expired"}},
		{name: "configured_one_failure", retryPolicy: `{"max_attempts":1}`, outcomes: []string{"failed"}},
		{name: "configured_one_expiry", retryPolicy: `{"max_attempts":1}`, outcomes: []string{"expired"}},
		{name: "configured_two_mixed", retryPolicy: `{"max_attempts":2}`, outcomes: []string{"failed", "expired"}},
		{name: "review_rejection_after_policy_failure", retryPolicy: `{"max_attempts":1,"on_failure":"needs_review"}`, outcomes: []string{"failed"}, needsReview: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			ctx := fixture.context
			workspace, project := createScope(t, fixture.runtime, ctx)
			ticket := createClaimableTicket(t, fixture.runtime, ctx, workspace.ID, project.ID)
			if test.retryPolicy != "" {
				if _, err := fixture.runtime.Pool.Exec(ctx, "UPDATE tickets SET retry_policy = $1::jsonb WHERE id = $2", test.retryPolicy, ticket.ID); err != nil {
					t.Fatalf("set retry policy: %v", err)
				}
			}
			ticket, err := fixture.runtime.GetTicket(ctx, ticket.ID)
			if err != nil {
				t.Fatal(err)
			}

			// All claims and recovery events deliberately share PostgreSQL's now().
			// A timestamp-only boundary cannot distinguish these retry cycles.
			tx, err := fixture.runtime.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			queries := fixture.runtime.Queries.WithTx(tx)
			tickets := services.NewTicketService(queries)
			claims := services.NewClaimService(queries)
			attempts := services.NewAttemptService(queries)
			var transactionTime time.Time
			if err := tx.QueryRow(ctx, "SELECT now()").Scan(&transactionTime); err != nil {
				t.Fatal(err)
			}
			recoveries := []string{"reopen", "review_rejection", "reopen"}
			terminalStatus := services.TicketStatusFailed
			if test.needsReview {
				recoveries = []string{"review_rejection", "review_rejection"}
				terminalStatus = services.TicketStatusNeedsReview
			}
			request := claimRequest(workspace.ID, project.ID, "retry-cycle-agent", "")
			transitionRequest := services.TicketTransitionRequest{TicketID: ticket.ID, ActorType: services.ActorHuman, ActorID: "retry-operator"}
			reviewRequest := services.ReviewTicketRequest{TicketID: ticket.ID, Decision: services.ReviewDecisionReject, ActorType: services.ActorHuman, ActorID: "retry-operator"}
			transition := func(attemptID pgtype.UUID, outcome string) (services.AttemptTransitionResult, error) {
				if outcome == services.AttemptStatusExpired {
					return attempts.Expire(ctx, services.ExpireAttemptRequest{AttemptID: attemptID, ExpirationCutoff: transactionTime})
				}
				return attempts.Fail(ctx, services.FailAttemptRequest{AttemptID: attemptID, FailureReason: "retry cycle regression"})
			}
			history := make(map[pgtype.UUID]db.Attempt)
			var priorEvents []db.TicketEvent
			var previousAttempt pgtype.UUID
			for cycle := 0; cycle <= len(recoveries); cycle++ {
				var recoverTicket func() (db.Ticket, error)
				if cycle > 0 {
					if recoveries[cycle-1] == "reopen" {
						recoverTicket = func() (db.Ticket, error) { return tickets.Reopen(ctx, transitionRequest) }
					} else {
						if !test.needsReview {
							if _, err := tickets.RequestReview(ctx, transitionRequest); err != nil {
								t.Fatalf("cycle %d request review: %v", cycle, err)
							}
						}
						recoverTicket = func() (db.Ticket, error) { return tickets.Review(ctx, reviewRequest) }
					}
					reset, err := recoverTicket()
					if err != nil || reset.Status != services.TicketStatusTodo {
						t.Fatalf("cycle %d recovery: status=%q err=%v", cycle, reset.Status, err)
					}
					if _, err := recoverTicket(); !errors.Is(err, services.ErrTicketTransitionNotAllowed) {
						t.Fatalf("duplicate recovery must not start another cycle: %v", err)
					}
				}

				for index, outcome := range test.outcomes {
					claim, err := claims.ClaimNext(ctx, request)
					if err != nil {
						t.Fatalf("cycle %d claim %d: %v", cycle, index+1, err)
					}
					if claim.Ticket.ID != ticket.ID || len(claim.Context.PriorAttempts) != len(history) {
						t.Fatalf("claim must retain all %d prior attempts: %#v", len(history), claim)
					}
					if !claim.Attempt.StartedAt.Time.Equal(transactionTime) {
						t.Fatalf("expected tied attempt timestamps, got %v", claim.Attempt.StartedAt)
					}
					if recoverTicket != nil {
						if _, err := recoverTicket(); !errors.Is(err, services.ErrTicketTransitionNotAllowed) {
							t.Fatalf("recovery while running must not reset the cycle: %v", err)
						}
					}
					if previousAttempt.Valid {
						for _, staleOutcome := range []string{"failed", "expired"} {
							if _, err := transition(previousAttempt, staleOutcome); !errors.Is(err, services.ErrAttemptNotRunning) {
								t.Fatalf("stale %s must not change the new cycle: %v", staleOutcome, err)
							}
						}
					}
					if outcome == services.AttemptStatusExpired {
						if _, err := tx.Exec(ctx, "UPDATE attempts SET lease_expires_at = $1 WHERE id = $2", transactionTime.Add(-time.Minute), claim.Attempt.ID); err != nil {
							t.Fatal(err)
						}
					}
					result, err := transition(claim.Attempt.ID, outcome)
					wantStatus := services.TicketStatusTodo
					if index == len(test.outcomes)-1 {
						wantStatus = terminalStatus
					}
					if err != nil || result.AttemptStatus != outcome || result.TicketStatus != wantStatus {
						t.Fatalf("cycle %d attempt %d: want %s/%s, got %#v err=%v", cycle, index+1, outcome, wantStatus, result, err)
					}
					if _, err := transition(claim.Attempt.ID, outcome); !errors.Is(err, services.ErrAttemptNotRunning) {
						t.Fatalf("duplicate outcome must not consume or reset retries: %v", err)
					}
					stored, err := queries.GetAttempt(ctx, claim.Attempt.ID)
					if err != nil {
						t.Fatal(err)
					}
					history[stored.ID] = stored
					previousAttempt = stored.ID
				}

				storedTicket, err := queries.GetTicket(ctx, ticket.ID)
				if err != nil || storedTicket.Status != terminalStatus || !reflect.DeepEqual(storedTicket.RetryPolicy, ticket.RetryPolicy) {
					t.Fatalf("cycle %d must exhaust without changing policy: %#v err=%v", cycle, storedTicket, err)
				}
				if _, err := claims.ClaimNext(ctx, request); !errors.Is(err, services.ErrNoClaimableTickets) {
					t.Fatalf("exhausted cycle must not be claimable: %v", err)
				}
				// Probe the claim budget independently of the terminal status guard.
				// A bare todo update, without a recovery event, must NOT reset it.
				if _, err := tx.Exec(ctx, "UPDATE tickets SET status = 'todo' WHERE id = $1", ticket.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := claims.ClaimNext(ctx, request); !errors.Is(err, services.ErrNoClaimableTickets) {
					t.Fatalf("todo alone must not reset an exhausted cycle: %v", err)
				}
				if _, err := tx.Exec(ctx, "UPDATE tickets SET status = $1 WHERE id = $2", terminalStatus, ticket.ID); err != nil {
					t.Fatal(err)
				}
				events, err := queries.ListTicketEventsByTicket(ctx, ticket.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(events) < len(priorEvents) || (len(priorEvents) > 0 && !reflect.DeepEqual(events[:len(priorEvents)], priorEvents)) {
					t.Fatal("recovery changed historical events")
				}
				for _, event := range events {
					if event.Type != services.EventTicketCreated && !event.CreatedAt.Time.Equal(transactionTime) {
						t.Fatalf("expected tied event timestamps, got %v", event.CreatedAt)
					}
				}
				if countEvents(events, "failed")+countEvents(events, "expired") != len(history) || countEvents(events, "reopened")+countEvents(events, "reviewed") != cycle {
					t.Fatalf("duplicate transitions wrote extra events: %#v", events)
				}
				priorEvents = events
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			storedAttempts, err := fixture.runtime.Queries.ListAttemptsByTicket(ctx, ticket.ID)
			if err != nil || len(storedAttempts) != len(history) {
				t.Fatalf("expected %d historical attempts, got %d err=%v", len(history), len(storedAttempts), err)
			}
			for _, attempt := range storedAttempts {
				if !reflect.DeepEqual(attempt, history[attempt.ID]) {
					t.Fatalf("recovery changed historical attempt %v", attempt.ID)
				}
			}
		})
	}
}

func TestRetryCycleOtherTransitionsDoNotResetBudget(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context
	workspace, project := createScope(t, fixture.runtime, ctx)
	ticket := createClaimableTicket(t, fixture.runtime, ctx, workspace.ID, project.ID)
	request := claimRequest(workspace.ID, project.ID, "retry-cycle-agent", "")
	transitionRequest := services.TicketTransitionRequest{TicketID: ticket.ID, ActorType: services.ActorHuman, ActorID: "retry-operator"}
	claim := func() services.ClaimNextResult {
		t.Helper()
		result, err := fixture.runtime.ClaimNext(ctx, request)
		if err != nil {
			t.Fatalf("claim ticket: %v", err)
		}
		return result
	}
	first := claim()
	if result, err := fixture.runtime.Fail(ctx, services.FailAttemptRequest{AttemptID: first.Attempt.ID, FailureReason: "first failure"}); err != nil || result.TicketStatus != services.TicketStatusTodo {
		t.Fatalf("first failure: %#v err=%v", result, err)
	}
	if _, err := fixture.runtime.RequestReview(ctx, transitionRequest); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.runtime.Review(ctx, services.ReviewTicketRequest{TicketID: ticket.ID, Decision: services.ReviewDecisionApprove, ActorType: services.ActorHuman}); err != nil || result.Status != services.TicketStatusDone {
		t.Fatalf("approve review: %#v err=%v", result, err)
	}
	// Bypass only the status guard to verify that reviewed/status=done is not
	// a retry boundary. A real Reopen would intentionally reset the budget.
	if _, err := fixture.runtime.Pool.Exec(ctx, "UPDATE tickets SET status = 'todo' WHERE id = $1", ticket.ID); err != nil {
		t.Fatal(err)
	}
	cancelled := claim()
	if _, err := fixture.runtime.Cancel(ctx, services.CancelAttemptRequest{AttemptID: cancelled.Attempt.ID}); err != nil {
		t.Fatal(err)
	}
	blocked := claim()
	if _, err := fixture.runtime.Block(ctx, services.BlockAttemptRequest{AttemptID: blocked.Attempt.ID, BlockerReason: "needs input"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runtime.Unblock(ctx, transitionRequest); err != nil {
		t.Fatal(err)
	}
	second := claim()
	cutoff := time.Now().UTC()
	setAttemptLease(t, fixture, second.Attempt.ID, cutoff.Add(-time.Minute))
	if result, err := fixture.runtime.Attempts.Expire(ctx, services.ExpireAttemptRequest{AttemptID: second.Attempt.ID, ExpirationCutoff: cutoff}); err != nil || result.TicketStatus != services.TicketStatusTodo {
		t.Fatalf("second failure (expiry): %#v err=%v", result, err)
	}
	third := claim()
	if result, err := fixture.runtime.Fail(ctx, services.FailAttemptRequest{AttemptID: third.Attempt.ID, FailureReason: "third failure"}); err != nil || result.TicketStatus != services.TicketStatusFailed {
		t.Fatalf("approval, cancellation and unblock must not reset failures: %#v err=%v", result, err)
	}
	if _, err := fixture.runtime.Pool.Exec(ctx, "UPDATE tickets SET status = 'todo' WHERE id = $1", ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runtime.ClaimNext(ctx, request); !errors.Is(err, services.ErrNoClaimableTickets) {
		t.Fatalf("claim eligibility must also retain the exhausted budget: %v", err)
	}
	if _, err := fixture.runtime.Pool.Exec(ctx, "UPDATE tickets SET status = 'failed' WHERE id = $1", ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.runtime.Reopen(ctx, transitionRequest); err != nil {
		t.Fatal(err)
	}
	recovered := claim()
	if len(recovered.Context.PriorAttempts) != 5 {
		t.Fatalf("reopen must preserve failed, expired, cancelled and blocked history: %#v", recovered.Context.PriorAttempts)
	}
	if result, err := fixture.runtime.Fail(ctx, services.FailAttemptRequest{AttemptID: recovered.Attempt.ID, FailureReason: "first failure after reopen"}); err != nil || result.TicketStatus != services.TicketStatusTodo {
		t.Fatalf("reopen must reset the budget across committed transactions: %#v err=%v", result, err)
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

func TestWebhookStaleClaimCannotOverwriteNewOwner(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	subscription, err := fixture.runtime.Queries.CreateWebhookSubscription(fixture.context, db.CreateWebhookSubscriptionParams{WorkspaceID: workspace.ID, ProjectID: project.ID, EndpointUrl: "https://example.com/hooks", EventTypes: []string{"updated"}, Active: true, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	_, err = fixture.runtime.Queries.CreateTicketEvent(fixture.context, db.CreateTicketEventParams{WorkspaceID: workspace.ID, ProjectID: project.ID, TicketID: ticket.ID, Type: "updated", ActorType: services.ActorHuman, Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	now := time.Now().UTC()
	if _, err := fixture.runtime.Pool.Exec(fixture.context, "UPDATE webhook_deliveries SET next_attempt_at = $2 WHERE subscription_id = $1", subscription.ID, now.Add(-time.Second)); err != nil {
		t.Fatalf("make delivery claimable: %v", err)
	}
	tokenA := pgtype.UUID{Valid: true}
	tokenA.Bytes[15] = 1
	tokenB := pgtype.UUID{Valid: true}
	tokenB.Bytes[15] = 2
	first, err := fixture.runtime.Queries.ClaimPendingWebhookDeliveries(fixture.context, db.ClaimPendingWebhookDeliveriesParams{Now: pgtype.Timestamptz{Time: now, Valid: true}, LockedUntil: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true}, ClaimToken: tokenA, BatchLimit: 1})
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: rows=%#v err=%v", first, err)
	}
	if _, err := fixture.runtime.Pool.Exec(fixture.context, "UPDATE webhook_deliveries SET locked_until=$2 WHERE id=$1", first[0].ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := fixture.runtime.Queries.ClaimPendingWebhookDeliveries(fixture.context, db.ClaimPendingWebhookDeliveriesParams{Now: pgtype.Timestamptz{Time: now, Valid: true}, LockedUntil: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true}, ClaimToken: tokenB, BatchLimit: 1})
	if err != nil || len(second) != 1 {
		t.Fatalf("second claim: rows=%#v err=%v", second, err)
	}
	_, err = fixture.runtime.Queries.MarkWebhookDeliverySucceeded(fixture.context, db.MarkWebhookDeliverySucceededParams{ID: first[0].ID, ClaimToken: tokenA, AttemptCount: 1, AttemptedAt: pgtype.Timestamptz{Time: now, Valid: true}})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected stale owner rejection, got %v", err)
	}
}

func TestWebhookHeartbeatRequiresExplicitSubscription(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	ticket := createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	implicit, err := fixture.runtime.Queries.CreateWebhookSubscription(fixture.context, db.CreateWebhookSubscriptionParams{WorkspaceID: workspace.ID, ProjectID: project.ID, EndpointUrl: "https://all.example/hooks", EventTypes: []string{}, Active: true, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("create implicit subscription: %v", err)
	}
	explicit, err := fixture.runtime.Queries.CreateWebhookSubscription(fixture.context, db.CreateWebhookSubscriptionParams{WorkspaceID: workspace.ID, ProjectID: project.ID, EndpointUrl: "https://heartbeat.example/hooks", EventTypes: []string{"heartbeat"}, Active: true, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("create explicit subscription: %v", err)
	}
	event, err := fixture.runtime.Queries.CreateTicketEvent(fixture.context, db.CreateTicketEventParams{WorkspaceID: workspace.ID, ProjectID: project.ID, TicketID: ticket.ID, Type: "heartbeat", ActorType: services.ActorAgent, Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("create heartbeat event: %v", err)
	}
	deliveries, err := fixture.runtime.Queries.ListWebhookDeliveriesByEvent(fixture.context, event.ID)
	if err != nil {
		t.Fatalf("list heartbeat deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].SubscriptionID != explicit.ID {
		t.Fatalf("expected only explicit heartbeat subscription %v, got %#v (implicit %v)", explicit.ID, deliveries, implicit.ID)
	}
}

func TestScopeIntegrityRejectsCrossScopeWrites(t *testing.T) {
	fixture := newFixture(t)
	workspaceA, projectA := createScope(t, fixture.runtime, fixture.context)
	workspaceB, err := fixture.runtime.CreateWorkspace(fixture.context, "integration-workspace-b")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	projectB, err := fixture.runtime.CreateProject(fixture.context, workspaceB.ID, "integration-project-b")
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	ticketA := createClaimableTicket(t, fixture.runtime, fixture.context, workspaceA.ID, projectA.ID)
	ticketB := createClaimableTicket(t, fixture.runtime, fixture.context, workspaceB.ID, projectB.ID)
	now := time.Now().UTC().Add(time.Hour)
	assertScopeRejected(t, fixture, `INSERT INTO tickets (workspace_id, project_id, title, type, created_by) VALUES ($1, $2, 'cross project', 'task', 'human')`, workspaceA.ID, projectB.ID)
	assertScopeRejected(t, fixture, `INSERT INTO ticket_dependencies (ticket_id, depends_on_ticket_id, workspace_id, project_id) VALUES ($1, $2, $3, $4)`, ticketA.ID, ticketB.ID, workspaceA.ID, projectA.ID)
	assertScopeRejected(t, fixture, `INSERT INTO attempts (workspace_id, project_id, ticket_id, agent_id, harness, lease_expires_at) VALUES ($1, $2, $3, 'agent', 'harness', $4)`, workspaceA.ID, projectA.ID, ticketB.ID, now)

	claimA, err := fixture.runtime.ClaimNext(fixture.context, services.ClaimNextRequest{WorkspaceID: workspaceA.ID, ProjectID: projectA.ID, AgentID: "scope-agent-a", Harness: "test", Lease: time.Minute})
	if err != nil {
		t.Fatalf("claim scope A ticket: %v", err)
	}
	claimB, err := fixture.runtime.ClaimNext(fixture.context, services.ClaimNextRequest{WorkspaceID: workspaceB.ID, ProjectID: projectB.ID, AgentID: "scope-agent-b", Harness: "test", Lease: time.Minute})
	if err != nil {
		t.Fatalf("claim scope B ticket: %v", err)
	}
	assertScopeRejected(t, fixture, `INSERT INTO attempt_checkpoints (workspace_id, project_id, ticket_id, attempt_id, summary) VALUES ($1, $2, $3, $4, 'cross attempt')`, workspaceA.ID, projectA.ID, ticketA.ID, claimB.Attempt.ID)
	assertScopeRejected(t, fixture, `INSERT INTO ticket_events (workspace_id, project_id, ticket_id, attempt_id, type, actor_type) VALUES ($1, $2, $3, $4, 'updated', 'agent')`, workspaceA.ID, projectA.ID, ticketA.ID, claimB.Attempt.ID)
	assertScopeRejected(t, fixture, `INSERT INTO artifacts (workspace_id, project_id, ticket_id, attempt_id, type, role, name, url, storage_backend) VALUES ($1, $2, $3, $4, 'log', 'evidence', 'cross.log', 'file:///cross.log', 'local')`, workspaceA.ID, projectA.ID, ticketA.ID, claimB.Attempt.ID)
	assertScopeRejected(t, fixture, `INSERT INTO attempt_metrics (attempt_id, workspace_id, project_id) VALUES ($1, $2, $3)`, claimB.Attempt.ID, workspaceA.ID, projectA.ID)
	assertScopeRejected(t, fixture, `INSERT INTO agent_capabilities (workspace_id, project_id, agent_id, harness) VALUES ($1, $2, 'cross-agent', 'test')`, workspaceA.ID, projectB.ID)
	assertScopeRejected(t, fixture, `INSERT INTO webhook_subscriptions (workspace_id, project_id, endpoint_url) VALUES ($1, $2, 'https://example.com/hooks')`, workspaceA.ID, projectB.ID)

	eventB, err := fixture.runtime.Queries.CreateTicketEvent(fixture.context, db.CreateTicketEventParams{WorkspaceID: workspaceB.ID, ProjectID: projectB.ID, TicketID: ticketB.ID, AttemptID: claimB.Attempt.ID, Type: "updated", ActorType: services.ActorAgent, Data: []byte(`{}`)})
	if err != nil {
		t.Fatalf("create scope B event: %v", err)
	}
	subscriptionB, err := fixture.runtime.Queries.CreateWebhookSubscription(fixture.context, db.CreateWebhookSubscriptionParams{WorkspaceID: workspaceB.ID, ProjectID: projectB.ID, EndpointUrl: "https://example.com/scope-b", EventTypes: []string{}, Active: true, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("create scope B subscription: %v", err)
	}
	assertScopeRejected(t, fixture, `INSERT INTO webhook_deliveries (subscription_id, event_id, workspace_id, project_id, ticket_id, attempt_id, payload) VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb)`, subscriptionB.ID, eventB.ID, workspaceA.ID, projectA.ID, ticketA.ID, claimA.Attempt.ID)
}

func assertScopeRejected(t *testing.T, fixture *fixture, statement string, args ...any) {
	t.Helper()
	if _, err := fixture.runtime.Pool.Exec(fixture.context, statement, args...); err == nil {
		t.Fatalf("expected cross-scope write to fail: %s", statement)
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
