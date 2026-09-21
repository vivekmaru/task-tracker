package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

type firstUseRuntime struct {
	fakeRuntime
	createReq   services.CreateTicketRequest
	createErr   error
	createCalls int
	reviewReq   services.ReviewTicketRequest
}

func (f *firstUseRuntime) CreateTicket(_ context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	f.createCalls++
	f.createReq = req
	return db.Ticket{ID: testUUID(3)}, f.createErr
}

func (f *firstUseRuntime) Review(_ context.Context, req services.ReviewTicketRequest) (db.Ticket, error) {
	f.reviewReq = req
	return db.Ticket{ID: req.TicketID}, f.ticketActionErr
}

func firstUseFixture() *firstUseRuntime {
	return &firstUseRuntime{fakeRuntime: fakeRuntime{
		workspaces: []db.Workspace{{ID: testUUID(1), Name: "Engineering & research"}},
		projects:   []db.Project{{ID: testUUID(2), WorkspaceID: testUUID(1), Name: "Agent operations"}},
	}}
}

func TestScopePickersUseNamesAndRetainWorkspaceWhileChoosingProject(t *testing.T) {
	for _, path := range []string{"/tickets", "/search", "/proposed", "/artifacts", "/events", "/tickets/new"} {
		t.Run(path, func(t *testing.T) {
			rt := firstUseFixture()
			rec := httptest.NewRecorder()
			NewHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?workspace_id="+uuidString(testUUID(1)), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range []string{`<select name="workspace_id"`, `value="` + uuidString(testUUID(1)) + `" selected>Engineering &amp; research`, "Agent operations", `<select name="project_id"`} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
			if strings.Contains(body, `<input name="workspace_id"`) {
				t.Error("scope should use a named selector")
			}
		})
	}
}

func TestDetailNavigationKeepsProjectScope(t *testing.T) {
	rt := firstUseFixture()
	rt.ticket = db.Ticket{ID: testUUID(3), WorkspaceID: testUUID(1), ProjectID: testUUID(2), Title: "Scoped work", Status: "needs_review"}
	rt.attempt = db.Attempt{ID: testUUID(4), TicketID: testUUID(3), WorkspaceID: testUUID(1), ProjectID: testUUID(2)}
	rt.artifact = db.Artifact{ID: testUUID(5), WorkspaceID: testUUID(1), ProjectID: testUUID(2), TicketID: testUUID(3)}
	for _, path := range []string{"/tickets/" + uuidString(testUUID(3)), "/attempts/" + uuidString(testUUID(4)), "/artifacts/" + uuidString(testUUID(5))} {
		rec := httptest.NewRecorder()
		NewHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		for _, route := range []string{"/tickets", "/search", "/proposed", "/artifacts", "/events"} {
			want := `href="` + esc(scopedPagePath(route, uuidString(testUUID(1)), uuidString(testUUID(2)))) + `"`
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("%s missing scoped %s", path, route)
			}
		}
	}
}

func TestNewTicketCreatesFromBrowserForm(t *testing.T) {
	rt := firstUseFixture()
	values := url.Values{"action": {"create"}, "workspace_id": {uuidString(testUUID(1))}, "project_id": {uuidString(testUUID(2))}, "title": {" Fix retries "}, "type": {"bug"}, "priority": {"1"}, "description": {"A bounded task"}, "acceptance": {"One outcome\n\nAnother outcome"}, "verification": {"go test ./...\n"}}
	req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewHandler(rt).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/tickets/"+uuidString(testUUID(3)) {
		t.Fatalf("unexpected create response: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rt.createCalls != 1 || rt.createReq.WorkspaceID != testUUID(1) || rt.createReq.ProjectID != testUUID(2) || rt.createReq.Type != "bug" || rt.createReq.Title != "Fix retries" || rt.createReq.CreatedBy != services.ActorHuman || rt.createReq.CreatedByID != "web" || *rt.createReq.Priority != 1 || len(rt.createReq.AcceptanceCriteria) != 2 || len(rt.createReq.VerificationCommands) != 1 {
		t.Fatalf("unexpected request: %#v", rt.createReq)
	}
}

func newTicketDraftValues() url.Values {
	return url.Values{
		"workspace_id": {uuidString(testUUID(1))}, "project_id": {uuidString(testUUID(2))},
		"title":        {`"><script>alert("draft")</script>`},
		"description":  {"Context & details\n</textarea><script>alert(1)</script>"},
		"acceptance":   {"One <outcome>\n\nKeep \"quotes\" & spacing"},
		"verification": {"go test ./...\n</textarea><img src=x onerror=alert(1)>"},
		"type":         {"customtype"}, "priority": {"4"},
	}
}

