package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

var (
	ErrUnknownTool     = errors.New("unknown MCP tool")
	ErrRuntimeRequired = errors.New("runtime is required")
)

type Runtime interface {
	CreateTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	ProposeTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	CreateTicketFromAttempt(context.Context, services.CreateTicketFromAttemptRequest) (db.Ticket, error)
	UpdateTicket(context.Context, services.UpdateTicketRequest) (db.Ticket, error)
	ClaimNext(context.Context, services.ClaimNextRequest) (services.ClaimNextResult, error)
	Heartbeat(context.Context, services.HeartbeatRequest) (db.Attempt, error)
	Checkpoint(context.Context, services.CheckpointRequest) (services.CheckpointResult, error)
	Complete(context.Context, services.CompleteAttemptRequest) (services.AttemptTransitionResult, error)
	Fail(context.Context, services.FailAttemptRequest) (services.AttemptTransitionResult, error)
	Block(context.Context, services.BlockAttemptRequest) (services.AttemptTransitionResult, error)
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	RegisterArtifact(context.Context, services.RegisterArtifactRequest) (db.Artifact, error)
	DecomposeTicket(context.Context, services.DecomposeTicketRequest) (services.DecomposeTicketResult, error)
	RegisterCapabilities(context.Context, services.RegisterCapabilitiesRequest) (db.AgentCapability, error)
}

type Tool struct {
	Name         string
	Summary      string
	Description  string
	InputSchema  []byte
	OutputSchema []byte
}

type Server struct {
	runtime  Runtime
	tools    map[string]Tool
	order    []string
	handlers map[string]toolHandler
}

type toolHandler func(context.Context, json.RawMessage) (any, error)

func NewServer(rt Runtime, operations []contracts.Operation) (*Server, error) {
	server := &Server{
		runtime:  rt,
		tools:    map[string]Tool{},
		order:    make([]string, 0, len(operations)),
		handlers: map[string]toolHandler{},
	}
	for _, operation := range operations {
		name := operation.Bindings.MCPTool
		if name == "" {
			name = operation.Name
		}
		if _, exists := server.tools[name]; exists {
			return nil, fmt.Errorf("duplicate MCP tool %q", name)
		}
		inputSchema, err := operation.InputSchema.JSON()
		if err != nil {
			return nil, fmt.Errorf("marshal %s input schema: %w", name, err)
		}
		outputSchema, err := operation.OutputSchema.JSON()
		if err != nil {
			return nil, fmt.Errorf("marshal %s output schema: %w", name, err)
		}
		server.tools[name] = Tool{
			Name:         name,
			Summary:      operation.Summary,
			Description:  operation.Description,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		}
		server.order = append(server.order, name)
	}
	server.registerHandlers()
	return server, nil
}

func (s *Server) Runtime() Runtime {
	if s == nil {
		return nil
	}
	return s.runtime
}

func (s *Server) Tools() []Tool {
	if s == nil {
		return nil
	}
	out := make([]Tool, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.tools[name])
	}
	return out
}

func (s *Server) Tool(name string) (Tool, bool) {
	if s == nil {
		return Tool{}, false
	}
	tool, ok := s.tools[name]
	return tool, ok
}

func (s *Server) CanCall(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.handlers[name]
	return ok
}

func (s *Server) Call(ctx context.Context, name string, input json.RawMessage) ([]byte, error) {
	if s == nil {
		return nil, ErrUnknownTool
	}
	handler, ok := s.handlers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	if s.runtime == nil {
		return nil, ErrRuntimeRequired
	}
	output, err := handler(ctx, input)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("marshal %s output: %w", name, err)
	}
	return data, nil
}

func (s *Server) registerHandlers() {
	s.handlers[contracts.OperationCreateTicket] = s.callCreateTicket
	s.handlers[contracts.OperationProposeTicket] = s.callProposeTicket
	s.handlers[contracts.OperationCreateTicketFromAttempt] = s.callCreateTicketFromAttempt
	s.handlers[contracts.OperationClaimNextTicket] = s.callClaimNextTicket
	s.handlers[contracts.OperationHeartbeatAttempt] = s.callHeartbeatAttempt
	s.handlers[contracts.OperationCheckpointAttempt] = s.callCheckpointAttempt
	s.handlers[contracts.OperationUpdateTicket] = s.callUpdateTicket
	s.handlers[contracts.OperationCompleteAttempt] = s.callCompleteAttempt
	s.handlers[contracts.OperationFailAttempt] = s.callFailAttempt
	s.handlers[contracts.OperationBlockAttempt] = s.callBlockAttempt
	s.handlers[contracts.OperationListTickets] = s.callListTickets
	s.handlers[contracts.OperationGetTicket] = s.callGetTicket
	s.handlers[contracts.OperationAttachArtifact] = s.callAttachArtifact
	s.handlers[contracts.OperationDecomposeTicket] = s.callDecomposeTicket
	s.handlers[contracts.OperationRegisterAgentCapabilities] = s.callRegisterCapabilities
}

func (s *Server) callCreateTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload createTicketInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	ticket, err := s.runtime.CreateTicket(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket": ticketPayload(ticket)}, nil
}

