package services

import (
	"context"
	"testing"
	"time"

	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestCapabilityServiceRegistersCodexCapabilities(t *testing.T) {
	store := &fakeCapabilityStore{}
	service := NewCapabilityService(store, WithCapabilityClock(func() time.Time {
		return time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)
	}))

	record, err := service.Register(context.Background(), RegisterCapabilitiesRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		AgentID:        "codex-worker-1",
		Harness:        "codex",
		Model:          "gpt-5",
		Transports:     []string{"cli", "mcp"},
		Capabilities:   []string{"codegen", "tests", "postgres"},
		ToolNames:      []string{"shell", "apply_patch"},
		ArtifactRoles:  []string{ArtifactRoleEvidence, ArtifactRolePatch},
		PreferredClaim: map[string]any{"lease_seconds": float64(1800)},
		Metadata:       map[string]any{"cwd": "/repo"},
	})
	if err != nil {
		t.Fatalf("register capabilities: %v", err)
	}

	if record.AgentID != "codex-worker-1" || record.Harness != "codex" {
		t.Fatalf("unexpected record: %#v", record)
	}
	if got := store.upsert.Model; got != "gpt-5" {
		t.Fatalf("expected model gpt-5, got %q", got)
	}
	if got := store.upsert.Transports; len(got) != 2 || got[0] != "cli" || got[1] != "mcp" {
		t.Fatalf("expected compact transports, got %#v", got)
	}
	if string(store.upsert.PreferredClaim) != `{"lease_seconds":1800}` {
		t.Fatalf("unexpected preferred claim JSON: %s", string(store.upsert.PreferredClaim))
	}
	if !store.upsert.LastSeenAt.Valid || !store.upsert.LastSeenAt.Time.Equal(time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected deterministic last_seen_at, got %#v", store.upsert.LastSeenAt)
	}
}

func TestCapabilityServiceRejectsMalformedDeclarations(t *testing.T) {
	service := NewCapabilityService(&fakeCapabilityStore{})

	_, err := service.Register(context.Background(), RegisterCapabilitiesRequest{
		WorkspaceID:   testUUID(1),
		ProjectID:     testUUID(2),
		AgentID:       "codex",
		Harness:       "codex",
		Transports:    []string{"cli"},
		Capabilities:  nil,
		ArtifactRoles: []string{"not-a-role"},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if !containsProblem(validation.Problems, "capabilities is required") {
		t.Fatalf("expected capability validation problem, got %#v", validation.Problems)
	}
	if !containsProblem(validation.Problems, "artifact_roles contains an invalid role") {
		t.Fatalf("expected artifact role validation problem, got %#v", validation.Problems)
	}
}

func TestCapabilityServiceListsAndLooksUpCapabilities(t *testing.T) {
	store := &fakeCapabilityStore{
		get: db.AgentCapability{AgentID: "codex", Harness: "codex"},
		list: []db.AgentCapability{
			{AgentID: "codex", Harness: "codex"},
			{AgentID: "doc-agent", Harness: "opencode"},
		},
	}
	service := NewCapabilityService(store)

	_, err := service.Get(context.Background(), GetCapabilitiesRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		AgentID:     "codex",
		Harness:     "codex",
	})
	if err != nil {
		t.Fatalf("get capabilities: %v", err)
	}
	if store.getReq.AgentID != "codex" {
		t.Fatalf("unexpected get request: %#v", store.getReq)
	}

	records, err := service.List(context.Background(), ListCapabilitiesRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Harness:     "codex",
		Capability:  "tests",
	})
	if err != nil {
		t.Fatalf("list capabilities: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected list records, got %#v", records)
	}
	if store.listReq.Harness.String != "codex" || store.listReq.Capability.String != "tests" {
		t.Fatalf("unexpected list request: %#v", store.listReq)
	}
}

type fakeCapabilityStore struct {
	upsert  db.UpsertAgentCapabilityParams
	getReq  db.GetAgentCapabilityParams
	listReq db.ListAgentCapabilitiesParams
	get     db.AgentCapability
	list    []db.AgentCapability
}

func (f *fakeCapabilityStore) UpsertAgentCapability(_ context.Context, params db.UpsertAgentCapabilityParams) (db.AgentCapability, error) {
	f.upsert = params
	return db.AgentCapability{
		WorkspaceID:    params.WorkspaceID,
		ProjectID:      params.ProjectID,
		AgentID:        params.AgentID,
		Harness:        params.Harness,
		Model:          params.Model,
		Transports:     params.Transports,
		Capabilities:   params.Capabilities,
		ToolNames:      params.ToolNames,
		ArtifactRoles:  params.ArtifactRoles,
		PreferredClaim: params.PreferredClaim,
		Metadata:       params.Metadata,
		LastSeenAt:     params.LastSeenAt,
	}, nil
}

func (f *fakeCapabilityStore) GetAgentCapability(_ context.Context, params db.GetAgentCapabilityParams) (db.AgentCapability, error) {
	f.getReq = params
	return f.get, nil
}

func (f *fakeCapabilityStore) ListAgentCapabilities(_ context.Context, params db.ListAgentCapabilitiesParams) ([]db.AgentCapability, error) {
	f.listReq = params
	return f.list, nil
}

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if problem == want {
			return true
		}
	}
	return false
}
