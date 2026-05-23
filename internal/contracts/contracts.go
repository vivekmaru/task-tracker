package contracts

import (
	"encoding/json"
	"fmt"
)

const (
	OperationCreateTicket              = "create_ticket"
	OperationProposeTicket             = "propose_ticket"
	OperationCreateTicketFromAttempt   = "create_ticket_from_attempt"
	OperationClaimNextTicket           = "claim_next_ticket"
	OperationHeartbeatAttempt          = "heartbeat_attempt"
	OperationCheckpointAttempt         = "checkpoint_attempt"
	OperationUpdateTicket              = "update_ticket"
	OperationMarkTicketReady           = "mark_ticket_ready"
	OperationReopenTicket              = "reopen_ticket"
	OperationUnblockTicket             = "unblock_ticket"
	OperationRequestTicketReview       = "request_ticket_review"
	OperationReviewTicket              = "review_ticket"
	OperationArchiveTicket             = "archive_ticket"
	OperationCompleteAttempt           = "complete_attempt"
	OperationFailAttempt               = "fail_attempt"
	OperationBlockAttempt              = "block_attempt"
	OperationListTickets               = "list_tickets"
	OperationGetTicket                 = "get_ticket"
	OperationAttachArtifact            = "attach_artifact"
	OperationDecomposeTicket           = "decompose_ticket"
	OperationRegisterAgentCapabilities = "register_agent_capabilities"
	OperationAnalyticsSummary          = "analytics_summary"
	OperationAnalyticsByModel          = "analytics_by_model"
	OperationAnalyticsByHarness        = "analytics_by_harness"
	OperationAnalyticsByStatus         = "analytics_by_status"
	OperationAnalyticsByAgent          = "analytics_by_agent"

	RESTCreateTicket       = "create-ticket"
	RESTProposeTicket      = "propose-ticket"
	RESTClaimNextTicket    = "claim-next-ticket"
	RESTHeartbeat          = "heartbeat-attempt"
	RESTCheckpoint         = "checkpoint-attempt"
	RESTUpdateTicket       = "update-ticket"
	RESTMarkTicketReady    = "ready-ticket"
	RESTReopenTicket       = "reopen-ticket"
	RESTUnblockTicket      = "unblock-ticket"
	RESTRequestReview      = "request-ticket-review"
	RESTReviewTicket       = "review-ticket"
	RESTArchiveTicket      = "archive-ticket"
	RESTCompleteAttempt    = "complete-attempt"
	RESTFailAttempt        = "fail-attempt"
	RESTBlockAttempt       = "block-attempt"
	RESTListTickets        = "list-tickets"
	RESTGetTicket          = "get-ticket"
	RESTAttachArtifact     = "create-artifact"
	RESTDecomposeTicket    = "decompose-ticket"
	RESTAnalyticsSummary   = "analytics-summary"
	RESTAnalyticsByModel   = "analytics-by-model"
	RESTAnalyticsByHarness = "analytics-by-harness"
	RESTAnalyticsByStatus  = "analytics-by-status"
	RESTAnalyticsByAgent   = "analytics-by-agent"

	CLICreateTicket    = "create"
	CLIProposeTicket   = "propose"
	CLIClaimNextTicket = "claim-next"
	CLIHeartbeat       = "heartbeat"
	CLICheckpoint      = "checkpoint"
	CLICompleteAttempt = "complete"
	CLIFailAttempt     = "fail"
	CLIBlockAttempt    = "block"
	CLIListTickets     = "list"
	CLIGetTicket       = "get"
	CLIAttachArtifact  = "attach"
	CLIAnalytics       = "analytics"
)

type SurfaceBinding struct {
	RESTOperationID string
	CLICommand      string
	MCPTool         string
}

type Operation struct {
	Name         string
	Summary      string
	Description  string
	Bindings     SurfaceBinding
	InputSchema  Schema
	OutputSchema Schema
}

type Schema map[string]any

func AllOperations() []Operation {
	out := make([]Operation, len(operations))
	copy(out, operations)
	return out
}

func OperationByName(name string) (Operation, bool) {
	for _, operation := range operations {
		if operation.Name == name {
			return operation, true
		}
	}
	return Operation{}, false
}

func MustOperation(name string) Operation {
	operation, ok := OperationByName(name)
	if !ok {
		panic(fmt.Sprintf("unknown Forge operation %q", name))
	}
	return operation
}

func (s Schema) JSON() ([]byte, error) {
	return json.Marshal(s)
}

