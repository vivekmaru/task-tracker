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
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
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
