package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/storage"
	forgetui "github.com/vivek/agent-task-tracker/internal/tui"
)

func TestRunPrintsTopLevelHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"--help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Forge",
		"Usage:",
		"claim-next",
		"checkpoint",
		"codex",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected help to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"nope"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected unknown command error, got %q", stderr.String())
	}
}

func TestRunAdvertisesPhaseOneCommandSkeletons(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run([]string{"help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	out := stdout.String()
	for _, command := range []string{
		"server",
		"worker",
		"mcp",
		"tui",
		"create",
		"propose",
		"claim-next",
		"heartbeat",
		"checkpoint",
		"complete",
		"fail",
		"block",
		"cancel",
		"attach",
		"list",
		"get",
		"codex",
	} {
		if !strings.Contains(out, command) {
			t.Fatalf("expected command %q in help output:\n%s", command, out)
		}
	}
}

func TestContractCLIBindingsAreRunnableRuntimeCommands(t *testing.T) {
	for _, operation := range contracts.AllOperations() {
		command := operation.Bindings.CLICommand
		if command == "" {
			continue
		}
		if !isKnownCommand(command) {
			t.Fatalf("%s declares unknown CLI command %q", operation.Name, command)
		}
		if !isRuntimeCommand(command) {
			t.Fatalf("%s CLI command %q should route through shared runtime commands", operation.Name, command)
		}
	}
}

func TestRunCodexDefaultsRuntimeOpenerWithEmptyDependencies(t *testing.T) {
	t.Setenv("FORGE_DATABASE_URL", "://bad-url")
	var stdout, stderr bytes.Buffer

	code := RunWithDependencies([]string{
		"codex", "claim",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
	}, &stdout, &stderr, Dependencies{})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "codex claim runtime error") {
		t.Fatalf("expected runtime error, got %q", stderr.String())
	}
}

func TestRunServerReportsClearConfigValidationError(t *testing.T) {
	t.Setenv("FORGE_DATABASE_URL", "")
	var stdout, stderr bytes.Buffer

	code := Run([]string{"server"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "server configuration error: database_url is required") {
		t.Fatalf("expected clear validation error, got %q", stderr.String())
	}
}

func TestRunWorkerLoadsConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{
		"database_url": "postgres://db",
		"worker_concurrency": 3
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer

	var opened config.Config
	code := RunWithDependencies([]string{"worker", "--config", path}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(_ context.Context, cfg config.Config) (RuntimeHandle, error) {
			opened = cfg
			return &noopRuntime{}, nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if opened.DatabaseURL != "postgres://db" {
		t.Fatalf("expected runtime opener to receive database URL, got %#v", opened)
	}
	if opened.WorkerConcurrency != 3 {
		t.Fatalf("expected runtime opener to receive worker concurrency, got %#v", opened)
	}
	if !strings.Contains(stdout.String(), "worker runtime configuration ok") {
		t.Fatalf("expected worker startup message, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunMCPBootsRuntimeAndRegistersContractTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer

	var opened config.Config
	code := RunWithDependencies([]string{"mcp", "--config", path}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(_ context.Context, cfg config.Config) (RuntimeHandle, error) {
			opened = cfg
			return &noopRuntime{}, nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if opened.DatabaseURL != "postgres://db" {
		t.Fatalf("expected runtime opener to receive database URL, got %#v", opened)
	}
	if !strings.Contains(stdout.String(), "mcp runtime configuration ok") {
		t.Fatalf("expected MCP startup message, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), fmt.Sprintf("registered %d tools", len(contracts.AllOperations()))) {
		t.Fatalf("expected registered tool count, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunServerStartsHTTPRouter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://db","http_addr":"127.0.0.1:4100","admin_token":"secret-token"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{}
	var gotAddr string
	var gotHandler http.Handler

	code := RunWithDependencies([]string{"server", "--config", path}, &stdout, &stderr, Dependencies{
		OpenRuntime: fakeRuntimeOpener(fake),
		ServeHTTP: func(_ context.Context, addr string, handler http.Handler) error {
			gotAddr = addr
			gotHandler = handler
			return nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if gotAddr != "127.0.0.1:4100" {
		t.Fatalf("expected configured HTTP address, got %q", gotAddr)
	}
	if gotHandler == nil {
		t.Fatal("expected server to receive a router")
	}
	req := httptest.NewRequest(http.MethodGet, "/tickets?workspace_id=00000000-0000-0000-0000-000000000001&project_id=00000000-0000-0000-0000-000000000002", nil)
	rec := httptest.NewRecorder()
	gotHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected server web routes to require login, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(stdout.String(), "server listening on 127.0.0.1:4100") {
		t.Fatalf("expected server listening message, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestNewHTTPServerConfiguresTimeouts(t *testing.T) {
	server := newHTTPServer("127.0.0.1:4100", http.NewServeMux())

	if server.ReadHeaderTimeout <= 0 {
		t.Fatal("expected ReadHeaderTimeout to be configured")
	}
	if server.ReadTimeout <= 0 {
		t.Fatal("expected ReadTimeout to be configured")
	}
	if server.WriteTimeout <= 0 {
		t.Fatal("expected WriteTimeout to be configured")
	}
	if server.IdleTimeout <= 0 {
		t.Fatal("expected IdleTimeout to be configured")
	}
}

func TestWebAuthOptionsUseConfiguredSecureCookiePolicy(t *testing.T) {
	if webAuthOptions(config.Config{HTTPAddr: "127.0.0.1:4100", AdminToken: "secret"}).SecureCookie {
		t.Fatal("expected default HTTP server cookies to work without the secure flag")
	}
	if !webAuthOptions(config.Config{HTTPAddr: "127.0.0.1:4100", AdminToken: "secret", AuthCookieSecure: true}).SecureCookie {
		t.Fatal("expected configured HTTPS deployments to force secure auth cookies")
	}
}

func TestRunTUILoadsRuntimeAndDelegatesQueueOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{}
	var gotOptions forgetui.Options
	var gotRuntime RuntimeHandle

	code := RunWithDependencies([]string{
		"tui",
		"--config", path,
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--status", services.TicketStatusTodo,
		"--type", services.TicketTypeBug,
		"--limit", "25",
	}, &stdout, &stderr, Dependencies{
		OpenRuntime: fakeRuntimeOpener(fake),
		RunTUI: func(_ context.Context, _ io.Writer, rt RuntimeHandle, opts forgetui.Options) error {
			gotRuntime = rt
			gotOptions = opts
			return nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if gotRuntime != fake {
		t.Fatalf("expected TUI runner to receive opened runtime")
	}
	if gotOptions.WorkspaceID != testUUID(2) || gotOptions.ProjectID != testUUID(3) {
		t.Fatalf("unexpected TUI scope: %#v", gotOptions)
	}
	if gotOptions.Status != services.TicketStatusTodo || gotOptions.Type != services.TicketTypeBug || gotOptions.Limit != 25 {
		t.Fatalf("unexpected TUI filters: %#v", gotOptions)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunTUIRejectsInvalidUUIDFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	opened := false

	code := RunWithDependencies([]string{
		"tui",
		"--config", path,
		"--workspace-id", "not-a-uuid",
	}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
			opened = true
			return &fakeRuntime{}, nil
		},
	})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if opened {
		t.Fatal("runtime should not open when TUI UUID filters are invalid")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "tui argument error: --workspace-id must be a UUID") {
		t.Fatalf("expected UUID argument error, got %q", stderr.String())
	}
}

func TestRunTUIRejectsLimitAboveInt32Range(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opened := false

	code := RunWithDependencies([]string{
		"tui",
		"--limit", "3000000000",
	}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
			opened = true
			return &fakeRuntime{}, nil
		},
	})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if opened {
		t.Fatal("runtime should not open when TUI limit is out of range")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--limit must be less than or equal to 2147483647") {
		t.Fatalf("expected limit range error, got %q", stderr.String())
	}
}

func TestRunWorkerRejectsTUIOnlyFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opened := false

	code := RunWithDependencies([]string{
		"worker",
		"--status", services.TicketStatusTodo,
	}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
			opened = true
			return &fakeRuntime{}, nil
		},
	})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if opened {
		t.Fatal("runtime should not open for unknown process flags")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unknown flag \"--status\"") {
		t.Fatalf("expected unknown flag error, got %q", stderr.String())
	}
}

func TestRunServerReportsRuntimeOpenError(t *testing.T) {
	t.Setenv("FORGE_DATABASE_URL", "postgres://db")
	t.Setenv("FORGE_ADMIN_TOKEN", "secret-token")
	var stdout, stderr bytes.Buffer

	code := RunWithDependencies([]string{"server"}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
			return nil, errors.New("dial failed")
		},
	})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "server runtime error: dial failed") {
		t.Fatalf("expected runtime error, got %q", stderr.String())
	}
}

func TestRunCreateTicketJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		createTicket: db.Ticket{
			ID:        testUUID(1),
			Title:     "Fix auth",
			Status:    services.TicketStatusTodo,
			CreatedBy: services.ActorHuman,
		},
	}

	code := RunWithDependencies([]string{
		"create",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--title", "Fix auth",
		"--type", services.TicketTypeBug,
		"--acceptance", "Auth tests pass",
		"--verify", "go test ./...",
		"--json",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.createReq.Title != "Fix auth" || fake.createReq.Type != services.TicketTypeBug {
		t.Fatalf("unexpected create request: %#v", fake.createReq)
	}
	if fake.createReq.AcceptanceCriteria[0] != "Auth tests pass" {
		t.Fatalf("expected acceptance criteria, got %#v", fake.createReq.AcceptanceCriteria)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout JSON: %v; stdout=%s", err, stdout.String())
	}
	if body["status"] != services.TicketStatusTodo {
		t.Fatalf("expected ticket status in JSON, got %#v", body)
	}
}

func TestRunClaimNextJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		claimResult: services.ClaimNextResult{
			Ticket:  db.Ticket{ID: testUUID(4), Title: "Fix auth"},
			Attempt: db.Attempt{ID: testUUID(5), AgentID: "codex", Harness: "codex"},
		},
	}

	code := RunWithDependencies([]string{
		"claim-next",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--agent-id", "codex",
		"--harness", "codex",
		"--capability", "codegen",
		"--lease", "15m",
		"--json",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.claimReq.AgentID != "codex" || fake.claimReq.Lease != 15*time.Minute {
		t.Fatalf("unexpected claim request: %#v", fake.claimReq)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout JSON: %v; stdout=%s", err, stdout.String())
	}
	if body["attempt_id"] == "" {
		t.Fatalf("expected attempt_id in JSON, got %#v", body)
	}
}

func TestRunAttachRegistersArtifactJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		artifact: db.Artifact{
			ID:   testUUID(6),
			Type: services.ArtifactTypeTestOutput,
			Role: services.ArtifactRoleEvidence,
			Name: "test-output.txt",
			Url:  "local://test-output.txt",
		},
	}

	code := RunWithDependencies([]string{
		"attach",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--ticket-id", uuidString(t, testUUID(4)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--type", services.ArtifactTypeTestOutput,
		"--role", services.ArtifactRoleEvidence,
		"--name", "test-output.txt",
		"--url", "local://test-output.txt",
		"--json",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.artifactReq.Name != "test-output.txt" || fake.artifactReq.Role != services.ArtifactRoleEvidence {
		t.Fatalf("unexpected artifact request: %#v", fake.artifactReq)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout JSON: %v; stdout=%s", err, stdout.String())
	}
	if body["type"] != services.ArtifactTypeTestOutput {
		t.Fatalf("expected artifact type in JSON, got %#v", body)
	}
}

func TestRunCodexClaimDefaultsHarnessAndWritesJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		claimResult: services.ClaimNextResult{
			Ticket:  db.Ticket{ID: testUUID(4), Title: "Wire Codex command"},
			Attempt: db.Attempt{ID: testUUID(5), AgentID: "codex-local", Harness: "codex"},
		},
	}

	code := RunWithDependencies([]string{
		"codex", "claim",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--agent-id", "codex-local",
		"--capability", "codegen",
		"--lease", "20m",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.claimReq.Harness != "codex" || fake.claimReq.Lease != 20*time.Minute {
		t.Fatalf("unexpected codex claim request: %#v", fake.claimReq)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout JSON: %v; stdout=%s", err, stdout.String())
	}
	if body["attempt_id"] == "" {
		t.Fatalf("expected attempt_id in JSON, got %#v", body)
	}
}

func TestRunCodexClaimRejectsMalformedScopeBeforeRuntime(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name: "missing workspace",
			args: []string{
				"codex", "claim",
				"--project-id", uuidString(t, testUUID(3)),
			},
			wantStderr: "codex claim argument error: --workspace-id is required",
		},
		{
			name: "malformed workspace",
			args: []string{
				"codex", "claim",
				"--workspace-id", "not-a-uuid",
				"--project-id", uuidString(t, testUUID(3)),
			},
			wantStderr: "codex claim argument error: --workspace-id must be a UUID",
		},
		{
			name: "malformed project",
			args: []string{
				"codex", "claim",
				"--workspace-id", uuidString(t, testUUID(2)),
				"--project-id", "not-a-uuid",
			},
			wantStderr: "codex claim argument error: --project-id must be a UUID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opened := false

			code := RunWithDependencies(tt.args, &stdout, &stderr, Dependencies{
				OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
					opened = true
					return &fakeRuntime{}, nil
				},
			})

			if code != 2 {
				t.Fatalf("expected exit code 2, got %d", code)
			}
			if opened {
				t.Fatalf("runtime should not open for invalid scope")
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("expected stderr to contain %q, got %q", tt.wantStderr, stderr.String())
			}
		})
	}
}

func TestRunCodexCheckpointUsesSharedRuntime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		checkpointResult: services.CheckpointResult{
			Checkpoint:      db.AttemptCheckpoint{ID: testUUID(6), AttemptID: testUUID(5), Summary: "Tests are green"},
			ProgressPercent: 80,
		},
	}

	code := RunWithDependencies([]string{
		"codex", "checkpoint",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Tests are green",
		"--progress", "80",
		"--file", "internal/cli/cli.go",
		"--command", "go test ./internal/cli",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.checkpointReq.Summary != "Tests are green" || fake.checkpointReq.ProgressPercent != 80 {
		t.Fatalf("unexpected checkpoint request: %#v", fake.checkpointReq)
	}
	if !strings.Contains(stdout.String(), `"progress":80`) {
		t.Fatalf("expected checkpoint JSON, got %s", stdout.String())
	}
}

func TestRunCodexCheckpointParsesFlagsAfterPositionalAttemptID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		checkpointResult: services.CheckpointResult{
			Checkpoint:      db.AttemptCheckpoint{ID: testUUID(6), AttemptID: testUUID(5), Summary: "Tests are green"},
			ProgressPercent: 80,
		},
	}

	code := RunWithDependencies([]string{
		"codex", "checkpoint",
		uuidString(t, testUUID(5)),
		"--summary", "Tests are green",
		"--progress", "80",
		"--file", "internal/cli/cli.go",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.checkpointReq.AttemptID != testUUID(5) || fake.checkpointReq.Summary != "Tests are green" || fake.checkpointReq.ProgressPercent != 80 {
		t.Fatalf("expected positional attempt and trailing flags to parse, got %#v", fake.checkpointReq)
	}
}

func TestRunCodexCompleteRegistersProofArtifacts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		completeResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusSucceeded,
			TicketStatus:  services.TicketStatusDone,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeTestOutput, Role: services.ArtifactRoleEvidence, Name: "cli-test.log", Url: "local://cli-test.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Implemented and verified",
		"--proof", "local://cli-test.log",
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.completeReq.Output["summary"] != "Implemented and verified" {
		t.Fatalf("unexpected complete request: %#v", fake.completeReq)
	}
	if len(fake.artifactReqs) != 1 || fake.artifactReqs[0].AttemptID != fake.completeResult.AttemptID {
		t.Fatalf("expected proof artifact registration, got %#v", fake.artifactReqs)
	}
	if !strings.Contains(stdout.String(), `"artifacts"`) {
		t.Fatalf("expected artifacts in JSON, got %s", stdout.String())
	}
}

func TestRunCodexCompleteParsesFlagsAfterPositionalAttemptID(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.json")
	if err := os.WriteFile(configPath, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		completeResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusSucceeded,
			TicketStatus:  services.TicketStatusDone,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeTestOutput, Role: services.ArtifactRoleEvidence, Name: "cli-test.log", Url: "local://cli-test.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--config", configPath,
		uuidString(t, testUUID(5)),
		"--summary", "Implemented and verified",
		"--proof", "local://cli-test.log",
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.completeReq.AttemptID != testUUID(5) || fake.completeReq.Output["summary"] != "Implemented and verified" {
		t.Fatalf("expected positional attempt and trailing summary to parse, got %#v", fake.completeReq)
	}
	if len(fake.artifactReqs) != 1 || fake.artifactReqs[0].URL != "local://cli-test.log" {
		t.Fatalf("expected trailing proof flag to parse, got %#v", fake.artifactReqs)
	}
}

func TestRunCodexFollowUpCreatesTicketFromAttempt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:                 db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
		createFromAttemptTicket: db.Ticket{ID: testUUID(8), Title: "Fix follow-up", Type: services.TicketTypeBug, Status: services.TicketStatusBacklog},
	}

	code := RunWithDependencies([]string{
		"codex", "follow-up",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--title", "Fix follow-up",
		"--description", "Observed while completing another task",
		"--type", services.TemplateBug,
		"--acceptance", "Regression is covered",
		"--verify", "go test ./...",
		"--reason", "Codex discovered this while testing",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.createFromAttemptReq.TemplateKind != services.TemplateBug {
		t.Fatalf("unexpected follow-up request: %#v", fake.createFromAttemptReq)
	}
	if fake.createFromAttemptReq.WorkspaceID != fake.attempt.WorkspaceID || fake.createFromAttemptReq.ProjectID != fake.attempt.ProjectID {
		t.Fatalf("expected follow-up scope to come from source attempt, got %#v", fake.createFromAttemptReq)
	}
	if !strings.Contains(stdout.String(), `"title":"Fix follow-up"`) {
		t.Fatalf("expected ticket JSON, got %s", stdout.String())
	}
}

func TestRunCodexFollowUpRejectsSourceAttemptScopeMismatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
	}

	code := RunWithDependencies([]string{
		"codex", "follow-up",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(10)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--title", "Fix follow-up",
		"--description", "Observed while completing another task",
		"--type", services.TemplateBug,
		"--reason", "Codex discovered this while testing",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if fake.createFromAttemptReq.Title != "" {
		t.Fatalf("follow-up should not run after source attempt scope mismatch: %#v", fake.createFromAttemptReq)
	}
	if !strings.Contains(stderr.String(), "--workspace-id does not match source attempt") {
		t.Fatalf("expected scope mismatch error, got %q", stderr.String())
	}
}

func TestRunCodexFollowUpAllowsHelpAsFlagValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:                 db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
		createFromAttemptTicket: db.Ticket{ID: testUUID(8), Title: "help", Type: services.TicketTypeBug, Status: services.TicketStatusBacklog},
	}

	code := RunWithDependencies([]string{
		"codex", "follow-up",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--title", "help",
		"--description", "Observed while completing another task",
		"--type", services.TemplateBug,
		"--reason", "Codex discovered this while testing",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.createFromAttemptReq.Title != "help" {
		t.Fatalf("expected follow-up title to be preserved, got %#v", fake.createFromAttemptReq)
	}
	if !strings.Contains(stdout.String(), `"title":"help"`) {
		t.Fatalf("expected ticket JSON, got %s", stdout.String())
	}
}

func TestRunCodexAttemptCommandsRejectMissingOrMalformedAttemptID(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "checkpoint missing",
			args:       []string{"codex", "checkpoint", "--summary", "Progress"},
			wantStderr: "codex checkpoint argument error: --attempt-id is required",
		},
		{
			name:       "checkpoint malformed positional",
			args:       []string{"codex", "checkpoint", "not-a-uuid", "--summary", "Progress"},
			wantStderr: "codex checkpoint argument error: --attempt-id must be a UUID",
		},
		{
			name:       "complete missing",
			args:       []string{"codex", "complete", "--summary", "Done"},
			wantStderr: "codex complete argument error: --attempt-id is required",
		},
		{
			name:       "block malformed",
			args:       []string{"codex", "block", "--attempt-id", "not-a-uuid", "--reason", "Waiting"},
			wantStderr: "codex block argument error: --attempt-id must be a UUID",
		},
		{
			name:       "follow-up missing",
			args:       []string{"codex", "follow-up", "--title", "Fix follow-up"},
			wantStderr: "codex follow-up argument error: --attempt-id is required",
		},
		{
			name:       "follow-up malformed",
			args:       []string{"codex", "follow-up", "--attempt-id", "not-a-uuid", "--title", "Fix follow-up"},
			wantStderr: "codex follow-up argument error: --attempt-id must be a UUID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opened := false

			code := RunWithDependencies(tt.args, &stdout, &stderr, Dependencies{
				OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
					opened = true
					return &fakeRuntime{}, nil
				},
			})

			if code != 2 {
				t.Fatalf("expected exit code 2, got %d", code)
			}
			if opened {
				t.Fatalf("runtime should not open for invalid attempt id")
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("expected stderr to contain %q, got %q", tt.wantStderr, stderr.String())
			}
		})
	}
}

func TestRunCodexFollowUpRejectsMalformedArtifactID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{}

	code := RunWithDependencies([]string{
		"codex", "follow-up",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--artifact-id", "not-a-uuid",
		"--title", "Fix follow-up",
		"--reason", "Codex discovered this while testing",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if fake.createFromAttemptReq.Title != "" {
		t.Fatalf("follow-up should not run after artifact-id parse failure: %#v", fake.createFromAttemptReq)
	}
	if !strings.Contains(stderr.String(), "--artifact-id must be a UUID") {
		t.Fatalf("expected artifact-id error, got %q", stderr.String())
	}
}

func TestRunCodexFollowUpRejectsUnsupportedEnqueue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{}

	code := RunWithDependencies([]string{
		"codex", "follow-up",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--title", "Fix follow-up",
		"--type", services.TemplateBug,
		"--reason", "Codex discovered this while testing",
		"--enqueue",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if fake.createFromAttemptReq.Title != "" {
		t.Fatalf("follow-up should not run with unsupported enqueue flag: %#v", fake.createFromAttemptReq)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -enqueue") {
		t.Fatalf("expected unsupported enqueue flag error, got %q", stderr.String())
	}
}

func TestRunCodexBlockCapturesProofs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		blockResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusBlocked,
			TicketStatus:  services.TicketStatusBlocked,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeLog, Role: services.ArtifactRoleEvidence, Name: "blocked.log", Url: "local://blocked.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "block",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--reason", "Waiting for API credentials",
		"--category", "external_dependency",
		"--proof", "local://blocked.log",
		"--proof-type", services.ArtifactTypeLog,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.blockReq.BlockerReason != "Waiting for API credentials" {
		t.Fatalf("unexpected block request: %#v", fake.blockReq)
	}
	if len(fake.artifactReqs) != 1 || fake.artifactReqs[0].Type != services.ArtifactTypeLog {
		t.Fatalf("expected proof artifact registration, got %#v", fake.artifactReqs)
	}
}

func TestRunCodexBlockParsesFlagsAfterPositionalAttemptID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		blockResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusBlocked,
			TicketStatus:  services.TicketStatusBlocked,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeLog, Role: services.ArtifactRoleEvidence, Name: "blocked.log", Url: "local://blocked.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "block",
		uuidString(t, testUUID(5)),
		"--reason", "Waiting for API credentials",
		"--category", "external_dependency",
		"--proof", "local://blocked.log",
		"--proof-type", services.ArtifactTypeLog,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.blockReq.AttemptID != testUUID(5) || fake.blockReq.BlockerReason != "Waiting for API credentials" || fake.blockReq.FailureCategory != "external_dependency" {
		t.Fatalf("expected positional attempt and trailing flags to parse, got %#v", fake.blockReq)
	}
	if len(fake.artifactReqs) != 1 || fake.artifactReqs[0].URL != "local://blocked.log" {
		t.Fatalf("expected trailing proof flag to parse, got %#v", fake.artifactReqs)
	}
}

func TestRunCodexCompleteReportsAtomicProofFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:     db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		artifactErr: errors.New("artifact rejected"),
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Implemented and verified",
		"--proof", "local://cli-test.log",
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if fake.completeCalls != 1 {
		t.Fatalf("complete should be attempted inside the atomic transition path: %#v", fake.completeReq)
	}
	if len(fake.artifactReqs) != 0 {
		t.Fatalf("failed atomic transition should not expose persisted artifacts: %#v", fake.artifactReqs)
	}
	if !strings.Contains(stderr.String(), "codex complete error") {
		t.Fatalf("expected complete error, got %q", stderr.String())
	}
}