func (s *Server) callProposeTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload createTicketInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	req := payload.request()
	if req.CreatedBy == "" {
		req.CreatedBy = services.ActorAgent
	}
	ticket, err := s.runtime.ProposeTicket(ctx, req)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket": ticketPayload(ticket)}, nil
}

func (s *Server) callCreateTicketFromAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload createFromAttemptInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	ticket, err := s.runtime.CreateTicketFromAttempt(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket": ticketPayload(ticket)}, nil
}

func (s *Server) callClaimNextTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload claimNextInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.ClaimNext(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ticket":  ticketPayload(result.Ticket),
		"attempt": attemptPayload(result.Attempt),
		"context": result.Context,
	}, nil
}

func (s *Server) callHeartbeatAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload attemptLeaseInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	if payload.AttemptID == "" {
		return nil, services.ValidationError{Problems: []string{"attempt_id is required"}}
	}
	attempt, err := s.runtime.Heartbeat(ctx, services.HeartbeatRequest{
		AttemptID: mustUUID(payload.AttemptID),
		Lease:     time.Duration(payload.LeaseSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"attempt": attemptPayload(attempt)}, nil
}

func (s *Server) callCheckpointAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload checkpointInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.Checkpoint(ctx, services.CheckpointRequest{
		AttemptID:       mustUUID(payload.AttemptID),
		Summary:         payload.Summary,
		ProgressPercent: int32(payload.ProgressPercent),
		FilesTouched:    payload.FilesTouched,
		CommandsRun:     payload.CommandsRun,
		NextStep:        payload.NextStep,
		Risk:            payload.Risk,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"checkpoint_id":    uuidText(result.Checkpoint.ID),
		"attempt_id":       uuidText(result.Checkpoint.AttemptID),
		"summary":          result.Checkpoint.Summary,
		"progress_percent": result.ProgressPercent,
	}, nil
}

func (s *Server) callUpdateTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload updateTicketInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	ticket, err := s.runtime.UpdateTicket(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket": ticketPayload(ticket)}, nil
}

func (s *Server) callCompleteAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload completeInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.Complete(ctx, services.CompleteAttemptRequest{
		AttemptID:    mustUUID(payload.AttemptID),
		Output:       payload.Output,
		OutputSchema: payload.OutputSchema,
	})
	if err != nil {
		return nil, err
	}
	return transitionPayload(result), nil
}

func (s *Server) callFailAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload failInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.Fail(ctx, services.FailAttemptRequest{
		AttemptID:       mustUUID(payload.AttemptID),
		FailureReason:   payload.FailureReason,
		FailureCategory: payload.FailureCategory,
		Output:          payload.Output,
	})
	if err != nil {
		return nil, err
	}
	return transitionPayload(result), nil
}

func (s *Server) callBlockAttempt(ctx context.Context, input json.RawMessage) (any, error) {
	var payload blockInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.Block(ctx, services.BlockAttemptRequest{
		AttemptID:       mustUUID(payload.AttemptID),
		BlockerReason:   payload.BlockerReason,
		FailureCategory: payload.FailureCategory,
		Blocker:         payload.Blocker,
	})
	if err != nil {
		return nil, err
	}
	return transitionPayload(result), nil
}

func (s *Server) callListTickets(ctx context.Context, input json.RawMessage) (any, error) {
	var payload listTicketsInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	tickets, err := s.runtime.ListTickets(ctx, services.ListTicketsRequest{
		WorkspaceID: mustUUID(payload.WorkspaceID),
		ProjectID:   mustUUID(payload.ProjectID),
		Status:      payload.Status,
		Type:        payload.Type,
		Offset:      int32(payload.Offset),
		Limit:       int32(payload.Limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, ticketPayload(ticket))
	}
	return map[string]any{"tickets": out}, nil
}

func (s *Server) callGetTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload getTicketInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	ticket, err := s.runtime.GetTicket(ctx, mustUUID(payload.TicketID))
	if err != nil {
		return nil, err
	}
	return map[string]any{"ticket": ticketPayload(ticket)}, nil
}

func (s *Server) callAttachArtifact(ctx context.Context, input json.RawMessage) (any, error) {
	var payload attachArtifactInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	artifact, err := s.runtime.RegisterArtifact(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifact": artifactPayload(artifact)}, nil
}

func (s *Server) callDecomposeTicket(ctx context.Context, input json.RawMessage) (any, error) {
	var payload decomposeInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	result, err := s.runtime.DecomposeTicket(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	children := make([]map[string]any, 0, len(result.Children))
	for _, child := range result.Children {
		children = append(children, ticketPayload(child))
	}
	return map[string]any{"children": children}, nil
}

func (s *Server) callRegisterCapabilities(ctx context.Context, input json.RawMessage) (any, error) {
	var payload registerCapabilitiesInput
	if err := decodeInput(input, &payload); err != nil {
		return nil, err
	}
	record, err := s.runtime.RegisterCapabilities(ctx, payload.request())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"agent_id":     record.AgentID,
		"harness":      record.Harness,
		"capabilities": record.Capabilities,
	}, nil
}

func decodeInput(input json.RawMessage, out any) error {
	if len(input) == 0 {
		input = []byte("{}")
	}
	if err := json.Unmarshal(input, out); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	return nil
}
