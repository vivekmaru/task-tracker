package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/config"
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
			return noopRuntime{}, nil
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

type noopRuntime struct{}

func (noopRuntime) Close() {}
