package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	modelcontext "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vivek/agent-task-tracker/internal/services"
)

// ServeStdio exposes the existing Forge tool catalog over the official MCP
// stdio transport. It never writes diagnostics to stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.serve(ctx, &modelcontext.StdioTransport{})
}

func (s *Server) serve(ctx context.Context, transport modelcontext.Transport) error {
	if s == nil || s.runtime == nil {
		return ErrRuntimeRequired
	}
	server := modelcontext.NewServer(&modelcontext.Implementation{Name: "forge", Version: "0.1.0"}, nil)
	for _, forgeTool := range s.Tools() {
		tool := forgeTool
		server.AddTool(&modelcontext.Tool{Name: tool.Name, Description: tool.Description, InputSchema: json.RawMessage(tool.InputSchema), OutputSchema: json.RawMessage(tool.OutputSchema)}, func(ctx context.Context, request *modelcontext.CallToolRequest) (*modelcontext.CallToolResult, error) {
			arguments := request.Params.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			output, err := s.Call(ctx, tool.Name, arguments)
			if err != nil {
				return toolErrorResult(err), nil
			}
			var structured map[string]any
			if err := json.Unmarshal(output, &structured); err != nil {
				return toolErrorResult(err), nil
			}
			return &modelcontext.CallToolResult{Content: []modelcontext.Content{&modelcontext.TextContent{Text: string(output)}}, StructuredContent: structured}, nil
		})
	}
	return server.Run(ctx, transport)
}

func toolErrorResult(err error) *modelcontext.CallToolResult {
	return &modelcontext.CallToolResult{Content: []modelcontext.Content{&modelcontext.TextContent{Text: safeToolError(err)}}, IsError: true}
}

func safeToolError(err error) string {
	// Error wrappers may contain credentials; expose only typed public messages.
	var validation services.ValidationError
	if errors.As(err, &validation) {
		return validation.Error()
	}
	for _, domain := range []error{
		services.ErrNoClaimableTickets,
		services.ErrIdempotencyConflict,
		services.ErrTicketTransitionNotAllowed,
		services.ErrAttemptNotRunning,
		services.ErrTicketNotFound,
		services.ErrTicketIsNotProposed,
		services.ErrEnqueuePermissionRequired,
		services.ErrPolicyDenied,
		services.ErrArtifactDeleteUnsupported,
	} {
		if errors.Is(err, domain) {
			return domain.Error()
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "resource not found"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return "resource already exists"
		case "23503":
			return "referenced resource not found"
		case "23502", "23514":
			return "invalid input"
		}
	}
	return "tool request failed"
}