func TestRunCodexCompleteForwardsAttemptMetrics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		completeResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusSucceeded,
			TicketStatus:  services.TicketStatusDone,
		},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		uuidString(t, testUUID(5)),
		"--summary", "Implemented analytics",
		"--tokens-in", "1200",
		"--tokens-out", "340",
		"--cost-usd", "0.0425",
		"--duration", "91.25s",
		"--retries", "2",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.completeReq.Metrics == nil {
		t.Fatalf("expected metrics request, got %#v", fake.completeReq)
	}
	if fake.completeReq.Metrics.TokensIn != 1200 || fake.completeReq.Metrics.TokensOut != 340 || fake.completeReq.Metrics.RetryCount != 2 {
		t.Fatalf("unexpected token/retry metrics: %#v", fake.completeReq.Metrics)
	}
	if fake.completeReq.Metrics.CostUSD != 0.0425 || fake.completeReq.Metrics.DurationSeconds != 91.25 {
		t.Fatalf("unexpected cost/duration metrics: %#v", fake.completeReq.Metrics)
	}
}

func TestRunCodexBlockReportsAtomicProofFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:     db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		artifactErr: errors.New("artifact rejected"),
	}

	code := RunWithDependencies([]string{
		"codex", "block",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--reason", "Waiting for API credentials",
		"--proof", "local://blocked.log",
		"--proof-type", services.ArtifactTypeLog,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if fake.blockCalls != 1 {
		t.Fatalf("block should be attempted inside the atomic transition path: %#v", fake.blockReq)
	}
	if len(fake.artifactReqs) != 0 {
		t.Fatalf("failed atomic transition should not expose persisted artifacts: %#v", fake.artifactReqs)
	}
	if !strings.Contains(stderr.String(), "codex block error") {
		t.Fatalf("expected block error, got %q", stderr.String())
	}
}

func TestRunAnalyticsSummaryPrintsMinimalHumanOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		analyticsSummary: services.AnalyticsSummary{
			AttemptCount:         3,
			SucceededAttempts:    2,
			FailedAttempts:       1,
			TotalTokensIn:        2200,
			TotalTokensOut:       900,
			TotalCostUSD:         0.34,
			TotalDurationSeconds: 180.5,
			TotalRetries:         1,
			AttemptsWithMetrics:  2,
		},
	}

	code := RunWithDependencies([]string{
		"analytics", "summary",
		"--workspace-id", uuidString(t, testUUID(2)),
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.analyticsFilter.WorkspaceID != testUUID(2) {
		t.Fatalf("expected workspace filter, got %#v", fake.analyticsFilter)
	}
	out := stdout.String()
	for _, want := range []string{"Attempts: 3", "Succeeded: 2", "Cost: $0.340000", "Tokens: 3100", "Retries: 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected analytics output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunAnalyticsByModelWritesJSONWhenRequested(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		analyticsGroups: []services.AnalyticsGroup{
			{Group: "gpt-5.4", AttemptCount: 2, SucceededAttempts: 1, FailedAttempts: 1, TotalCostUSD: 0.12},
		},
	}

	code := RunWithDependencies([]string{
		"analytics", "by-model",
		"--json",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"groups":[{"group":"gpt-5.4"`) {
		t.Fatalf("expected analytics JSON groups, got %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"failed_attempts":1`) {
		t.Fatalf("expected analytics JSON failed totals, got %s", stdout.String())
	}
}

func TestRunAnalyticsByStatusAndAgentUseScopedFilters(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		analyticsGroups: []services.AnalyticsGroup{
			{Group: "blocked", AttemptCount: 2, BlockedAttempts: 2, TotalDurationSeconds: 45.5},
		},
	}

	code := RunWithDependencies([]string{
		"analytics", "by-status",
		"--workspace-id", uuidString(t, testUUID(2)),
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.analyticsCall != "status" || fake.analyticsFilter.WorkspaceID != testUUID(2) {
		t.Fatalf("expected by-status workspace filter, got call=%q filter=%#v", fake.analyticsCall, fake.analyticsFilter)
	}
	if !strings.Contains(stdout.String(), "Status\tAttempts\tSucceeded\tFailed\tBlocked\tCost\tTokens\tDuration\tRetries") {
		t.Fatalf("expected expanded analytics header, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = RunWithDependencies([]string{
		"analytics", "by-agent",
		"--project-id", uuidString(t, testUUID(3)),
		"--json",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.analyticsCall != "agent" || fake.analyticsFilter.ProjectID != testUUID(3) {
		t.Fatalf("expected by-agent project filter, got call=%q filter=%#v", fake.analyticsCall, fake.analyticsFilter)
	}
	if !strings.Contains(stdout.String(), `"blocked_attempts":2`) {
		t.Fatalf("expected by-agent JSON blocked totals, got %s", stdout.String())
	}
}

func TestRunCodexCompleteDoesNotPersistProofsWhenTransitionFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:     db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		completeErr: errors.New("attempt is already closed"),
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Implemented and verified",
		"--proof", "local://cli-test.log",
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if fake.completeCalls != 1 {
		t.Fatalf("expected complete attempt, got %d", fake.completeCalls)
	}
	if len(fake.artifactReqs) != 0 {
		t.Fatalf("transition failure should not persist proof artifacts: %#v", fake.artifactReqs)
	}
	if !strings.Contains(stderr.String(), "attempt is already closed") {
		t.Fatalf("expected transition error, got %q", stderr.String())
	}
}

func TestRunCodexCompleteRemovesUploadedProofWhenTransitionFails(t *testing.T) {
	proofPath := filepath.Join(t.TempDir(), "go-test.log")
	if err := os.WriteFile(proofPath, []byte("ok\n"), 0o600); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt:     db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(2), ProjectID: testUUID(3), TicketID: testUUID(4)},
		completeErr: errors.New("attempt is already closed"),
		storedArtifact: storage.StoredArtifact{
			Name: "go-test.log",
			URL:  "local://artifacts/go-test.log",
			Size: 3,
		},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Implemented and verified",
		"--proof", proofPath,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if fake.removedLocalArtifactURL != "local://artifacts/go-test.log" {
		t.Fatalf("expected uploaded proof cleanup, got %q", fake.removedLocalArtifactURL)
	}
}

func TestRunCodexCompleteUsesAttemptScopeForProofs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
		completeResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusSucceeded,
			TicketStatus:  services.TicketStatusDone,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeTestOutput, Role: services.ArtifactRoleEvidence, Name: "cli-test.log", Url: "local://cli-test.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Done",
		"--proof", " local://cli-test.log ",
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if len(fake.artifactReqs) != 1 {
		t.Fatalf("expected proof artifact registration, got %#v", fake.artifactReqs)
	}
	if fake.artifactReqs[0].WorkspaceID != fake.attempt.WorkspaceID || fake.artifactReqs[0].ProjectID != fake.attempt.ProjectID {
		t.Fatalf("expected attempt scope for proof artifact, got %#v", fake.artifactReqs[0])
	}
	if fake.artifactReqs[0].URL != "local://cli-test.log" || fake.artifactReqs[0].Name != "cli-test.log" {
		t.Fatalf("expected normalized proof artifact input, got %#v", fake.artifactReqs[0])
	}
}

func TestRunCodexCompleteUploadsFilesystemProofs(t *testing.T) {
	proofPath := filepath.Join(t.TempDir(), "go-test.log")
	if err := os.WriteFile(proofPath, []byte("ok\n"), 0o600); err != nil {
		t.Fatalf("write proof: %v", err)
	}
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
		completeResult: services.AttemptTransitionResult{
			AttemptID:     testUUID(5),
			TicketID:      testUUID(4),
			AttemptStatus: services.AttemptStatusSucceeded,
			TicketStatus:  services.TicketStatusDone,
		},
		storedArtifact: storage.StoredArtifact{
			Name:     "go-test.log",
			URL:      "local://artifacts/go-test.log",
			MimeType: "text/plain",
			Size:     3,
		},
		artifact: db.Artifact{ID: testUUID(7), Type: services.ArtifactTypeTestOutput, Role: services.ArtifactRoleEvidence, Name: "go-test.log", Url: "local://artifacts/go-test.log"},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Done",
		"--proof", proofPath,
		"--proof-type", services.ArtifactTypeTestOutput,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if fake.storeLocalArtifactPath != proofPath || fake.storeLocalArtifactName != "go-test.log" {
		t.Fatalf("expected proof file upload, got path=%q name=%q", fake.storeLocalArtifactPath, fake.storeLocalArtifactName)
	}
	if len(fake.artifactReqs) != 1 || fake.artifactReqs[0].URL != "local://artifacts/go-test.log" || fake.artifactReqs[0].SizeBytes != 3 {
		t.Fatalf("expected uploaded proof registration, got %#v", fake.artifactReqs)
	}
}

func TestRunCodexCompleteRejectsMissingFilesystemProof(t *testing.T) {
	missingProof := filepath.Join(t.TempDir(), "go-test-output.txt")
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Done",
		"--proof", missingProof,
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d; stderr=%q", code, stderr.String())
	}
	if fake.completeCalls != 0 || fake.storeLocalArtifactPath != "" {
		t.Fatalf("missing proof should stop before transition/upload, completeCalls=%d storePath=%q", fake.completeCalls, fake.storeLocalArtifactPath)
	}
	if !strings.Contains(stderr.String(), "--proof file does not exist") {
		t.Fatalf("expected missing proof error, got %q", stderr.String())
	}
}

func TestRunCodexCompleteRejectsProofScopeMismatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
	}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--workspace-id", uuidString(t, testUUID(2)),
		"--project-id", uuidString(t, testUUID(10)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Done",
		"--proof", "local://cli-test.log",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if fake.completeCalls != 0 || len(fake.artifactReqs) != 0 {
		t.Fatalf("scope mismatch should fail before transition: completeCalls=%d artifacts=%#v", fake.completeCalls, fake.artifactReqs)
	}
	if !strings.Contains(stderr.String(), "--workspace-id does not match source attempt") {
		t.Fatalf("expected proof scope mismatch error, got %q", stderr.String())
	}
}

func TestRunCodexBlockRejectsProofScopeMismatch(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{
		attempt: db.Attempt{ID: testUUID(5), WorkspaceID: testUUID(9), ProjectID: testUUID(10), TicketID: testUUID(4)},
	}

	code := RunWithDependencies([]string{
		"codex", "block",
		"--workspace-id", uuidString(t, testUUID(9)),
		"--project-id", uuidString(t, testUUID(3)),
		"--attempt-id", uuidString(t, testUUID(5)),
		"--reason", "Waiting for API credentials",
		"--proof", "local://blocked.log",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if fake.blockCalls != 0 || len(fake.artifactReqs) != 0 {
		t.Fatalf("scope mismatch should fail before transition: blockCalls=%d artifacts=%#v", fake.blockCalls, fake.artifactReqs)
	}
	if !strings.Contains(stderr.String(), "--project-id does not match source attempt") {
		t.Fatalf("expected proof scope mismatch error, got %q", stderr.String())
	}
}

func TestRunCodexCompleteRejectsBlankProofBeforeRegistration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fake := &fakeRuntime{}

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--summary", "Done",
		"--proof", "local://cli-test.log",
		"--proof", " ",
	}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(fake)})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if len(fake.artifactReqs) != 0 || fake.completeCalls != 0 {
		t.Fatalf("blank proof should fail before artifact registration or transition: artifacts=%#v completeCalls=%d", fake.artifactReqs, fake.completeCalls)
	}
	if !strings.Contains(stderr.String(), "--proof[1] must not be empty") {
		t.Fatalf("expected proof validation error, got %q", stderr.String())
	}
}

func TestRunCodexSubcommandHelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := RunWithDependencies([]string{"codex", "claim", "--help"}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(&fakeRuntime{})})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "forge codex claim") {
		t.Fatalf("expected codex claim help, got %q", stdout.String())
	}
}

func TestDocumentedCodexHarnessCommandsExposeHelp(t *testing.T) {
	for _, command := range []string{"claim", "checkpoint", "complete", "follow-up", "block"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opened := false

			code := RunWithDependencies([]string{"codex", command, "--help"}, &stdout, &stderr, Dependencies{
				OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
					opened = true
					return &fakeRuntime{}, nil
				},
			})

			if code != 0 {
				t.Fatalf("expected exit code 0, got %d", code)
			}
			if opened {
				t.Fatalf("runtime should not open for help")
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected no stderr, got %q", stderr.String())
			}
			if !strings.Contains(stdout.String(), "forge codex "+command) {
				t.Fatalf("expected codex %s help, got %q", command, stdout.String())
			}
		})
	}
}

func TestRunCodexSubcommandHelpSucceedsAfterFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opened := false

	code := RunWithDependencies([]string{
		"codex", "complete",
		"--attempt-id", uuidString(t, testUUID(5)),
		"--help",
	}, &stdout, &stderr, Dependencies{
		OpenRuntime: func(context.Context, config.Config) (RuntimeHandle, error) {
			opened = true
			return &fakeRuntime{}, nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if opened {
		t.Fatalf("runtime should not open for help")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "forge codex complete") {
		t.Fatalf("expected codex complete help, got %q", stdout.String())
	}
}

func TestRunCodexRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := RunWithDependencies([]string{"codex", "wat"}, &stdout, &stderr, Dependencies{OpenRuntime: fakeRuntimeOpener(&fakeRuntime{})})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown codex command") {
		t.Fatalf("expected unknown codex command error, got %q", stderr.String())
	}
}

