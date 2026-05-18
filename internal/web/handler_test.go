package web

import (
	"context"
	"errors"
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
}

func TestTicketListRendersFilterFormWhenScopeIsMissing(t *testing.T) {
	runtime := &fakeRuntime{}
	handler := NewHandler(runtime)
	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing scope status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`<form method="get" action="/tickets">`, `name="workspace_id"`, `name="project_id"`, "workspace_id and project_id are required"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected missing scope page to contain %q, got:\n%s", want, body)
		}
	}
	if runtime.listReq.WorkspaceID.Valid || runtime.listReq.ProjectID.Valid {
		t.Fatalf("missing scope should not call ListTickets, got %#v", runtime.listReq)
	}
}

func TestTicketListReturnsBadRequestForInvalidFilterValidation(t *testing.T) {
	runtime := &fakeRuntime{
		listErr: services.ValidationError{Problems: []string{"status filter is not valid"}},
	}
	handler := NewHandler(runtime)
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(testUUID(1))+"&project_id="+uuidString(testUUID(2))+"&status=not-real", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid filter status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "status filter is not valid") {
		t.Fatalf("expected validation message, got:\n%s", rec.Body.String())
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

func TestTicketDetailDoesNotHideTimelineWhenUnusedCheckpointsFail(t *testing.T) {
	ticketID := testUUID(11)
	runtime := &fakeRuntime{
		ticket: db.Ticket{
			ID:          ticketID,
			WorkspaceID: testUUID(1),
			ProjectID:   testUUID(2),
			Title:       "Keep visible timeline",
			Status:      services.TicketStatusTodo,
			Type:        services.TicketTypeBug,
		},
		attempts: []db.Attempt{
			{
				ID:             testUUID(12),
				TicketID:       ticketID,
				Status:         "running",
				AgentID:        "codex",
				CurrentSummary: pgtype.Text{String: "Still visible", Valid: true},
			},
		},
		events: []db.TicketEvent{
			{TicketID: ticketID, Type: services.EventTicketReady, Data: []byte(`{"reason":"still visible"}`)},
		},
		artifacts:      []db.Artifact{{TicketID: ticketID, Name: "proof", Url: "https://example.test/proof"}},
		checkpointsErr: errors.New("checkpoint store unavailable"),
	}
	handler := NewHandler(runtime)
	req := httptest.NewRequest(http.MethodGet, "/tickets/"+uuidString(ticketID), nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected detail status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Still visible", "still visible", "https://example.test/proof"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected detail body to keep %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Timeline unavailable") {
		t.Fatalf("checkpoint failure should not hide displayed timeline sections:\n%s", body)
	}
}

func TestTicketDetailSuppressesUnsafeArtifactLinks(t *testing.T) {
	ticketID := testUUID(13)
	runtime := &fakeRuntime{
		ticket: db.Ticket{
			ID:          ticketID,
			WorkspaceID: testUUID(1),
			ProjectID:   testUUID(2),
			Title:       "Unsafe proof",
			Status:      services.TicketStatusTodo,
			Type:        services.TicketTypeBug,
		},
		artifacts: []db.Artifact{
			{TicketID: ticketID, Name: "safe", Url: "https://example.test/proof.png"},
			{TicketID: ticketID, Name: "unsafe", Url: "javascript:alert(1)"},
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
	if !strings.Contains(body, `href="https://example.test/proof.png"`) {
		t.Fatalf("expected safe proof link, got:\n%s", body)
	}
	if strings.Contains(body, "javascript:alert") || strings.Contains(body, `href="javascript`) {
		t.Fatalf("unsafe artifact URL should not render as text or href:\n%s", body)
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
	listErr        error
	ticket         db.Ticket
	attempts       []db.Attempt
	checkpoints    []db.AttemptCheckpoint
	checkpointsErr error
	events         []db.TicketEvent
	artifacts      []db.Artifact
}

func (f *fakeRuntime) ListTickets(_ context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	f.listReq = req
	if f.listErr != nil {
		return nil, f.listErr
	}
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
	if f.checkpointsErr != nil {
		return nil, f.checkpointsErr
	}
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