var operations = []Operation{
	{
		Name:        OperationCreateTicket,
		Summary:     "Create a ticket",
		Description: "Create human- or system-authored work with enough context for agents to claim and execute.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTCreateTicket,
			CLICommand:      CLICreateTicket,
			MCPTool:         OperationCreateTicket,
		},
		InputSchema:  ticketInputSchema("Create ticket input", []string{"workspace_id", "project_id", "title", "type", "acceptance_criteria"}),
		OutputSchema: ticketOutputSchema("Created ticket"),
	},
	{
		Name:        OperationProposeTicket,
		Summary:     "Propose agent-discovered work",
		Description: "Capture agent-discovered work as low-friction backlog without silently enqueueing it for execution.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTProposeTicket,
			CLICommand:      CLIProposeTicket,
			MCPTool:         OperationProposeTicket,
		},
		InputSchema:  ticketInputSchema("Propose ticket input", []string{"workspace_id", "project_id", "title", "type", "acceptance_criteria", "creation_reason"}),
		OutputSchema: ticketOutputSchema("Proposed ticket"),
	},
	{
		Name:        OperationCreateTicketFromAttempt,
		Summary:     "Create a ticket from an attempt",
		Description: "Turn a blocked or failed attempt into structured follow-up work with source attribution.",
		Bindings: SurfaceBinding{
			MCPTool: OperationCreateTicketFromAttempt,
		},
		InputSchema: objectSchema("Create ticket from attempt input", []string{"workspace_id", "project_id", "attempt_id", "title", "type", "acceptance_criteria", "creation_reason"}, map[string]any{
			"workspace_id":        uuidSchema("Workspace ID"),
			"project_id":          uuidSchema("Project ID"),
			"attempt_id":          uuidSchema("Source attempt ID"),
			"source_artifact_id":  optionalUUIDSchema("Evidence artifact that explains the discovered work"),
			"created_by_id":       stringSchema("Creator identifier for ticket-event attribution"),
			"title":               stringSchema("Short imperative ticket title"),
			"description":         stringSchema("Context captured from the attempt"),
			"type":                enumSchema("Ticket template", "bug", "feature", "documentation", "review", "investigation", "cleanup", "follow_up"),
			"priority":            integerSchema("Priority from 0 highest to 4 lowest", 0, 4),
			"tags":                stringArraySchema("Search and routing tags"),
			"acceptance_criteria": stringArraySchema("Observable conditions that make the follow-up done"),
			"verification_commands": stringArraySchema(
				"Commands or checks a future agent should run when completing the ticket",
			),
			"expected_artifacts":    stringArraySchema("Proof artifacts expected at completion"),
			"relevant_paths":        stringArraySchema("Files, packages, docs, or external paths related to the work"),
			"required_tools":        stringArraySchema("Tools the future worker likely needs"),
			"required_permissions":  stringArraySchema("Permissions the future worker likely needs"),
			"required_capabilities": stringArraySchema("Capabilities required to claim the follow-up"),
			"allowed_harnesses":     stringArraySchema("Harnesses allowed to claim the follow-up"),
			"environment":           freeformObjectSchema("Environment facts for the future worker"),
			"input":                 freeformObjectSchema("Structured task input"),
			"creation_reason":       stringSchema("Why the attempt created this work"),
		}),
		OutputSchema: ticketOutputSchema("Created follow-up ticket"),
	},
	{
		Name:        OperationClaimNextTicket,
		Summary:     "Claim next eligible ticket",
		Description: "Atomically claim work and return a context bundle that lets an agent start without extra lookup ceremony.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTClaimNextTicket,
			CLICommand:      CLIClaimNextTicket,
			MCPTool:         OperationClaimNextTicket,
		},
		InputSchema: objectSchema("Claim next ticket input", []string{"workspace_id", "project_id", "agent_id", "harness", "lease_seconds"}, map[string]any{
			"workspace_id":    uuidSchema("Workspace ID"),
			"project_id":      uuidSchema("Project ID"),
			"type":            stringSchema("Optional ticket type filter"),
			"tags":            stringArraySchema("Optional tag filters"),
			"harness":         stringSchema("Harness claiming the work, such as codex"),
			"capabilities":    stringArraySchema("Capabilities available to the claiming agent"),
			"agent_id":        stringSchema("Stable agent or worker identifier"),
			"model":           stringSchema("Model or agent runtime label"),
			"lease_seconds":   integerSchema("Initial claim lease in seconds", 1, 86400),
			"idempotency_key": stringSchema("Optional replay key for retry-safe claim-next calls"),
			"idempotency_ttl": integerSchema("Optional idempotency retention in seconds", 0, 604800),
		}),
		OutputSchema: objectSchema("Claim next ticket output", []string{"ticket", "attempt", "context"}, map[string]any{
			"ticket":  ticketReferenceSchema("Claimed ticket"),
			"attempt": attemptReferenceSchema("Created attempt"),
			"context": claimContextSchema(),
		}),
	},
	{
		Name:        OperationHeartbeatAttempt,
		Summary:     "Heartbeat attempt",
		Description: "Extend an active attempt lease without recording resumable progress.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTHeartbeat,
			CLICommand:      CLIHeartbeat,
			MCPTool:         OperationHeartbeatAttempt,
		},
		InputSchema: objectSchema("Heartbeat attempt input", []string{"attempt_id", "lease_seconds"}, map[string]any{
			"attempt_id":    uuidSchema("Attempt ID"),
			"lease_seconds": integerSchema("Lease extension in seconds", 1, 86400),
		}),
		OutputSchema: attemptOutputSchema("Heartbeat result"),
	},
	{
		Name:        OperationCheckpointAttempt,
		Summary:     "Checkpoint attempt",
		Description: "Record resumable progress, touched files, commands run, next step, and risk.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTCheckpoint,
			CLICommand:      CLICheckpoint,
			MCPTool:         OperationCheckpointAttempt,
		},
		InputSchema: objectSchema("Checkpoint attempt input", []string{"attempt_id", "summary", "progress_percent"}, map[string]any{
			"attempt_id":       uuidSchema("Attempt ID"),
			"summary":          stringSchema("Concise progress summary"),
			"progress_percent": integerSchema("Approximate completion percentage", 0, 100),
			"files_touched":    stringArraySchema("Files changed or inspected"),
			"commands_run":     stringArraySchema("Commands run since the last checkpoint"),
			"next_step":        stringSchema("Most useful next action"),
			"risk":             stringSchema("Known risk, uncertainty, or blocker signal"),
		}),
		OutputSchema: objectSchema("Checkpoint attempt output", []string{"checkpoint_id", "attempt_id", "summary", "progress_percent"}, map[string]any{
			"checkpoint_id":    uuidSchema("Checkpoint ID"),
			"attempt_id":       uuidSchema("Attempt ID"),
			"summary":          stringSchema("Recorded summary"),
			"progress_percent": integerSchema("Recorded progress percentage", 0, 100),
		}),
	},
	{
		Name:        OperationUpdateTicket,
		Summary:     "Update ticket",
		Description: "Patch ticket metadata without changing attempt lifecycle state.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTUpdateTicket,
			MCPTool:         OperationUpdateTicket,
		},
		InputSchema: objectSchema("Update ticket input", []string{"ticket_id", "patch"}, map[string]any{
			"ticket_id": uuidSchema("Ticket ID"),
			"actor_id":  stringSchema("Actor ID for ticket-event attribution"),
			"patch": nonEmptyObjectSchema("Patch fields", nil, map[string]any{
				"title":                 stringSchema("Updated title"),
				"description":           stringSchema("Updated description"),
				"acceptance_criteria":   stringArraySchema("Updated acceptance criteria"),
				"verification_commands": stringArraySchema("Updated verification commands"),
				"relevant_paths":        stringArraySchema("Updated relevant paths"),
				"tags":                  stringArraySchema("Updated tags"),
			}),
		}),
		OutputSchema: ticketOutputSchema("Updated ticket"),
	},
	{
		Name:        OperationMarkTicketReady,
		Summary:     "Mark ticket ready",
		Description: "Move backlog work into the claimable todo queue.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTMarkTicketReady,
			MCPTool:         OperationMarkTicketReady,
		},
		InputSchema:  ticketTransitionInputSchema("Mark ticket ready input"),
		OutputSchema: ticketOutputSchema("Ready ticket"),
	},
	{
		Name:        OperationReopenTicket,
		Summary:     "Reopen ticket",
		Description: "Move terminal work back to todo for another attempt.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTReopenTicket,
			MCPTool:         OperationReopenTicket,
		},
		InputSchema:  ticketTransitionInputSchema("Reopen ticket input"),
		OutputSchema: ticketOutputSchema("Reopened ticket"),
	},
	{
		Name:        OperationUnblockTicket,
		Summary:     "Unblock ticket",
		Description: "Move blocked work back to todo after the blocker is resolved.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTUnblockTicket,
			MCPTool:         OperationUnblockTicket,
		},
		InputSchema:  ticketTransitionInputSchema("Unblock ticket input"),
		OutputSchema: ticketOutputSchema("Unblocked ticket"),
	},
	{
		Name:        OperationRequestTicketReview,
		Summary:     "Request ticket review",
		Description: "Move work into needs_review for a human or authorized reviewer decision.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTRequestReview,
			MCPTool:         OperationRequestTicketReview,
		},
		InputSchema:  ticketTransitionInputSchema("Request ticket review input"),
		OutputSchema: ticketOutputSchema("Ticket needing review"),
	},
	{
		Name:        OperationReviewTicket,
		Summary:     "Review ticket",
		Description: "Approve or reject work that is waiting in needs_review.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTReviewTicket,
			MCPTool:         OperationReviewTicket,
		},
		InputSchema: objectSchema("Review ticket input", []string{"ticket_id", "decision"}, map[string]any{
			"ticket_id": uuidSchema("Ticket ID"),
			"decision":  enumSchema("Review decision", "approve", "reject"),
			"actor_id":  stringSchema("Actor ID for ticket-event attribution"),
			"reason":    stringSchema("Reason for the review decision"),
		}),
		OutputSchema: ticketOutputSchema("Reviewed ticket"),
	},
	{
		Name:        OperationArchiveTicket,
		Summary:     "Archive ticket",
		Description: "Move non-running work out of active queues.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTArchiveTicket,
			MCPTool:         OperationArchiveTicket,
		},
		InputSchema:  ticketTransitionInputSchema("Archive ticket input"),
		OutputSchema: ticketOutputSchema("Archived ticket"),
	},
	{
		Name:        OperationCompleteAttempt,
		Summary:     "Complete attempt",
		Description: "Finish a running attempt successfully and move its ticket according to workflow rules.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTCompleteAttempt,
			CLICommand:      CLICompleteAttempt,
			MCPTool:         OperationCompleteAttempt,
		},
		InputSchema: transitionInputSchema("Complete attempt input", []string{"attempt_id", "output"}, map[string]any{
			"output":        objectSchema("Structured completion output", nil, nil),
			"output_schema": stringSchema("Optional schema identifier for output"),
			"metrics":       attemptMetricsSchema(),
		}),
		OutputSchema: transitionOutputSchema("Complete attempt output"),
	},
	{
		Name:        OperationFailAttempt,
		Summary:     "Fail attempt",
		Description: "Finish a running attempt unsuccessfully with actionable failure context.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTFailAttempt,
			CLICommand:      CLIFailAttempt,
			MCPTool:         OperationFailAttempt,
		},
		InputSchema: transitionInputSchema("Fail attempt input", []string{"attempt_id", "failure_reason"}, map[string]any{
			"failure_reason":   stringSchema("Human-readable reason the attempt failed"),
			"failure_category": stringSchema("Optional normalized failure category"),
			"output":           objectSchema("Structured failure output", nil, nil),
			"metrics":          attemptMetricsSchema(),
		}),
		OutputSchema: transitionOutputSchema("Fail attempt output"),
	},
	{
		Name:        OperationBlockAttempt,
		Summary:     "Block attempt",
		Description: "Finish a running attempt as blocked and capture enough blocker context to unblock later.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTBlockAttempt,
			CLICommand:      CLIBlockAttempt,
			MCPTool:         OperationBlockAttempt,
		},
		InputSchema: transitionInputSchema("Block attempt input", []string{"attempt_id", "blocker_reason"}, map[string]any{
			"blocker_reason":   stringSchema("Reason the attempt is blocked"),
			"failure_category": stringSchema("Optional normalized blocker category"),
			"blocker":          objectSchema("Structured blocker details", nil, nil),
			"metrics":          attemptMetricsSchema(),
		}),
		OutputSchema: transitionOutputSchema("Block attempt output"),
	},
	{
		Name:        OperationListTickets,
		Summary:     "List tickets",
		Description: "List tickets by workspace, project, status, and type.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTListTickets,
			CLICommand:      CLIListTickets,
			MCPTool:         OperationListTickets,
		},
		InputSchema: objectSchema("List tickets input", []string{"workspace_id", "project_id"}, map[string]any{
			"workspace_id": uuidSchema("Workspace ID"),
			"project_id":   uuidSchema("Project ID"),
			"status":       stringSchema("Optional status filter"),
			"type":         stringSchema("Optional type filter"),
			"offset":       integerSchema("Pagination offset", 0, 1000000),
			"limit":        integerSchema("Maximum result count", 1, 200),
		}),
		OutputSchema: objectSchema("List tickets output", []string{"tickets"}, map[string]any{
			"tickets": arraySchema("Tickets", ticketReferenceSchema("Ticket")),
		}),
	},
	{
		Name:        OperationGetTicket,
		Summary:     "Get ticket",
		Description: "Fetch a ticket by ID.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTGetTicket,
			CLICommand:      CLIGetTicket,
			MCPTool:         OperationGetTicket,
		},
		InputSchema: objectSchema("Get ticket input", []string{"ticket_id"}, map[string]any{
			"ticket_id": uuidSchema("Ticket ID"),
		}),
		OutputSchema: ticketOutputSchema("Ticket"),
	},
	{
		Name:        OperationAttachArtifact,
		Summary:     "Attach artifact",
		Description: "Register proof, context, diagnostic, handoff, or output artifact metadata for a ticket or attempt.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAttachArtifact,
			CLICommand:      CLIAttachArtifact,
			MCPTool:         OperationAttachArtifact,
		},
		InputSchema: objectSchema("Attach artifact input", []string{"workspace_id", "project_id", "ticket_id", "type", "role", "name", "url"}, map[string]any{
			"workspace_id":    uuidSchema("Workspace ID"),
			"project_id":      uuidSchema("Project ID"),
			"ticket_id":       uuidSchema("Ticket ID"),
			"attempt_id":      optionalUUIDSchema("Attempt ID when artifact belongs to an attempt"),
			"type":            enumSchema("Artifact type", "code", "document", "image", "dataset", "log", "diff", "trace", "test_output", "screenshot", "handoff", "diagnostic", "final_response", "other"),
			"role":            enumSchema("Artifact role", "evidence", "patch", "context", "output", "diagnostic", "handoff"),
			"name":            stringSchema("Display name"),
			"url":             stringSchema("Local or remote artifact URL"),
			"storage_backend": enumSchema("Storage backend", "local", "s3"),
			"size_bytes":      integerSchema("Artifact size in bytes", 0, 1<<62),
			"mime_type":       stringSchema("MIME type"),
			"metadata":        objectSchema("Additional metadata", nil, nil),
		}),
		OutputSchema: objectSchema("Attach artifact output", []string{"artifact"}, map[string]any{
			"artifact": artifactReferenceSchema("Registered artifact"),
		}),
	},
	{
		Name:        OperationDecomposeTicket,
		Summary:     "Decompose ticket",
		Description: "Create or propose child tasks and dependencies from a larger ticket or phase.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTDecomposeTicket,
			MCPTool:         OperationDecomposeTicket,
		},
		InputSchema: objectSchema("Decompose ticket input", []string{"workspace_id", "project_id", "ticket_id", "creation_reason", "children"}, map[string]any{
			"workspace_id":    uuidSchema("Workspace ID"),
			"project_id":      uuidSchema("Project ID"),
			"ticket_id":       uuidSchema("Parent ticket ID"),
			"root_id":         optionalUUIDSchema("Root ticket ID for nested decomposition"),
			"mode":            enumSchema("Creation mode", "propose", "create"),
			"can_enqueue":     falseBooleanSchema("MCP callers cannot self-grant enqueue authority; create mode authorization is adapter-controlled"),
			"created_by_id":   stringSchema("Creator identifier"),
			"creation_reason": stringSchema("Why this decomposition is being created"),
			"children": arraySchema("Child ticket proposals", objectSchema("Child ticket proposal", []string{"key", "title", "type", "acceptance_criteria"}, map[string]any{
				"key":                   stringSchema("Stable child key used for dependency wiring"),
				"title":                 stringSchema("Child title"),
				"description":           stringSchema("Child context"),
				"type":                  stringSchema("Child ticket type"),
				"tags":                  stringArraySchema("Child tags"),
				"acceptance_criteria":   stringArraySchema("Child acceptance criteria"),
				"verification_commands": stringArraySchema("Child verification commands"),
				"expected_artifacts":    stringArraySchema("Expected child proof artifacts"),
				"relevant_paths":        stringArraySchema("Relevant files, packages, docs, or external paths"),
				"required_tools":        stringArraySchema("Required tools"),
				"required_permissions":  stringArraySchema("Required permissions"),
				"required_capabilities": stringArraySchema("Required claiming capabilities"),
				"allowed_harnesses":     stringArraySchema("Harnesses allowed to claim this child"),
				"environment":           freeformObjectSchema("Environment facts"),
				"input":                 freeformObjectSchema("Structured child input"),
				"depends_on":            stringArraySchema("Sibling child keys this child depends on"),
			})),
		}),
		OutputSchema: objectSchema("Decompose ticket output", []string{"children"}, map[string]any{
			"children": arraySchema("Created or proposed child tickets", ticketReferenceSchema("Child ticket")),
		}),
	},
	{
		Name:        OperationRegisterAgentCapabilities,
		Summary:     "Register agent capabilities",
		Description: "Register the tools, transports, artifact abilities, and execution preferences a harness or worker supports.",
		Bindings: SurfaceBinding{
			MCPTool: OperationRegisterAgentCapabilities,
		},
		InputSchema: objectSchema("Register agent capabilities input", []string{"workspace_id", "project_id", "agent_id", "harness", "transports", "capabilities"}, map[string]any{
			"workspace_id":    uuidSchema("Workspace ID"),
			"project_id":      uuidSchema("Project ID"),
			"agent_id":        stringSchema("Stable agent or worker identifier"),
			"harness":         stringSchema("Harness name, such as codex"),
			"model":           stringSchema("Model or runtime label"),
			"transports":      stringArraySchema("Supported transports, such as cli, rest, or mcp"),
			"capabilities":    stringArraySchema("Capability labels used for matching claims"),
			"tool_names":      stringArraySchema("Specific tool names this agent can call"),
			"artifact_roles":  stringArraySchema("Artifact roles this agent can produce"),
			"preferred_claim": freeformObjectSchema("Preferred claim filters and lease settings"),
			"metadata":        freeformObjectSchema("Additional harness metadata"),
		}),
		OutputSchema: objectSchema("Register agent capabilities output", []string{"agent_id", "harness", "capabilities"}, map[string]any{
			"agent_id":     stringSchema("Registered agent ID"),
			"harness":      stringSchema("Registered harness"),
			"capabilities": stringArraySchema("Registered capabilities"),
		}),
	},
	{
		Name:        OperationAnalyticsSummary,
		Summary:     "Analytics summary",
		Description: "Summarize attempts, provided metrics, token usage, cost, duration, and retries.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAnalyticsSummary,
			CLICommand:      CLIAnalytics,
			MCPTool:         OperationAnalyticsSummary,
		},
		InputSchema:  analyticsFilterSchema("Analytics summary input"),
		OutputSchema: analyticsSummarySchema("Analytics summary output"),
	},
	{
		Name:        OperationAnalyticsByModel,
		Summary:     "Analytics by model",
		Description: "Aggregate attempt counts, success counts, tokens, cost, duration, and retries by model.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAnalyticsByModel,
			CLICommand:      CLIAnalytics,
			MCPTool:         OperationAnalyticsByModel,
		},
		InputSchema:  analyticsFilterSchema("Analytics by model input"),
		OutputSchema: analyticsGroupSchema("Analytics by model output"),
	},
	{
		Name:        OperationAnalyticsByHarness,
		Summary:     "Analytics by harness",
		Description: "Aggregate attempt counts, outcome counts, tokens, cost, duration, and retries by harness.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAnalyticsByHarness,
			CLICommand:      CLIAnalytics,
			MCPTool:         OperationAnalyticsByHarness,
		},
		InputSchema:  analyticsFilterSchema("Analytics by harness input"),
		OutputSchema: analyticsGroupSchema("Analytics by harness output"),
	},
	{
		Name:        OperationAnalyticsByStatus,
		Summary:     "Analytics by status",
		Description: "Aggregate attempt counts, outcome counts, tokens, cost, duration, and retries by status.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAnalyticsByStatus,
			CLICommand:      CLIAnalytics,
			MCPTool:         OperationAnalyticsByStatus,
		},
		InputSchema:  analyticsFilterSchema("Analytics by status input"),
		OutputSchema: analyticsGroupSchema("Analytics by status output"),
	},
	{
		Name:        OperationAnalyticsByAgent,
		Summary:     "Analytics by agent",
		Description: "Aggregate attempt counts, outcome counts, tokens, cost, duration, and retries by agent.",
		Bindings: SurfaceBinding{
			RESTOperationID: RESTAnalyticsByAgent,
			CLICommand:      CLIAnalytics,
			MCPTool:         OperationAnalyticsByAgent,
		},
		InputSchema:  analyticsFilterSchema("Analytics by agent input"),
		OutputSchema: analyticsGroupSchema("Analytics by agent output"),
	},
}

