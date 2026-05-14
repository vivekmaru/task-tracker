package mcp

import (
	"testing"

	"github.com/vivek/agent-task-tracker/internal/contracts"
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