type noopRuntime struct {
	fakeRuntime
}

func (noopRuntime) Close() {}

type fakeRuntime struct {
	createReq               services.CreateTicketRequest
	createTicket            db.Ticket
	proposeReq              services.CreateTicketRequest
	proposeTicket           db.Ticket
	createFromAttemptReq    services.CreateTicketFromAttemptRequest
	createFromAttemptTicket db.Ticket
	claimReq                services.ClaimNextRequest
	claimResult             services.ClaimNextResult
	checkpointReq           services.CheckpointRequest
	checkpointResult        services.CheckpointResult
	completeReq             services.CompleteAttemptRequest
	completeCalls           int
	completeErr             error
	completeResult          services.AttemptTransitionResult
	blockReq                services.BlockAttemptRequest
	blockCalls              int
	blockErr                error
	blockResult             services.AttemptTransitionResult
	attempt                 db.Attempt
	attemptErr              error
	artifactReqs            []services.RegisterArtifactRequest
	artifactReq             services.RegisterArtifactRequest
	artifact                db.Artifact
	artifactErr             error
	storedArtifact          storage.StoredArtifact
	storeLocalArtifactPath  string
	storeLocalArtifactName  string
	storeLocalArtifactErr   error
	removedLocalArtifactURL string
	removeLocalArtifactErr  error
	analyticsFilter         services.AnalyticsFilter
	analyticsCall           string
	analyticsSummary        services.AnalyticsSummary
	analyticsGroups         []services.AnalyticsGroup
}