func ticketInputSchema(title string, required []string) Schema {
	return objectSchema(title, required, map[string]any{
		"workspace_id":          uuidSchema("Workspace ID"),
		"project_id":            uuidSchema("Project ID"),
		"parent_id":             optionalUUIDSchema("Parent ticket ID"),
		"root_id":               optionalUUIDSchema("Root ticket ID"),
		"source_attempt_id":     optionalUUIDSchema("Attempt that produced this ticket"),
		"source_artifact_id":    optionalUUIDSchema("Artifact that supports this ticket"),
		"title":                 stringSchema("Short imperative title"),
		"description":           stringSchema("Useful context for the worker"),
		"type":                  enumSchema("Ticket type", "feature", "bug", "documentation", "research", "analysis", "planning", "review", "integration", "investigation", "cleanup", "follow_up", "custom"),
		"status":                enumSchema("Initial status", "backlog", "todo"),
		"priority":              integerSchema("Priority from 0 highest to 4 lowest", 0, 4),
		"tags":                  stringArraySchema("Search and routing tags"),
		"acceptance_criteria":   stringArraySchema("Observable conditions that make the work done"),
		"verification_commands": stringArraySchema("Commands or checks the worker should run"),
		"expected_artifacts":    stringArraySchema("Proof artifacts expected at completion"),
		"relevant_paths":        stringArraySchema("Files, packages, docs, or external paths related to the work"),
		"required_tools":        stringArraySchema("Tools the worker likely needs"),
		"required_permissions":  stringArraySchema("Permissions the worker likely needs"),
		"environment":           objectSchema("Environment facts for the worker", nil, nil),
		"input":                 objectSchema("Structured task input", nil, nil),
		"input_schema":          stringSchema("Optional schema identifier for input"),
		"required_capabilities": stringArraySchema("Capabilities required to claim the ticket"),
		"allowed_harnesses":     stringArraySchema("Harnesses allowed to claim the ticket"),
		"retry_policy":          objectSchema("Retry policy", nil, nil),
		"dependencies":          stringArraySchema("Ticket IDs this ticket depends on"),
		"created_by":            enumSchema("Creator actor type", "human", "agent", "system"),
		"created_by_id":         stringSchema("Creator identifier"),
		"creation_reason":       stringSchema("Why this ticket exists"),
		"enqueue":               booleanSchema("Whether to create directly in the claimable queue"),
	})
}

