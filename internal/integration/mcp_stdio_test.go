//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	modelcontext "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPStdio(t *testing.T) {
	fixture := newFixture(t)
	workspace, project := createScope(t, fixture.runtime, fixture.context)
	createClaimableTicket(t, fixture.runtime, fixture.context, workspace.ID, project.ID)
	binary := filepath.Join(t.TempDir(), "forge")
	build := exec.Command("go", "build", "-o", binary, "./cmd/forge")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build forge: %v: %s", err, output)
	}
	configPath := filepath.Join(t.TempDir(), "forge.json")
	if err := os.WriteFile(configPath, []byte(`{"database_url":`+mustJSON(t, fixture.database.URL)+`,"artifact_root":`+mustJSON(t, t.TempDir())+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := modelcontext.NewClient(&modelcontext.Implementation{Name: "forge-integration", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &modelcontext.CommandTransport{Command: exec.Command(binary, "mcp", "--config", configPath)}, nil)
	if err != nil {
		t.Fatalf("connect MCP stdio: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, &modelcontext.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("expected MCP tools")
	}
	create, err := session.CallTool(ctx, &modelcontext.CallToolParams{Name: "create_ticket", Arguments: map[string]any{"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "title": "MCP stdio ticket", "description": "Created by the MCP subprocess.", "type": "task", "acceptance_criteria": []string{"Claim over MCP"}}})
	if err != nil || create.IsError {
		t.Fatalf("create tool: result=%#v err=%v", create, err)
	}
	claim, err := session.CallTool(ctx, &modelcontext.CallToolParams{Name: "claim_next_ticket", Arguments: map[string]any{"workspace_id": uuidText(t, workspace.ID), "project_id": uuidText(t, project.ID), "agent_id": "mcp-agent", "harness": "integration", "lease_seconds": 120}})
	if err != nil || claim.IsError {
		t.Fatalf("claim tool: result=%#v err=%v", claim, err)
	}
	var claimed struct {
		Attempt struct {
			ID string `json:"id"`
		} `json:"attempt"`
	}
	decodeStructured(t, claim.StructuredContent, &claimed)
	if claimed.Attempt.ID == "" {
		t.Fatalf("claim output lacks attempt id: %#v", claim.StructuredContent)
	}
	checkpoint, err := session.CallTool(ctx, &modelcontext.CallToolParams{Name: "checkpoint_attempt", Arguments: map[string]any{"attempt_id": claimed.Attempt.ID, "summary": "mcp checkpoint", "progress_percent": 50}})
	if err != nil || checkpoint.IsError {
		t.Fatalf("checkpoint tool: result=%#v err=%v", checkpoint, err)
	}
	complete, err := session.CallTool(ctx, &modelcontext.CallToolParams{Name: "complete_attempt", Arguments: map[string]any{"attempt_id": claimed.Attempt.ID, "output": map[string]any{"summary": "mcp complete"}, "output_schema": "summary.v1"}})
	if err != nil || complete.IsError {
		t.Fatalf("complete tool: result=%#v err=%v", complete, err)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodeStructured(t *testing.T, value any, out any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}