func fakeRuntimeOpener(rt *fakeRuntime) func(context.Context, config.Config) (RuntimeHandle, error) {
	return func(context.Context, config.Config) (RuntimeHandle, error) {
		return rt, nil
	}
}

func (f *fakeRuntime) Close() {}

func (f *fakeRuntime) CreateTicket(_ context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	f.createReq = req
	return f.createTicket, nil
}

func (f *fakeRuntime) ProposeTicket(_ context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	f.proposeReq = req
	return f.proposeTicket, nil
}

func (f *fakeRuntime) CreateTicketFromAttempt(_ context.Context, req services.CreateTicketFromAttemptRequest) (db.Ticket, error) {
	f.createFromAttemptReq = req
	return f.createFromAttemptTicket, nil
}

func (f *fakeRuntime) UpdateTicket(context.Context, services.UpdateTicketRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) MarkReady(context.Context, services.TicketTransitionRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) Reopen(context.Context, services.TicketTransitionRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) Unblock(context.Context, services.TicketTransitionRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) RequestReview(context.Context, services.TicketTransitionRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) Review(context.Context, services.ReviewTicketRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) Archive(context.Context, services.TicketTransitionRequest) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) ClaimNext(_ context.Context, req services.ClaimNextRequest) (services.ClaimNextResult, error) {
	f.claimReq = req
	return f.claimResult, nil
}