func transitionInputSchema(title string, required []string, extra map[string]any) Schema {
	properties := map[string]any{"attempt_id": uuidSchema("Attempt ID")}
	for key, value := range extra {
		properties[key] = value
	}
	return objectSchema(title, required, properties)
}

func attemptMetricsSchema() Schema {
	return objectSchema("Optional attempt metrics supplied by a harness", nil, map[string]any{
		"tokens_in":        integerSchema("Input tokens used by the attempt", 0, 1<<62),
		"tokens_out":       integerSchema("Output tokens produced by the attempt", 0, 1<<62),
		"cost_usd":         numberSchema("Attempt cost in USD", 0),
		"duration_seconds": numberSchema("Attempt duration in seconds", 0),
		"retry_count":      integerSchema("Retries used inside the harness", 0, 1<<31-1),
	})
}

func ticketTransitionInputSchema(title string) Schema {
	return objectSchema(title, []string{"ticket_id"}, map[string]any{
		"ticket_id": uuidSchema("Ticket ID"),
		"actor_id":  stringSchema("Actor ID for ticket-event attribution"),
		"reason":    stringSchema("Reason for the transition"),
	})
}

func ticketOutputSchema(title string) Schema {
	return objectSchema(title, []string{"ticket"}, map[string]any{
		"ticket": ticketReferenceSchema("Ticket"),
	})
}

