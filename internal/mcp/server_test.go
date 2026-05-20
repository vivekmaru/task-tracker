package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
		"retry_policy":{"max_attempts":1},
		"dependencies":["00000000-0000-0000-0000-00000000000b"],
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
	if len(rt.createReq.RetryPolicy) == 0 || len(rt.createReq.Dependencies) != 1 {
		t.Fatalf("expected retry policy and dependencies to be forwarded: %#v", rt.createReq)
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

func TestServerCallCreateTicketEnforcesAgentActorAndDoesNotTrustHiddenCanEnqueue(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"title":"Hidden authority",
		"description":"Hidden can_enqueue should not be trusted.",
		"type":"feature",
		"acceptance_criteria":["Hidden can_enqueue is ignored"],
		"created_by":"human",
		"created_by_id":"vivek",
		"creation_reason":"permission boundary regression test",
		"enqueue":true,
		"can_enqueue":true
	}`))
	if err != nil {
		t.Fatalf("call create_ticket: %v", err)
	}

	if !rt.createReq.Enqueue {
		t.Fatalf("expected enqueue intent to be forwarded, got %#v", rt.createReq)
	}
	if rt.createReq.CreatedBy != services.ActorAgent {
		t.Fatalf("MCP create_ticket should force agent actor, got %#v", rt.createReq)
	}
	if rt.createReq.CanEnqueue {
		t.Fatalf("MCP create_ticket should not trust hidden can_enqueue, got %#v", rt.createReq)
	}
}

func TestServerCallCreateTicketDefaultsMissingCreationReason(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"title":"Create without explicit reason",
		"description":"Schema-compatible MCP create should remain executable.",
		"type":"feature",
		"acceptance_criteria":["Creation reason is defaulted"],
		"creation_reason":"   "
	}`))
	if err != nil {
		t.Fatalf("call create_ticket: %v", err)
	}

	if rt.createReq.CreatedBy != services.ActorAgent || rt.createReq.CreationReason == "" {
		t.Fatalf("expected MCP create_ticket to set agent creation reason, got %#v", rt.createReq)
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
	checkpoints := contextBundle["checkpoints"].([]any)
	checkpoint := checkpoints[0].(map[string]any)
	if _, ok := checkpoint["progress_percent"]; ok {
		t.Fatalf("checkpoint should omit progress_percent when unavailable, got %#v", checkpoint)
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

func TestServerCallTicketTransitionsDelegateToRuntime(t *testing.T) {
	tests := []struct {
		name       string
		operation  string
		wantCall   func(*fakeRuntime) services.TicketTransitionRequest
		wantStatus string
		wantReason string
	}{
		{
			name:       "ready",
			operation:  contracts.OperationMarkTicketReady,
			wantCall:   func(rt *fakeRuntime) services.TicketTransitionRequest { return rt.markReadyReq },
			wantStatus: services.TicketStatusTodo,
			wantReason: "ready for an agent",
		},
		{
			name:       "reopen",
			operation:  contracts.OperationReopenTicket,
			wantCall:   func(rt *fakeRuntime) services.TicketTransitionRequest { return rt.reopenReq },
			wantStatus: services.TicketStatusTodo,
			wantReason: "needs another attempt",
		},
		{
			name:       "unblock",
			operation:  contracts.OperationUnblockTicket,
			wantCall:   func(rt *fakeRuntime) services.TicketTransitionRequest { return rt.unblockReq },
			wantStatus: services.TicketStatusTodo,
			wantReason: "secret configured",
		},
		{
			name:       "request review",
			operation:  contracts.OperationRequestTicketReview,
			wantCall:   func(rt *fakeRuntime) services.TicketTransitionRequest { return rt.requestReviewReq },
			wantStatus: services.TicketStatusNeedsReview,
			wantReason: "human decision needed",
		},
		{
			name:       "archive",
			operation:  contracts.OperationArchiveTicket,
			wantCall:   func(rt *fakeRuntime) services.TicketTransitionRequest { return rt.archiveReq },
			wantStatus: services.TicketStatusArchived,
			wantReason: "superseded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &fakeRuntime{}
			server, err := NewServer(rt, contracts.AllOperations())
			if err != nil {
				t.Fatalf("new server: %v", err)
			}

			out, err := server.Call(context.Background(), tt.operation, json.RawMessage(fmt.Sprintf(`{
				"ticket_id":"00000000-0000-0000-0000-000000000003",
				"actor_id":"codex",
				"reason":%q
			}`, tt.wantReason)))
			if err != nil {
				t.Fatalf("call %s: %v", tt.operation, err)
			}

			req := tt.wantCall(rt)
			if req.ActorType != services.ActorAgent || req.ActorID != "codex" || req.Reason != tt.wantReason {
				t.Fatalf("unexpected transition request: %#v", req)
			}
			if !req.TicketID.Valid {
				t.Fatalf("expected ticket id in request: %#v", req)
			}
			if !strings.Contains(string(out), `"status":"`+tt.wantStatus+`"`) {
				t.Fatalf("expected status %q in output, got %s", tt.wantStatus, string(out))
			}
		})
	}
}

func TestServerCallReviewTicketDelegatesToRuntime(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationReviewTicket, json.RawMessage(`{
		"ticket_id":"00000000-0000-0000-0000-000000000003",
		"decision":"reject",
		"actor_id":"codex",
		"reason":"tests failed"
	}`))
	if err != nil {
		t.Fatalf("call review ticket: %v", err)
	}

	if rt.reviewReq.Decision != services.ReviewDecisionReject || rt.reviewReq.ActorType != services.ActorAgent || rt.reviewReq.ActorID != "codex" || rt.reviewReq.Reason != "tests failed" {
		t.Fatalf("unexpected review request: %#v", rt.reviewReq)
	}
}

