package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	modelcontext "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServeStdio exposes the existing Forge tool catalog over the official MCP
// stdio transport. It never writes diagnostics to stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
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
				return &modelcontext.CallToolResult{Content: []modelcontext.Content{&modelcontext.TextContent{Text: "tool request failed"}}, IsError: true}, nil
			}
			var structured map[string]any
			if err := json.Unmarshal(output, &structured); err != nil {
				return nil, fmt.Errorf("decode %s output: %w", tool.Name, err)
			}
			return &modelcontext.CallToolResult{Content: []modelcontext.Content{&modelcontext.TextContent{Text: string(output)}}, StructuredContent: structured}, nil
		})
	}
	return server.Run(ctx, &modelcontext.StdioTransport{})
}
