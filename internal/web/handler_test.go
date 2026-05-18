package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestTicketListRendersRowsAndStableDetailLinks(t *testing.T) {
	workspaceID := testUUID(1)
	projectID := testUUID(2)
	ticketID := testUUID(3)
	runtime := &fakeRuntime{
		tickets: []db.Ticket{
			{
				ID:          ticketID,
				WorkspaceID: workspaceID,
				ProjectID:   projectID,
				Title:       "Fix auth retry",
				Type:        services.TicketTypeBug,
				Status:      services.TicketStatusTodo,
				Priority:    1,
				Tags:        []string{"auth", "retry"},
				CreatedBy:   services.ActorAgent,
			},
		},
	}
	handler := NewHandler(runtime)

	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(workspaceID)+"&project_id="+uuidString(projectID)+"&status=todo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected html content type, got %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{"Forge Tickets", "Fix auth retry", "todo", "bug", "P1", "auth", "retry", "/tickets/" + uuidString(ticketID), `hx-boost="true"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected ticket list to contain %q, got:\n%s", want, body)
		}
	}
	if runtime.listReq.WorkspaceID != workspaceID || runtime.listReq.ProjectID != projectID {
		t.Fatalf("unexpected list scope: %#v", runtime.listReq)
	}
	if runtime.listReq.Status != services.TicketStatusTodo || runtime.listReq.Limit != 50 {
		t.Fatalf("unexpected list filters: %#v", runtime.listReq)
	}
}

func TestTicketListRendersEmptyAndBadRequestStates(t *testing.T) {
	handler := NewHandler(&fakeRuntime{})
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(testUUID(1))+"&project_id="+uuidString(testUUID(2)), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected empty list status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No tickets match") {
		t.Fatalf("expected empty state, got:\n%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/tickets", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing scope status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "workspace_id and project_id are required") {
		t.Fatalf("expected scope guidance, got:\n%s", rec.Body.String())
	}
}

func TestTicketDetailRendersContextAndTimeline(t *testing.T) {
	ticketID := testUUID(9)
	attemptID := testUUID(10)
	runtime := &fakeRuntime{
		ticket: db.Ticket{
			ID:                   ticketID,
			WorkspaceID:          testUUID(1),
			ProjectID:            testUUID(2),
			Title:                "Ship web inspection",
			Description:          "Make shared review links useful.",
			Type:                 services.TicketTypeFeature,
			Status:               services.TicketStatusInProgress,
			Priority:             2,
			AcceptanceCriteria:   []string{"Ticket detail renders context"},
			VerificationCommands: []byte(`["go test ./internal/web"]`),
			RelevantPaths:        []string{"internal/web/handler.go"},
			CreatedBy:            services.ActorHuman,
		},
		attempts: []db.Attempt{
			{
				ID:             attemptID,
				TicketID:       ticketID,
				Status:         "running",
				AgentID:        "codex",
				Model:          "gpt-5",
				CurrentSummary: pgtype.Text{String: "Building handlers", Valid: true},
			},
		},
		events: []db.TicketEvent{
			{
				TicketID:  ticketID,
				Type:      services.EventTicketReady,
				ActorType: services.ActorHuman,
				Data:      []byte(`{"reason":"ready for implementation"}`),
			},
		},
		artifacts: []db.Artifact{
			{
				TicketID: ticketID,
				Name:     "screenshot",
				Role:     "proof",
				Type:     "image",
				Url:      "https://example.test/proof.png",
			},
		},
	}
	handler := NewHandler(runtime)

	req := httptest.NewRequest(http.MethodGet, "/tickets/"+uuidString(ticketID), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected detail status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Ship web inspection",
		"Make shared review links useful.",
		"Ticket detail renders context",
		"go test ./internal/web",
		"internal/web/handler.go",
		"Attempts",
		"Building handlers",
		"ready for implementation",
		"https://example.test/proof.png",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected ticket detail to contain %q, got:\n%s", want, body)
		}
	}
	if runtime.detailTicketID != ticketID {
		t.Fatalf("expected detail loaders to use ticket id, got %#v", runtime.detailTicketID)
	}
}

func TestTicketDetailHandlesBadIDAndMissingRuntime(t *testing.T) {
	handler := NewHandler(&fakeRuntime{})
	req := httptest.NewRequest(http.MethodGet, "/tickets/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid id status 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ticket id must be a UUID") {
		t.Fatalf("expected invalid id guidance, got:\n%s", rec.Body.String())
	}

	handler = NewHandler(nil)
	req = httptest.NewRequest(http.MethodGet, "/tickets/"+uuidString(testUUID(1)), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected missing runtime status 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "runtime is not configured") {
		t.Fatalf("expected missing runtime message, got:\n%s", rec.Body.String())
	}
}

type fakeRuntime struct {
	listReq        services.ListTicketsRequest
	detailTicketID pgtype.UUID
	tickets        []db.Ticket
	ticket         db.Ticket
	attempts       []db.Attempt
	checkpoints    []db.AttemptCheckpoint
	events         []db.TicketEvent
	artifacts      []db.Artifact
}

func (f *fakeRuntime) ListTickets(_ context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	f.listReq = req
	return f.tickets, nil
}

func (f *fakeRuntime) GetTicket(_ context.Context, id pgtype.UUID) (db.Ticket, error) {
	f.detailTicketID = id
	return f.ticket, nil
}

func (f *fakeRuntime) ListAttemptsByTicket(_ context.Context, id pgtype.UUID) ([]db.Attempt, error) {
	f.detailTicketID = id
	return f.attempts, nil
}

func (f *fakeRuntime) ListAttemptCheckpointsByTicket(_ context.Context, id pgtype.UUID) ([]db.AttemptCheckpoint, error) {
	f.detailTicketID = id
	return f.checkpoints, nil
}

func (f *fakeRuntime) ListTicketEventsByTicket(_ context.Context, id pgtype.UUID) ([]db.TicketEvent, error) {
	f.detailTicketID = id
	return f.events, nil
}

func (f *fakeRuntime) ListArtifactsByTicket(_ context.Context, id pgtype.UUID) ([]db.Artifact, error) {
	f.detailTicketID = id
	return f.artifacts, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func uuidString(id pgtype.UUID) string {
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}