func (f *fakeRuntime) Heartbeat(context.Context, services.HeartbeatRequest) (db.Attempt, error) {
	return db.Attempt{}, nil
}

func (f *fakeRuntime) Checkpoint(_ context.Context, req services.CheckpointRequest) (services.CheckpointResult, error) {
	f.checkpointReq = req
	return f.checkpointResult, nil
}

func (f *fakeRuntime) Complete(_ context.Context, req services.CompleteAttemptRequest) (services.AttemptTransitionResult, error) {
	f.completeReq = req
	f.completeCalls++
	return f.completeResult, f.completeErr
}

func (f *fakeRuntime) CompleteWithArtifacts(_ context.Context, req services.CompleteAttemptRequest, artifactReqs []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error) {
	f.completeReq = req
	f.completeCalls++
	if f.completeErr != nil {
		return services.AttemptTransitionResult{}, nil, f.completeErr
	}
	if f.artifactErr != nil {
		return services.AttemptTransitionResult{}, nil, f.artifactErr
	}
	artifacts := make([]db.Artifact, 0, len(artifactReqs))
	for _, req := range artifactReqs {
		f.artifactReq = req
		f.artifactReqs = append(f.artifactReqs, req)
		artifacts = append(artifacts, f.artifact)
	}
	return f.completeResult, artifacts, nil
}