func TestServerCallListTicketsDefaultsLimit(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationListTickets, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002"
	}`))
	if err != nil {
		t.Fatalf("call list_tickets: %v", err)
	}

	if rt.listReq.Limit != defaultListTicketsLimit {
		t.Fatalf("expected default list limit, got %#v", rt.listReq)
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

func TestServerCallCreateFromAttemptRejectsHiddenTemplateKind(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicketFromAttempt, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"attempt_id":"00000000-0000-0000-0000-000000000003",
		"template_kind":"cleanup",
		"type":"bug",
		"title":"Follow up",
		"acceptance_criteria":["Hidden template_kind is rejected"],
		"creation_reason":"schema consistency regression test"
	}`))
	if err == nil {
		t.Fatal("expected hidden template_kind validation error")
	}
}

func TestServerCallCreateFromAttemptUsesDeclaredType(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicketFromAttempt, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"attempt_id":"00000000-0000-0000-0000-000000000003",
		"type":"bug",
		"title":"Follow up",
		"acceptance_criteria":["Type drives template kind"],
		"creation_reason":"schema consistency regression test"
	}`))
	if err != nil {
		t.Fatalf("call create_ticket_from_attempt: %v", err)
	}
	if rt.createFromAttemptReq.TemplateKind != services.TemplateBug {
		t.Fatalf("expected declared type to drive template kind, got %#v", rt.createFromAttemptReq)
	}
}

func TestServerCallCreateFromAttemptDoesNotTrustHiddenEnqueueFlags(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationCreateTicketFromAttempt, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"attempt_id":"00000000-0000-0000-0000-000000000003",
		"title":"Follow up",
		"type":"bug",
		"acceptance_criteria":["Hidden enqueue authority is ignored"],
		"creation_reason":"permission boundary regression test",
		"enqueue":true,
		"can_enqueue":true
	}`))
	if err != nil {
		t.Fatalf("call create_ticket_from_attempt: %v", err)
	}

	if rt.createFromAttemptReq.Enqueue || rt.createFromAttemptReq.CanEnqueue {
		t.Fatalf("MCP create_ticket_from_attempt should ignore hidden enqueue flags, got %#v", rt.createFromAttemptReq)
	}
}

func TestServerCallDecomposeTicketRejectsClientCanEnqueue(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationDecomposeTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"ticket_id":"00000000-0000-0000-0000-000000000003",
		"mode":"create",
		"can_enqueue":true,
		"created_by":"human",
		"created_by_id":"vivek",
		"creation_reason":"permission boundary regression test",
		"children":[{
			"key":"impl",
			"title":"Implement child",
			"type":"feature",
			"acceptance_criteria":["Child exists"]
		}]
	}`))
	if err == nil {
		t.Fatal("expected can_enqueue validation error")
	}
}

func TestServerCallDecomposeTicketEnforcesAgentActor(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationDecomposeTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"ticket_id":"00000000-0000-0000-0000-000000000003",
		"mode":"propose",
		"can_enqueue":false,
		"created_by":"human",
		"created_by_id":"vivek",
		"creation_reason":"permission boundary regression test",
		"children":[{
			"key":"impl",
			"title":"Implement child",
			"type":"feature",
			"acceptance_criteria":["Child exists"]
		}]
	}`))
	if err != nil {
		t.Fatalf("call decompose_ticket: %v", err)
	}

	if rt.decomposeReq.CreatedBy != services.ActorAgent || rt.decomposeReq.CanEnqueue {
		t.Fatalf("MCP decompose_ticket should force agent actor without enqueue authority, got %#v", rt.decomposeReq)
	}
}