func attemptOutputSchema(title string) Schema {
	return objectSchema(title, []string{"attempt"}, map[string]any{
		"attempt": attemptReferenceSchema("Attempt"),
	})
}

func transitionOutputSchema(title string) Schema {
	return objectSchema(title, []string{"attempt_id", "ticket_id", "attempt_status", "ticket_status"}, map[string]any{
		"attempt_id":     uuidSchema("Attempt ID"),
		"ticket_id":      uuidSchema("Ticket ID"),
		"attempt_status": stringSchema("Final attempt status"),
		"ticket_status":  stringSchema("Final ticket status"),
	})
}

func ticketReferenceSchema(description string) Schema {
	return objectSchema(description, []string{"id", "title", "type", "status"}, map[string]any{
		"id":           uuidSchema("Ticket ID"),
		"title":        stringSchema("Ticket title"),
		"type":         stringSchema("Ticket type"),
		"status":       stringSchema("Ticket status"),
		"priority":     integerSchema("Ticket priority", 0, 4),
		"project_id":   uuidSchema("Project ID"),
		"workspace_id": uuidSchema("Workspace ID"),
	})
}

func attemptReferenceSchema(description string) Schema {
	return objectSchema(description, []string{"id", "ticket_id", "status", "agent_id", "harness"}, map[string]any{
		"id":        uuidSchema("Attempt ID"),
		"ticket_id": uuidSchema("Ticket ID"),
		"status":    stringSchema("Attempt status"),
		"agent_id":  stringSchema("Agent ID"),
		"harness":   stringSchema("Harness name"),
		"model":     stringSchema("Model or runtime label"),
	})
}

