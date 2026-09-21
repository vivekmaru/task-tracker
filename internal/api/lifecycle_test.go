package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	forgeruntime "github.com/vivek/agent-task-tracker/internal/runtime"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/web"
)

func TestTicketTransitionEndpoints(t *testing.T) {
	for _, action := range []string{"ready", "reopen", "unblock", "request-review", "review", "archive"} {
		t.Run(action, func(t *testing.T) {
			rt := &fakeTicketTransitionRuntime{}
			router := NewRouterWithRuntimeAndAuth(rt, web.AuthOptions{AdminToken: "operator-token"})
			body := `{"actor_type":"human","actor_id":"operator","reason":"verified"}`
			if action == "review" {
				body = `{"actor_type":"human","actor_id":"operator","reason":"verified","decision":"approve"}`
			}
			for _, tc := range []struct {
				name       string
				id         string
				body       string
				token      string
				err        error
				status     int
				wantCalled bool
			}{
				{name: "success", id: uuidText(testUUID(1)), body: body, token: "operator-token", status: http.StatusOK, wantCalled: true},
				{name: "unauthenticated", id: uuidText(testUUID(1)), body: body, status: http.StatusUnauthorized},
				{name: "wrong token", id: uuidText(testUUID(1)), body: body, token: "wrong", status: http.StatusUnauthorized},
				{name: "invalid id", id: "not-a-uuid", body: body, token: "operator-token", status: http.StatusBadRequest},
				{name: "not found", id: uuidText(testUUID(1)), body: body, token: "operator-token", err: fmt.Errorf("lookup: %w", services.ErrTicketNotFound), status: http.StatusNotFound, wantCalled: true},
				{name: "no rows", id: uuidText(testUUID(1)), body: body, token: "operator-token", err: pgx.ErrNoRows, status: http.StatusNotFound, wantCalled: true},
				{name: "conflict", id: uuidText(testUUID(1)), body: body, token: "operator-token", err: fmt.Errorf("transition: %w", services.ErrTicketTransitionNotAllowed), status: http.StatusConflict, wantCalled: true},
				{name: "validation", id: uuidText(testUUID(1)), body: body, token: "operator-token", err: services.ValidationError{Problems: []string{"actor_type must be human, agent, or system"}}, status: http.StatusBadRequest, wantCalled: true},
				{name: "internal", id: uuidText(testUUID(1)), body: body, token: "operator-token", err: errors.New("database unavailable"), status: http.StatusInternalServerError, wantCalled: true},
				{name: "wrong body type", id: uuidText(testUUID(1)), body: `{"actor_id":123,"decision":"approve"}`, token: "operator-token", status: http.StatusUnprocessableEntity},
			} {
				t.Run(tc.name, func(t *testing.T) {
					rt.called, rt.err = "", tc.err
					req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/"+tc.id+"/"+action, strings.NewReader(tc.body))
					req.Header.Set("Content-Type", "application/json")
					if tc.token != "" {
						req.Header.Set("Authorization", "Bearer "+tc.token)
					}
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					if rec.Code != tc.status {
						t.Fatalf("status %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
					}
					if tc.wantCalled {
						want := services.TicketTransitionRequest{TicketID: testUUID(1), ActorType: "human", ActorID: "operator", Reason: "verified"}
						if rt.called != action || rt.req != want {
							t.Fatalf("runtime call %q: %#v, want %q: %#v", rt.called, rt.req, action, want)
						}
						if action == "review" && rt.decision != "approve" {
							t.Fatalf("review decision = %q", rt.decision)
						}
					} else if rt.called != "" {
						t.Fatalf("invalid or unauthorized request called %s", rt.called)
					}
					if tc.status == http.StatusOK {
						var ticket ticketResponse
						if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
							t.Fatal(err)
						}
						if ticket.ID != tc.id || ticket.Status != "todo" {
							t.Fatalf("unexpected ticket: %#v", ticket)
						}
					}
				})
			}
		})
	}
}

