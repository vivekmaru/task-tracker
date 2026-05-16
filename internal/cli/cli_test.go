package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
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
	if !strings.Contains(stdout.String(), "registered 15 tools") {
		t.Fatalf("expected registered tool count, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunServerReportsRuntimeOpenError(t *testing.T) {
	t.Setenv("FORGE_DATABASE_URL", "postgres://db")
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

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
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

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
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

func (f *fakeRuntime) GetTicket(context.Context, pgtype.UUID) (db.Ticket, error) {
	return db.Ticket{}, nil
}

func (f *fakeRuntime) GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error) {
	return f.attempt, f.attemptErr
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