func assertNewTicketDraft(t *testing.T, body string, values url.Values) {
	t.Helper()
	if strings.Count(body, "<form ") != 1 {
		t.Fatal("scope and draft must share a single form")
	}
	_, form, ok := strings.Cut(body, `<form class="panel ticket-create"`)
	if !ok {
		t.Fatal("missing editable draft form")
	}
	form, _, ok = strings.Cut(form, "</form>")
	if !ok || !strings.Contains(form, `method="post" action="/tickets/new" hx-boost="false"`) {
		t.Fatal("draft must post to /tickets/new without query parameters")
	}
	for _, field := range []string{"workspace_id", "project_id", "title", "description", "acceptance", "verification", "type", "priority"} {
		if strings.Count(form, `name="`+field+`"`) != 1 {
			t.Errorf("expected exactly one %s control in draft form", field)
		}
		if field != "workspace_id" && field != "project_id" && (strings.Contains(body, "?"+field+"=") || strings.Contains(body, "&amp;"+field+"=")) {
			t.Errorf("%s must not be transported in a query", field)
		}
	}
	if !strings.Contains(form, `<input name="title" value="`+esc(values.Get("title"))+`"`) {
		t.Error("title not retained and escaped")
	}
	for _, field := range []string{"description", "acceptance", "verification"} {
		_, textarea, _ := strings.Cut(form, `<textarea name="`+field+`"`)
		_, content, _ := strings.Cut(textarea, ">")
		if !strings.HasPrefix(content, esc(values.Get(field))+"</textarea>") {
			t.Errorf("%s not retained and escaped", field)
		}
	}
	for _, field := range []string{"type", "priority"} {
		_, selectField, _ := strings.Cut(form, `<select name="`+field+`"`)
		selectField, _, _ = strings.Cut(selectField, "</select>")
		if !strings.Contains(selectField, `value="`+esc(values.Get(field))+`" selected>`) {
			t.Errorf("%s selection reset on redisplay", field)
		}
	}
	if strings.Contains(form, "<script>") || strings.Contains(form, "<img src=x") {
		t.Error("draft rendered unescaped HTML")
	}
	if strings.Contains(form, "<fieldset") || strings.Count(form, " disabled") > 1 {
		t.Error("scope selection and draft editing must remain enabled")
	}
	for _, want := range []string{
		`name="action" value="select_project" formnovalidate>Select project</button>`,
		`onchange="` + esc("this.form.elements.project_id.value='';this.form.requestSubmit(this.form.querySelector('button[value=select_project]'))") + `"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("missing validation-free scope submission: %s", want)
		}
	}
}

func TestNewTicketScopeChangesPreserveDraft(t *testing.T) {
	for _, tc := range []struct {
		name       string
		workspace  string
		project    string
		canCreate  bool
		emptyTitle bool
		emptyDB    bool
	}{
		{name: "select valid project", workspace: uuidString(testUUID(1)), project: uuidString(testUUID(2)), canCreate: true},
		{name: "select project without title", workspace: uuidString(testUUID(1)), project: uuidString(testUUID(2)), canCreate: true, emptyTitle: true},
		{name: "workspace change clears project", workspace: uuidString(testUUID(7))},
		{name: "workspace change without title", workspace: uuidString(testUUID(7)), emptyTitle: true},
		{name: "workspace change with stale project", workspace: uuidString(testUUID(7)), project: uuidString(testUUID(2))},
		{name: "project in new workspace", workspace: uuidString(testUUID(7)), project: uuidString(testUUID(8)), canCreate: true},
		{name: "unknown project", workspace: uuidString(testUUID(1)), project: uuidString(testUUID(9))},
		{name: "malformed project", workspace: uuidString(testUUID(1)), project: "not-a-uuid"},
		{name: "no project", workspace: uuidString(testUUID(1))},
		{name: "cleared workspace"},
		{name: "no workspaces yet", emptyDB: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := firstUseFixture()
			rt.workspaces = append(rt.workspaces, db.Workspace{ID: testUUID(7), Name: "Second workspace"})
			if tc.workspace == uuidString(testUUID(7)) {
				rt.projects = []db.Project{{ID: testUUID(8), WorkspaceID: testUUID(7), Name: "Second project"}}
			}
			if tc.emptyDB {
				rt.workspaces, rt.projects = nil, nil
			}
			values := newTicketDraftValues()
			values.Set("action", "select_project")
			values.Set("workspace_id", tc.workspace)
			values.Set("project_id", tc.project)
			if tc.emptyTitle {
				values.Set("title", "")
			}
			req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			NewHandler(rt).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rt.createCalls != 0 {
				t.Fatalf("scope selection attempted creation: status %d, calls %d", rec.Code, rt.createCalls)
			}
			body := rec.Body.String()
			assertNewTicketDraft(t, body, values)
			disabled := strings.Contains(body, `value="create" disabled>Create ticket</button>`)
			if disabled == tc.canCreate {
				t.Errorf("Create ticket disabled = %v, want %v", disabled, !tc.canCreate)
			}
			if tc.workspace != "" && !strings.Contains(body, `value="`+tc.workspace+`" selected>`) {
				t.Error("workspace selection was lost")
			}
			if tc.canCreate && !strings.Contains(body, `value="`+tc.project+`" selected>`) {
				t.Error("project selection was lost")
			}
			if strings.Contains(body, `role="alert"`) || strings.Contains(body, "create your first workspace") || rt.createdWorkspaceName != "" {
				t.Error("scope selection should not trigger validation or require workspace creation")
			}
			if rec.Header().Get("Location") != "" || rec.Header().Get("Set-Cookie") != "" {
				t.Error("draft must redisplay directly, not redirect or persist in a session")
			}
		})
	}
}

func TestNewTicketGETAllowsEditingBeforeSelectingScope(t *testing.T) {
	for _, query := range []string{"", "?workspace_id=" + uuidString(testUUID(1)), "?workspace_id=" + uuidString(testUUID(1)) + "&project_id=" + uuidString(testUUID(9))} {
		rt := firstUseFixture()
		rec := httptest.NewRecorder()
		NewHandler(rt).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tickets/new"+query, nil))
		if rec.Code != http.StatusOK || rt.createCalls != 0 {
			t.Fatalf("unexpected GET response: %d", rec.Code)
		}
		assertNewTicketDraft(t, rec.Body.String(), url.Values{"type": {"task"}, "priority": {"2"}})
		if !strings.Contains(rec.Body.String(), `value="create" disabled>Create ticket</button>`) {
			t.Error("Create ticket must be disabled until a valid project is selected")
		}
	}
}

func TestNewTicketRejectsCrossWorkspaceProject(t *testing.T) {
	rt := firstUseFixture()
	rt.projects[0].WorkspaceID = testUUID(7)
	values := newTicketDraftValues()
	values.Set("action", "create")
	req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewHandler(rt).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || rt.createCalls != 0 {
		t.Fatalf("cross-workspace creation reached runtime: status %d", rec.Code)
	}
	assertNewTicketDraft(t, rec.Body.String(), values)
	if !strings.Contains(rec.Body.String(), `value="create" disabled>Create ticket</button>`) {
		t.Error("Create ticket must be disabled for a cross-workspace project")
	}
}

func TestNewTicketPreservesAndEscapesInvalidForm(t *testing.T) {
	rt := firstUseFixture()
	rt.createErr = services.ValidationError{Problems: []string{"acceptance_criteria is required <script>alert(2)</script>"}}
	values := newTicketDraftValues()
	req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewHandler(rt).ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || rt.createCalls != 1 || !strings.Contains(body, "acceptance_criteria is required &lt;script&gt;") || strings.Contains(body, "<script>alert") || !strings.Contains(body, `role="alert"`) {
		t.Fatalf("invalid form not preserved safely: status %d", rec.Code)
	}
	assertNewTicketDraft(t, body, values)
	if rt.createReq.Type != "customtype" || *rt.createReq.Priority != 4 {
		t.Fatalf("draft choices reset before creation: %#v", rt.createReq)
	}
}

func TestNewTicketPreservesInvalidChoicesOnRedisplay(t *testing.T) {
	for _, action := range []string{"select_project", "create"} {
		for _, choice := range []string{"", `"><script>alert(1)</script>`} {
			t.Run(action+"/"+choice, func(t *testing.T) {
				rt := firstUseFixture()
				values := newTicketDraftValues()
				values.Set("action", action)
				values.Set("type", choice)
				values.Set("priority", choice)
				req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader(values.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rec := httptest.NewRecorder()
				NewHandler(rt).ServeHTTP(rec, req)
				wantStatus := http.StatusOK
				if action == "create" {
					wantStatus = http.StatusBadRequest
				}
				if rec.Code != wantStatus || rt.createCalls != 0 {
					t.Fatalf("unexpected redisplay: status %d, calls %d", rec.Code, rt.createCalls)
				}
				assertNewTicketDraft(t, rec.Body.String(), values)
			})
		}
	}
}

