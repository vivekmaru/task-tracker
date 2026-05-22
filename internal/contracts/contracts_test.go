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
		OperationMarkTicketReady,
		OperationReopenTicket,
		OperationUnblockTicket,
		OperationRequestTicketReview,
		OperationReviewTicket,
		OperationArchiveTicket,
		OperationCompleteAttempt,
		OperationFailAttempt,
		OperationBlockAttempt,
		OperationListTickets,
		OperationGetTicket,
		OperationAttachArtifact,
		OperationDecomposeTicket,
		OperationRegisterAgentCapabilities,
		OperationAnalyticsSummary,
		OperationAnalyticsByModel,
		OperationAnalyticsByHarness,
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

func TestPhaseTwoOperationsExposeSurfaceParityMatrix(t *testing.T) {
	cases := []struct {
		name string
		rest string
		cli  string
		mcp  string
	}{
		{OperationCreateTicket, RESTCreateTicket, CLICreateTicket, OperationCreateTicket},
		{OperationProposeTicket, RESTProposeTicket, CLIProposeTicket, OperationProposeTicket},
		{OperationCreateTicketFromAttempt, "", "", OperationCreateTicketFromAttempt},
		{OperationClaimNextTicket, RESTClaimNextTicket, CLIClaimNextTicket, OperationClaimNextTicket},
		{OperationHeartbeatAttempt, RESTHeartbeat, CLIHeartbeat, OperationHeartbeatAttempt},
		{OperationCheckpointAttempt, RESTCheckpoint, CLICheckpoint, OperationCheckpointAttempt},
		{OperationUpdateTicket, RESTUpdateTicket, "", OperationUpdateTicket},
		{OperationMarkTicketReady, RESTMarkTicketReady, "", OperationMarkTicketReady},
		{OperationReopenTicket, RESTReopenTicket, "", OperationReopenTicket},
		{OperationUnblockTicket, RESTUnblockTicket, "", OperationUnblockTicket},
		{OperationRequestTicketReview, RESTRequestReview, "", OperationRequestTicketReview},
		{OperationReviewTicket, RESTReviewTicket, "", OperationReviewTicket},
		{OperationArchiveTicket, RESTArchiveTicket, "", OperationArchiveTicket},
		{OperationCompleteAttempt, RESTCompleteAttempt, CLICompleteAttempt, OperationCompleteAttempt},
		{OperationFailAttempt, RESTFailAttempt, CLIFailAttempt, OperationFailAttempt},
		{OperationBlockAttempt, RESTBlockAttempt, CLIBlockAttempt, OperationBlockAttempt},
		{OperationListTickets, RESTListTickets, CLIListTickets, OperationListTickets},
		{OperationGetTicket, RESTGetTicket, CLIGetTicket, OperationGetTicket},
		{OperationAttachArtifact, RESTAttachArtifact, CLIAttachArtifact, OperationAttachArtifact},
		{OperationDecomposeTicket, RESTDecomposeTicket, "", OperationDecomposeTicket},
		{OperationRegisterAgentCapabilities, "", "", OperationRegisterAgentCapabilities},
		{OperationAnalyticsSummary, RESTAnalyticsSummary, CLIAnalytics, OperationAnalyticsSummary},
		{OperationAnalyticsByModel, RESTAnalyticsByModel, CLIAnalytics, OperationAnalyticsByModel},
		{OperationAnalyticsByHarness, RESTAnalyticsByHarness, CLIAnalytics, OperationAnalyticsByHarness},
	}

	if len(cases) != len(AllOperations()) {
		t.Fatalf("parity matrix covers %d operations, want %d", len(cases), len(AllOperations()))
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
	for _, name := range []string{OperationCreateTicket, OperationProposeTicket, OperationCreateTicketFromAttempt} {
		operation := MustOperation(name)
		props := operation.InputSchema["properties"].(map[string]any)
		for _, field := range []string{"acceptance_criteria", "verification_commands", "relevant_paths", "creation_reason"} {
			if _, ok := props[field]; !ok {
				t.Fatalf("%s input schema should include %q", name, field)
			}
		}
	}
}

func TestCreateTicketSchemaKeepsCreationReasonAgentScoped(t *testing.T) {
	operation := MustOperation(OperationCreateTicket)
	required := map[string]bool{}
	for _, field := range operation.InputSchema["required"].([]string) {
		required[field] = true
	}
	if required["creation_reason"] {
		t.Fatalf("create_ticket schema should not require creation_reason for non-agent REST and CLI callers")
	}
	props := operation.InputSchema["properties"].(map[string]any)
	if _, ok := props["creation_reason"]; !ok {
		t.Fatalf("create_ticket schema should expose creation_reason for agent callers")
	}
}

func TestCreateFromAttemptSchemaTypeEnumMatchesSupportedTemplates(t *testing.T) {
	operation := MustOperation(OperationCreateTicketFromAttempt)
	props := operation.InputSchema["properties"].(map[string]any)
	typeSchema := props["type"].(Schema)
	enumValues := typeSchema["enum"].([]string)

	want := map[string]bool{
		"bug":           true,
		"feature":       true,
		"documentation": true,
		"review":        true,
		"investigation": true,
		"cleanup":       true,
		"follow_up":     true,
	}
	if len(enumValues) != len(want) {
		t.Fatalf("unexpected template enum values: %#v", enumValues)
	}
	for _, value := range enumValues {
		if !want[value] {
			t.Fatalf("schema exposes unsupported create-from-attempt template %q", value)
		}
	}
}

func TestCreateFromAttemptSchemaExposesForwardedFields(t *testing.T) {
	operation := MustOperation(OperationCreateTicketFromAttempt)
	props := operation.InputSchema["properties"].(map[string]any)
	for _, field := range []string{
		"priority",
		"tags",
		"created_by_id",
		"expected_artifacts",
		"required_tools",
		"required_permissions",
		"required_capabilities",
		"allowed_harnesses",
		"environment",
		"input",
	} {
		if _, ok := props[field]; !ok {
			t.Fatalf("create_ticket_from_attempt schema should expose forwarded field %q", field)
		}
	}
	for _, field := range []string{"environment", "input"} {
		schema := props[field].(Schema)
		if schema["additionalProperties"] != true {
			t.Fatalf("create_ticket_from_attempt %s schema should permit structured context, got %#v", field, schema)
		}
	}
}

func TestDecomposeSchemaExposesRuntimeRequiredFields(t *testing.T) {
	operation := MustOperation(OperationDecomposeTicket)
	required := map[string]bool{}
	for _, field := range operation.InputSchema["required"].([]string) {
		required[field] = true
	}
	if !required["creation_reason"] {
		t.Fatalf("decompose schema should require creation_reason")
	}

	props := operation.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"can_enqueue", "created_by_id", "creation_reason"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("decompose schema should expose %q", field)
		}
	}
	if _, ok := props["created_by"]; ok {
		t.Fatalf("decompose schema should not expose created_by because MCP always records agent")
	}

	children := props["children"].(Schema)
	child := children["items"].(Schema)
	required = map[string]bool{}
	for _, field := range child["required"].([]string) {
		required[field] = true
	}
	if !required["key"] {
		t.Fatalf("decompose child schema should require key for dependency wiring")
	}
	childProps := child["properties"].(map[string]any)
	for _, field := range []string{"key", "required_capabilities", "allowed_harnesses", "depends_on"} {
		if _, ok := childProps[field]; !ok {
			t.Fatalf("decompose child schema should expose %q", field)
		}
	}
	for _, field := range []string{"environment", "input"} {
		schema := childProps[field].(Schema)
		if schema["additionalProperties"] != true {
			t.Fatalf("decompose child %s schema should permit structured context, got %#v", field, schema)
		}
	}
	if props["can_enqueue"].(Schema)["const"] != false {
		t.Fatalf("decompose can_enqueue schema should not allow client-granted enqueue authority")
	}
}