func artifactReferenceSchema(description string) Schema {
	return objectSchema(description, []string{"id", "ticket_id", "type", "role", "name", "url"}, map[string]any{
		"id":              uuidSchema("Artifact ID"),
		"ticket_id":       uuidSchema("Ticket ID"),
		"attempt_id":      optionalUUIDSchema("Attempt ID"),
		"type":            stringSchema("Artifact type"),
		"role":            stringSchema("Artifact role"),
		"name":            stringSchema("Display name"),
		"url":             stringSchema("Artifact URL"),
		"storage_backend": stringSchema("Storage backend"),
		"size_bytes":      integerSchema("Artifact size in bytes", 0, 1<<62),
		"mime_type":       stringSchema("MIME type"),
	})
}

func analyticsFilterSchema(title string) Schema {
	return objectSchema(title, nil, map[string]any{
		"workspace_id": optionalUUIDSchema("Workspace scope"),
		"project_id":   optionalUUIDSchema("Project scope"),
	})
}

func analyticsSummarySchema(title string) Schema {
	return objectSchema(title, []string{"summary"}, map[string]any{
		"summary": objectSchema("Summary", []string{"attempt_count", "total_cost_usd"}, map[string]any{
			"attempt_count":          integerSchema("Total attempts", 0, 1<<62),
			"succeeded_attempts":     integerSchema("Succeeded attempts", 0, 1<<62),
			"failed_attempts":        integerSchema("Failed attempts", 0, 1<<62),
			"blocked_attempts":       integerSchema("Blocked attempts", 0, 1<<62),
			"total_tokens_in":        integerSchema("Input tokens", 0, 1<<62),
			"total_tokens_out":       integerSchema("Output tokens", 0, 1<<62),
			"total_cost_usd":         numberSchema("Total cost in USD", 0),
			"total_duration_seconds": numberSchema("Total duration in seconds", 0),
			"total_retries":          integerSchema("Total retries", 0, 1<<62),
			"attempts_with_metrics":  integerSchema("Attempts that submitted metrics", 0, 1<<62),
		}),
	})
}

