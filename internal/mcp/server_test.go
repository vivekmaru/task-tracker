package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestNewServerRegistersContractTools(t *testing.T) {
	server, err := NewServer(nil, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	tools := server.Tools()
	if len(tools) != len(contracts.AllOperations()) {
		t.Fatalf("expected one MCP tool per contract operation, got %d want %d", len(tools), len(contracts.AllOperations()))
	}

	tool, ok := server.Tool(contracts.OperationClaimNextTicket)
	if !ok {
		t.Fatalf("expected claim-next tool to be registered")
	}
	if tool.Name != contracts.OperationClaimNextTicket {
		t.Fatalf("unexpected tool name: %#v", tool)
	}
	if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
		t.Fatalf("expected JSON schemas to be serialized")
	}
}

func TestNewServerRejectsDuplicateTools(t *testing.T) {
	operation := contracts.MustOperation(contracts.OperationClaimNextTicket)

	_, err := NewServer(nil, []contracts.Operation{operation, operation})
	if err == nil {
		t.Fatal("expected duplicate tool error")
	}
}

func TestServerCallCreateTicketDelegatesToRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	out, err := server.Call(context.Background(), contracts.OperationCreateTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"title":"Add MCP handlers",
		"description":"Wire MCP tool calls to runtime services.",
		"type":"feature",
		"acceptance_criteria":["MCP create calls runtime"],
		"verification_commands":["go test ./internal/mcp"],
		"created_by":"agent",
		"created_by_id":"codex",
		"creation_reason":"Phase 2 MCP integration"
	}`))
	if err != nil {
		t.Fatalf("call create_ticket: %v", err)
	}

	if rt.createReq.Title != "Add MCP handlers" || rt.createReq.CreatedBy != services.ActorAgent {
		t.Fatalf("unexpected create request: %#v", rt.createReq)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	ticket := body["ticket"].(map[string]any)
	if ticket["id"] == "" || ticket["title"] != "Add MCP handlers" {
		t.Fatalf("unexpected output: %#v", body)
	}
}

func TestServerCallRejectsOperationsOutsideConfiguredAllowlist(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, []contracts.Operation{
		contracts.MustOperation(contracts.OperationCreateTicket),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	if server.CanCall(contracts.OperationClaimNextTicket) {
		t.Fatal("expected unregistered operation to be unavailable")
	}
	_, err = server.Call(context.Background(), contracts.OperationClaimNextTicket, nil)
	if err == nil {
		t.Fatal("expected unregistered operation call to fail")
	}
}

func TestServerCallClaimNextConvertsLeaseSeconds(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	out, err := server.Call(context.Background(), contracts.OperationClaimNextTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"agent_id":"codex",
		"harness":"codex",
		"capabilities":["codegen","tests"],
		"lease_seconds":900,
		"idempotency_key":"stable"
	}`))
	if err != nil {
		t.Fatalf("call claim_next_ticket: %v", err)
	}

	if rt.claimReq.Lease != 15*time.Minute {
		t.Fatalf("expected 15m lease, got %s", rt.claimReq.Lease)
	}
	if rt.claimReq.IdempotencyKey != "stable" {
		t.Fatalf("expected idempotency key, got %#v", rt.claimReq)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	contextBundle := body["context"].(map[string]any)
	if _, ok := contextBundle["AcceptanceCriteria"]; ok {
		t.Fatalf("context should not expose Go field names: %#v", contextBundle)
	}
	if _, ok := contextBundle["acceptance_criteria"]; !ok {
		t.Fatalf("context should expose schema-compatible field names: %#v", contextBundle)
	}
}

