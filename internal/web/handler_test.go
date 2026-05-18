package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestAuthenticatedHandlerRedirectsUnauthenticatedWebRequests(t *testing.T) {
	handler := NewHandlerWithAuth(&fakeRuntime{}, AuthOptions{AdminToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(testUUID(1))+"&project_id="+uuidString(testUUID(2)), nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect to login, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/login?next=%2Ftickets%3Fworkspace_id%3D00000000-0000-0000-0000-000000000001%26project_id%3D00000000-0000-0000-0000-000000000002" {
		t.Fatalf("unexpected login redirect: %q", got)
	}
}

func TestAuthenticatedHandlerAcceptsBearerToken(t *testing.T) {
	workspaceID := testUUID(1)
	projectID := testUUID(2)
	runtime := &fakeRuntime{}
	handler := NewHandlerWithAuth(runtime, AuthOptions{AdminToken: "secret-token"})
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(workspaceID)+"&project_id="+uuidString(projectID), nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected authorized request status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if runtime.listReq.WorkspaceID != workspaceID || runtime.listReq.ProjectID != projectID {
		t.Fatalf("expected authorized request to reach runtime, got %#v", runtime.listReq)
	}
}

func TestLoginCreatesSessionCookieWithoutEchoingToken(t *testing.T) {
	workspaceID := testUUID(1)
	projectID := testUUID(2)
	runtime := &fakeRuntime{}
	handler := NewHandlerWithAuth(runtime, AuthOptions{AdminToken: "secret-token"})
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("admin_token=secret-token&next=%2Ftickets%3Fworkspace_id%3D"+uuidString(workspaceID)+"%26project_id%3D"+uuidString(projectID)))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()

	handler.ServeHTTP(loginRec, login)

	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("expected successful login redirect, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	if strings.Contains(loginRec.Body.String(), "secret-token") {
		t.Fatalf("login response should not echo the admin token:\n%s", loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %#v", cookies)
	}
	if cookies[0].Value == "" || cookies[0].Value == "secret-token" {
		t.Fatalf("session cookie should be opaque, got %q", cookies[0].Value)
	}

	req := httptest.NewRequest(http.MethodGet, loginRec.Header().Get("Location"), nil)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected session request status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if runtime.listReq.WorkspaceID != workspaceID || runtime.listReq.ProjectID != projectID {
		t.Fatalf("expected session request to reach runtime, got %#v", runtime.listReq)
	}
}

func TestAuthenticatedHandlerRejectsExpiredSessionCookie(t *testing.T) {
	now := time.Date(2026, 5, 18, 20, 0, 0, 0, time.UTC)
	auth := AuthOptions{
		AdminToken: "secret-token",
		SessionTTL: time.Hour,
		Now: func() time.Time {
			return now
		},
	}.normalized()
	runtime := &fakeRuntime{}
	handler := NewHandlerWithAuth(runtime, auth)
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id="+uuidString(testUUID(1))+"&project_id="+uuidString(testUUID(2)), nil)
	req.AddCookie(&http.Cookie{
		Name:  auth.cookieName(),
		Value: auth.sessionValue(now.Add(-time.Minute)),
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected expired session redirect, got %d: %s", rec.Code, rec.Body.String())
	}
	if runtime.listReq.WorkspaceID.Valid || runtime.listReq.ProjectID.Valid {
		t.Fatalf("expired session should not reach runtime, got %#v", runtime.listReq)
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

func TestTicketDetailRendersShareableDeepLinks(t *testing.T) {
	ticketID := testUUID(31)
	attemptID := testUUID(32)
	artifactID := testUUID(33)
	runtime := &fakeRuntime{
		ticket: db.Ticket{
			ID:          ticketID,
			WorkspaceID: testUUID(1),
			ProjectID:   testUUID(2),
			Title:       "Proposed follow-up",
			Type:        services.TicketTypeFollowUp,
			Status:      services.TicketStatusBacklog,
			CreatedBy:   services.ActorAgent,
		},
		attempts: []db.Attempt{{ID: attemptID, TicketID: ticketID, Status: services.AttemptStatusRunning, AgentID: "codex"}},
		artifacts: []db.Artifact{{
			ID:       artifactID,
			TicketID: ticketID,
			Name:     "proof",
			Role:     services.ArtifactRoleEvidence,
			Type:     services.ArtifactTypeTestOutput,
			Url:      "https://example.test/proof.txt",
		}},
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
		"Share links",
		"/tickets/" + uuidString(ticketID),
		"/proposed/" + uuidString(ticketID),
		"/attempts/" + uuidString(attemptID),
		"/artifacts/" + uuidString(artifactID),
		"copy-link",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected share links to contain %q, got:\n%s", want, body)
		}
	}
}

func TestAttemptArtifactAndProposedRoutesRenderStableInspectionPages(t *testing.T) {
	ticketID := testUUID(41)
	attemptID := testUUID(42)
	artifactID := testUUID(43)
	runtime := &fakeRuntime{
		ticket: db.Ticket{
			ID:          ticketID,
			WorkspaceID: testUUID(1),
			ProjectID:   testUUID(2),
			Title:       "Follow-up from attempt",
			Type:        services.TicketTypeFollowUp,
			Status:      services.TicketStatusBacklog,
			CreatedBy:   services.ActorAgent,
		},
		attempt: db.Attempt{
			ID:             attemptID,
			TicketID:       ticketID,
			Status:         services.AttemptStatusBlocked,
			AgentID:        "codex",
			Model:          "gpt-5",
			CurrentSummary: pgtype.Text{String: "Needs staging token", Valid: true},
		},
		attemptArtifacts: []db.Artifact{{
			ID:        artifactID,
			TicketID:  ticketID,
			AttemptID: attemptID,
			Name:      "blocked-proof",
			Role:      services.ArtifactRoleEvidence,
			Type:      services.ArtifactTypeTestOutput,
		}},
		artifact: db.Artifact{
			ID:        artifactID,
			TicketID:  ticketID,
			AttemptID: attemptID,
			Name:      "blocked-proof",
			Role:      services.ArtifactRoleEvidence,
			Type:      services.ArtifactTypeTestOutput,
			Url:       "https://example.test/blocked-proof.txt",
		},
	}
	handler := NewHandler(runtime)

	for _, tc := range []struct {
		path string
		want []string
	}{
		{path: "/attempts/" + uuidString(attemptID), want: []string{"Attempt Detail", "Needs staging token", "/tickets/" + uuidString(ticketID), "/artifacts/" + uuidString(artifactID)}},
		{path: "/artifacts/" + uuidString(artifactID), want: []string{"Artifact Detail", "blocked-proof", "https://example.test/blocked-proof.txt", "/tickets/" + uuidString(ticketID), "/attempts/" + uuidString(attemptID)}},
		{path: "/proposed/" + uuidString(ticketID), want: []string{"Proposed Follow-up", "Follow-up from attempt", "/tickets/" + uuidString(ticketID)}},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected status 200, got %d: %s", tc.path, rec.Code, rec.Body.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("%s: expected body to contain %q, got:\n%s", tc.path, want, rec.Body.String())
			}
		}
	}
}

func TestWorkspaceAdminRendersAndCreatesWorkspaceAndProject(t *testing.T) {
	workspaceID := testUUID(51)
	projectID := testUUID(52)
	runtime := &fakeRuntime{
		workspaces:       []db.Workspace{{ID: workspaceID, Name: "Core"}},
		projects:         []db.Project{{ID: projectID, WorkspaceID: workspaceID, Name: "Runtime"}},
		createdWorkspace: db.Workspace{ID: testUUID(53), Name: "Docs"},
		createdProject:   db.Project{ID: testUUID(54), WorkspaceID: workspaceID, Name: "Web"},
	}
	handler := NewHandler(runtime)

	req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected workspace index status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"Workspaces", "Core", "/workspaces/" + uuidString(workspaceID), `name="name"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("expected workspace index to contain %q, got:\n%s", want, rec.Body.String())
		}
	}
	for _, forbidden := range []string{"Kanban", "Sprint", "custom field"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("workspace admin should avoid dashboard language %q, got:\n%s", forbidden, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/workspaces/"+uuidString(workspaceID), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected workspace detail status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"Workspace", "Core", "Projects", "Runtime", "/tickets?workspace_id=" + uuidString(workspaceID) + "&project_id=" + uuidString(projectID), `action="/workspaces/` + uuidString(workspaceID) + `/projects"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("expected workspace detail to contain %q, got:\n%s", want, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader("name=Docs"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/workspaces/"+uuidString(testUUID(53)) {
		t.Fatalf("expected workspace create redirect, got %d %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if runtime.createdWorkspaceName != "Docs" {
		t.Fatalf("expected workspace create name, got %q", runtime.createdWorkspaceName)
	}

	req = httptest.NewRequest(http.MethodPost, "/workspaces/"+uuidString(workspaceID)+"/projects", strings.NewReader("name=Web"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/workspaces/"+uuidString(workspaceID) {
		t.Fatalf("expected project create redirect, got %d %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if runtime.createdProjectWorkspaceID != workspaceID || runtime.createdProjectName != "Web" {
		t.Fatalf("unexpected project create request: %#v %q", runtime.createdProjectWorkspaceID, runtime.createdProjectName)
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
	listReq                   services.ListTicketsRequest
	detailTicketID            pgtype.UUID
	tickets                   []db.Ticket
	listErr                   error
	ticket                    db.Ticket
	attempt                   db.Attempt
	attempts                  []db.Attempt
	checkpoints               []db.AttemptCheckpoint
	checkpointsErr            error
	events                    []db.TicketEvent
	artifacts                 []db.Artifact
	attemptArtifacts          []db.Artifact
	artifact                  db.Artifact
	workspaces                []db.Workspace
	projects                  []db.Project
	createdWorkspace          db.Workspace
	createdWorkspaceName      string
	createdProject            db.Project
	createdProjectWorkspaceID pgtype.UUID
	createdProjectName        string
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

func (f *fakeRuntime) GetAttempt(_ context.Context, id pgtype.UUID) (db.Attempt, error) {
	f.detailTicketID = id
	return f.attempt, nil
}

func (f *fakeRuntime) ListArtifactsByAttempt(_ context.Context, id pgtype.UUID) ([]db.Artifact, error) {
	f.detailTicketID = id
	return f.attemptArtifacts, nil
}

func (f *fakeRuntime) GetArtifact(_ context.Context, id pgtype.UUID) (db.Artifact, error) {
	f.detailTicketID = id
	return f.artifact, nil
}

func (f *fakeRuntime) ListWorkspaces(context.Context) ([]db.Workspace, error) {
	return f.workspaces, nil
}

func (f *fakeRuntime) GetWorkspace(_ context.Context, id pgtype.UUID) (db.Workspace, error) {
	for _, workspace := range f.workspaces {
		if workspace.ID == id {
			return workspace, nil
		}
	}
	return db.Workspace{}, nil
}

func (f *fakeRuntime) CreateWorkspace(_ context.Context, name string) (db.Workspace, error) {
	f.createdWorkspaceName = name
	return f.createdWorkspace, nil
}

func (f *fakeRuntime) ListProjectsByWorkspace(_ context.Context, id pgtype.UUID) ([]db.Project, error) {
	f.detailTicketID = id
	return f.projects, nil
}

func (f *fakeRuntime) CreateProject(_ context.Context, workspaceID pgtype.UUID, name string) (db.Project, error) {
	f.createdProjectWorkspaceID = workspaceID
	f.createdProjectName = name
	return f.createdProject, nil
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
