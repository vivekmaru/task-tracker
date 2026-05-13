package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestClaimNextReturnsTicketAttemptAndContextBundle(t *testing.T) {
	now := time.Date(2026, 5, 13, 9, 30, 0, 0, time.UTC)
	ticketID := testUUID(10)
	attemptID := testUUID(11)
	priorAttemptID := testUUID(12)
	store := &fakeClaimStore{
		claimRow: db.ClaimNextTicketRow{
			TicketID:  ticketID,
			AttemptID: attemptID,
		},
		ticket: db.Ticket{
			ID:                   ticketID,
			WorkspaceID:          testUUID(1),
			ProjectID:            testUUID(2),
			Title:                "Fix auth retry bug",
			Type:                 TicketTypeBug,
			Status:               "in_progress",
			AcceptanceCriteria:   []string{"Retry succeeds after transient auth failure"},
			VerificationCommands: mustJSON(t, []string{"go test ./..."}),
			Environment:          mustJSON(t, map[string]any{"repo": "agent-task-tracker"}),
			Input:                mustJSON(t, map[string]any{"branch": "main"}),
			RelevantPaths:        []string{"internal/services"},
			RequiredTools:        []string{"go"},
			RequiredPermissions:  []string{"filesystem"},
			ExpectedArtifacts:    []string{"test output"},
		},
		attempt: db.Attempt{
			ID:       attemptID,
			TicketID: ticketID,
			AgentID:  "codex",
			Harness:  "codex",
			Model:    "gpt-5",
			Status:   "running",
		},
		attempts: []db.Attempt{
			{ID: attemptID, TicketID: ticketID, Status: "running"},
			{ID: priorAttemptID, TicketID: ticketID, Status: "failed"},
		},
		checkpoints: []db.AttemptCheckpoint{
			{ID: testUUID(13), TicketID: ticketID, AttemptID: priorAttemptID, Summary: "Found failing retry branch"},
		},
		artifacts: []db.Artifact{
			{ID: testUUID(14), TicketID: ticketID, Role: "diagnostic", Name: "failure.log"},
		},
	}
	service := NewClaimService(store, WithClaimClock(func() time.Time { return now }))

	result, err := service.ClaimNext(context.Background(), ClaimNextRequest{
		WorkspaceID:  testUUID(1),
		ProjectID:    testUUID(2),
		Type:         TicketTypeBug,
		Tags:         []string{" backend ", ""},
		Harness:      "codex",
		Capabilities: []string{"codegen", "testing"},
		AgentID:      "codex",
		Model:        "gpt-5",
		Lease:        30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("claim next: %v", err)
	}

	params := store.claimParams[0]
	if params.TicketType.String != TicketTypeBug || !params.TicketType.Valid {
		t.Fatalf("expected bug type filter, got %#v", params.TicketType)
	}
	if got, want := params.Tags, []string{"backend"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected compacted tags %#v, got %#v", want, got)
	}
	if !params.LeaseExpiresAt.Time.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("expected lease expiry %v, got %v", now.Add(30*time.Minute), params.LeaseExpiresAt.Time)
	}
	if result.Ticket.ID != ticketID {
		t.Fatalf("expected ticket %v, got %v", ticketID, result.Ticket.ID)
	}
	if result.Attempt.ID != attemptID {
		t.Fatalf("expected attempt %v, got %v", attemptID, result.Attempt.ID)
	}
	if len(result.Context.PriorAttempts) != 1 || result.Context.PriorAttempts[0].ID != priorAttemptID {
		t.Fatalf("expected only prior attempt in context, got %#v", result.Context.PriorAttempts)
	}
	if result.Context.VerificationCommands[0] != "go test ./..." {
		t.Fatalf("expected decoded verification command, got %#v", result.Context.VerificationCommands)
	}
	if result.Context.Environment["repo"] != "agent-task-tracker" {
		t.Fatalf("expected decoded environment, got %#v", result.Context.Environment)
	}
	if len(result.Context.Checkpoints) != 1 || len(result.Context.Artifacts) != 1 {
		t.Fatalf("expected checkpoint and artifact context, got %#v %#v", result.Context.Checkpoints, result.Context.Artifacts)
	}
}

func TestClaimNextValidationRequiresCorrectnessFields(t *testing.T) {
	service := NewClaimService(&fakeClaimStore{})

	_, err := service.ClaimNext(context.Background(), ClaimNextRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	for _, want := range []string{
		"workspace_id is required",
		"project_id is required",
		"agent_id is required",
		"harness is required",
		"lease must be positive",
	} {
		if !containsString(validationErr.Problems, want) {
			t.Fatalf("expected validation problem %q in %#v", want, validationErr.Problems)
		}
	}
}

func TestClaimNextMapsNoRowsToNoClaimableTickets(t *testing.T) {
	service := NewClaimService(&fakeClaimStore{claimErr: pgx.ErrNoRows})

	_, err := service.ClaimNext(context.Background(), ClaimNextRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Harness:     "codex",
		AgentID:     "codex",
		Lease:       time.Minute,
	})
	if !errors.Is(err, ErrNoClaimableTickets) {
		t.Fatalf("expected no claimable tickets error, got %v", err)
	}
}

type fakeClaimStore struct {
	claimParams []db.ClaimNextTicketParams
	claimRow    db.ClaimNextTicketRow
	claimErr    error
	ticket      db.Ticket
	attempt     db.Attempt
	attempts    []db.Attempt
	checkpoints []db.AttemptCheckpoint
	artifacts   []db.Artifact
}

func (s *fakeClaimStore) ClaimNextTicket(_ context.Context, params db.ClaimNextTicketParams) (db.ClaimNextTicketRow, error) {
	s.claimParams = append(s.claimParams, params)
	return s.claimRow, s.claimErr
}

func (s *fakeClaimStore) GetTicket(_ context.Context, id pgtype.UUID) (db.Ticket, error) {
	if id != s.ticket.ID {
		return db.Ticket{}, pgx.ErrNoRows
	}
	return s.ticket, nil
}

func (s *fakeClaimStore) GetAttempt(_ context.Context, id pgtype.UUID) (db.Attempt, error) {
	if id != s.attempt.ID {
		return db.Attempt{}, pgx.ErrNoRows
	}
	return s.attempt, nil
}

func (s *fakeClaimStore) ListAttemptsByTicket(_ context.Context, id pgtype.UUID) ([]db.Attempt, error) {
	if id != s.ticket.ID {
		return nil, pgx.ErrNoRows
	}
	return s.attempts, nil
}

func (s *fakeClaimStore) ListAttemptCheckpointsByTicket(_ context.Context, id pgtype.UUID) ([]db.AttemptCheckpoint, error) {
	if id != s.ticket.ID {
		return nil, pgx.ErrNoRows
	}
	return s.checkpoints, nil
}

func (s *fakeClaimStore) ListArtifactsByTicket(_ context.Context, id pgtype.UUID) ([]db.Artifact, error) {
	if id != s.ticket.ID {
		return nil, pgx.ErrNoRows
	}
	return s.artifacts, nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