func TestRegisterCapabilitiesSchemaRequiresTransports(t *testing.T) {
	operation := MustOperation(OperationRegisterAgentCapabilities)
	required := map[string]bool{}
	for _, field := range operation.InputSchema["required"].([]string) {
		required[field] = true
	}
	if !required["transports"] {
		t.Fatalf("register capabilities schema should require transports")
	}
}

func TestRegisterCapabilitiesSchemaExposesToolNames(t *testing.T) {
	operation := MustOperation(OperationRegisterAgentCapabilities)
	props := operation.InputSchema["properties"].(map[string]any)
	if _, ok := props["tool_names"]; !ok {
		t.Fatalf("register capabilities schema should expose tool_names")
	}
}

func TestRegisterCapabilitiesSchemaAllowsStructuredMetadata(t *testing.T) {
	operation := MustOperation(OperationRegisterAgentCapabilities)
	props := operation.InputSchema["properties"].(map[string]any)
	for _, field := range []string{"preferred_claim", "metadata"} {
		schema := props[field].(Schema)
		if schema["additionalProperties"] != true {
			t.Fatalf("register capabilities %s schema should permit structured metadata, got %#v", field, schema)
		}
	}
}

func TestUpdateTicketSchemaExposesActorID(t *testing.T) {
	operation := MustOperation(OperationUpdateTicket)
	props := operation.InputSchema["properties"].(map[string]any)
	if _, ok := props["actor_id"]; !ok {
		t.Fatalf("update ticket schema should expose actor_id for event attribution")
	}
}

func TestUpdateTicketSchemaRejectsEmptyPatch(t *testing.T) {
	operation := MustOperation(OperationUpdateTicket)
	props := operation.InputSchema["properties"].(map[string]any)
	patch := props["patch"].(Schema)
	if patch["minProperties"] != 1 {
		t.Fatalf("update_ticket patch schema should reject empty patches, got %#v", patch)
	}
}