func analyticsGroupSchema(title string) Schema {
	return objectSchema(title, []string{"groups"}, map[string]any{
		"groups": arraySchema("Grouped analytics rows", objectSchema("Group", []string{"group", "attempt_count"}, map[string]any{
			"group":                  stringSchema("Analytics group value"),
			"attempt_count":          integerSchema("Total attempts", 0, 1<<62),
			"succeeded_attempts":     integerSchema("Succeeded attempts", 0, 1<<62),
			"failed_attempts":        integerSchema("Failed attempts", 0, 1<<62),
			"blocked_attempts":       integerSchema("Blocked attempts", 0, 1<<62),
			"total_tokens_in":        integerSchema("Input tokens", 0, 1<<62),
			"total_tokens_out":       integerSchema("Output tokens", 0, 1<<62),
			"total_cost_usd":         numberSchema("Total cost in USD", 0),
			"total_duration_seconds": numberSchema("Total duration in seconds", 0),
			"total_retries":          integerSchema("Total retries", 0, 1<<62),
			"attempts_with_metrics":  integerSchema("Attempts that submitted metrics", 0, 1<<62),
		})),
	})
}

func claimContextSchema() Schema {
	return objectSchema("Claim context bundle", []string{"ticket", "attempt"}, map[string]any{
		"ticket":                ticketReferenceSchema("Claimed ticket"),
		"attempt":               attemptReferenceSchema("Current attempt"),
		"acceptance_criteria":   stringArraySchema("Acceptance criteria to satisfy"),
		"verification_commands": stringArraySchema("Commands or checks to run"),
		"environment":           objectSchema("Environment facts", nil, nil),
		"input":                 objectSchema("Structured ticket input", nil, nil),
		"relevant_paths":        stringArraySchema("Relevant files, packages, docs, or external paths"),
		"required_tools":        stringArraySchema("Required tools"),
		"required_permissions":  stringArraySchema("Required permissions"),
		"expected_artifacts":    stringArraySchema("Expected proof artifacts"),
		"prior_attempts":        arraySchema("Previous attempts", attemptReferenceSchema("Prior attempt")),
		"checkpoints": arraySchema("Prior checkpoints", objectSchema("Checkpoint", nil, map[string]any{
			"id":               uuidSchema("Checkpoint ID"),
			"summary":          stringSchema("Checkpoint summary"),
			"progress_percent": integerSchema("Progress percentage", 0, 100),
			"next_step":        stringSchema("Suggested next step"),
			"risk":             stringSchema("Risk or blocker note"),
		})),
		"artifacts": arraySchema("Known artifacts", artifactReferenceSchema("Artifact")),
	})
}

