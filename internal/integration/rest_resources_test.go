//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/api"
	"github.com/vivek/agent-task-tracker/internal/web"
)

func TestRESTResourceWorkflow(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	server := httptest.NewServer(api.NewRouterWithRuntimeAndAuth(fixture.runtime, web.AuthOptions{AdminToken: "integration-token"}))
	t.Cleanup(server.Close)

	create := map[string]any{
		"workspace_id":        uuidText(t, workspace.ID),
		"project_id":          uuidText(t, project.ID),
		"title":               "REST resource ticket",
		"description":         "A real HTTP resource workflow.",
		"type":                "task",
		"acceptance_criteria": []string{"Create through REST"},
	}
	var created struct {
		ID string `json:"id"`
	}
	postJSON(t, server.URL+"/api/v1/tickets", create, &created)
	if created.ID == "" {
		t.Fatalf("expected ticket id in create response: %#v", created)
	}

	var listed struct {
		Tickets []struct {
			ID string `json:"id"`
		} `json:"tickets"`
	}
	getJSON(t, server.URL+"/api/v1/tickets?workspace_id="+uuidText(t, workspace.ID)+"&project_id="+uuidText(t, project.ID), &listed)
	if len(listed.Tickets) != 1 || listed.Tickets[0].ID != created.ID {
		t.Fatalf("unexpected list payload: %#v", listed)
	}

	update := map[string]any{"title": "REST resource ticket updated", "actor_type": "human"}
	var updated struct {
		Title string `json:"title"`
	}
	patchJSON(t, server.URL+"/api/v1/tickets/"+created.ID, update, &updated)
	if updated.Title != "REST resource ticket updated" {
		t.Fatalf("unexpected update payload: %#v", updated)
	}

	artifact := map[string]any{
		"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "ticket_id": created.ID,
		"type": "log", "role": "evidence", "name": "proof.log", "url": "file:///tmp/proof.log", "storage_backend": "local",
	}
	var registered struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	postJSON(t, server.URL+"/api/v1/artifacts", artifact, &registered)
	if registered.ID == "" || registered.Name != "proof.log" {
		t.Fatalf("unexpected artifact response: %#v", registered)
	}
	var fetched struct {
		ID       string `json:"id"`
		TicketID string `json:"ticket_id"`
	}
	getJSON(t, server.URL+"/api/v1/artifacts/"+registered.ID, &fetched)
	if fetched.ID != registered.ID || fetched.TicketID != created.ID {
		t.Fatalf("unexpected get artifact response: %#v", fetched)
	}
}

func TestRESTExecutionAndIdempotency(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	server := httptest.NewServer(api.NewRouterWithRuntimeAndAuth(fixture.runtime, web.AuthOptions{AdminToken: "integration-token"}))
	t.Cleanup(server.Close)
	create := map[string]any{"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "title": "REST execution ticket", "description": "HTTP lifecycle coverage.", "type": "task", "acceptance_criteria": []string{"Complete via REST"}}
	var ticket struct {
		ID string `json:"id"`
	}
	postJSON(t, server.URL+"/api/v1/tickets", create, &ticket)
	claim := map[string]any{"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "agent_id": "rest-agent", "harness": "integration", "lease_seconds": 120}
	var first struct {
		Ticket struct {
			ID string `json:"id"`
		} `json:"ticket"`
		Attempt struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"attempt"`
	}
	requestJSONWithHeader(t, http.MethodPost, server.URL+"/api/v1/tickets/claim-next", claim, "claim-replay", http.StatusOK, &first)
	if first.Ticket.ID != ticket.ID || first.Attempt.ID == "" || first.Attempt.Status != "running" {
		t.Fatalf("unexpected claim result: %#v", first)
	}
	var replay = first
	requestJSONWithHeader(t, http.MethodPost, server.URL+"/api/v1/tickets/claim-next", claim, "claim-replay", http.StatusOK, &replay)
	if replay.Attempt.ID != first.Attempt.ID {
		t.Fatalf("expected idempotent replay to return attempt %s, got %#v", first.Attempt.ID, replay)
	}
	checkpoint := map[string]any{"summary": "halfway", "progress_percent": 50, "commands_run": []string{"go test ./..."}}
	var checkpointResult struct {
		CheckpointID    string `json:"checkpoint_id"`
		ProgressPercent int32  `json:"progress_percent"`
	}
	postJSON(t, server.URL+"/api/v1/attempts/"+first.Attempt.ID+"/checkpoint", checkpoint, &checkpointResult)
	if checkpointResult.CheckpointID == "" || checkpointResult.ProgressPercent != 50 {
		t.Fatalf("unexpected checkpoint result: %#v", checkpointResult)
	}
	complete := map[string]any{"output": map[string]any{"summary": "done"}, "output_schema": "summary.v1", "metrics": map[string]any{"tokens_in": 10, "tokens_out": 20, "duration_seconds": 1.5}}
	var completed struct {
		AttemptStatus string `json:"attempt_status"`
		TicketStatus  string `json:"ticket_status"`
	}
	postJSON(t, server.URL+"/api/v1/attempts/"+first.Attempt.ID+"/complete", complete, &completed)
	if completed.AttemptStatus != "succeeded" || completed.TicketStatus != "done" {
		t.Fatalf("unexpected terminal result: %#v", completed)
	}
	requestJSONWithHeader(t, http.MethodPost, server.URL+"/api/v1/attempts/"+first.Attempt.ID+"/complete", complete, "", http.StatusConflict, &map[string]any{})
	requestJSONWithHeader(t, http.MethodPost, server.URL+"/api/v1/tickets/claim-next", map[string]any{"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "agent_id": "different", "harness": "integration"}, "claim-replay", http.StatusConflict, &map[string]any{})
}

func postJSON(t *testing.T, rawURL string, value any, out any) {
	t.Helper()
	requestJSON(t, http.MethodPost, rawURL, value, out)
}
func patchJSON(t *testing.T, rawURL string, value any, out any) {
	t.Helper()
	requestJSON(t, http.MethodPatch, rawURL, value, out)
}
func getJSON(t *testing.T, rawURL string, out any) {
	t.Helper()
	requestJSON(t, http.MethodGet, rawURL, nil, out)
}
func requestJSON(t *testing.T, method, rawURL string, value any, out any) {
	requestJSONWithHeader(t, method, rawURL, value, "", 0, out)
}
func requestJSONWithHeader(t *testing.T, method, rawURL string, value any, idempotencyKey string, expectedStatus int, out any) {
	t.Helper()
	var body *bytes.Reader
	if value == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer integration-token")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if expectedStatus != 0 && response.StatusCode != expectedStatus {
		t.Fatalf("%s %s: status %d, want %d", method, rawURL, response.StatusCode, expectedStatus)
	}
	if expectedStatus == 0 && (response.StatusCode < 200 || response.StatusCode >= 300) {
		t.Fatalf("%s %s: status %d", method, rawURL, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		t.Fatalf("decode %s %s: %v", method, rawURL, err)
	}
}

func uuidText(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	value, err := id.Value()
	if err != nil {
		t.Fatal(err)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("uuid value has type %T", value)
	}
	return text
}
