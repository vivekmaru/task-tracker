package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/config"
)

func TestRunInitWritesConfigAndCreatesArtifactRoot(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "forge.local.json")

	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{
		"init",
		"--path", configPath,
		"--database-url", "postgres://localhost:5432/forge_smoke?sslmode=disable",
		"--admin-token", "local-token",
		"--artifact-root", ".forge/artifacts",
		"--json",
	}, &stdout, &stderr, Dependencies{})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost:5432/forge_smoke?sslmode=disable" {
		t.Fatalf("unexpected database URL: %q", cfg.DatabaseURL)
	}
	if cfg.AdminToken != "local-token" {
		t.Fatalf("unexpected admin token: %q", cfg.AdminToken)
	}
	if cfg.HTTPAddr != "127.0.0.1:3017" {
		t.Fatalf("unexpected http addr: %q", cfg.HTTPAddr)
	}
	if cfg.WorkerConcurrency != 1 {
		t.Fatalf("unexpected worker concurrency: %d", cfg.WorkerConcurrency)
	}
	if cfg.ArtifactRoot != ".forge/artifacts" {
		t.Fatalf("unexpected artifact root: %q", cfg.ArtifactRoot)
	}
	if cfg.ArtifactBackend != "local" {
		t.Fatalf("unexpected artifact backend: %q", cfg.ArtifactBackend)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "artifacts")); err != nil {
		t.Fatalf("expected artifact root to be created: %v", err)
	}

	var result initResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode init result: %v; stdout=%s", err, stdout.String())
	}
	if result.Path != configPath {
		t.Fatalf("unexpected result path: %q", result.Path)
	}
	if result.ArtifactRoot != filepath.Join(dir, ".forge", "artifacts") {
		t.Fatalf("unexpected result artifact root: %q", result.ArtifactRoot)
	}
}

func TestRunInitRefusesExistingConfigWithoutForce(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.local.json")
	if err := os.WriteFile(configPath, []byte("original"), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{
		"init",
		"--path", configPath,
		"--database-url", "postgres://localhost:5432/forge?sslmode=disable",
		"--admin-token", "local-token",
	}, &stdout, &stderr, Dependencies{})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("expected existing file error, got %q", stderr.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read existing config: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("expected existing config to remain unchanged, got %q", string(data))
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
}

func TestRunInitForceOverwritesExistingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.local.json")
	if err := os.WriteFile(configPath, []byte("original"), 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{
		"init",
		"--path", configPath,
		"--database-url", "postgres://localhost:5432/forge?sslmode=disable",
		"--admin-token", "local-token",
		"--force",
	}, &stdout, &stderr, Dependencies{})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) == "original" {
		t.Fatalf("expected config to be overwritten")
	}
	if !strings.Contains(stdout.String(), "wrote ") {
		t.Fatalf("expected human output, got %q", stdout.String())
	}
}

func TestRunInitGeneratesUnprintedAdminToken(t *testing.T) {
	var tokens []string
	for i := 0; i < 2; i++ {
		path := filepath.Join(t.TempDir(), "forge.local.json")
		var stdout, stderr strings.Builder
		if code := RunWithDependencies([]string{"init", "--path", path}, &stdout, &stderr, Dependencies{}); code != 0 {
			t.Fatalf("init run %d failed: code=%d stderr=%q", i, code, stderr.String())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read generated config: %v", err)
		}
		var cfg config.Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("decode generated config: %v", err)
		}
		if len(cfg.AdminToken) < 43 || cfg.AdminToken == config.DeprecatedDevelopmentAdminToken {
			t.Fatalf("expected generated admin token, got %q", cfg.AdminToken)
		}
		if strings.Contains(stdout.String(), cfg.AdminToken) || strings.Contains(stderr.String(), cfg.AdminToken) {
			t.Fatal("init output must not disclose generated admin token")
		}
		tokens = append(tokens, cfg.AdminToken)
	}
	if tokens[0] == tokens[1] {
		t.Fatal("expected distinct generated admin tokens")
	}
}