func (f *fakeRuntime) Fail(context.Context, services.FailAttemptRequest) (services.AttemptTransitionResult, error) {
	return services.AttemptTransitionResult{}, nil
}

func (f *fakeRuntime) Block(_ context.Context, req services.BlockAttemptRequest) (services.AttemptTransitionResult, error) {
	f.blockReq = req
	f.blockCalls++
	return f.blockResult, f.blockErr
}

func (f *fakeRuntime) BlockWithArtifacts(_ context.Context, req services.BlockAttemptRequest, artifactReqs []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error) {
	f.blockReq = req
	f.blockCalls++
	if f.blockErr != nil {
		return services.AttemptTransitionResult{}, nil, f.blockErr
	}
	if f.artifactErr != nil {
		return services.AttemptTransitionResult{}, nil, f.artifactErr
	}
	artifacts := make([]db.Artifact, 0, len(artifactReqs))
	for _, req := range artifactReqs {
		f.artifactReq = req
		f.artifactReqs = append(f.artifactReqs, req)
		artifacts = append(artifacts, f.artifact)
	}
	return f.blockResult, artifacts, nil
}

func (f *fakeRuntime) Cancel(context.Context, services.CancelAttemptRequest) (services.AttemptTransitionResult, error) {
	return services.AttemptTransitionResult{}, nil
}