func TestServerCallRegisterCapabilitiesDelegatesToRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationRegisterAgentCapabilities, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"agent_id":"codex-worker",
		"harness":"codex",
		"model":"gpt-5",
		"transports":["cli","mcp"],
		"capabilities":["codegen","tests"],
		"artifact_roles":["evidence","patch"],
		"preferred_claim":{"lease_seconds":1800}
	}`))
	if err != nil {
		t.Fatalf("call register capabilities: %v", err)
	}

	if rt.registerCapabilitiesReq.AgentID != "codex-worker" || rt.registerCapabilitiesReq.Harness != "codex" {
		t.Fatalf("unexpected capability registration: %#v", rt.registerCapabilitiesReq)
	}
	if got := rt.registerCapabilitiesReq.PreferredClaim["lease_seconds"]; got != float64(1800) {
		t.Fatalf("expected preferred claim metadata, got %#v", rt.registerCapabilitiesReq.PreferredClaim)
	}
}

func TestServerCallUpdateTicketDelegatesToRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationUpdateTicket, json.RawMessage(`{
		"ticket_id":"00000000-0000-0000-0000-000000000003",
		"patch":{
			"title":"Tighten MCP lifecycle docs",
			"tags":["mcp","docs"]
		},
		"actor_id":"codex"
	}`))
	if err != nil {
		t.Fatalf("call update ticket: %v", err)
	}

	if rt.updateReq.Title == nil || *rt.updateReq.Title != "Tighten MCP lifecycle docs" {
		t.Fatalf("expected title patch, got %#v", rt.updateReq)
	}
	if rt.updateReq.Tags == nil || len(*rt.updateReq.Tags) != 2 {
		t.Fatalf("expected tags patch, got %#v", rt.updateReq)
	}
	if rt.updateReq.ActorType != services.ActorAgent || rt.updateReq.ActorID != "codex" {
		t.Fatalf("expected agent attribution, got %#v", rt.updateReq)
	}
}

func TestServerCallRejectsInvalidPayload(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationHeartbeatAttempt, json.RawMessage(`{"lease_seconds":60}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestServerCallRejectsMalformedOptionalUUID(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicketFromAttempt, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"attempt_id":"00000000-0000-0000-0000-000000000003",
		"source_artifact_id":"not-a-uuid",
		"title":"Follow up",
		"type":"bug",
		"acceptance_criteria":["Malformed UUIDs are rejected"],
		"creation_reason":"test"
	}`))
	if err == nil {
		t.Fatal("expected malformed UUID validation error")
	}
}

func TestEveryContractOperationHasMCPHandler(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	for _, operation := range contracts.AllOperations() {
		if !server.CanCall(operation.Name) {
			t.Fatalf("operation %q has no MCP handler", operation.Name)
		}
	}
}

type fakeRuntime struct {
	createReq               services.CreateTicketRequest
	proposeReq              services.CreateTicketRequest
	updateReq               services.UpdateTicketRequest
	claimReq                services.ClaimNextRequest
	heartbeatReq            services.HeartbeatRequest
	checkpointReq           services.CheckpointRequest
	completeReq             services.CompleteAttemptRequest
	failReq                 services.FailAttemptRequest
	blockReq                services.BlockAttemptRequest
	listReq                 services.ListTicketsRequest
	artifactReq             services.RegisterArtifactRequest
	decomposeReq            services.DecomposeTicketRequest
	createFromAttemptReq    services.CreateTicketFromAttemptRequest
	registerCapabilitiesReq services.RegisterCapabilitiesRequest
}

func (f *fakeRuntime) CreateTicket(_ context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	f.createReq = req
	return ticketFor(req), nil
}

func (f *fakeRuntime) ProposeTicket(_ context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	f.proposeReq = req
	return ticketFor(req), nil
}

func (f *fakeRuntime) CreateTicketFromAttempt(_ context.Context, req services.CreateTicketFromAttemptRequest) (db.Ticket, error) {
	f.createFromAttemptReq = req
	return db.Ticket{ID: testUUID(10), Title: req.Title, Type: services.TicketTypeBug, Status: services.TicketStatusBacklog}, nil
}

func (f *fakeRuntime) UpdateTicket(_ context.Context, req services.UpdateTicketRequest) (db.Ticket, error) {
	f.updateReq = req
	title := "Updated"
	if req.Title != nil {
		title = *req.Title
	}
	return db.Ticket{ID: req.TicketID, Title: title, Type: services.TicketTypeDocumentation, Status: services.TicketStatusTodo}, nil
}

func (f *fakeRuntime) ClaimNext(_ context.Context, req services.ClaimNextRequest) (services.ClaimNextResult, error) {
	f.claimReq = req
	ticket := db.Ticket{
		ID:                 testUUID(3),
		Title:              "Claimed",
		Type:               services.TicketTypeFeature,
		Status:             services.TicketStatusInProgress,
		AcceptanceCriteria: []string{"MCP claim returns schema context"},
		RelevantPaths:      []string{"internal/mcp/server.go"},
	}
	attempt := db.Attempt{ID: testUUID(4), TicketID: testUUID(3), AgentID: req.AgentID, Harness: req.Harness, Status: services.AttemptStatusRunning}
	return services.ClaimNextResult{
		Ticket:  ticket,
		Attempt: attempt,
		Context: services.ClaimContextBundle{
			Ticket:               ticket,
			Attempt:              attempt,
			AcceptanceCriteria:   ticket.AcceptanceCriteria,
			VerificationCommands: []string{"go test ./internal/mcp"},
			RelevantPaths:        ticket.RelevantPaths,
			Checkpoints: []db.AttemptCheckpoint{{
				ID:        testUUID(11),
				AttemptID: attempt.ID,
				Summary:   "Halfway",
				NextStep:  pgtype.Text{String: "Finish tests", Valid: true},
			}},
		},
	}, nil
}

func (f *fakeRuntime) Heartbeat(_ context.Context, req services.HeartbeatRequest) (db.Attempt, error) {
	f.heartbeatReq = req
	return db.Attempt{ID: req.AttemptID, Status: services.AttemptStatusRunning}, nil
}

func (f *fakeRuntime) Checkpoint(_ context.Context, req services.CheckpointRequest) (services.CheckpointResult, error) {
	f.checkpointReq = req
	return services.CheckpointResult{Checkpoint: db.AttemptCheckpoint{ID: testUUID(5), AttemptID: req.AttemptID, Summary: req.Summary}, ProgressPercent: req.ProgressPercent}, nil
}

func (f *fakeRuntime) Complete(_ context.Context, req services.CompleteAttemptRequest) (services.AttemptTransitionResult, error) {
	f.completeReq = req
	return transitionFor(req.AttemptID, services.AttemptStatusSucceeded, services.TicketStatusDone), nil
}

func (f *fakeRuntime) Fail(_ context.Context, req services.FailAttemptRequest) (services.AttemptTransitionResult, error) {
	f.failReq = req
	return transitionFor(req.AttemptID, services.AttemptStatusFailed, services.TicketStatusFailed), nil
}

func (f *fakeRuntime) Block(_ context.Context, req services.BlockAttemptRequest) (services.AttemptTransitionResult, error) {
	f.blockReq = req
	return transitionFor(req.AttemptID, services.AttemptStatusBlocked, services.TicketStatusBlocked), nil
}

func (f *fakeRuntime) ListTickets(_ context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	f.listReq = req
	return []db.Ticket{{ID: testUUID(6), Title: "Listed", Type: services.TicketTypeFeature, Status: services.TicketStatusTodo}}, nil
}

func (f *fakeRuntime) GetTicket(_ context.Context, id pgtype.UUID) (db.Ticket, error) {
	return db.Ticket{ID: id, Title: "Fetched", Type: services.TicketTypeBug, Status: services.TicketStatusTodo}, nil
}

func (f *fakeRuntime) RegisterArtifact(_ context.Context, req services.RegisterArtifactRequest) (db.Artifact, error) {
	f.artifactReq = req
	return db.Artifact{ID: testUUID(7), TicketID: req.TicketID, AttemptID: req.AttemptID, Type: req.Type, Role: req.Role, Name: req.Name, Url: req.URL}, nil
}

func (f *fakeRuntime) DecomposeTicket(_ context.Context, req services.DecomposeTicketRequest) (services.DecomposeTicketResult, error) {
	f.decomposeReq = req
	return services.DecomposeTicketResult{Children: []db.Ticket{{ID: testUUID(8), Title: req.Children[0].Title, Type: req.Children[0].Type, Status: services.TicketStatusBacklog}}}, nil
}

func (f *fakeRuntime) RegisterCapabilities(_ context.Context, req services.RegisterCapabilitiesRequest) (db.AgentCapability, error) {
	f.registerCapabilitiesReq = req
	return db.AgentCapability{ID: testUUID(9), AgentID: req.AgentID, Harness: req.Harness, Capabilities: req.Capabilities}, nil
}

func ticketFor(req services.CreateTicketRequest) db.Ticket {
	return db.Ticket{ID: testUUID(1), WorkspaceID: req.WorkspaceID, ProjectID: req.ProjectID, Title: req.Title, Type: req.Type, Status: services.TicketStatusTodo, CreatedBy: req.CreatedBy}
}

func transitionFor(attemptID pgtype.UUID, attemptStatus, ticketStatus string) services.AttemptTransitionResult {
	return services.AttemptTransitionResult{AttemptID: attemptID, TicketID: testUUID(2), AttemptStatus: attemptStatus, TicketStatus: ticketStatus}
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
