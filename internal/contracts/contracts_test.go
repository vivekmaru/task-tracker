package contracts

import (
	"encoding/json"
	"testing"
)

func TestAllOperationsContainsPhaseTwoSurface(t *testing.T) {
	required := []string{
		OperationCreateTicket,
		OperationProposeTicket,
		OperationCreateTicketFromAttempt,
		OperationClaimNextTicket,
		OperationHeartbeatAttempt,
		OperationCheckpointAttempt,
		OperationUpdateTicket,
		OperationCompleteAttempt,
		OperationFailAttempt,
		OperationBlockAttempt,
		OperationListTickets,
		OperationGetTicket,
		OperationAttachArtifact,
		OperationDecomposeTicket,
		OperationRegisterAgentCapabilities,
	}

	seen := map[string]bool{}
	for _, operation := range AllOperations() {
		if seen[operation.Name] {
			t.Fatalf("duplicate operation %q", operation.Name)
		}
		seen[operation.Name] = true
	}

	for _, name := range required {
		if !seen[name] {
			t.Fatalf("missing operation %q", name)
		}
	}
}

func TestOperationSchemasAreSerializableObjects(t *testing.T) {
	for _, operation := range AllOperations() {
		for label, schema := range map[string]Schema{
			"input":  operation.InputSchema,
			"output": operation.OutputSchema,
		} {
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("%s %s schema is not JSON serializable: %v", operation.Name, label, err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("%s %s schema is not JSON: %v", operation.Name, label, err)
			}
			if decoded["type"] != "object" {
				t.Fatalf("%s %s schema should be an object schema, got %#v", operation.Name, label, decoded["type"])
			}
			if decoded["description"] == "" {
				t.Fatalf("%s %s schema should include a description", operation.Name, label)
			}
			if _, ok := decoded["properties"].(map[string]any); !ok {
				t.Fatalf("%s %s schema should include properties", operation.Name, label)
			}
		}
	}
}

func TestImplementedOperationsExposeSharedSurfaceBindings(t *testing.T) {
	cases := []struct {
		name string
		rest string
		cli  string
		mcp  string
	}{
		{OperationCreateTicket, RESTCreateTicket, CLICreateTicket, OperationCreateTicket},
		{OperationProposeTicket, RESTProposeTicket, CLIProposeTicket, OperationProposeTicket},
		{OperationClaimNextTicket, RESTClaimNextTicket, CLIClaimNextTicket, OperationClaimNextTicket},
		{OperationHeartbeatAttempt, RESTHeartbeat, CLIHeartbeat, OperationHeartbeatAttempt},
		{OperationCheckpointAttempt, RESTCheckpoint, CLICheckpoint, OperationCheckpointAttempt},
		{OperationCompleteAttempt, RESTCompleteAttempt, CLICompleteAttempt, OperationCompleteAttempt},
		{OperationFailAttempt, RESTFailAttempt, CLIFailAttempt, OperationFailAttempt},
		{OperationBlockAttempt, RESTBlockAttempt, CLIBlockAttempt, OperationBlockAttempt},
		{OperationListTickets, RESTListTickets, CLIListTickets, OperationListTickets},
		{OperationGetTicket, RESTGetTicket, CLIGetTicket, OperationGetTicket},
		{OperationAttachArtifact, RESTAttachArtifact, CLIAttachArtifact, OperationAttachArtifact},
	}

	for _, tc := range cases {
		operation, ok := OperationByName(tc.name)
		if !ok {
			t.Fatalf("missing operation %q", tc.name)
		}
		if operation.Bindings.RESTOperationID != tc.rest {
			t.Fatalf("%s REST binding = %q, want %q", tc.name, operation.Bindings.RESTOperationID, tc.rest)
		}
		if operation.Bindings.CLICommand != tc.cli {
			t.Fatalf("%s CLI binding = %q, want %q", tc.name, operation.Bindings.CLICommand, tc.cli)
		}
		if operation.Bindings.MCPTool != tc.mcp {
			t.Fatalf("%s MCP binding = %q, want %q", tc.name, operation.Bindings.MCPTool, tc.mcp)
		}
	}
}

func TestAgentCreationSchemasEncourageUsefulContext(t *testing.T) {
	for _, name := range []string{OperationProposeTicket, OperationCreateTicketFromAttempt} {
		operation := MustOperation(name)
		props := operation.InputSchema["properties"].(map[string]any)
		for _, field := range []string{"acceptance_criteria", "verification_commands", "relevant_paths", "creation_reason"} {
			if _, ok := props[field]; !ok {
				t.Fatalf("%s input schema should include %q", name, field)
			}
		}
	}
}