func TestReviewEndpointValidatesDecision(t *testing.T) {
	rt := &fakeTicketTransitionRuntime{}
	router := NewRouterWithRuntime(rt)
	for _, tc := range []struct {
		body     string
		status   int
		decision string
	}{
		{`{"decision":"approve"}`, http.StatusOK, "approve"},
		{`{"decision":"reject"}`, http.StatusOK, "reject"},
		{`{"decision":"accept"}`, http.StatusUnprocessableEntity, ""},
		{`{"decision":""}`, http.StatusUnprocessableEntity, ""},
		{`{}`, http.StatusUnprocessableEntity, ""},
	} {
		rt.called = ""
		rec := postJSON(router, "/api/v1/tickets/"+uuidText(testUUID(1))+"/review", tc.body)
		if rec.Code != tc.status {
			t.Fatalf("%s: status %d, want %d: %s", tc.body, rec.Code, tc.status, rec.Body.String())
		}
		if (rt.called != "") != (tc.status == http.StatusOK) {
			t.Fatalf("%s: unexpected runtime call %q", tc.body, rt.called)
		}
		if tc.status == http.StatusOK && rt.decision != tc.decision {
			t.Fatalf("decision = %q, want %q", rt.decision, tc.decision)
		}
	}
}

func TestTicketTransitionsWithoutRuntime(t *testing.T) {
	router := NewRouter()
	for _, action := range []string{"ready", "reopen", "unblock", "request-review", "review", "archive"} {
		body := `{}`
		if action == "review" {
			body = `{"decision":"approve"}`
		}
		rec := postJSON(router, "/api/v1/tickets/"+uuidText(testUUID(1))+"/"+action, body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d: %s", action, rec.Code, rec.Body.String())
		}
	}
	rec := postJSON(NewRouterWithRuntime(&fakeReadyRuntime{}), "/api/v1/tickets/"+uuidText(testUUID(1))+"/review", `{"decision":"approve"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing review capability: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestClaimNextEndpointReturnsHydratedContext(t *testing.T) {
	ticket := db.Ticket{ID: testUUID(1), WorkspaceID: testUUID(2), ProjectID: testUUID(3), Title: "Hydrated ticket", Status: "in_progress"}
	attempt := db.Attempt{ID: testUUID(4), TicketID: ticket.ID, AgentID: "worker", Harness: "test", Model: "test-model", Status: "running"}
	bundle := services.ClaimContextBundle{
		Ticket: ticket, Attempt: attempt,
		AcceptanceCriteria: []string{"tests pass"}, VerificationCommands: []string{"go test ./..."},
		Environment: map[string]any{"os": "linux"}, Input: map[string]any{"task": "implement"},
		RelevantPaths: []string{"internal/api"}, RequiredTools: []string{"go"}, RequiredPermissions: []string{"repo:write"}, ExpectedArtifacts: []string{"patch"},
		PriorAttempts: []db.Attempt{{ID: testUUID(5), TicketID: ticket.ID, Status: "failed", AgentID: "previous", Harness: "test", Model: "prior-model", FailureReason: pgtype.Text{String: "Tests timed out", Valid: true}, FailureCategory: pgtype.Text{String: "environment_failed", Valid: true}, NextStep: pgtype.Text{String: "Restore the test database", Valid: true}, Output: []byte(`{"exit_code":1}`)}},
		Checkpoints:   []db.AttemptCheckpoint{{ID: testUUID(6), Summary: "halfway", NextStep: pgtype.Text{String: "run tests", Valid: true}, Risk: pgtype.Text{String: "none", Valid: true}}},
		Artifacts:     []db.Artifact{{ID: testUUID(7), TicketID: ticket.ID, AttemptID: testUUID(5), Type: "patch", Role: "output", Name: "changes.diff", Url: "https://example.test/changes.diff", StorageBackend: "external", SizeBytes: 123, MimeType: "text/plain"}},
	}
	rt := &fakeClaimRuntime{result: services.ClaimNextResult{Ticket: ticket, Attempt: attempt, Context: bundle}}
	router := NewRouterWithRuntime(rt)
	body := `{"workspace_id":"` + uuidText(testUUID(2)) + `","project_id":"` + uuidText(testUUID(3)) + `","agent_id":"worker","harness":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/claim-next", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "stable-claim")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rt.req.WorkspaceID != testUUID(2) || rt.req.ProjectID != testUUID(3) || rt.req.Lease != 5*time.Minute || rt.req.IdempotencyKey != "stable-claim" {
		t.Fatalf("unexpected claim request: %#v", rt.req)
	}
	var payload struct {
		Context map[string]json.RawMessage `json:"context"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"acceptance_criteria": bundle.AcceptanceCriteria, "verification_commands": bundle.VerificationCommands,
		"environment": bundle.Environment, "input": bundle.Input, "relevant_paths": bundle.RelevantPaths,
		"required_tools": bundle.RequiredTools, "required_permissions": bundle.RequiredPermissions, "expected_artifacts": bundle.ExpectedArtifacts,
		"ticket": makeTicketResponse(ticket), "attempt": makeAttemptResponse(attempt),
		"prior_attempts": []attemptResponse{makeAttemptResponse(bundle.PriorAttempts[0])},
		"checkpoints":    []claimCheckpointResponse{{ID: uuidText(testUUID(6)), Summary: "halfway", NextStep: "run tests", Risk: "none"}},
		"artifacts":      []artifactResponse{makeArtifactResponse(bundle.Artifacts[0])},
	}
	if len(payload.Context) != len(want) {
		t.Fatalf("unexpected context keys: %s", rec.Body.String())
	}
	for key, value := range want {
		expected, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var gotJSON, wantJSON any
		if err := json.Unmarshal(payload.Context[key], &gotJSON); err != nil {
			t.Fatalf("context.%s: %v", key, err)
		}
		if err := json.Unmarshal(expected, &wantJSON); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotJSON, wantJSON) {
			t.Errorf("context.%s = %s, want %s", key, payload.Context[key], expected)
		}
	}
	for _, unexpected := range []string{`"AcceptanceCriteria"`, `"PriorAttempts"`, `"NextStep"`, `"StorageBackend"`} {
		if strings.Contains(rec.Body.String(), unexpected) {
			t.Errorf("Go field leaked: %s", unexpected)
		}
	}

	for _, want := range []string{`"failure_reason":"Tests timed out"`, `"failure_category":"environment_failed"`, `"next_step":"Restore the test database"`, `"output":{"exit_code":1}`} {
		if !strings.Contains(string(payload.Context["prior_attempts"]), want) {
			t.Errorf("claim history missing recovery detail %s", want)
		}
	}

	rt.result.Context = services.ClaimContextBundle{}
	rec = postJSON(router, "/api/v1/tickets/claim-next", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty context: status %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"acceptance_criteria", "verification_commands", "relevant_paths", "required_tools", "required_permissions", "expected_artifacts", "prior_attempts", "checkpoints", "artifacts"} {
		if string(payload.Context[key]) != "[]" {
			t.Errorf("empty context.%s = %s, want []", key, payload.Context[key])
		}
	}
	for _, key := range []string{"environment", "input"} {
		if string(payload.Context[key]) != "{}" {
			t.Errorf("empty context.%s = %s, want {}", key, payload.Context[key])
		}
	}
}

func postJSON(router http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type fakeTicketTransitionRuntime struct {
	web.Runtime
	called   string
	req      services.TicketTransitionRequest
	decision string
	err      error
}

func (f *fakeTicketTransitionRuntime) transition(action string, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.called, f.req = action, req
	return db.Ticket{ID: req.TicketID, Status: "todo"}, f.err
}
func (f *fakeTicketTransitionRuntime) MarkReady(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return f.transition("ready", req)
}
func (f *fakeTicketTransitionRuntime) Reopen(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return f.transition("reopen", req)
}
func (f *fakeTicketTransitionRuntime) Unblock(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return f.transition("unblock", req)
}
func (f *fakeTicketTransitionRuntime) RequestReview(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return f.transition("request-review", req)
}
func (f *fakeTicketTransitionRuntime) Archive(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return f.transition("archive", req)
}
func (f *fakeTicketTransitionRuntime) Review(_ context.Context, req services.ReviewTicketRequest) (db.Ticket, error) {
	f.decision = req.Decision
	return f.transition("review", services.TicketTransitionRequest{TicketID: req.TicketID, ActorType: req.ActorType, ActorID: req.ActorID, Reason: req.Reason})
}

type fakeClaimRuntime struct {
	*forgeruntime.Runtime
	req    services.ClaimNextRequest
	result services.ClaimNextResult
}

func (f *fakeClaimRuntime) ClaimNext(_ context.Context, req services.ClaimNextRequest) (services.ClaimNextResult, error) {
	f.req = req
	return f.result, nil
}
