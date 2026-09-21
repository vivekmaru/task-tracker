package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	modelcontext "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/services"
)

const transportClaimArguments = `{"workspace_id":"00000000-0000-0000-0000-000000000001","project_id":"00000000-0000-0000-0000-000000000002","agent_id":"worker","harness":"test"}`

func TestTransportReportsSafeDomainErrors(t *testing.T) {
	const secret = "postgres://admin:super-secret@private-db/forge"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no work", services.ErrNoClaimableTickets, "no claimable tickets"},
		{"wrapped no work", fmt.Errorf("%s: %w", secret, services.ErrNoClaimableTickets), "no claimable tickets"},
		{"idempotency conflict", fmt.Errorf("%s: %w", secret, services.ErrIdempotencyConflict), "idempotency key reused with a different request"},
		{"transition conflict", fmt.Errorf("%s: %w", secret, services.ErrTicketTransitionNotAllowed), "ticket transition is not allowed"},
		{"attempt conflict", services.ErrAttemptNotRunning, "attempt is not running"},
		{"not found", services.ErrTicketNotFound, "ticket not found"},
		{"no rows", fmt.Errorf("%s: %w", secret, pgx.ErrNoRows), "resource not found"},
		{"not proposed", services.ErrTicketIsNotProposed, "ticket is not proposed work"},
		{"permission", services.ErrEnqueuePermissionRequired, "enqueue permission required"},
		{"policy", fmt.Errorf("%w: %s", services.ErrPolicyDenied, secret), "policy denied workflow action"},
		{"validation", services.ValidationError{Problems: []string{"agent_id is required"}}, "validation failed: agent_id is required"},
		{"wrapped validation", fmt.Errorf("%s: %w", secret, services.ValidationError{Problems: []string{"lease must be greater than zero"}}), "validation failed: lease must be greater than zero"},
		{"unique constraint", &pgconn.PgError{Code: "23505", Message: secret, Detail: secret}, "resource already exists"},
		{"foreign key", &pgconn.PgError{Code: "23503", Message: secret, Detail: secret}, "referenced resource not found"},
		{"not null", &pgconn.PgError{Code: "23502", Message: secret}, "invalid input"},
		{"check constraint", &pgconn.PgError{Code: "23514", Message: secret}, "invalid input"},
		{"unknown database failure", &pgconn.PgError{Code: "XX000", Message: secret, Detail: secret}, "tool request failed"},
		{"unexpected failure", errors.New(secret), "tool request failed"},
		{"domain-looking untyped failure", errors.New("no claimable tickets: " + secret), "tool request failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := NewServer(&transportClaimRuntime{err: tc.err}, contracts.AllOperations())
			if err != nil {
				t.Fatal(err)
			}
			ctx, client := connectTransport(t, server)
			result, err := client.CallTool(ctx, &modelcontext.CallToolParams{Name: contracts.OperationClaimNextTicket, Arguments: json.RawMessage(transportClaimArguments)})
			assertToolError(t, result, err, tc.want, secret)
		})
	}
}

func TestTransportReportsInvalidInputWithoutEchoingValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arguments any
		want      string
	}{
		{"missing arguments", nil, "validation failed: workspace_id is required"},
		{"bad uuid", map[string]any{"workspace_id": "super-secret"}, "validation failed: workspace_id must be a valid UUID"},
		{"wrong field type", map[string]any{"workspace_id": []string{"super-secret"}}, "validation failed: input must be valid JSON matching the tool schema"},
		{"wrong payload type", "super-secret", "validation failed: input must be valid JSON matching the tool schema"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A nil embedded Runtime panics if invalid input reaches the service.
			server, err := NewServer(&transportClaimRuntime{}, contracts.AllOperations())
			if err != nil {
				t.Fatal(err)
			}
			ctx, client := connectTransport(t, server)
			result, err := client.CallTool(ctx, &modelcontext.CallToolParams{Name: contracts.OperationClaimNextTicket, Arguments: tc.arguments})
			assertToolError(t, result, err, tc.want, "super-secret")
		})
	}
}

func TestTransportSanitizesOutputFailures(t *testing.T) {
	for _, output := range []any{secretJSONValue{}, "super-secret"} {
		server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
		if err != nil {
			t.Fatal(err)
		}
		server.handlers[contracts.OperationClaimNextTicket] = func(context.Context, json.RawMessage) (any, error) {
			return output, nil
		}
		ctx, client := connectTransport(t, server)
		result, err := client.CallTool(ctx, &modelcontext.CallToolParams{Name: contracts.OperationClaimNextTicket, Arguments: json.RawMessage(transportClaimArguments)})
		assertToolError(t, result, err, "tool request failed", "super-secret")
	}
}

func TestTransportSuccessfulClaimPreservesStructuredContext(t *testing.T) {
	server, err := NewServer(&fakeRuntime{}, contracts.AllOperations())
	if err != nil {
		t.Fatal(err)
	}
	ctx, client := connectTransport(t, server)
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(contracts.AllOperations()) {
		t.Fatalf("advertised %d tools", len(tools.Tools))
	}
	result, err := client.CallTool(ctx, &modelcontext.CallToolParams{Name: contracts.OperationClaimNextTicket, Arguments: json.RawMessage(transportClaimArguments)})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil || len(result.Content) != 1 {
		t.Fatalf("unexpected success result: %#v", result)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var structured map[string]json.RawMessage
	if err := json.Unmarshal(data, &structured); err != nil {
		t.Fatal(err)
	}
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(structured["context"], &bundle); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"acceptance_criteria", "verification_commands", "environment", "input", "relevant_paths", "required_tools", "required_permissions", "expected_artifacts", "prior_attempts", "checkpoints", "artifacts"} {
		if _, ok := bundle[key]; !ok {
			t.Errorf("missing structured context.%s", key)
		}
	}
	text, ok := result.Content[0].(*modelcontext.TextContent)
	if !ok || !strings.Contains(text.Text, `"context"`) {
		t.Fatalf("missing text context: %#v", result.Content)
	}
}

func TestServeStdioRequiresRuntime(t *testing.T) {
	for _, server := range []*Server{nil, {}} {
		if err := server.ServeStdio(context.Background()); !errors.Is(err, ErrRuntimeRequired) {
			t.Fatalf("expected runtime error, got %v", err)
		}
	}
}

// InMemoryTransport uses the same newline-delimited JSON framing as stdio,
// exercising the real SDK initialize/tools/call path without replacing os.Stdin.
func connectTransport(t *testing.T, server *Server) (context.Context, *modelcontext.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := modelcontext.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- server.serve(ctx, serverTransport) }()
	client := modelcontext.NewClient(&modelcontext.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ctx, session
}

func assertToolError(t *testing.T, result *modelcontext.CallToolResult, err error, want, secret string) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected tool result, not protocol error: %v", err)
	}
	if result == nil || !result.IsError || len(result.Content) != 1 || result.StructuredContent != nil {
		t.Fatalf("unexpected tool error result: %#v", result)
	}
	text, ok := result.Content[0].(*modelcontext.TextContent)
	if !ok || text.Text != want {
		t.Fatalf("tool content = %#v, want %q", result.Content, want)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("secret exposed in tool result: %s", data)
	}
}

type transportClaimRuntime struct {
	Runtime
	err error
}

func (r *transportClaimRuntime) ClaimNext(context.Context, services.ClaimNextRequest) (services.ClaimNextResult, error) {
	if r.err == nil {
		panic("invalid request reached runtime")
	}
	return services.ClaimNextResult{}, r.err
}

type secretJSONValue struct{}

func (secretJSONValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("super-secret")
}
