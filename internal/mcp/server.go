package mcp

import (
	"fmt"

	"github.com/vivek/agent-task-tracker/internal/contracts"
)

type Runtime interface{}

type Tool struct {
	Name         string
	Summary      string
	Description  string
	InputSchema  []byte
	OutputSchema []byte
}

type Server struct {
	runtime Runtime
	tools   map[string]Tool
	order   []string
}

func NewServer(rt Runtime, operations []contracts.Operation) (*Server, error) {
	server := &Server{
		runtime: rt,
		tools:   map[string]Tool{},
		order:   make([]string, 0, len(operations)),
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