func TestServerCallDecomposeTicketCreateModeSetsInternalEnqueueAuthority(t *testing.T) {
	rt := &fakeRuntime{}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	_, err = server.Call(context.Background(), contracts.OperationDecomposeTicket, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"project_id":"00000000-0000-0000-0000-000000000002",
		"ticket_id":"00000000-0000-0000-0000-000000000003",
		"mode":"create",
		"can_enqueue":false,
		"created_by":"human",
		"created_by_id":"vivek",
		"creation_reason":"permission boundary regression test",
		"children":[{
			"key":"impl",
			"title":"Implement child",
			"type":"feature",
			"acceptance_criteria":["Child exists"]
		}]
	}`))
	if err != nil {
		t.Fatalf("call decompose_ticket: %v", err)
	}

	if rt.decomposeReq.CreatedBy != services.ActorAgent || !rt.decomposeReq.CanEnqueue {
		t.Fatalf("MCP decompose_ticket create mode should authorize enqueue internally, got %#v", rt.decomposeReq)
	}
}

func TestArtifactPayloadOmitsTicketScopedAttemptID(t *testing.T) {
	payload := artifactPayload(db.Artifact{
		ID:       testUUID(1),
		TicketID: testUUID(2),
		Type:     services.ArtifactTypeDocument,
		Role:     services.ArtifactRoleEvidence,
		Name:     "notes.md",
		Url:      "local://notes.md",
	})

	if _, ok := payload["attempt_id"]; ok {
		t.Fatalf("ticket-scoped artifact should omit attempt_id, got %#v", payload)
	}
}

func TestCallAnalyticsByModelUsesScopedFilter(t *testing.T) {
	rt := &fakeRuntime{
		analyticsGroups: []services.AnalyticsGroup{{Group: "gpt-5.4", AttemptCount: 2, TotalCostUSD: 0.12}},
	}
	server, err := NewServer(rt, contracts.AllOperations())
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	out, err := server.Call(context.Background(), contracts.OperationAnalyticsByModel, json.RawMessage(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001"
	}`))
	if err != nil {
		t.Fatalf("call analytics_by_model: %v", err)
	}

	if rt.analyticsFilter.WorkspaceID != testUUID(1) {
		t.Fatalf("expected workspace filter, got %#v", rt.analyticsFilter)
	}
	if !strings.Contains(string(out), `"group":"gpt-5.4"`) {
		t.Fatalf("expected analytics group output, got %s", string(out))
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
	markReadyReq            services.TicketTransitionRequest
	reopenReq               services.TicketTransitionRequest
	unblockReq              services.TicketTransitionRequest
	requestReviewReq        services.TicketTransitionRequest
	reviewReq               services.ReviewTicketRequest
	archiveReq              services.TicketTransitionRequest
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
	analyticsFilter         services.AnalyticsFilter
	analyticsSummary        services.AnalyticsSummary
	analyticsGroups         []services.AnalyticsGroup
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

func (f *fakeRuntime) MarkReady(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.markReadyReq = req
	return transitionedTicket(req.TicketID, services.TicketStatusTodo), nil
}

func (f *fakeRuntime) Reopen(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.reopenReq = req
	return transitionedTicket(req.TicketID, services.TicketStatusTodo), nil
}

func (f *fakeRuntime) Unblock(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.unblockReq = req
	return transitionedTicket(req.TicketID, services.TicketStatusTodo), nil
}

func (f *fakeRuntime) RequestReview(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.requestReviewReq = req
	return transitionedTicket(req.TicketID, services.TicketStatusNeedsReview), nil
}

func (f *fakeRuntime) Review(_ context.Context, req services.ReviewTicketRequest) (db.Ticket, error) {
	f.reviewReq = req
	status := services.TicketStatusDone
	if req.Decision == services.ReviewDecisionReject {
		status = services.TicketStatusTodo
	}
	return transitionedTicket(req.TicketID, status), nil
}

func (f *fakeRuntime) Archive(_ context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	f.archiveReq = req
	return transitionedTicket(req.TicketID, services.TicketStatusArchived), nil
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

func (f *fakeRuntime) AnalyticsSummary(_ context.Context, filter services.AnalyticsFilter) (services.AnalyticsSummary, error) {
	f.analyticsFilter = filter
	return f.analyticsSummary, nil
}

func (f *fakeRuntime) AnalyticsByModel(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	return f.analyticsGroups, nil
}

func (f *fakeRuntime) AnalyticsByHarness(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	return f.analyticsGroups, nil
}

func ticketFor(req services.CreateTicketRequest) db.Ticket {
	return db.Ticket{ID: testUUID(1), WorkspaceID: req.WorkspaceID, ProjectID: req.ProjectID, Title: req.Title, Type: req.Type, Status: services.TicketStatusTodo, CreatedBy: req.CreatedBy}
}

func transitionedTicket(id pgtype.UUID, status string) db.Ticket {
	return db.Ticket{ID: id, Title: "Transitioned", Type: services.TicketTypeFeature, Status: status}
}

func transitionFor(attemptID pgtype.UUID, attemptStatus, ticketStatus string) services.AttemptTransitionResult {
	return services.AttemptTransitionResult{AttemptID: attemptID, TicketID: testUUID(2), AttemptStatus: attemptStatus, TicketStatus: ticketStatus}
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