func (f *fakeRuntime) ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error) {
	return nil, nil
}

func (f *fakeRuntime) SearchTickets(context.Context, services.SearchTicketsRequest) ([]services.SearchResult, error) {
	return nil, nil
}

func (f *fakeRuntime) GetTicket(context.Context, pgtype.UUID) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error) {
	return f.attempt, f.attemptErr
}

func (f *fakeRuntime) ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error) {
	return nil, nil
}

func (f *fakeRuntime) ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error) {
	return nil, nil
}

func (f *fakeRuntime) ListTicketEventsByTicket(context.Context, pgtype.UUID) ([]db.TicketEvent, error) {
	return nil, nil
}

func (f *fakeRuntime) ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error) {
	return nil, nil
}

func (f *fakeRuntime) ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error) {
	return nil, nil
}

func (f *fakeRuntime) GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error) {
	return db.Artifact{}, nil
}

func (f *fakeRuntime) OpenArtifact(context.Context, db.Artifact) (storage.ArtifactContent, error) {
	return storage.ArtifactContent{}, nil
}

func (f *fakeRuntime) StoreLocalArtifact(_ context.Context, sourcePath string, preferredName string) (storage.StoredArtifact, error) {
	f.storeLocalArtifactPath = sourcePath
	f.storeLocalArtifactName = preferredName
	return f.storedArtifact, f.storeLocalArtifactErr
}

func (f *fakeRuntime) RemoveLocalArtifact(_ context.Context, rawURL string) error {
	f.removedLocalArtifactURL = rawURL
	return f.removeLocalArtifactErr
}

func (f *fakeRuntime) ListWorkspaces(context.Context) ([]db.Workspace, error) {
	return nil, nil
}

func (f *fakeRuntime) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return db.Workspace{}, nil
}

func (f *fakeRuntime) CreateWorkspace(context.Context, string) (db.Workspace, error) {
	return db.Workspace{}, nil
}

func (f *fakeRuntime) ListProjectsByWorkspace(context.Context, pgtype.UUID) ([]db.Project, error) {
	return nil, nil
}

func (f *fakeRuntime) CreateProject(context.Context, pgtype.UUID, string) (db.Project, error) {
	return db.Project{}, nil
}

func (f *fakeRuntime) RegisterArtifact(_ context.Context, req services.RegisterArtifactRequest) (db.Artifact, error) {
	f.artifactReq = req
	f.artifactReqs = append(f.artifactReqs, req)
	return f.artifact, f.artifactErr
}

func (f *fakeRuntime) DecomposeTicket(context.Context, services.DecomposeTicketRequest) (services.DecomposeTicketResult, error) {
	return services.DecomposeTicketResult{}, nil
}

func (f *fakeRuntime) RegisterCapabilities(context.Context, services.RegisterCapabilitiesRequest) (db.AgentCapability, error) {
	return db.AgentCapability{}, nil
}

func (f *fakeRuntime) AnalyticsSummary(_ context.Context, filter services.AnalyticsFilter) (services.AnalyticsSummary, error) {
	f.analyticsFilter = filter
	return f.analyticsSummary, nil
}

func (f *fakeRuntime) AnalyticsByModel(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	f.analyticsCall = "model"
	return f.analyticsGroups, nil
}

func (f *fakeRuntime) AnalyticsByHarness(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	f.analyticsCall = "harness"
	return f.analyticsGroups, nil
}

func (f *fakeRuntime) AnalyticsByStatus(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	f.analyticsCall = "status"
	return f.analyticsGroups, nil
}

func (f *fakeRuntime) AnalyticsByAgent(_ context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	f.analyticsFilter = filter
	f.analyticsCall = "agent"
	return f.analyticsGroups, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func uuidString(t *testing.T, id pgtype.UUID) string {
	t.Helper()

	value, err := id.Value()
	if err != nil {
		t.Fatalf("uuid value: %v", err)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected uuid string, got %T", value)
	}
	return text
}