func TestNewTicketRequiresLoginAndSameOrigin(t *testing.T) {
	rt := firstUseFixture()
	auth := AuthOptions{AdminToken: "first-use-test-token"}.normalized()
	handler := NewHandlerWithAuth(rt, auth)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tickets/new", nil))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatal("ticket creation page must require login")
	}
	for _, action := range []string{"select_project", "create"} {
		req := httptest.NewRequest(http.MethodPost, "/tickets/new", strings.NewReader("title=forged&action="+action))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: auth.cookieName(), Value: auth.sessionValue(auth.now().Add(auth.sessionTTL()))})
		req.Header.Set("Origin", "https://other.example")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rt.createCalls != 0 {
			t.Fatalf("cross-origin %s was accepted", action)
		}
	}
}

func TestBrowserReviewActionsUseSharedRuntime(t *testing.T) {
	for action, decision := range map[string]string{"approve": services.ReviewDecisionApprove, "request-changes": services.ReviewDecisionReject} {
		t.Run(action, func(t *testing.T) {
			rt := firstUseFixture()
			req := httptest.NewRequest(http.MethodPost, "/tickets/"+uuidString(testUUID(3))+"/"+action, strings.NewReader("reason=Evidence+reviewed"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			NewHandler(rt).ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther || rt.reviewReq.Decision != decision || rt.reviewReq.ActorType != services.ActorHuman || rt.reviewReq.Reason != "Evidence reviewed" {
				t.Fatalf("unexpected review: %d %#v", rec.Code, rt.reviewReq)
			}
		})
	}
}