func objectSchema(description string, required []string, properties map[string]any) Schema {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := Schema{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"description":          description,
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func freeformObjectSchema(description string) Schema {
	return Schema{
		"type":                 "object",
		"description":          description,
		"additionalProperties": true,
	}
}

func nonEmptyObjectSchema(description string, required []string, properties map[string]any) Schema {
	schema := objectSchema(description, required, properties)
	schema["minProperties"] = 1
	return schema
}

func arraySchema(description string, items any) Schema {
	return Schema{
		"type":        "array",
		"description": description,
		"items":       items,
	}
}

func stringArraySchema(description string) Schema {
	return arraySchema(description, Schema{"type": "string"})
}

func stringSchema(description string) Schema {
	return Schema{"type": "string", "description": description}
}

func booleanSchema(description string) Schema {
	return Schema{"type": "boolean", "description": description}
}

func falseBooleanSchema(description string) Schema {
	return Schema{"type": "boolean", "const": false, "description": description}
}

func uuidSchema(description string) Schema {
	return Schema{"type": "string", "format": "uuid", "description": description}
}

func optionalUUIDSchema(description string) Schema {
	return Schema{"type": "string", "format": "uuid", "description": description}
}

func integerSchema(description string, minimum, maximum int64) Schema {
	return Schema{
		"type":        "integer",
		"description": description,
		"minimum":     minimum,
		"maximum":     maximum,
	}
}

func numberSchema(description string, minimum float64) Schema {
	return Schema{
		"type":        "number",
		"description": description,
		"minimum":     minimum,
	}
}

func enumSchema(description string, values ...string) Schema {
	return Schema{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}
